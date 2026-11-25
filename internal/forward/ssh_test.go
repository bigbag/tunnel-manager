package forward

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bigbag/tunnel-manager/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func testKey(t *testing.T) (ed25519.PrivateKey, ssh.Signer) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, signer
}

func testSSHServer(t *testing.T, cfg *ssh.ServerConfig) (string, *atomic.Int32) {
	t.Helper()
	_, hostKey := testKey(t)
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var accepts atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			go func() {
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					ch.Reject(ssh.UnknownChannelType, "unsupported")
				}
			}()
		}
	}()
	return ln.Addr().String(), &accepts
}

func testAgent(t *testing.T, ag agent.Agent) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_AUTH_SOCK", sock)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = agent.ServeAgent(ag, conn)
			}()
		}
	}()
}

func TestPasswordRejectionsRequirePasswordAttempt(t *testing.T) {
	var submissions atomic.Int32
	keyOnly, _ := testSSHServer(t, &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, errors.New("rejected")
		},
	})
	passwordServer, accepts := testSSHServer(t, &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			submissions.Add(1)
			return nil, errors.New("rejected")
		},
	})
	aborts := 0
	m := NewManager(map[string]string{"user@host": "synthetic"}, func() { aborts++ })
	tun := &config.Tunnel{Auth: "password", SSHUser: "user", SSHBastion: "host"}
	for range 2 {
		if client, err := m.connectSSH(context.Background(), tun, keyOnly, func(string) {}); err == nil {
			client.Close()
			t.Fatal("key-only server accepted password authentication")
		}
	}
	if got := m.passwordFailures(tun.SSHTarget()); got != 0 || aborts != 0 {
		t.Fatalf("key-only server: failures %d, aborts %d", got, aborts)
	}
	for range 3 {
		if client, err := m.connectSSH(context.Background(), tun, passwordServer, func(string) {}); err == nil {
			client.Close()
			t.Fatal("server accepted rejected password")
		}
	}
	if got := submissions.Load(); got != 2 || aborts != 1 || accepts.Load() != 2 {
		t.Fatalf("password submissions %d, aborts %d, connections %d", got, aborts, accepts.Load())
	}
}

func TestPasswordNetworkFailureDoesNotCountAsRejection(t *testing.T) {
	_, hostKey := testKey(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	attempted := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			close(attempted)
			conn.Close()
			return nil, errors.New("connection lost")
		}}
		cfg.AddHostKey(hostKey)
		ssh.NewServerConn(conn, cfg)
	}()
	m := NewManager(map[string]string{"user@host": "synthetic"}, nil)
	tun := &config.Tunnel{Auth: "password", SSHUser: "user", SSHBastion: "host"}
	client, err := m.connectSSH(context.Background(), tun, ln.Addr().String(), func(string) {})
	if client != nil {
		client.Close()
	}
	if err == nil {
		t.Fatal("connection unexpectedly succeeded")
	}
	select {
	case <-attempted:
	default:
		t.Fatal("server did not receive a password")
	}
	if got := m.passwordFailures(tun.SSHTarget()); got != 0 {
		t.Fatalf("network failure counted as %d password rejections", got)
	}
}

func TestFileKeyFallsBackToAgent(t *testing.T) {
	fileKey, _ := testKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(fileKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	agentKey, agentSigner := testKey(t)
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: agentKey}); err != nil {
		t.Fatal(err)
	}
	testAgent(t, keyring)
	addr, _ := testSSHServer(t, &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(agentSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, errors.New("rejected")
		},
	})
	m := NewManager(nil, nil)
	client, err := m.connectSSH(context.Background(), &config.Tunnel{SSHUser: "user", SSHBastion: "host", IdentityFile: path}, addr, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
}

