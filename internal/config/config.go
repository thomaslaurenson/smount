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
func Defaults() *Config {
	return &Config{
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
func DirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// Dir returns smount's configuration directory, creating it if absent.
//
// Use this before any write.
func Dir() (string, error) {
	dir, err := DirPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Path returns the path to the configuration file.
func Path() (string, error) {
	dir, err := DirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFilename), nil
}

// Exists reports whether a configuration file has been written.
func Exists() bool {
	path, err := Path()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Load reads the configuration file, returning defaults when it is absent.
//
// Fields absent from the file keep their default, so a partial config file
// only overrides what it actually mentions.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Defaults(), nil
		}
		return nil, err
	}

	c := Defaults()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	c.applyDefaults()
	return c, nil
}

// Save writes the configuration file, creating the directory if needed.
func Save(c *Config) error {
	if _, err := Dir(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// MountBaseDir returns the mount base as an absolute path.
func (c *Config) MountBaseDir() string {
	return tilde.Expand(c.MountBase)
}

// SSHConfigPath returns the OpenSSH client config as an absolute path.
func (c *Config) SSHConfigPath() string {
	return tilde.Expand(c.SSHConfig)
}

// applyDefaults fills in any field the config file left empty.
func (c *Config) applyDefaults() {
	d := Defaults()
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
