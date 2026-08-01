package config

import (
	"io"
	"testing"
)

// env builds a getenv function over a fixed map.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestResolveDefaults(t *testing.T) {
	cfg, err := Resolve(nil, env(nil), io.Discard)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":8080")
	}
	if cfg.WhitelistPath == "" {
		t.Error("WhitelistPath is empty, want a default path")
	}
}

func TestResolveEnvOverridesDefaults(t *testing.T) {
	cfg, err := Resolve(nil, env(map[string]string{
		"ASKWEB_ADDR":      "127.0.0.1:9999",
		"ASKWEB_WHITELIST": "/etc/askweb/hosts.json",
	}), io.Discard)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9999" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:9999")
	}
	if cfg.WhitelistPath != "/etc/askweb/hosts.json" {
		t.Errorf("WhitelistPath = %q, want %q", cfg.WhitelistPath, "/etc/askweb/hosts.json")
	}
}

func TestResolveFlagsOverrideEnv(t *testing.T) {
	args := []string{"--addr", ":1234", "--whitelist", "/tmp/flag.json"}
	cfg, err := Resolve(args, env(map[string]string{
		"ASKWEB_ADDR":      "127.0.0.1:9999",
		"ASKWEB_WHITELIST": "/etc/askweb/hosts.json",
	}), io.Discard)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if cfg.Addr != ":1234" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":1234")
	}
	if cfg.WhitelistPath != "/tmp/flag.json" {
		t.Errorf("WhitelistPath = %q, want %q", cfg.WhitelistPath, "/tmp/flag.json")
	}
}

func TestResolveRejectsUnknownFlags(t *testing.T) {
	if _, err := Resolve([]string{"--nope"}, env(nil), io.Discard); err == nil {
		t.Error("Resolve accepted an unknown flag, want error")
	}
}
