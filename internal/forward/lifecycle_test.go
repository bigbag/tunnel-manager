package forward

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/bigbag/tunnel-manager/internal/config"
	"github.com/bigbag/tunnel-manager/internal/monitor"
)

func TestKubeProcessExitConcurrentMonitorAndShutdown(t *testing.T) {
	if os.Getenv("TUNNEL_MANAGER_LIFECYCLE_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(1)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestKubeProcessExitConcurrentMonitorAndShutdown$")
	cmd.Env = append(os.Environ(), "TUNNEL_MANAGER_LIFECYCLE_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	tun := &config.Tunnel{Name: "lifecycle", Type: "kubectl", PID: cmd.Process.Pid}
	tun.SetStatus(config.StatusRunning)
	sess := &kubeSession{cmd: cmd, done: make(chan struct{})}
	manager := NewManager(nil, nil)
	manager.kube[tun.Name] = sess
	mon := &monitor.Monitor{
		Tunnels: []*config.Tunnel{tun},
		Config:  monitor.NewConfig(time.Second, time.Second, 1),
		Check:   func(string, int, time.Duration) bool { return true },
		Restart: manager,
	}
	go waitKube(sess, tun, func(string) {})
	ready := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		mon.CheckOnce(context.Background(), time.Now())
		_ = sess.alive()
		close(ready)
		for {
			mon.CheckOnce(context.Background(), time.Now())
			_ = sess.alive()
			select {
			case <-sess.done:
				return
			default:
			}
		}
	}()
	<-ready
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.done:
	case <-time.After(10 * time.Second):
		t.Fatal("process did not complete")
	}
	<-readerDone
	if sess.alive() {
		t.Fatal("completed process is alive")
	}
	if got := tun.Status(); got != config.StatusError {
		t.Fatalf("status after process exit = %q", got)
	}
	if err := manager.Stop(tun, nil); err != nil {
		t.Fatal(err)
	}
	if got := tun.Status(); got != config.StatusStopped || tun.PID != 0 {
		t.Fatalf("status after cleanup = %q, PID = %d", got, tun.PID)
	}
}
