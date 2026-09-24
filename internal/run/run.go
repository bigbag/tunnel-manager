package run

import (
	"context"
	"fmt"

	"github.com/bigbag/tunnel-manager/internal/config"
	"github.com/bigbag/tunnel-manager/internal/monitor"
)

type Starter interface {
	Start(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error)
	Stop(t *config.Tunnel, log func(string)) error
	Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error)
}

type Options struct {
	ListName string
	Tunnels  []*config.Tunnel
	Monitor  *monitor.Config
	Start    Starter
	Stop     <-chan struct{}
	Check    monitor.Checker
	Print    func(string)
}

func Run(ctx context.Context, opt Options) int {
	print := opt.Print
	if print == nil {
		print = func(s string) { fmt.Println(s) }
	}
	for _, tun := range opt.Tunnels {
		print(fmt.Sprintf("[%s] Connecting...", tun.Name))
		ok, err := opt.Start.Start(ctx, tun, func(msg string) {
			print(fmt.Sprintf("[%s] %s", tun.Name, msg))
		})
		if ok {
			print(fmt.Sprintf("[%s] OK - localhost:%d -> %s", tun.Name, tun.LocalPort, tun.RemoteDisplay()))
			continue
		}
		msg := tun.ErrorMessage
		if msg == "" && err != nil {
			msg = err.Error()
		}
		print(fmt.Sprintf("[%s] FAILED - %s", tun.Name, msg))
	}
	running := 0
	for _, tun := range opt.Tunnels {
		if tun.Status == config.StatusRunning {
			running++
		}
	}
	if running == 0 {
		print("No tunnels started successfully.")
		return 1
	}
	monCtx, monCancel := context.WithCancel(ctx)
	defer monCancel()
	if opt.Monitor != nil {
		m := &monitor.Monitor{
			Tunnels: opt.Tunnels,
			Config:  *opt.Monitor,
			Check:   opt.Check,
			Restart: opt.Start,
			Log: func(name, msg string) {
				print(fmt.Sprintf("[%s] %s", name, msg))
			},
		}
		go m.Run(monCtx)
	}
	print(StatusLine(running, len(opt.Tunnels), opt.Monitor))
	print("Press Ctrl+C to stop.")
	if opt.Stop != nil {
		select {
		case <-opt.Stop:
		case <-ctx.Done():
		}
	}
	monCancel()
	print("Stopping all tunnels...")
	for _, tun := range opt.Tunnels {
		if tun.Status == config.StatusStopped {
			continue
		}
		_ = opt.Start.Stop(tun, func(msg string) {
			print(fmt.Sprintf("[%s] %s", tun.Name, msg))
		})
		print(fmt.Sprintf("[%s] Stopped", tun.Name))
	}
	print("Done.")
	return 0
}

func StatusLine(running, total int, mon *monitor.Config) string {
	if mon != nil {
		return fmt.Sprintf("%d/%d tunnels running. Health monitoring enabled (interval: %.0fs).",
			running, total, mon.Interval.Seconds())
	}
	return fmt.Sprintf("%d/%d tunnels running.", running, total)
}
