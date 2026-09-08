// Package favourites stores named sshfs targets in ~/.smount/favourites.json.
package favourites

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/thomaslaurenson/smount/internal/config"
	"github.com/thomaslaurenson/smount/internal/tilde"
)

// Version is the schema version written to the favourites file.
const Version = 1

const filename = "favourites.json"

// Favourite is a saved mount target.
//
// Name, not Host, is what identifies it. Keying on the host is what limited the
// shell function this replaces to one mount per machine, because two entries
// for the same host had nothing to tell them apart or to derive distinct mount
// points from.
type Favourite struct {
	Name       string   `json:"name"`
	Host       string   `json:"host"`
	Path       string   `json:"path"`
	Mountpoint string   `json:"mountpoint,omitempty"`
	Options    []string `json:"options,omitempty"`
	ReadOnly   bool     `json:"readonly,omitempty"`
}

// Describe renders the favourite as a one line summary for a menu or a table.
//
// A favourite for a remote home directory has no path to show, so it says so
// rather than trailing a bare colon.
func (f Favourite) Describe() string {
	out := f.Host + ":" + f.Path
	if f.Path == "" {
		out = f.Host + ": (home)"
	}
	if f.ReadOnly {
		out += " (read only)"
	}
	return out
}

// Store is the on-disk collection of favourites.
type Store struct {
	Version    int         `json:"version"`
	Favourites []Favourite `json:"favourites"`
}

// Errors reported when looking up or changing favourites.
var (
	ErrNotFound    = errors.New("no such favourite")
	ErrExists      = errors.New("favourite already exists")
	ErrInvalidName = errors.New("invalid favourite name")
)

// Path returns the path to the favourites file.
func Path(home tilde.Home) string {
	return filepath.Join(config.DirPath(home), filename)
}

// Load reads the favourites file, returning an empty store when none exists.
func Load(home tilde.Home) (*Store, error) {
	path := Path(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Store{Version: Version}, nil
		}
		return nil, err
	}

	s := &Store{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if s.Version == 0 {
		s.Version = Version
	}
	return s, nil
}

// Save writes the favourites file, creating the directory if needed.
func Save(home tilde.Home, s *Store) error {
	if _, err := config.Dir(home); err != nil {
		return err
	}
	path := Path(home)
	s.Version = Version
	slices.SortFunc(s.Favourites, func(a, b Favourite) int {
		return cmp.Compare(a.Name, b.Name)
	})
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Get returns the favourite with the given name.
func (s *Store) Get(name string) (*Favourite, error) {
	for i := range s.Favourites {
		if s.Favourites[i].Name == name {
			return &s.Favourites[i], nil
		}
	}
	return nil, fmt.Errorf("%q: %w", name, ErrNotFound)
}

// Has reports whether a favourite with the given name exists.
func (s *Store) Has(name string) bool {
	_, err := s.Get(name)
	return err == nil
}

// Names returns every favourite name, sorted.
func (s *Store) Names() []string {
	out := make([]string, 0, len(s.Favourites))
	for _, f := range s.Favourites {
		out = append(out, f.Name)
	}
	slices.Sort(out)
	return out
}

// Add appends a favourite, rejecting a name that is already taken.
func (s *Store) Add(f Favourite) error {
	if err := ValidName(f.Name); err != nil {
		return err
	}
	if s.Has(f.Name) {
		return fmt.Errorf("%q: %w", f.Name, ErrExists)
	}
	s.Favourites = append(s.Favourites, f)
	return nil
}

// Remove deletes the favourite with the given name.
func (s *Store) Remove(name string) error {
	for i := range s.Favourites {
		if s.Favourites[i].Name == name {
			s.Favourites = slices.Delete(s.Favourites, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("%q: %w", name, ErrNotFound)
}

// ValidName reports whether name is usable as a favourite name.
//
// A favourite name is accepted as a bare argument to smount, so it has to be
// distinguishable from a host:path target and safe to use as the last element
// of a mount point path.
func ValidName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: name is empty", ErrInvalidName)
	case strings.ContainsAny(name, "/\\:"):
		return fmt.Errorf("%w: %q contains a path or host separator", ErrInvalidName, name)
	case strings.ContainsAny(name, " \t"):
		return fmt.Errorf("%w: %q contains whitespace", ErrInvalidName, name)
	case name == "." || name == "..":
		return fmt.Errorf("%w: %q is a directory reference", ErrInvalidName, name)
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("%w: %q would be read as a flag", ErrInvalidName, name)
	case !isASCII(name):
		return fmt.Errorf("%w: %q is not ASCII", ErrInvalidName, name)
	}
	return nil
}

// isASCII reports whether s is entirely ASCII.
//
// A favourite name is one the user invents, and it becomes a directory name
// under the mount base as well as a label in a menu that is measured in
// characters. Slug already produces nothing else, so this only constrains a
// name typed straight into "fav add".
//
// Host aliases are deliberately not held to this. They come from a config file
// smount only reads, and refusing one would hide a host ssh can reach.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// Slug converts free text into something ValidName accepts.
//
// It derives a favourite name from a label typed at a prompt.
func Slug(text string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// UniqueName returns base, or base with a numeric suffix when the store already
// holds base.
//
// The search always terminates: the suffix only produces names of the form
// "base-2", "base-3" and so on, and the store holds finitely many of them.
func (s *Store) UniqueName(base string) string {
	if !s.Has(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !s.Has(candidate) {
			return candidate
		}
	}
}
