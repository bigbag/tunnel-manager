package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadString(t *testing.T, body string) *File {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

func TestOmittedSSHFieldUsesDefault(t *testing.T) {
	f := loadString(t, `{
		"defaults": {"ssh_user": "user", "ssh_bastion": "10.0.0.1", "identity_file": "/keys/id"},
		"tunnels": [{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2}]
	}`)
	tun := f.byName["a"]
	if tun.SSHUser != "user" || tun.SSHBastion != "10.0.0.1" || tun.IdentityFile != "/keys/id" {
		t.Fatalf("defaults not applied: %+v", tun)
	}
}

func TestNullDoesNotUseDefault(t *testing.T) {
	f := loadString(t, `{
		"defaults": {"ssh_user": "user", "ssh_bastion": "10.0.0.1", "identity_file": "/keys/id"},
		"tunnels": [{
			"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2,
			"identity_file": null, "ssh_bastion": null
		}]
	}`)
	tun := f.byName["a"]
	if tun.IdentityFile != "" || tun.SSHBastion != "" {
		t.Fatalf("null used default: %+v", tun)
	}
	if tun.SSHUser != "user" {
		t.Fatalf("omitted user = %q", tun.SSHUser)
	}
}

func TestExplicitEmptyStringWins(t *testing.T) {
	f := loadString(t, `{
		"defaults": {"identity_file": "/keys/id"},
		"tunnels": [{
			"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2,
			"identity_file": ""
		}]
	}`)
	if f.byName["a"].IdentityFile != "" {
		t.Fatalf("empty string did not win: %q", f.byName["a"].IdentityFile)
	}
}

func TestOmittedRemoteHost(t *testing.T) {
	f := loadString(t, `{
		"tunnels": [{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2}]
	}`)
	if f.byName["a"].RemoteHost != "127.0.0.1" {
		t.Fatalf("remote host %q", f.byName["a"].RemoteHost)
	}
}

func TestNullRemoteHostStaysEmpty(t *testing.T) {
	f := loadString(t, `{
		"tunnels": [{
			"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2,
			"remote_host": null
		}]
	}`)
	if f.byName["a"].RemoteHost != "" {
		t.Fatalf("remote host %q", f.byName["a"].RemoteHost)
	}
}

func TestListOrderAndMissingName(t *testing.T) {
	f := loadString(t, `{
		"lists": {"dev": ["b", "missing", "a"]},
		"tunnels": [
			{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2},
			{"name": "b", "type": "ssh", "local_port": 3, "remote_port": 4}
		]
	}`)
	got, warnings, err := f.TunnelsByList("dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "b" || got[1].Name != "a" {
		t.Fatalf("order = %#v", namesOf(got))
	}
	if len(warnings) != 1 || warnings[0] != "Warning: tunnel 'missing' not found in config" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestUnknownList(t *testing.T) {
	f := loadString(t, `{
		"lists": {"dev": ["a"]},
		"tunnels": [{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2}]
	}`)
	_, _, err := f.TunnelsByList("nope")
	if err != ErrListNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyList(t *testing.T) {
	f := loadString(t, `{
		"lists": {"dev": ["missing"]},
		"tunnels": [{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2}]
	}`)
	_, warnings, err := f.TunnelsByList("dev")
	if err != ErrEmptyList {
		t.Fatalf("err = %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestMissingPortIsSchemaError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.json")
	body := `{"tunnels": [{"name": "a", "type": "ssh", "remote_port": 2}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "local_port") {
		t.Fatalf("err = %v", err)
	}
}

func TestDuplicateNameLastWins(t *testing.T) {
	f := loadString(t, `{
		"tunnels": [
			{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2, "remote_host": "one"},
			{"name": "a", "type": "ssh", "local_port": 9, "remote_port": 8, "remote_host": "two"}
		]
	}`)
	if f.byName["a"].LocalPort != 9 || f.byName["a"].RemoteHost != "two" {
		t.Fatalf("last did not win: %+v", f.byName["a"])
	}
}

func TestRepeatedListNameResolvesTwice(t *testing.T) {
	f := loadString(t, `{
		"lists": {"dev": ["a", "a"]},
		"tunnels": [{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2}]
	}`)
	got, _, err := f.TunnelsByList("dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != got[1] {
		t.Fatalf("resolved %#v", got)
	}
}

func TestListOrderInFile(t *testing.T) {
	f := loadString(t, `{
		"lists": {"b": ["a"], "a": ["a"]},
		"tunnels": [{"name": "a", "type": "ssh", "local_port": 1, "remote_port": 2}]
	}`)
	if len(f.Lists) != 2 || f.Lists[0].Name != "b" || f.Lists[1].Name != "a" {
		t.Fatalf("lists = %#v", f.Lists)
	}
}

func TestRealCatalogNullKey(t *testing.T) {
	f, err := Load("../../tunnels.json")
	if err != nil {
		t.Fatal(err)
	}
	got, warnings, err := f.TunnelsByList("mcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(got) != 7 {
		t.Fatalf("len %d warnings %#v", len(got), warnings)
	}
	for _, name := range []string{"rds_sdip_prod", "starrock_mcp"} {
		tun := f.byName[name]
		if tun == nil || tun.IdentityFile != "" {
			t.Fatalf("%s identity = %#v", name, tun)
		}
	}
}

func namesOf(ts []*Tunnel) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}
