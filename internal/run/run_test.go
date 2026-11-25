package run

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bigbag/tunnel-manager/internal/config"
	"github.com/bigbag/tunnel-manager/internal/monitor"
)

type fakeStart struct {
	fail      map[string]bool
	started   []string
	stopped   []string
	onStart   func(*config.Tunnel)
	onRestart func(*config.Tunnel) (bool, error)
}

func (f *fakeStart) Start(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	f.started = append(f.started, t.Name)
	if f.onStart != nil {
		f.onStart(t)
	}
	if f.fail[t.Name] {
		t.SetStatus(config.StatusError)
		t.ErrorMessage = "boom"
		return false, nil
	}
	t.SetStatus(config.StatusRunning)
	return true, nil
}

func (f *fakeStart) Stop(t *config.Tunnel, log func(string)) error {
	f.stopped = append(f.stopped, t.Name)
	t.SetStatus(config.StatusStopped)
	return nil
}

func (f *fakeStart) Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	if f.onRestart != nil {
		return f.onRestart(t)
	}
	return f.Start(ctx, t, log)
}

func TestZeroRunningExits1(t *testing.T) {
	f := &fakeStart{fail: map[string]bool{"a": true}}
	code := Run(context.Background(), Options{
		ListName: "dev",
		Tunnels:  []*config.Tunnel{{Name: "a", Type: "ssh", LocalPort: 1, RemotePort: 2}},
		Start:    f,
		Print:    func(string) {},
	})
	if code != 1 {
		t.Fatalf("code %d", code)
	}
	if len(f.stopped) != 0 {
		t.Fatalf("stopped %#v", f.stopped)
	}
}

func TestShutdownStopsErrorTunnel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeStart{
		fail: map[string]bool{"bad": true},
		onStart: func(tun *config.Tunnel) {
			if tun.Name == "bad" {
				cancel()
			}
		},
	}
	var lines []string
	code := Run(ctx, Options{
		ListName: "dev",
		Tunnels: []*config.Tunnel{
			{Name: "ok", Type: "ssh", LocalPort: 8081, RemotePort: 8081, RemoteHost: "127.0.0.1"},
			{Name: "bad", Type: "ssh", LocalPort: 1, RemotePort: 2},
		},
		Start: f,
		Print: func(s string) { lines = append(lines, s) },
	})
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if len(f.stopped) != 2 || f.stopped[0] != "ok" || f.stopped[1] != "bad" {
		t.Fatalf("stopped %#v", f.stopped)
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "[ok] OK - localhost:8081 -> 127.0.0.1:8081") {
		t.Fatalf("missing OK line:\n%s", text)
	}
	if !strings.Contains(text, "[bad] FAILED - boom") {
		t.Fatalf("missing FAILED line:\n%s", text)
	}
	if !strings.Contains(text, "[ok] Stopped") || !strings.Contains(text, "[bad] Stopped") {
		t.Fatalf("missing Stopped:\n%s", text)
	}
}

func TestAbortStopsStartingTunnels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeStart{
		fail: map[string]bool{"rejected-1": true, "rejected-2": true},
		onStart: func(tun *config.Tunnel) {
			if tun.Name == "rejected-2" {
				cancel()
			}
		},
	}
	tunnels := []*config.Tunnel{
		{Name: "running"},
		{Name: "rejected-1"},
		{Name: "rejected-2"},
		{Name: "later"},
	}
	for _, tun := range tunnels {
		tun.SetStatus(config.StatusStopped)
	}
	Run(ctx, Options{
		Tunnels: tunnels,
		Start:   f,
		Print:   func(string) {},
	})
	if got := strings.Join(f.started, ","); got != "running,rejected-1,rejected-2" {
		t.Fatalf("started %s", got)
	}
	for _, tun := range tunnels {
		if tun.Status() != config.StatusStopped {
			t.Errorf("%s status = %s", tun.Name, tun.Status())
		}
	}
}

func TestShutdownWaitsForMonitor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		tun := &config.Tunnel{Name: "a"}
		tun.SetStatus(config.StatusStopped)
		f := &fakeStart{
			onRestart: func(tun *config.Tunnel) (bool, error) {
				cancel()
				<-release
				tun.SetStatus(config.StatusError)
				return false, nil
			},
		}
		cfg := monitor.NewConfig(time.Second, time.Second, 5)
		done := make(chan struct{})
		go func() {
			defer close(done)
			Run(ctx, Options{
				Tunnels: []*config.Tunnel{tun},
				Start:   f,
				Monitor: &cfg,
				Check:   func(string, int, time.Duration) bool { return false },
				Print:   func(string) {},
			})
		}()
		synctest.Wait()
		select {
		case <-done:
			t.Error("runner returned while a reconnect was active")
		default:
		}
		close(release)
		synctest.Wait()
		<-done
		if tun.Status() != config.StatusStopped {
			t.Fatalf("status after shutdown = %s", tun.Status())
		}
	})
}

func TestStatusLineWholeSeconds(t *testing.T) {
	cfg := monitor.NewConfig(30400*time.Millisecond, time.Second, 5)
	got := StatusLine(1, 1, &cfg)
	want := "1/1 tunnels running. Health monitoring enabled (interval: 30s)."
	if got != want {
		t.Fatalf("%q", got)
	}
}
