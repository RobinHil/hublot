// Package config reads the optional user configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is everything the user may set in ~/.config/hublot/config.yaml.
// Every field has a working default, so a missing file is not an error.
type Config struct {
	// Theme names a palette: "default" or "mono".
	Theme string `yaml:"theme"`
	// StopTimeout is how long a stop waits before the daemon sends SIGKILL.
	StopTimeout time.Duration `yaml:"stop_timeout"`
	// MaxStatStreams caps concurrent stats streams. Above it, only containers
	// visible in the viewport are streamed (AGENTS.md section 6.3).
	MaxStatStreams int `yaml:"max_stat_streams"`
	// LogTail is how many past lines the log viewer opens with.
	LogTail int `yaml:"log_tail"`
	// Shell is the command an exec session runs. Empty means the default
	// probe, which prefers bash and falls back to sh.
	Shell []string `yaml:"shell"`
	// ReadOnly starts the session with every mutating action disabled. The
	// --read-only flag sets the same thing.
	ReadOnly bool `yaml:"read_only"`
	// DefaultSort names the starting sort column of the containers view.
	DefaultSort string `yaml:"default_sort"`
	// SortDescending starts that column in descending order.
	SortDescending bool `yaml:"sort_descending"`
	// ConfirmDestructive can be turned off by users who want fewer dialogs;
	// the system-wide prune always confirms regardless.
	ConfirmDestructive bool `yaml:"confirm_destructive"`
}

// Default is the configuration used when no file exists.
func Default() Config {
	return Config{
		Theme:              "default",
		StopTimeout:        10 * time.Second,
		MaxStatStreams:     50,
		LogTail:            500,
		DefaultSort:        "name",
		ConfirmDestructive: true,
	}
}

// Path is where the configuration file is looked for.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(dir, "hublot", "config.yaml"), nil
}

// Load reads the configuration, falling back to defaults when the file does
// not exist. A malformed file is an error: silently ignoring it would leave the
// user wondering why their settings do nothing.
func Load() (Config, error) {
	cfg := Default()

	path, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parsing %s: %w", path, err)
	}

	return cfg.normalised(), nil
}

// normalised replaces nonsensical values with the defaults rather than letting
// them reach the UI.
func (c Config) normalised() Config {
	d := Default()
	if c.StopTimeout <= 0 {
		c.StopTimeout = d.StopTimeout
	}
	if c.MaxStatStreams <= 0 {
		c.MaxStatStreams = d.MaxStatStreams
	}
	if c.LogTail <= 0 {
		c.LogTail = d.LogTail
	}
	if c.Theme == "" {
		c.Theme = d.Theme
	}
	if c.DefaultSort == "" {
		c.DefaultSort = d.DefaultSort
	}
	return c
}
