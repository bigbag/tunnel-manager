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

func TestUnknownType(t *testing.T) {
	m := NewManager("")
	tun := &config.Tunnel{Name: "x", Type: "nope"}
	ok, err := m.Start(context.Background(), tun, nil)
	if err != nil || ok {
		t.Fatalf("ok %v err %v", ok, err)
	}
	if tun.Status != config.StatusError || tun.ErrorMessage != "Unknown tunnel type: nope" {
		t.Fatalf("status %s message %q", tun.Status, tun.ErrorMessage)
	}
}

func TestStopMissingIsNoop(t *testing.T) {
	m := NewManager("")
	tun := &config.Tunnel{Name: "x", Status: config.StatusError}
	if err := m.Stop(tun, func(string) { t.Fatal("unexpected log") }); err != nil {
		t.Fatal(err)
	}
}
