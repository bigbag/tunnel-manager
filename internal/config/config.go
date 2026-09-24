package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusError    Status = "error"
	StatusStopping Status = "stopping"
)

var (
	ErrListNotFound = errors.New("list not found")
	ErrEmptyList    = errors.New("empty list")
)

type Field struct {
	Value string
	Null  bool
	Set   bool
}

func (f *Field) UnmarshalJSON(b []byte) error {
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		f.Null = true
		f.Value = ""
		return nil
	}
	f.Null = false
	return json.Unmarshal(b, &f.Value)
}

type Defaults struct {
	SSHUser      string
	SSHBastion   string
	IdentityFile string
}

type Tunnel struct {
	Name         string
	Type         string
	Description  string
	Tags         []string
	SSHUser      string
	SSHBastion   string
	IdentityFile string
	RemoteHost   string
	Context      string
	Namespace    string
	Service      string
	Address      string
	LocalPort    int
	RemotePort   int
	Status       Status
	PID          int
	ErrorMessage string
}

func (t *Tunnel) RemoteDisplay() string {
	switch t.Type {
	case "ssh":
		return fmt.Sprintf("%s:%d", t.RemoteHost, t.RemotePort)
	case "kubectl":
		return fmt.Sprintf("%s:%d", t.Service, t.RemotePort)
	default:
		return fmt.Sprintf("%d", t.RemotePort)
	}
}

type List struct {
	Name    string
	Tunnels []string
}

type File struct {
	Version  string
	Defaults Defaults
	Lists    []List
	byName   map[string]*Tunnel
}

func (f *File) TunnelsByList(name string) ([]*Tunnel, []string, error) {
	var names []string
	found := false
	for _, list := range f.Lists {
		if list.Name == name {
			names = list.Tunnels
			found = true
			break
		}
	}
	if !found {
		return nil, nil, ErrListNotFound
	}
	var out []*Tunnel
	var warnings []string
	for _, n := range names {
		tun, ok := f.byName[n]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Warning: tunnel '%s' not found in config", n))
			continue
		}
		out = append(out, tun)
	}
	if len(out) == 0 {
		return nil, warnings, ErrEmptyList
	}
	return out, warnings, nil
}

type defaultsJSON struct {
	SSHUser      string `json:"ssh_user"`
	SSHBastion   string `json:"ssh_bastion"`
	IdentityFile string `json:"identity_file"`
}

type tunnelJSON struct {
	Name         *string  `json:"name"`
	Type         *string  `json:"type"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags"`
	SSHUser      Field    `json:"ssh_user"`
	SSHBastion   Field    `json:"ssh_bastion"`
	IdentityFile Field    `json:"identity_file"`
	RemoteHost   Field    `json:"remote_host"`
	Context      *string  `json:"context"`
	Namespace    *string  `json:"namespace"`
	Service      *string  `json:"service"`
	Address      *string  `json:"address"`
	LocalPort    *int     `json:"local_port"`
	RemotePort   *int     `json:"remote_port"`
}

type rootJSON struct {
	Version  string          `json:"version"`
	Defaults *defaultsJSON   `json:"defaults"`
	Lists    json.RawMessage `json:"lists"`
	Tunnels  json.RawMessage `json:"tunnels"`
}

func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root rootJSON
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Tunnels) == 0 || string(root.Tunnels) == "null" {
		return nil, errors.New("missing tunnels")
	}
	var rawTunnels []tunnelJSON
	if err := json.Unmarshal(root.Tunnels, &rawTunnels); err != nil {
		return nil, err
	}
	lists, err := parseLists(root.Lists)
	if err != nil {
		return nil, err
	}
	file := &File{
		Version: root.Version,
		Lists:   lists,
		byName:  make(map[string]*Tunnel),
	}
	if file.Version == "" {
		file.Version = "1.0"
	}
	if root.Defaults != nil {
		file.Defaults = Defaults{
			SSHUser:      root.Defaults.SSHUser,
			SSHBastion:   root.Defaults.SSHBastion,
			IdentityFile: root.Defaults.IdentityFile,
		}
	}
	for i, raw := range rawTunnels {
		tun, err := buildTunnel(i, raw, file.Defaults)
		if err != nil {
			return nil, err
		}
		file.byName[tun.Name] = tun
	}
	return file, nil
}

func buildTunnel(i int, raw tunnelJSON, defaults Defaults) (*Tunnel, error) {
	label := fmt.Sprintf("%d", i)
	if raw.Name != nil && *raw.Name != "" {
		label = fmt.Sprintf("%q", *raw.Name)
	}
	if raw.Name == nil || *raw.Name == "" {
		return nil, fmt.Errorf("tunnel %s: missing name", label)
	}
	if raw.Type == nil || *raw.Type == "" {
		return nil, fmt.Errorf("tunnel %s: missing type", label)
	}
	if raw.LocalPort == nil {
		return nil, fmt.Errorf("tunnel %s: missing local_port", label)
	}
	if raw.RemotePort == nil {
		return nil, fmt.Errorf("tunnel %s: missing remote_port", label)
	}
	tags := raw.Tags
	if tags == nil {
		tags = []string{}
	}
	tun := &Tunnel{
		Name:         *raw.Name,
		Type:         *raw.Type,
		Description:  raw.Description,
		Tags:         tags,
		SSHUser:      resolve(raw.SSHUser, defaults.SSHUser),
		SSHBastion:   resolve(raw.SSHBastion, defaults.SSHBastion),
		IdentityFile: resolve(raw.IdentityFile, defaults.IdentityFile),
		RemoteHost:   resolve(raw.RemoteHost, "127.0.0.1"),
		LocalPort:    *raw.LocalPort,
		RemotePort:   *raw.RemotePort,
		Status:       StatusStopped,
	}
	if raw.Context != nil {
		tun.Context = *raw.Context
	}
	if raw.Namespace != nil {
		tun.Namespace = *raw.Namespace
	}
	if raw.Service != nil {
		tun.Service = *raw.Service
	}
	if raw.Address != nil {
		tun.Address = *raw.Address
	}
	return tun, nil
}

func resolve(field Field, fallback string) string {
	if !field.Set {
		return fallback
	}
	if field.Null {
		return ""
	}
	return field.Value
}

func parseLists(raw json.RawMessage) ([]List, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("lists must be an object")
	}
	var lists []List
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("lists key must be a string")
		}
		var names []string
		if err := dec.Decode(&names); err != nil {
			return nil, fmt.Errorf("list %q: %w", key, err)
		}
		if names == nil {
			names = []string{}
		}
		lists = append(lists, List{Name: key, Tunnels: names})
	}
	return lists, nil
}
