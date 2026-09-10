package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/thomaslaurenson/smount/internal/tilde"
)

func TestLoadWithoutFileReturnsDefaults(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	got, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, Defaults(home)) {
		t.Errorf("Load() = %+v, want %+v", got, Defaults(home))
	}
	if Exists(home) {
		t.Error("Exists() = true before anything was written, want false")
	}
}

func TestDefaultsPairKeepaliveWithReconnect(t *testing.T) {
	t.Parallel()

	var reconnect, interval, count bool
	// The defaults do not depend on the home directory, only the paths do.
	for _, opt := range Defaults("").Options {
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
	t.Parallel()
	home := tilde.Home(t.TempDir())
	writeConfig(t, home, `{"mount_base": "~/mnt"}`)

	got, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.MountBase != "~/mnt" {
		t.Errorf("MountBase = %q, want ~/mnt", got.MountBase)
	}
	if !reflect.DeepEqual(got.Options, Defaults(home).Options) {
		t.Errorf("Options = %v, want the defaults %v", got.Options, Defaults(home).Options)
	}
	if got.SSHConfig != DefaultSSHConfig {
		t.Errorf("SSHConfig = %q, want %q", got.SSHConfig, DefaultSSHConfig)
	}
}

// TestLoadEmptyOptionsIsHonoured covers the difference between a field the file
// does not mention and one it sets to an empty list. The first takes the
// default, the second means no options at all.
func TestLoadEmptyOptionsIsHonoured(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())
	writeConfig(t, home, `{"options": []}`)

	got, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Options) != 0 {
		t.Errorf("Options = %v, want an empty list", got.Options)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())
	writeConfig(t, home, "{not json")

	if _, err := Load(home); err == nil {
		t.Error("Load() on malformed JSON = nil error, want an error")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	want := Defaults(home)
	want.MountBase = "~/mnt"
	want.Options = []string{"reconnect"}
	if err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !Exists(home) {
		t.Error("Exists() = false after Save(), want true")
	}

	got, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// Two smount processes starting at once in a fresh home both write the
// defaults out. Writing in place let one of them read the other's half-written
// file, so this is the guard for the write landing in one step.
func TestConcurrentSaveAndLoad(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	const writers = 8
	errs := make(chan error, writers*2)
	var wg sync.WaitGroup
	for range writers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			errs <- Save(Defaults(home))
		}()
		go func() {
			defer wg.Done()
			_, err := Load(home)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent save and load: %v", err)
		}
	}
}

func TestPathsExpandTilde(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	c := Defaults(home)
	if got, want := c.MountBaseDir(), filepath.Join(string(home), "sshfs"); got != want {
		t.Errorf("MountBaseDir() = %q, want %q", got, want)
	}
	if got, want := c.SSHConfigPath(), filepath.Join(string(home), ".ssh/config"); got != want {
		t.Errorf("SSHConfigPath() = %q, want %q", got, want)
	}
}

func TestDirCreatesWithOwnerOnlyPermissions(t *testing.T) {
	t.Parallel()
	home := tilde.Home(t.TempDir())

	dir, err := Dir(home)
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

func writeConfig(t *testing.T, home tilde.Home, content string) {
	t.Helper()
	dir := filepath.Join(string(home), DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFilename), []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
}
