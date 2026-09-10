// Package config loads and persists smount's settings from ~/.smount/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thomaslaurenson/smount/internal/tilde"
)

const (
	// DirName is smount's configuration directory, relative to the home directory.
	DirName = ".smount"

	// DefaultMountBase is the directory that ad hoc mounts are created under.
	DefaultMountBase = "~/sshfs"

	// DefaultSSHConfig is the OpenSSH client config that host discovery starts from.
	DefaultSSHConfig = "~/.ssh/config"

	configFilename = "config.json"
)

// Config holds the resolved settings for an smount invocation.
type Config struct {
	MountBase string   `json:"mount_base"`
	Options   []string `json:"options"`
	SSHConfig string   `json:"ssh_config"`

	// Home is the directory the ~ paths above resolve against.
	//
	// It is not part of the config file, which is why it is not marshalled: it
	// is a fact about the process, settled in cmd and carried here so that no
	// package below has to read the environment to resolve a path.
	Home tilde.Home `json:"-"`
}

// Defaults returns the settings smount uses when no config file is present.
//
// compression is deliberately absent, unlike the shell function this replaces.
// sshfs compresses only when asked, and above roughly 10 Mbit the CPU cost
// outweighs the bytes saved, so it slows down the common case to help the rare
// one. Add "compression=yes" to options for a slow or metered link.
//
// ServerAliveInterval and ServerAliveCountMax are not optional extras next to
// reconnect: without them ssh never notices a dropped link, so reconnect has no
// failure to react to and the mount hangs instead of recovering.
func Defaults(home tilde.Home) *Config {
	return &Config{
		Home:      home,
		MountBase: DefaultMountBase,
		Options: []string{
			"reconnect",
			"ServerAliveInterval=15",
			"ServerAliveCountMax=3",
			"follow_symlinks",
			"idmap=user",
		},
		SSHConfig: DefaultSSHConfig,
	}
}

// DirPath returns the path to smount's configuration directory.
//
// It does not create the directory, so use this for read-only lookups.
func DirPath(home tilde.Home) string {
	return filepath.Join(string(home), DirName)
}

// Dir returns smount's configuration directory, creating it if absent.
//
// Use this before any write.
func Dir(home tilde.Home) (string, error) {
	dir := DirPath(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Path returns the path to the configuration file.
func Path(home tilde.Home) string {
	return filepath.Join(DirPath(home), configFilename)
}

// Exists reports whether a configuration file has been written.
func Exists(home tilde.Home) bool {
	_, err := os.Stat(Path(home))
	return err == nil
}

// Load reads the configuration file, returning defaults when it is absent.
//
// Fields absent from the file keep their default, so a partial config file
// only overrides what it actually mentions.
func Load(home tilde.Home) (*Config, error) {
	path := Path(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Defaults(home), nil
		}
		return nil, err
	}

	c := Defaults(home)
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	c.applyDefaults()
	return c, nil
}

// Save writes the configuration file, creating the directory if needed.
//
// The file is written under a temporary name and renamed into place, which is
// one step as far as any reader is concerned. Two smount processes starting at
// once in a fresh home both write the defaults, and writing in place would let
// one of them read the other's half-written file and fail to parse it.
func Save(c *Config) error {
	dir, err := Dir(c.Home)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, configFilename+".*")
	if err != nil {
		return err
	}
	// Removed on every path that does not rename it away, so a write that
	// failed part way leaves nothing behind for the next run to trip over.
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	// Closed before the rename rather than deferred: a buffered write can still
	// fail here, and renaming first would publish a file that never landed.
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), Path(c.Home))
}

// MountBaseDir returns the mount base as an absolute path.
func (c *Config) MountBaseDir() string {
	return c.Home.Expand(c.MountBase)
}

// SSHConfigPath returns the OpenSSH client config as an absolute path.
func (c *Config) SSHConfigPath() string {
	return c.Home.Expand(c.SSHConfig)
}

// applyDefaults fills in any field the config file left empty.
func (c *Config) applyDefaults() {
	d := Defaults(c.Home)
	if c.MountBase == "" {
		c.MountBase = d.MountBase
	}
	if c.SSHConfig == "" {
		c.SSHConfig = d.SSHConfig
	}
	if c.Options == nil {
		c.Options = d.Options
	}
}
