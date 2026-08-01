package whitelist

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes contents to a temp file and returns its path.
func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "whitelist.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing whitelist file: %v", err)
	}
	return path
}

func TestLoadAllowsHostInFile(t *testing.T) {
	store, err := Load(writeFile(t, `["example.com", "go.dev"]`))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	for _, host := range []string{"example.com", "go.dev"} {
		if !store.Allowed(host) {
			t.Errorf("Allowed(%q) = false, want true", host)
		}
	}
}

// Matching is exact. Every name below is related to the whitelisted
// "example.com" by substring, suffix, prefix, or homograph, and every one of
// them must be refused (ADR-0002).
func TestAllowedRejectsNamesRelatedToAWhitelistedEntry(t *testing.T) {
	store, err := Load(writeFile(t, `["example.com"]`))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	tests := []struct {
		name string
		host string
	}{
		{"subdomain", "sub.example.com"},
		{"deep subdomain", "a.b.example.com"},
		{"www subdomain", "www.example.com"},
		{"suffix-extended domain", "example.com.evil.com"},
		{"prefix-extended domain", "evil-example.com"},
		{"hyphenated prefix", "notexample.com"},
		{"different TLD", "example.org"},
		{"bare substring", "example"},
		{"trailing dot", "example.com."},
		{"homograph", "xn--pple-43d.com"},
		{"empty host", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if store.Allowed(tt.host) {
				t.Errorf("Allowed(%q) = true, want false", tt.host)
			}
		})
	}
}

// A parent entry is not implied by a subdomain entry either.
func TestAllowedRejectsParentOfWhitelistedSubdomain(t *testing.T) {
	store, err := Load(writeFile(t, `["sub.example.com"]`))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if store.Allowed("example.com") {
		t.Error(`Allowed("example.com") = true, want false`)
	}
}

// The store does no normalization, so a non-canonical entry would be a silently
// dead line in the file. Refuse to start instead of pretending it grants access.
func TestLoadRejectsNonCanonicalEntries(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{"uppercase", `["Example.COM"]`},
		{"unicode not punycode", `["münchen.de"]`},
		{"full URL", `["https://example.com/"]`},
		{"host with port", `["example.com:8443"]`},
		{"empty entry", `[""]`},
		{"leading whitespace", `[" example.com"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeFile(t, tt.contents)); err == nil {
				t.Errorf("Load(%s) succeeded, want error", tt.contents)
			}
		})
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	if _, err := Load(writeFile(t, `{"hosts": ["example.com"]}`)); err == nil {
		t.Error("Load of a JSON object succeeded, want error")
	}
}

// The default whitelist path need not exist on a first run. An absent file is
// an empty whitelist, which refuses everything — still fail-closed.
func TestLoadTreatsMissingFileAsEmptyWhitelist(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if store.Allowed("example.com") {
		t.Error(`Allowed("example.com") = true on an empty whitelist, want false`)
	}
}
