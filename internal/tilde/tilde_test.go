package tilde

import (
	"path/filepath"
	"testing"
)

// The tilde package reads HOME, which is process wide state, so these tests do
// not call t.Parallel().

func TestExpand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "bare tilde", input: "~", want: home},
		{name: "tilde path", input: "~/sshfs", want: filepath.Join(home, "sshfs")},
		{name: "nested tilde path", input: "~/a/b/c", want: filepath.Join(home, "a/b/c")},
		{name: "absolute path unchanged", input: "/etc/hosts", want: "/etc/hosts"},
		{name: "relative path unchanged", input: "sshfs", want: "sshfs"},
		{name: "other user left alone", input: "~bob/x", want: "~bob/x"},
		{name: "tilde in the middle left alone", input: "/a/~/b", want: "/a/~/b"},
		{name: "empty", input: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Expand(tc.input); got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCollapse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "home itself", input: home, want: "~"},
		{name: "path under home", input: filepath.Join(home, "sshfs"), want: "~/sshfs"},
		{name: "path outside home", input: "/etc/hosts", want: "/etc/hosts"},
		{name: "prefix match is not a path match", input: home + "extra", want: home + "extra"},
		{name: "empty", input: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Collapse(tc.input); got != tc.want {
				t.Errorf("Collapse(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExpandCollapseRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, path := range []string{"~", "~/sshfs", "~/a/b"} {
		if got := Collapse(Expand(path)); got != path {
			t.Errorf("Collapse(Expand(%q)) = %q, want %q", path, got, path)
		}
	}
}
