package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// These tests set HOME, which is process wide state, so they do not call
// t.Parallel().

func TestLoadWithoutFileReturnsDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, Defaults()) {
		t.Errorf("Load() = %+v, want %+v", got, Defaults())
	}
	if Exists() {
		t.Error("Exists() = true before anything was written, want false")
	}
}

func TestDefaultsPairKeepaliveWithReconnect(t *testing.T) {
	t.Parallel()

	var reconnect, interval, count bool
	for _, opt := range Defaults().Options {
		switch opt {
		case "reconnect":
			reconnect = true
		case "ServerAliveInterval=15":
			interval = true
		case "ServerAliveCountMax=3":
			count = true
		}
	}
	// reconnect without a keepalive leaves a dropped mount hanging rather than
	// reconnecting, so the three belong together or not at all.
	if reconnect && !(interval && count) {
		t.Error("defaults set reconnect without ServerAliveInterval and ServerAliveCountMax")
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, `{"mount_base": "~/mnt"}`)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.MountBase != "~/mnt" {
		t.Errorf("MountBase = %q, want ~/mnt", got.MountBase)
	}
	if !reflect.DeepEqual(got.Options, Defaults().Options) {
		t.Errorf("Options = %v, want the defaults %v", got.Options, Defaults().Options)
	}
	if got.SSHConfig != DefaultSSHConfig {
		t.Errorf("SSHConfig = %q, want %q", got.SSHConfig, DefaultSSHConfig)
	}
}

// TestLoadEmptyOptionsIsHonoured covers the difference between a field the file
// does not mention and one it sets to an empty list. The first takes the
// default, the second means no options at all.
func TestLoadEmptyOptionsIsHonoured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, `{"options": []}`)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Options) != 0 {
		t.Errorf("Options = %v, want an empty list", got.Options)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, "{not json")

	if _, err := Load(); err == nil {
		t.Error("Load() on malformed JSON = nil error, want an error")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := Defaults()
	want.MountBase = "~/mnt"
	want.Options = []string{"reconnect"}
	if err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !Exists() {
		t.Error("Exists() = false after Save(), want true")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestPathsExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	c := Defaults()
	if got, want := c.MountBaseDir(), filepath.Join(home, "sshfs"); got != want {
		t.Errorf("MountBaseDir() = %q, want %q", got, want)
	}
	if got, want := c.SSHConfigPath(), filepath.Join(home, ".ssh/config"); got != want {
		t.Errorf("SSHConfigPath() = %q, want %q", got, want)
	}
}

func TestDirCreatesWithOwnerOnlyPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("permissions = %o, want 700", perm)
	}
}

func writeConfig(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFilename), []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
}
