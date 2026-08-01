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

	"github.com/MrModest/askweb/internal/hostname"
)

// Store is a set of canonical hostnames backed by a flat JSON file.
type Store struct {
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
		return &Store{hosts: map[string]struct{}{}}, nil
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
	return &Store{hosts: hosts}, nil
}

// Allowed reports whether host is in the set. host must already be canonical —
// the store performs no normalization of its own.
func (s *Store) Allowed(host string) bool {
	_, ok := s.hosts[host]
	return ok
}
