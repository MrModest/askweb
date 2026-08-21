// Package whitelist holds the set of hostnames web_fetch is allowed to reach.
//
// The set is the security boundary of this server. Membership is exact: an
// entry grants access to that one canonical host and nothing else (ADR-0002).
package whitelist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/MrModest/askweb/internal/hostname"
)

// Store is a set of canonical hostnames backed by a flat JSON file.
//
// It is safe for concurrent use: one server serves many sessions, any of which
// may be reading the set while another persists an approval.
type Store struct {
	mu    sync.RWMutex
	path  string
	hosts map[string]struct{}
}

// Open loads the whitelist at path the way a starting server needs it, and is
// what a server should call rather than Load.
//
// A missing file is created holding an empty list. That is the normal first
// run, not a problem: an operator who mounts an empty data directory gets a
// whitelist file, and with it immediate confirmation that the mount is right.
// An empty list approves nothing, so the server still refuses every host until
// a human approves one.
//
// A path the whitelist cannot be written to is a startup error naming it. The
// whitelist is runtime state, and an approval that cannot be saved is logged
// and dropped — correct for the call in flight, but invisible to whoever
// deployed the server. Found at startup, the same misconfiguration is a
// deploy-time error against the path instead of an approval that quietly never
// persists.
//
// An existing whitelist is read and nothing more: never rewritten, reordered,
// or reformatted, since its contents and its formatting are the operator's.
func Open(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := save(path, []string{}); err != nil {
			return nil, fmt.Errorf("creating whitelist %s: %w", path, err)
		}
	} else if err := writable(path); err != nil {
		return nil, fmt.Errorf("whitelist %s cannot be written: %w", path, err)
	}
	return Load(path)
}

// writable reports whether save would be able to replace path, by doing the one
// thing save needs and no more: creating a file beside it, which is where the
// atomic rename comes from. The whitelist itself is never opened for writing.
func writable(path string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temp.Close()
	return os.Remove(temp.Name())
}

// Load reads a JSON array of canonical hostnames from path.
//
// A missing file is an empty whitelist, and so is a path that cannot be written
// at all: Load never writes, so it stays usable when the file underneath it is
// not. A running server depends on that — an approval it cannot save still
// allows the call the human approved (see Add). Open is the startup path that
// refuses such a file up front.
//
// Every entry must already be canonical: since Allowed does not normalize, a
// non-canonical entry would be a line in the file that grants nothing, so it is
// a startup error rather than a silent no-op.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{path: path, hosts: map[string]struct{}{}}, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing whitelist %s: %w", path, err)
	}

	hosts := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		canonical, err := hostname.CanonicalHost(entry)
		if err != nil {
			return nil, fmt.Errorf("whitelist %s: %w", path, err)
		}
		if canonical != entry {
			return nil, fmt.Errorf("whitelist %s: entry %q is not canonical, write it as %q", path, entry, canonical)
		}
		hosts[entry] = struct{}{}
	}
	return &Store{path: path, hosts: hosts}, nil
}

// Allowed reports whether host is in the set. host must already be canonical —
// the store performs no normalization of its own.
func (s *Store) Allowed(host string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.hosts[host]
	return ok
}

// Add records host as permanently allowed and saves the whitelist.
//
// host must already be canonical, exactly as Allowed expects it, or a saved
// approval would not match the next request.
//
// The set is only widened once the save succeeds. An approval that could not be
// written is not a persistent approval, so on failure the host is left out
// entirely rather than allowed in memory but absent from the file.
func (s *Store) Add(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.hosts[host]; ok {
		return nil
	}

	hosts := make([]string, 0, len(s.hosts)+1)
	for existing := range s.hosts {
		hosts = append(hosts, existing)
	}
	hosts = append(hosts, host)
	slices.Sort(hosts)

	if err := save(s.path, hosts); err != nil {
		return fmt.Errorf("saving whitelist %s: %w", s.path, err)
	}
	s.hosts[host] = struct{}{}
	return nil
}

// save writes hosts to path atomically: the data is written alongside, flushed,
// and renamed into place, so a reader only ever sees the whole old file or the
// whole new one — never a partially written whitelist that the next startup
// would refuse to parse.
//
// Saving replaces the file, so it carries the existing mode over rather than
// restyling permissions the operator chose. A whitelist created by a first
// approval is private.
func save(path string, hosts []string) error {
	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(temp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
