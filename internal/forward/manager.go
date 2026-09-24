package forward

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/bigbag/tunnel-manager/internal/config"
)

type Manager struct {
	password string
	mu       sync.Mutex
	ssh      map[string]*sshSession
	kube     map[string]*kubeSession
}

type sshSession struct {
	client    *ssh.Client
	ln        net.Listener
	agentConn net.Conn
	cancel    context.CancelFunc
}

type kubeSession struct {
	cmd      *exec.Cmd
	done     chan struct{}
	mu       sync.Mutex
	stopping bool
}

func NewManager(password string) *Manager {
	return &Manager{
		password: password,
		ssh:      make(map[string]*sshSession),
		kube:     make(map[string]*kubeSession),
	}
}

func (m *Manager) Start(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	if log == nil {
		log = func(string) {}
	}
	if m.isRunning(t.Name) {
		t.ErrorMessage = "already running"
		return false, nil
	}
	switch t.Type {
	case "ssh":
		return m.startSSH(ctx, t, log)
	case "kubectl":
		return m.startKube(t, log)
	default:
		t.Status = config.StatusError
		t.ErrorMessage = "Unknown tunnel type: " + t.Type
		return false, nil
	}
}

func (m *Manager) Stop(t *config.Tunnel, log func(string)) error {
	if log == nil {
		log = func(string) {}
	}
	switch t.Type {
	case "kubectl":
		return m.stopKube(t, log)
	default:
		return m.stopSSH(t, log)
	}
}

func (m *Manager) Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	if err := m.Stop(t, log); err != nil {
		return false, err
	}
	return m.Start(ctx, t, log)
}

func (m *Manager) isRunning(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.ssh[name]; ok && s.client != nil {
		return true
	}
	if s, ok := m.kube[name]; ok && s.alive() {
		return true
	}
	return false
}

func (s *kubeSession) alive() bool {
	return s != nil && s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil
}

func (m *Manager) startKube(t *config.Tunnel, log func(string)) (bool, error) {
	t.Status = config.StatusStarting
	log("Starting kubectl port-forward...")
	args := KubectlArgs(t)
	log("Command: " + strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Status = config.StatusError
		t.ErrorMessage = err.Error()
		return false, nil
	}
	if err := cmd.Start(); err != nil {
		t.Status = config.StatusError
		t.ErrorMessage = err.Error()
		return false, nil
	}
	t.PID = cmd.Process.Pid
	t.Status = config.StatusRunning
	log(fmt.Sprintf("Started (PID: %d)", t.PID))
	sess := &kubeSession{cmd: cmd, done: make(chan struct{})}
	m.mu.Lock()
	m.kube[t.Name] = sess
	m.mu.Unlock()
	go readKubeStderr(stderr, log)
	go waitKube(sess, t, log)
	return true, nil
}

func readKubeStderr(r io.Reader, log func(string)) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			log(line)
		}
	}
}

func waitKube(sess *kubeSession, t *config.Tunnel, log func(string)) {
	_ = sess.cmd.Wait()
	sess.mu.Lock()
	stopping := sess.stopping
	sess.mu.Unlock()
	if !stopping && sess.cmd.ProcessState != nil && !sess.cmd.ProcessState.Success() {
		log(fmt.Sprintf("Process exited with code %d", sess.cmd.ProcessState.ExitCode()))
		t.Status = config.StatusError
	}
	close(sess.done)
}

func (m *Manager) stopKube(t *config.Tunnel, log func(string)) error {
	m.mu.Lock()
	sess := m.kube[t.Name]
	delete(m.kube, t.Name)
	m.mu.Unlock()
	if sess == nil {
		t.Status = config.StatusStopped
		t.PID = 0
		return nil
	}
	t.Status = config.StatusStopping
	log("Stopping...")
	sess.mu.Lock()
	sess.stopping = true
	sess.mu.Unlock()
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Signal(sigTerm())
		select {
		case <-sess.done:
		case <-time.After(5 * time.Second):
			_ = sess.cmd.Process.Kill()
			<-sess.done
		}
	}
	t.PID = 0
	t.Status = config.StatusStopped
	log("Stopped")
	return nil
}

