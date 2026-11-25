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
	calls     int
	ok        bool
	err       error
	onRestart func()
}

func (f *fakeRestart) Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	f.calls++
	if f.onRestart != nil {
		f.onRestart()
	}
	return f.ok, f.err
}

func testTunnel(name string, status config.Status, port int) *config.Tunnel {
	tun := &config.Tunnel{Name: name, LocalPort: port}
	tun.SetStatus(status)
	return tun
}

func TestSkipStatuses(t *testing.T) {
	checks := 0
	m := &Monitor{
		Tunnels: []*config.Tunnel{
			testTunnel("a", config.StatusStopped, 1),
			testTunnel("b", config.StatusStarting, 2),
			testTunnel("c", config.StatusStopping, 3),
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
		Tunnels: []*config.Tunnel{testTunnel("a", config.StatusError, 9)},
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
	tun := testTunnel("a", config.StatusRunning, 1)
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
	if tun.Status() != config.StatusError || tun.ErrorMessage != "Reconnection failed after 1 attempts" {
		t.Fatalf("status %s message %q", tun.Status(), tun.ErrorMessage)
	}
}

func TestFutureAttemptWaits(t *testing.T) {
	rest := &fakeRestart{}
	m := &Monitor{
		Tunnels: []*config.Tunnel{testTunnel("a", config.StatusRunning, 1)},
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
		Tunnels: []*config.Tunnel{testTunnel("a", config.StatusRunning, 1)},
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
		Tunnels: []*config.Tunnel{testTunnel("a", config.StatusRunning, 1)},
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

func TestCheckOnceStopsOnCancellation(t *testing.T) {
	for _, when := range []string{"before pass", "during check", "during restart"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checks, wantChecks, wantRestarts := 0, 0, 0
			rest := &fakeRestart{}
			switch when {
			case "before pass":
				cancel()
			case "during check":
				wantChecks = 1
			case "during restart":
				wantChecks, wantRestarts = 1, 1
				rest.onRestart = cancel
			}
			m := &Monitor{
				Tunnels: []*config.Tunnel{
					testTunnel("a", config.StatusError, 0),
					testTunnel("b", config.StatusError, 0),
				},
				Config: NewConfig(time.Second, time.Second, 5),
				Check: func(string, int, time.Duration) bool {
					checks++
					if when == "during check" {
						cancel()
					}
					return false
				},
				Restart: rest,
			}
			m.CheckOnce(ctx, time.Now())
			if checks != wantChecks || rest.calls != wantRestarts {
				t.Fatalf("checks = %d, restarts = %d; want %d, %d", checks, rest.calls, wantChecks, wantRestarts)
			}
		})
	}
}
