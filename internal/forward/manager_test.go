package forward

import (
	"context"
	"errors"
	"testing"

	"github.com/bigbag/tunnel-manager/internal/config"
)

func TestFailMessage(t *testing.T) {
	if got := failMessage(context.DeadlineExceeded); got != "Connection timed out" {
		t.Fatalf("timeout = %q", got)
	}
	if got := failMessage(errors.New("ssh: unable to authenticate")); got != "Permission denied: ssh: unable to authenticate" {
		t.Fatalf("auth = %q", got)
	}
	if got := failMessage(errors.New("bind: address already in use")); got != "Connection failed: bind: address already in use" {
		t.Fatalf("bind = %q", got)
	}
}

func TestAuthLimitDisconnectIsRejection(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{`ssh: handshake failed: ssh: disconnect, reason 2: "too many authentication failures"`, true},
		{`ssh: handshake failed: ssh: disconnect, reason 2: "protocol error"`, false},
		{`read tcp: connection reset by peer`, false},
	} {
		if got := isAuth(errors.New(tc.message)); got != tc.want {
			t.Errorf("isAuth(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}

func TestUnknownType(t *testing.T) {
	m := NewManager(nil, nil)
	tun := &config.Tunnel{Name: "x", Type: "nope"}
	ok, err := m.Start(context.Background(), tun, nil)
	if err != nil || ok {
		t.Fatalf("ok %v err %v", ok, err)
	}
	if tun.Status() != config.StatusError || tun.ErrorMessage != "Unknown tunnel type: nope" {
		t.Fatalf("status %s message %q", tun.Status(), tun.ErrorMessage)
	}
}

func TestStopMissingIsNoop(t *testing.T) {
	m := NewManager(nil, nil)
	tun := &config.Tunnel{Name: "x"}
	tun.SetStatus(config.StatusError)
	if err := m.Stop(tun, func(string) { t.Fatal("unexpected log") }); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordRejectedTwiceAborts(t *testing.T) {
	aborts := 0
	m := NewManager(map[string]string{"u@b": "pw"}, func() { aborts++ })
	m.passwordRejected("u@b", func(string) {})
	if aborts != 0 {
		t.Fatalf("aborted after one rejection")
	}
	var logs []string
	m.passwordRejected("u@b", func(s string) { logs = append(logs, s) })
	if aborts != 1 || len(logs) != 1 || logs[0] != "Password for u@b was rejected 2 times. Stopping all tunnels." {
		t.Fatalf("aborts %d logs %#v", aborts, logs)
	}
	tun := &config.Tunnel{Name: "x", Type: "ssh", Auth: "password", SSHUser: "u", SSHBastion: "b"}
	ok, err := m.Start(context.Background(), tun, func(s string) { t.Fatalf("dial attempted: %s", s) })
	if ok || err != nil || tun.Status() != config.StatusError {
		t.Fatalf("ok %v err %v status %s", ok, err, tun.Status())
	}
}
