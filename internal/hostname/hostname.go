// Package hostname canonicalizes URLs down to a single comparable hostname.
//
// It is the one place canonicalization happens (ADR-0002): the whitelist store
// holds already-canonical entries and does no normalization of its own.
package hostname

import (
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// Canonical parses rawURL and returns its canonical hostname: lowercased,
// punycode-encoded, with any port stripped.
//
// It rejects URLs with no host and any scheme other than http or https.
func Canonical(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("URL %q has no host", rawURL)
	}
	return CanonicalHost(host)
}

// CanonicalHost canonicalizes a bare hostname — one with no scheme, port, or
// path. Use it to validate hostnames that did not come from a URL, such as
// whitelist file entries.
func CanonicalHost(host string) (string, error) {
	if host == "" {
		return "", fmt.Errorf("hostname is empty")
	}

	canonical, err := idna.Lookup.ToASCII(strings.ToLower(host))
	if err != nil {
		return "", fmt.Errorf("host %q is not a valid domain name: %w", host, err)
	}
	return canonical, nil
}
