package monitor

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"time"

	"github.com/bigbag/tunnel-manager/internal/config"
)

type Config struct {
	Interval       time.Duration
	Timeout        time.Duration
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
}

func NewConfig(interval, timeout time.Duration, maxRetries int) Config {
	return Config{
		Interval:       interval,
		Timeout:        timeout,
		MaxRetries:     maxRetries,
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     300 * time.Second,
		Multiplier:     2,
	}
}

func Backoff(attempt int, initial, max time.Duration, multiplier float64) time.Duration {
	d := float64(initial) * math.Pow(multiplier, float64(attempt-1))
	if d > float64(max) {
		return max
	}
	return time.Duration(d)
}

type Checker func(host string, port int, timeout time.Duration) bool

type Restarter interface {
	Restart(ctx context.Context, t *config.Tunnel, log func(string)) (bool, error)
}

type state struct {
	Attempts int
	Next     time.Time
	GivenUp  bool
}

type Monitor struct {
	Tunnels []*config.Tunnel
	Config  Config
	Check   Checker
	Restart Restarter
	Log     func(name, msg string)
	Now     func() time.Time
	states  map[string]*state
}

func TCPCheck(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (m *Monitor) Run(ctx context.Context) {
	m.log("monitor", "Health monitoring started")
	defer m.log("monitor", "Stopping health monitoring...")
	for {
		if ctx.Err() != nil {
			return
		}
		m.CheckOnce(ctx, m.now())
		if m.Config.Interval <= 0 {
			select {
			case <-ctx.Done():
				return
			default:
			}
			continue
		}
		timer := time.NewTimer(m.Config.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Monitor) CheckOnce(ctx context.Context, now time.Time) {
	for _, tun := range m.Tunnels {
		switch tun.Status {
		case config.StatusStopped, config.StatusStarting, config.StatusStopping:
			continue
		}
		if m.Config.Timeout > 0 && m.Check("127.0.0.1", tun.LocalPort, m.Config.Timeout) {
			st := m.state(tun.Name)
			if st.Attempts > 0 {
				m.log(tun.Name, fmt.Sprintf("Reconnected successfully after %d attempt(s)", st.Attempts))
			}
			*st = state{}
			continue
		}
		m.onDown(ctx, tun, now)
	}
}

func (m *Monitor) onDown(ctx context.Context, tun *config.Tunnel, now time.Time) {
	st := m.state(tun.Name)
	if st.GivenUp {
		return
	}
	if !st.Next.IsZero() && now.Before(st.Next) {
		return
	}
	st.Attempts++
	if st.Attempts > m.Config.MaxRetries {
		st.GivenUp = true
		tun.Status = config.StatusError
		tun.ErrorMessage = fmt.Sprintf("Reconnection failed after %d attempts", m.Config.MaxRetries)
		m.log(tun.Name, fmt.Sprintf("Giving up after %d failed reconnection attempts", m.Config.MaxRetries))
		return
	}
	m.log(tun.Name, fmt.Sprintf("Attempting reconnection (attempt %d/%d)", st.Attempts, m.Config.MaxRetries))
	ok, err := m.Restart.Restart(ctx, tun, func(msg string) { m.log(tun.Name, msg) })
	bo := Backoff(st.Attempts, m.Config.InitialBackoff, m.Config.MaxBackoff, m.Config.Multiplier)
	if err != nil {
		st.Next = now.Add(bo)
		m.log(tun.Name, fmt.Sprintf("Reconnection error: %s", err))
		return
	}
	if ok {
		m.log(tun.Name, "Reconnection successful")
		*st = state{}
		return
	}
	st.Next = now.Add(bo)
	m.log(tun.Name, fmt.Sprintf("Reconnection failed, next attempt in %.0fs", bo.Seconds()))
}

func (m *Monitor) state(name string) *state {
	if m.states == nil {
		m.states = make(map[string]*state)
	}
	st, ok := m.states[name]
	if !ok {
		st = &state{}
		m.states[name] = st
	}
	return st
}

func (m *Monitor) log(name, msg string) {
	if m.Log != nil {
		m.Log(name, msg)
	}
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