func TestAgentSupportsTwoPublicKeySteps(t *testing.T) {
	keyring := agent.NewKeyring()
	for range 2 {
		key, _ := testKey(t)
		if err := keyring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	testAgent(t, keyring)
	var secondKeyAccepted atomic.Bool
	addr, _ := testSSHServer(t, &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, first ssh.PublicKey) (*ssh.Permissions, error) {
			firstKey := first.Marshal()
			return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{
				PublicKeyCallback: func(_ ssh.ConnMetadata, next ssh.PublicKey) (*ssh.Permissions, error) {
					if bytes.Equal(firstKey, next.Marshal()) {
						return nil, errors.New("second key must differ")
					}
					secondKeyAccepted.Store(true)
					return nil, nil
				},
			}}
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewManager(nil, nil).connectSSH(ctx, &config.Tunnel{
		Auth: "key", SSHUser: "user", SSHBastion: "host",
	}, addr, func(string) {})
	if err != nil {
		t.Fatalf("two-key authentication failed: %v", err)
	}
	defer client.Close()
	if !secondKeyAccepted.Load() {
		t.Fatal("authentication succeeded without a distinct second key")
	}
}

type failingAgent struct{ agent.Agent }

func (a failingAgent) List() ([]*agent.Key, error) {
	return nil, errors.New("synthetic agent failure")
}

func TestFileKeyWorksWhenAgentFails(t *testing.T) {
	key, signer := testKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	testAgent(t, failingAgent{agent.NewKeyring()})
	addr, _ := testSSHServer(t, &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(signer.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, errors.New("rejected")
		},
	})
	var logs []string
	client, err := NewManager(nil, nil).connectSSH(context.Background(), &config.Tunnel{SSHUser: "user", SSHBastion: "host", IdentityFile: path}, addr, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if !strings.Contains(strings.Join(logs, "\n"), "Agent") {
		t.Fatalf("agent error was not logged: %v", logs)
	}
}

type blockingAgent struct {
	agent.Agent
	blockList bool
	entered   chan struct{}
	release   chan struct{}
}

func (a *blockingAgent) List() ([]*agent.Key, error) {
	if a.blockList {
		close(a.entered)
		<-a.release
	}
	return a.Agent.List()
}

func (a *blockingAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	if !a.blockList {
		close(a.entered)
		<-a.release
	}
	return a.Agent.Sign(key, data)
}

func TestAgentIOStopsOnCancellation(t *testing.T) {
	for _, blockList := range []bool{true, false} {
		name := "sign"
		if blockList {
			name = "list"
		}
		t.Run(name, func(t *testing.T) {
			key, _ := testKey(t)
			keyring := agent.NewKeyring()
			if err := keyring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
				t.Fatal(err)
			}
			ag := &blockingAgent{Agent: keyring, blockList: blockList, entered: make(chan struct{}), release: make(chan struct{})}
			defer close(ag.release)
			testAgent(t, ag)
			addr, _ := testSSHServer(t, &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil }})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				client, err := NewManager(nil, nil).connectSSH(ctx, &config.Tunnel{SSHUser: "user", SSHBastion: "host"}, addr, func(string) {})
				if client != nil {
					client.Close()
				}
				done <- err
			}()
			select {
			case <-ag.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("agent request not received")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled connection succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("agent I/O did not stop on cancellation")
			}
		})
	}
}

func TestHandshakeStopsOnCancellation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		client, err := NewManager(nil, nil).connectSSH(ctx, &config.Tunnel{Auth: "password", SSHUser: "user", SSHBastion: "host"}, ln.Addr().String(), func(string) {})
		if client != nil {
			client.Close()
		}
		done <- err
	}()
	conn := <-accepted
	defer conn.Close()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled handshake succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSH handshake did not stop on cancellation")
	}
}

type silentSSHConn struct {
	ssh.Conn
	requested chan struct{}
	closed    chan struct{}
	once      sync.Once
}

func (c *silentSSHConn) SendRequest(string, bool, []byte) (bool, []byte, error) {
	close(c.requested)
	<-c.closed
	return false, nil, io.EOF
}

func (c *silentSSHConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestKeepAliveClosesSilentConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &silentSSHConn{requested: make(chan struct{}), closed: make(chan struct{})}
		sess := &sshSession{client: &ssh.Client{Conn: conn}}
		go sess.keepAlive(context.Background())
		<-conn.requested
		time.Sleep(44 * time.Second)
		select {
		case <-conn.closed:
			t.Fatal("closed before reply deadline")
		default:
		}
		time.Sleep(time.Second)
		synctest.Wait()
		select {
		case <-conn.closed:
		default:
			t.Fatal("silent SSH connection stayed open past reply deadline")
		}
	})
}

func TestKeepAliveCancellationClosesBlockedRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &silentSSHConn{requested: make(chan struct{}), closed: make(chan struct{})}
		sess := &sshSession{client: &ssh.Client{Conn: conn}}
		ctx, cancel := context.WithCancel(context.Background())
		go sess.keepAlive(ctx)
		<-conn.requested
		cancel()
		synctest.Wait()
		select {
		case <-conn.closed:
		default:
			t.Fatal("canceled keepalive left SSH connection open")
		}
	})
}
