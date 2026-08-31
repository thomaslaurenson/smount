package tilde

import (
	"path/filepath"
	"testing"
)

func TestExpand(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

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
			t.Parallel()
			if got := Home(home).Expand(tc.input); got != tc.want {
				t.Errorf("Home.Expand(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCollapse(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

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
			t.Parallel()
			if got := Home(home).Collapse(tc.input); got != tc.want {
				t.Errorf("Home.Collapse(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestExpandCollapseRoundTrip(t *testing.T) {
	t.Parallel()
	home := Home(t.TempDir())

	for _, path := range []string{"~", "~/sshfs", "~/a/b"} {
		if got := home.Collapse(home.Expand(path)); got != path {
			t.Errorf("Home.Collapse(Home.Expand(%q)) = %q, want %q", path, got, path)
		}
	}
}
