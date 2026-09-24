package main

import (
	"testing"
)

func TestParseFlagsAfterList(t *testing.T) {
	opt, err := parseArgs([]string{"dev", "--no-monitor", "--check-interval", "60", "--check-timeout", "2", "--max-retries", "3"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.listName != "dev" || opt.monitor || opt.interval != 60 || opt.timeout != 2 || opt.maxRetries != 3 {
		t.Fatalf("opt = %+v", opt)
	}
}

func TestLastListNameWins(t *testing.T) {
	opt, err := parseArgs([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.listName != "b" {
		t.Fatalf("name %q", opt.listName)
	}
}

func TestListOnlyAsFirstArg(t *testing.T) {
	opt, err := parseArgs([]string{"--list", "dev"})
	if err != nil || !opt.listMode {
		t.Fatalf("opt %+v err %v", opt, err)
	}
	_, err = parseArgs([]string{"dev", "--list"})
	ae, ok := err.(*argError)
	if !ok || ae.kind != "unknown" || ae.value != "--list" {
		t.Fatalf("err = %#v", err)
	}
}

func TestBadNumber(t *testing.T) {
	_, err := parseArgs([]string{"dev", "--check-timeout", "nope"})
	ae, ok := err.(*argError)
	if !ok || ae.flag != "--check-timeout" || ae.value != "nope" {
		t.Fatalf("err = %#v", err)
	}
}

func TestNegativeTimeout(t *testing.T) {
	opt, err := parseArgs([]string{"dev", "--check-timeout", "-1"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.timeout != -1 {
		t.Fatalf("timeout %v", opt.timeout)
	}
}

func TestMissingFlagValue(t *testing.T) {
	_, err := parseArgs([]string{"--check-interval"})
	ae, ok := err.(*argError)
	if !ok || ae.kind != "unknown" || ae.value != "--check-interval" {
		t.Fatalf("err = %#v", err)
	}
}

func TestHelpExit(t *testing.T) {
	if code := run([]string{"dev", "--help"}); code != 0 {
		t.Fatalf("code %d", code)
	}
}

func TestMissingListExit(t *testing.T) {
	if code := run(nil); code != 1 {
		t.Fatalf("code %d", code)
	}
}

func TestBadNumberExit(t *testing.T) {
	if code := run([]string{"dev", "--max-retries", "nope"}); code != 1 {
		t.Fatalf("code %d", code)
	}
}

func TestListMissingConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	if code := run([]string{"--list"}); code != 1 {
		t.Fatalf("code %d", code)
	}
}
