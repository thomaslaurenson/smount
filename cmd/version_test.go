package cmd

import (
	"strings"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		injected string
		module   string
		want     string
	}{
		{
			name:     "an injected version wins",
			injected: "1.2.3", module: "v9.9.9", want: "1.2.3",
		},
		{
			name:     "go install falls back to the module version",
			injected: "dev", module: "v1.2.3", want: "1.2.3",
		},
		{
			name:     "a working tree build stays dev",
			injected: "dev", module: "(devel)", want: "dev",
		},
		{
			name:     "no module version stays dev",
			injected: "dev", module: "", want: "dev",
		},
		{
			name:     "a module version without a v prefix is kept as is",
			injected: "dev", module: "1.2.3", want: "1.2.3",
		},
		{
			name:     "a prerelease module version keeps its suffix",
			injected: "dev", module: "v1.2.3-rc.1", want: "1.2.3-rc.1",
		},
		{
			name:     "a pseudo-version is not a release, so it stays dev",
			injected: "dev", module: "v0.0.0-20260818120000-abcdef123456", want: "dev",
		},
		{
			name:     "a pseudo-version below a tag stays dev",
			injected: "dev", module: "v1.2.4-0.20260818120000-abcdef123456", want: "dev",
		},
		{
			name:     "a dirty working tree stays dev",
			injected: "dev", module: "v0.0.0-20260818120000-abcdef123456+dirty", want: "dev",
		},
		{
			name:     "build metadata on a tag is still not a plain release",
			injected: "dev", module: "v1.2.3+dirty", want: "dev",
		},
		{
			name:     "a pseudo-version above a prerelease tag stays dev",
			injected: "dev", module: "v1.2.3-rc.1.0.20260818120000-abcdef123456", want: "dev",
		},
		{
			name:     "a build number is not mistaken for a pseudo-version",
			injected: "dev", module: "v1.2.3-20260818120000", want: "1.2.3-20260818120000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := versionFrom(tc.injected, tc.module); got != tc.want {
				t.Errorf("versionFrom(%q, %q) = %q, want %q", tc.injected, tc.module, got, tc.want)
			}
		})
	}
}

// TestVersionCommand is the functional half: the version has to reach stdout,
// where something can read it back.
func TestVersionCommand(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, _, err := run(t, home, "version")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.HasPrefix(stdout, "smount version ") {
		t.Errorf("stdout = %q, want it to start with the binary name and version", stdout)
	}
	if !strings.Contains(stdout, Version) {
		t.Errorf("stdout = %q, want it to carry %q", stdout, Version)
	}
}

// version is the one command that overrides the root's persistent hook, so it
// is the command a flag checked in that hook would quietly accept. The check
// lives in the flag's own type instead, and this is the guard for that.
func TestVersionRejectsAnUnknownColourMode(t *testing.T) {
	t.Parallel()
	home := writeHome(t, "")

	stdout, _, err := run(t, home, "version", "--color", "beige")
	if err == nil {
		t.Fatal("--color beige was accepted by version, want an error")
	}
	if !strings.Contains(err.Error(), "--color") {
		t.Errorf("error = %v, want it to name the flag", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want no version printed on a failure", stdout)
	}
}
