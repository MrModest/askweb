package whitelist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// --- Ticket 02: creating the whitelist at startup ------------------------

// unwritableDir returns a directory the current user cannot write to. Root
// ignores the mode, so there is no way to make a directory unwritable for it.
func unwritableDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: the directory mode would be ignored")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore the mode so the temp directory can be cleaned up.
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	return dir
}

// The normal first run: an operator mounts an empty data directory and gets a
// whitelist file, which is immediate confirmation that the mount is right.
func TestOpenCreatesAnEmptyWhitelistWhenTheFileIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whitelist.json")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Open did not create the whitelist: %v", err)
	}
	if got := string(data); got != "[]\n" {
		t.Errorf("created whitelist holds %q, want %q", got, "[]\n")
	}
	// A created whitelist grants nothing: still fail-closed.
	for _, host := range []string{"example.com", "go.dev", ""} {
		if store.Allowed(host) {
			t.Errorf("Allowed(%q) = true on a freshly created whitelist, want false", host)
		}
	}
}

// The file the server creates holds the security boundary, so it is private,
// exactly as one created by a first approval is.
func TestOpenCreatesTheWhitelistPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whitelist.json")
	if _, err := Open(path); err != nil {
		t.Fatalf("Open returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("created whitelist mode = %o, want %o", got, 0o600)
	}
}

// An operator's own file is theirs. Open reads it; it never rewrites, reorders,
// or reformats it, and leaves no temporary file behind.
func TestOpenLeavesAnExistingWhitelistByteForByte(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whitelist.json")
	// Deliberately unsorted, compact, and without a trailing newline: none of
	// it is Open's business.
	const contents = `["go.dev","example.com"]`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing whitelist file: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	for _, host := range []string{"example.com", "go.dev"} {
		if !store.Allowed(host) {
			t.Errorf("Allowed(%q) = false, want true", host)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading whitelist: %v", err)
	}
	if got := string(data); got != contents {
		t.Errorf("existing whitelist was rewritten as %q, want %q untouched", got, contents)
	}

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	if len(names) != 1 || names[0].Name() != "whitelist.json" {
		var got []string
		for _, e := range names {
			got = append(got, e.Name())
		}
		t.Errorf("directory holds %v, want only whitelist.json", got)
	}
}

// The deployment mistake this guards against: a data directory the running user
// cannot write. The server would start, prompt, accept "always" — and forget
// it. Refuse to start instead, and name the path so the operator can fix it.
func TestOpenFailsWhenTheDirectoryCannotBeWritten(t *testing.T) {
	path := filepath.Join(unwritableDir(t), "whitelist.json")

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open succeeded on an unwritable directory, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path %q", err, path)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("Open created a whitelist it cannot write")
	}
}

// A missing directory is the same failure: the file cannot be created there.
func TestOpenFailsWhenTheDirectoryDoesNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "whitelist.json")

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open succeeded with no directory to write into, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path %q", err, path)
	}
}

// An existing whitelist that cannot be replaced is the same problem found one
// deploy later: an "always" would be accepted and dropped.
func TestOpenFailsWhenAnExistingWhitelistCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whitelist.json")
	const contents = "[\n  \"example.com\"\n]\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing whitelist file: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: the directory mode would be ignored")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open succeeded on an unwritable whitelist, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path %q", err, path)
	}
	if data, readErr := os.ReadFile(path); readErr != nil {
		t.Errorf("reading whitelist: %v", readErr)
	} else if string(data) != contents {
		t.Errorf("whitelist was changed to %q by a failed Open, want %q", data, contents)
	}
}

// Open loads through Load, so a dead entry is still a startup error.
func TestOpenRejectsNonCanonicalEntries(t *testing.T) {
	if _, err := Open(writeFile(t, `["Example.COM"]`)); err == nil {
		t.Error("Open of a non-canonical entry succeeded, want error")
	}
}

// Open is the startup entry point only. Load stays permissive about a path it
// cannot write, because a save failure while the server runs must not fail the
// call a human already approved — see the server package's
// TestApprovedCallSucceedsWhenTheWhitelistCannotBeSaved.
func TestLoadStillToleratesAnUnwritablePath(t *testing.T) {
	store, err := Load(filepath.Join(unwritableDir(t), "whitelist.json"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := store.Add("newsite.example"); err == nil {
		t.Error("Add succeeded on an unwritable directory, want an error")
	}
}
