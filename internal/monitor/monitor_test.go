package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/bigbag/tunnel-manager/internal/config"
)

func TestBackoff(t *testing.T) {
	initial := 5 * time.Second
	max := 300 * time.Second
	if got := Backoff(1, initial, max, 2); got != 5*time.Second {
		t.Fatalf("attempt 1 = %s", got)
	}
	if got := Backoff(2, initial, max, 2); got != 10*time.Second {
		t.Fatalf("attempt 2 = %s", got)
	}
	if got := Backoff(3, initial, max, 2); got != 20*time.Second {
		t.Fatalf("attempt 3 = %s", got)
	}
	if got := Backoff(20, initial, max, 2); got != 300*time.Second {
		t.Fatalf("cap = %s", got)
	}
}

type fakeRestart struct {
	calls int
	ok    bool
	err   error
}

func (f *fakeRestart) Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	f.calls++
	return f.ok, f.err
}

func TestSkipStatuses(t *testing.T) {
	checks := 0
	m := &Monitor{
		Tunnels: []*config.Tunnel{
			{Name: "a", Status: config.StatusStopped, LocalPort: 1},
			{Name: "b", Status: config.StatusStarting, LocalPort: 2},
			{Name: "c", Status: config.StatusStopping, LocalPort: 3},
		},
		Config:  NewConfig(time.Second, 5*time.Second, 5),
		Check:   func(host string, port int, timeout time.Duration) bool { checks++; return false },
		Restart: &fakeRestart{},
	}
	m.CheckOnce(context.Background(), time.Now())
	if checks != 0 {
		t.Fatalf("checks %d", checks)
	}
}

func TestErrorStatusRestarts(t *testing.T) {
	rest := &fakeRestart{ok: true}
	var host string
	var timeout time.Duration
	m := &Monitor{
		Tunnels: []*config.Tunnel{{Name: "a", Status: config.StatusError, LocalPort: 9}},
		Config:  NewConfig(time.Second, 2*time.Second, 5),
		Check: func(h string, port int, d time.Duration) bool {
			host = h
			timeout = d
			return false
		},
		Restart: rest,
		Log:     func(string, string) {},
	}
	m.CheckOnce(context.Background(), time.Now())
	if rest.calls != 1 {
		t.Fatalf("calls %d", rest.calls)
	}
	if host != "127.0.0.1" || timeout != 2*time.Second {
		t.Fatalf("host %s timeout %s", host, timeout)
	}
}

func TestGiveUpDoesNotStart(t *testing.T) {
	rest := &fakeRestart{}
	tun := &config.Tunnel{Name: "a", Status: config.StatusRunning, LocalPort: 1}
	m := &Monitor{
		Tunnels: []*config.Tunnel{tun},
		Config:  NewConfig(time.Second, time.Second, 1),
		Check:   func(string, int, time.Duration) bool { return false },
		Restart: rest,
		Log:     func(string, string) {},
	}
	now := time.Now()
	m.CheckOnce(context.Background(), now)
	m.CheckOnce(context.Background(), now.Add(time.Hour))
	if rest.calls != 1 {
		t.Fatalf("calls %d", rest.calls)
	}
	if tun.Status != config.StatusError || tun.ErrorMessage != "Reconnection failed after 1 attempts" {
		t.Fatalf("status %s message %q", tun.Status, tun.ErrorMessage)
	}
}

func TestFutureAttemptWaits(t *testing.T) {
	rest := &fakeRestart{}
	m := &Monitor{
		Tunnels: []*config.Tunnel{{Name: "a", Status: config.StatusRunning, LocalPort: 1}},
		Config:  NewConfig(time.Second, time.Second, 5),
		Check:   func(string, int, time.Duration) bool { return false },
		Restart: rest,
		Log:     func(string, string) {},
	}
	now := time.Now()
	m.CheckOnce(context.Background(), now)
	m.CheckOnce(context.Background(), now.Add(time.Second))
	if rest.calls != 1 {
		t.Fatalf("calls %d", rest.calls)
	}
}

func TestZeroTimeoutDoesNotDial(t *testing.T) {
	checks := 0
	rest := &fakeRestart{}
	m := &Monitor{
		Tunnels: []*config.Tunnel{{Name: "a", Status: config.StatusRunning, LocalPort: 1}},
		Config:  NewConfig(time.Second, 0, 5),
		Check:   func(string, int, time.Duration) bool { checks++; return true },
		Restart: rest,
		Log:     func(string, string) {},
	}
	m.CheckOnce(context.Background(), time.Now())
	if checks != 0 {
		t.Fatalf("checks %d", checks)
	}
	if rest.calls != 1 {
		t.Fatalf("calls %d", rest.calls)
	}
}

func TestHealthyResetsAttempts(t *testing.T) {
	rest := &fakeRestart{}
	var logs []string
	m := &Monitor{
		Tunnels: []*config.Tunnel{{Name: "a", Status: config.StatusRunning, LocalPort: 1}},
		Config:  NewConfig(time.Second, time.Second, 5),
		Check:   func(string, int, time.Duration) bool { return false },
		Restart: rest,
		Log:     func(name, msg string) { logs = append(logs, name+":"+msg) },
	}
	now := time.Now()
	m.CheckOnce(context.Background(), now)
	m.Check = func(string, int, time.Duration) bool { return true }
	m.CheckOnce(context.Background(), now.Add(time.Hour))
	found := false
	for _, line := range logs {
		if line == "a:Reconnected successfully after 1 attempt(s)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs = %#v", logs)
	}
}
