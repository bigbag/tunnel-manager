package forward

import (
	"reflect"
	"testing"

	"github.com/bigbag/tunnel-manager/internal/config"
)

func TestListenAddr(t *testing.T) {
	if got := ListenAddr(5432); got != ":5432" {
		t.Fatalf("addr %q", got)
	}
}

func TestKubectlArgs(t *testing.T) {
	base := &config.Tunnel{
		Namespace:  "ns",
		Context:    "ctx",
		Service:    "svc",
		LocalPort:  5432,
		RemotePort: 5432,
	}
	got := KubectlArgs(base)
	want := []string{
		"kubectl", "port-forward",
		"--namespace=ns", "--context=ctx",
		"service/svc", "5432:5432",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v", got)
	}
	base.Address = "0.0.0.0"
	got = KubectlArgs(base)
	want = []string{
		"kubectl", "port-forward",
		"--namespace=ns", "--context=ctx", "--address=0.0.0.0",
		"service/svc", "5432:5432",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args with address = %#v", got)
	}
}

func TestChooseAuth(t *testing.T) {
	got := ChooseAuth("/keys/id", "secret", true, true)
	if got.KeyPath != "/keys/id" || !got.UseAgent || !got.UsePassword {
		t.Fatalf("choice = %+v", got)
	}
	if len(got.Logs) != 2 || got.Logs[0] != "Using key: /keys/id" || got.Logs[1] != "Using password authentication" {
		t.Fatalf("logs = %#v", got.Logs)
	}

	missing := ChooseAuth("/keys/missing", "", false, false)
	if missing.KeyPath != "" || missing.UseAgent || missing.UsePassword {
		t.Fatalf("missing = %+v", missing)
	}
	if missing.Logs[0] != "Key not found: /keys/missing" || missing.Logs[1] != "Using key/agent authentication" {
		t.Fatalf("missing logs = %#v", missing.Logs)
	}

	none := ChooseAuth("", "", false, false)
	if none.UsePassword || none.UseAgent || none.KeyPath != "" {
		t.Fatalf("none = %+v", none)
	}
}
