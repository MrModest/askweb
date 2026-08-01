// Package config resolves askweb's runtime settings from flags and environment.
package config

import (
	"flag"
	"io"
)

// Defaults. Nothing about the deployment is baked into the binary, so
// containerization needs no code change (ADR-0004).
const (
	DefaultAddr          = ":8080"
	DefaultWhitelistPath = "whitelist.json"
)

// Config holds askweb's runtime settings.
type Config struct {
	// Addr is the listen address for the MCP HTTP server.
	Addr string
	// WhitelistPath is the JSON file holding the allowed hostnames.
	WhitelistPath string
}

// Resolve parses args into a Config. A flag wins over the matching environment
// variable, which wins over the default. Usage and errors are written to out.
func Resolve(args []string, getenv func(string) string, out io.Writer) (Config, error) {
	var cfg Config

	fs := flag.NewFlagSet("askweb", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.Addr, "addr", orDefault(getenv("ASKWEB_ADDR"), DefaultAddr),
		"listen address for the MCP HTTP server (env ASKWEB_ADDR)")
	fs.StringVar(&cfg.WhitelistPath, "whitelist", orDefault(getenv("ASKWEB_WHITELIST"), DefaultWhitelistPath),
		"path to the JSON file of allowed hostnames (env ASKWEB_WHITELIST)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