func (m *Manager) startSSH(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	t.Status = config.StatusStarting
	log(fmt.Sprintf("Connecting to %s@%s...", t.SSHUser, t.SSHBastion))

	var agentConn net.Conn
	agentAvailable := false
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			log("Agent not available: " + err.Error())
		} else {
			agentConn = conn
			agentAvailable = true
		}
	}
	keyPath := expandPath(t.IdentityFile)
	keyExists := false
	if keyPath != "" {
		if st, err := os.Stat(keyPath); err == nil && !st.IsDir() {
			keyExists = true
		}
	}
	choice := ChooseAuth(keyPath, m.password, agentAvailable, keyExists)
	for _, line := range choice.Logs {
		log(line)
	}
	methods, err := authMethods(choice, agentConn, m.password)
	if err != nil {
		closeConn(agentConn)
		t.Status = config.StatusError
		t.ErrorMessage = err.Error()
		return false, nil
	}
	log("Establishing SSH connection...")
	client, err := dialSSH(ctx, t, methods)
	if err != nil {
		closeConn(agentConn)
		t.Status = config.StatusError
		t.ErrorMessage = failMessage(err)
		return false, nil
	}
	log("Connected to " + t.SSHBastion)
	ln, err := net.Listen("tcp", ListenAddr(t.LocalPort))
	if err != nil {
		client.Close()
		closeConn(agentConn)
		t.Status = config.StatusError
		t.ErrorMessage = failMessage(err)
		return false, nil
	}
	log(fmt.Sprintf("Forwarding localhost:%d -> %s:%d", t.LocalPort, t.RemoteHost, t.RemotePort))
	kctx, cancel := context.WithCancel(context.Background())
	sess := &sshSession{client: client, ln: ln, agentConn: agentConn, cancel: cancel}
	m.mu.Lock()
	m.ssh[t.Name] = sess
	m.mu.Unlock()
	t.Status = config.StatusRunning
	go sess.keepAlive(kctx)
	go sess.accept(net.JoinHostPort(t.RemoteHost, fmt.Sprintf("%d", t.RemotePort)))
	go func() {
		client.Wait()
		ln.Close()
	}()
	return true, nil
}

func (m *Manager) stopSSH(t *config.Tunnel, log func(string)) error {
	m.mu.Lock()
	sess := m.ssh[t.Name]
	delete(m.ssh, t.Name)
	m.mu.Unlock()
	if sess == nil {
		t.Status = config.StatusStopped
		return nil
	}
	t.Status = config.StatusStopping
	log("Stopping...")
	if sess.cancel != nil {
		sess.cancel()
	}
	if sess.ln != nil {
		sess.ln.Close()
	}
	if sess.client != nil {
		sess.client.Close()
	}
	closeConn(sess.agentConn)
	t.Status = config.StatusStopped
	log("Disconnected")
	return nil
}

func (s *sshSession) accept(dest string) {
	for {
		local, err := s.ln.Accept()
		if err != nil {
			return
		}
		go forwardConn(s.client, local, dest)
	}
}

func forwardConn(client *ssh.Client, local net.Conn, dest string) {
	remote, err := client.Dial("tcp", dest)
	if err != nil {
		local.Close()
		return
	}
	go func() {
		io.Copy(local, remote)
		local.Close()
		remote.Close()
	}()
	go func() {
		io.Copy(remote, local)
		local.Close()
		remote.Close()
	}()
}

func (s *sshSession) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				misses++
				if misses >= 3 {
					s.client.Close()
					return
				}
				continue
			}
			misses = 0
		}
	}
}

func dialSSH(ctx context.Context, t *config.Tunnel, methods []ssh.AuthMethod) (*ssh.Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	addr := net.JoinHostPort(t.SSHBastion, "22")
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            t.SSHUser,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(cc, chans, reqs), nil
}

func authMethods(choice AuthChoice, agentConn net.Conn, password string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if choice.KeyPath != "" {
		method, err := keyAuth(choice.KeyPath)
		if err != nil {
			return nil, err
		}
		methods = append(methods, method)
	}
	if choice.UseAgent && agentConn != nil {
		ag := agent.NewClient(agentConn)
		methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
	}
	if choice.UsePassword {
		methods = append(methods, ssh.Password(password))
	}
	return methods, nil
}

func keyAuth(path string) (ssh.AuthMethod, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func closeConn(c net.Conn) {
	if c != nil {
		c.Close()
	}
}

func failMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return "Connection timed out"
	}
	if isAuth(err) {
		return "Permission denied: " + err.Error()
	}
	return "Connection failed: " + err.Error()
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func isAuth(err error) bool {
	if errors.Is(err, ssh.ErrNoAuth) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "no supported methods")
}
