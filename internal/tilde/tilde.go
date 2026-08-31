// Package tilde converts between absolute paths and their ~ abbreviated form.
package tilde

import (
	"path/filepath"
	"strings"
)

// Home is a resolved home directory.
//
// It is a value rather than a lookup so that the home directory is read once,
// at the wiring boundary in cmd, and everything below it takes the answer as an
// argument. An empty Home leaves every path alone, which is what a process with
// no home directory should see.
type Home string

// Expand replaces a leading ~ or ~/ in path with the home directory.
//
// Paths that do not begin with ~ are returned unchanged, as is the ~user form:
// resolving another user's home needs a passwd lookup that smount has no
// reason to make, and silently treating ~bob as a literal directory is less
// surprising than guessing.
func (h Home) Expand(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	if h == "" {
		return path
	}
	if path == "~" {
		return string(h)
	}
	return filepath.Join(string(h), path[2:])
}

// Collapse replaces a leading home directory in path with ~.
//
// This makes the paths smount prints read the same way as the ones written in
// its config file.
func (h Home) Collapse(path string) string {
	if h == "" {
		return path
	}
	if path == string(h) {
		return "~"
	}
	if strings.HasPrefix(path, string(h)+string(filepath.Separator)) {
		return "~" + path[len(h):]
	}
	return path
}
