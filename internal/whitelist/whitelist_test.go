package whitelist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// --- Ticket 03: persisting "always" approvals ---------------------------

func TestAddAllowsTheHostAndSurvivesReload(t *testing.T) {
	path := writeFile(t, `["example.com"]`)
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if err := store.Add("newsite.example"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if !store.Allowed("newsite.example") {
		t.Error("added host is not allowed in the live store")
	}

	// The restart: a fresh store reading the same file must agree.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reloading returned error: %v", err)
	}
	if !reloaded.Allowed("newsite.example") {
		t.Error("added host did not survive a reload")
	}
	if !reloaded.Allowed("example.com") {
		t.Error("pre-existing entry was lost by Add")
	}
}

// Entries must be written in the same canonical form matching uses, or a saved
// approval would not match the next request.
func TestAddWritesCanonicalEntriesThatReloadCleanly(t *testing.T) {
	path := writeFile(t, `[]`)
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := store.Add("xn--mnchen-3ya.de"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	// Load rejects any non-canonical entry, so a clean reload proves the form.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Add wrote an entry Load rejects: %v", err)
	}
	if !reloaded.Allowed("xn--mnchen-3ya.de") {
		t.Error("canonical host did not survive a reload")
	}
}

func TestAddIsIdempotent(t *testing.T) {
	path := writeFile(t, `["example.com"]`)
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	for range 3 {
		if err := store.Add("example.com"); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading whitelist: %v", err)
	}
	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parsing whitelist: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("whitelist holds %v, want one entry", entries)
	}
}

// A failed save must not leave the host allowed: an approval that could not be
// recorded is not a persistent approval.
func TestAddReportsSaveFailureAndDoesNotAllowTheHost(t *testing.T) {
	// A path whose parent directory does not exist. Load treats it as an empty
	// whitelist; writing to it fails.
	path := filepath.Join(t.TempDir(), "no-such-dir", "whitelist.json")
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if err := store.Add("newsite.example"); err == nil {
		t.Fatal("Add succeeded despite an unwritable whitelist, want an error")
	}
	if store.Allowed("newsite.example") {
		t.Error("host is allowed even though the approval could not be saved")
	}
}

// Run with -race. Readers and writers must not tear.
func TestAddAndAllowedAreSafeForConcurrentUse(t *testing.T) {
	store, err := Load(writeFile(t, `["example.com"]`))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := store.Add(fmt.Sprintf("host%d.example", i)); err != nil {
				t.Errorf("Add returned error: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			store.Allowed("example.com")
		}()
	}
	wg.Wait()

	for i := range 8 {
		host := fmt.Sprintf("host%d.example", i)
		if !store.Allowed(host) {
			t.Errorf("Allowed(%q) = false after a concurrent Add", host)
		}
	}
}

// Saving rewrites the file, so it must not quietly restyle permissions the
// operator chose deliberately.
func TestAddPreservesTheExistingFileMode(t *testing.T) {
	path := writeFile(t, `["example.com"]`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := store.Add("newsite.example"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("file mode = %o, want %o", got, 0o644)
	}
}

// A whitelist created by the first approval is private by default.
func TestAddCreatesANewFilePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whitelist.json")
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := store.Add("newsite.example"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("new file mode = %o, want %o", got, 0o600)
	}
}
