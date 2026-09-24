package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bigbag/tunnel-manager/internal/config"
	"github.com/bigbag/tunnel-manager/internal/monitor"
)

type fakeStart struct {
	fail    map[string]bool
	stopped []string
}

func (f *fakeStart) Start(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	if f.fail[t.Name] {
		t.Status = config.StatusError
		t.ErrorMessage = "boom"
		return false, nil
	}
	t.Status = config.StatusRunning
	return true, nil
}

func (f *fakeStart) Stop(t *config.Tunnel, log func(string)) error {
	f.stopped = append(f.stopped, t.Name)
	t.Status = config.StatusStopped
	return nil
}

func (f *fakeStart) Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error) {
	return f.Start(ctx, t, log)
}

func closed() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestZeroRunningExits1(t *testing.T) {
	f := &fakeStart{fail: map[string]bool{"a": true}}
	code := Run(context.Background(), Options{
		ListName: "dev",
		Tunnels:  []*config.Tunnel{{Name: "a", Type: "ssh", LocalPort: 1, RemotePort: 2}},
		Start:    f,
		Stop:     closed(),
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
	f := &fakeStart{fail: map[string]bool{"bad": true}}
	var lines []string
	code := Run(context.Background(), Options{
		ListName: "dev",
		Tunnels: []*config.Tunnel{
			{Name: "ok", Type: "ssh", LocalPort: 8081, RemotePort: 8081, RemoteHost: "127.0.0.1"},
			{Name: "bad", Type: "ssh", LocalPort: 1, RemotePort: 2},
		},
		Start: f,
		Stop:  closed(),
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

func TestStatusLineWholeSeconds(t *testing.T) {
	cfg := monitor.NewConfig(30400*time.Millisecond, time.Second, 5)
	got := StatusLine(1, 1, &cfg)
	want := "1/1 tunnels running. Health monitoring enabled (interval: 30s)."
	if got != want {
		t.Fatalf("%q", got)
	}
}
