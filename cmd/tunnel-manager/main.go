package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/bigbag/tunnel-manager/internal/config"
	"github.com/bigbag/tunnel-manager/internal/forward"
	"github.com/bigbag/tunnel-manager/internal/monitor"
	runner "github.com/bigbag/tunnel-manager/internal/run"
)

type options struct {
	listMode   bool
	help       bool
	monitor    bool
	interval   float64
	timeout    float64
	maxRetries int
	listName   string
}

type argError struct {
	kind  string
	flag  string
	value string
}

func (e *argError) Error() string {
	return e.kind + " " + e.flag + " " + e.value
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opt, err := parseArgs(args)
	if err != nil {
		var ae *argError
		if errors.As(err, &ae) {
			if ae.kind == "unknown" {
				fmt.Printf("Unknown option: %s\n", ae.value)
				printUsage()
				return 1
			}
			fmt.Printf("Error: invalid value for %s: %s\n", ae.flag, ae.value)
			return 1
		}
		fmt.Println(err)
		return 1
	}
	if opt.help {
		printUsage()
		return 0
	}
	if opt.listMode {
		return showLists()
	}
	if opt.listName == "" {
		printUsage()
		return 1
	}
	file, err := config.Load("tunnels.json")
	if err != nil {
		return configError(err)
	}
	tunnels, warnings, err := file.TunnelsByList(opt.listName)
	for _, warning := range warnings {
		fmt.Println(warning)
	}
	if err != nil {
		if errors.Is(err, config.ErrListNotFound) {
			fmt.Printf("Error: List '%s' not found\n\n", opt.listName)
			printLists(file)
			return 1
		}
		if errors.Is(err, config.ErrEmptyList) {
			fmt.Printf("Error: List '%s' is empty\n", opt.listName)
			return 1
		}
		fmt.Printf("Error loading config: %s\n", err)
		return 1
	}
	fmt.Printf("SSH Tunnel Runner - Starting '%s' (%d tunnels)\n\n", opt.listName, len(tunnels))
	password := ""
	if hasSSH(tunnels) {
		password, err = readPassword(file.Defaults.SSHUser, file.Defaults.SSHBastion)
		if err != nil {
			fmt.Println("Error: cannot read password from the terminal")
			return 1
		}
	}
	var mon *monitor.Config
	if opt.monitor {
		cfg := monitor.NewConfig(
			time.Duration(opt.interval*float64(time.Second)),
			time.Duration(opt.timeout*float64(time.Second)),
			opt.maxRetries,
		)
		mon = &cfg
	}
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		close(stop)
	}()
	return runner.Run(context.Background(), runner.Options{
		ListName: opt.listName,
		Tunnels:  tunnels,
		Monitor:  mon,
		Start:    forward.NewManager(password),
		Stop:     stop,
		Check:    monitor.TCPCheck,
	})
}

func parseArgs(args []string) (options, error) {
	opt := options{monitor: true, interval: 30, timeout: 5, maxRetries: 5}
	if len(args) > 0 && (args[0] == "--list" || args[0] == "-l") {
		opt.listMode = true
		return opt, nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-h", "--help":
			opt.help = true
			return opt, nil
		case "--no-monitor":
			opt.monitor = false
		case "--check-interval", "--check-timeout", "--max-retries":
			if i+1 >= len(args) || isFlag(args[i+1]) {
				return opt, &argError{kind: "unknown", value: a}
			}
			val := args[i+1]
			i++
			switch a {
			case "--check-interval":
				n, err := strconv.ParseFloat(val, 64)
				if err != nil {
					return opt, &argError{kind: "invalid", flag: a, value: val}
				}
				opt.interval = n
			case "--check-timeout":
				n, err := strconv.ParseFloat(val, 64)
				if err != nil {
					return opt, &argError{kind: "invalid", flag: a, value: val}
				}
				opt.timeout = n
			case "--max-retries":
				n, err := strconv.Atoi(val)
				if err != nil {
					return opt, &argError{kind: "invalid", flag: a, value: val}
				}
				opt.maxRetries = n
			}
		default:
			if strings.HasPrefix(a, "-") {
				return opt, &argError{kind: "unknown", value: a}
			}
			opt.listName = a
		}
	}
	return opt, nil
}

func isFlag(s string) bool {
	return s == "-h" || s == "-l" || strings.HasPrefix(s, "--")
}

func showLists() int {
	file, err := config.Load("tunnels.json")
	if err != nil {
		return configError(err)
	}
	printLists(file)
	return 0
}

func printLists(file *config.File) {
	fmt.Println("Available lists:")
	for _, list := range file.Lists {
		fmt.Printf("  %s (%d tunnels)\n", list.Name, len(list.Tunnels))
	}
}

func configError(err error) int {
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("Error: Config file not found: tunnels.json")
		return 1
	}
	fmt.Printf("Error loading config: %s\n", err)
	return 1
}

func hasSSH(tunnels []*config.Tunnel) bool {
	for _, tun := range tunnels {
		if tun.Type == "ssh" {
			return true
		}
	}
	return false
}

func readPassword(user, host string) (string, error) {
	if user == "" {
		user = "user"
	}
	if host == "" {
		host = "SSH server"
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("not a terminal")
	}
	fmt.Printf("Password for %s@%s: ", user, host)
	b, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Println()
	return string(b), nil
}

func printUsage() {
	fmt.Println(`Usage: tunnel-manager <list_name> [options]
       tunnel-manager --list

Options:
  --no-monitor         Disable automatic health monitoring
  --check-interval N   Health check interval in seconds (default: 30)
  --check-timeout N    Health check connect timeout in seconds (default: 5)
  --max-retries N      Max reconnection attempts (default: 5)
  -h, --help           Show this help message

Examples:
  tunnel-manager dev
  tunnel-manager dev --no-monitor
  tunnel-manager dev --check-interval 60 --check-timeout 2 --max-retries 3
  tunnel-manager --list`)
}
