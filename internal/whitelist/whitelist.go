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

// Load reads a JSON array of canonical hostnames from path.
//
// A missing file is an empty whitelist, so a first run needs no setup. Every
// entry must already be canonical: since Allowed does not normalize, a
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
