// Package tilde converts between absolute paths and their ~ abbreviated form.
package tilde

import (
	"os"
	"path/filepath"
	"strings"
)

// Expand replaces a leading ~ or ~/ in path with the user's home directory.
//
// Paths that do not begin with ~ are returned unchanged, as is the ~user form:
// resolving another user's home needs a passwd lookup that smount has no
// reason to make, and silently treating ~bob as a literal directory is less
// surprising than guessing.
func Expand(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// Collapse replaces a leading home directory in path with ~.
//
// This makes the paths smount prints read the same way as the ones written in
// its config file.
func Collapse(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}
