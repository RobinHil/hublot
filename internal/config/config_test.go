package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a missing config file is not an error: %v", err)
	}
	if cfg.MaxStatStreams != 50 || cfg.StopTimeout != 10*time.Second {
		t.Errorf("defaults must be used: %+v", cfg)
	}
}

func TestLoadReadsUserValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "hublot")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "theme: mono\nstop_timeout: 3s\nmax_stat_streams: 8\nread_only: true\nshell: [/bin/zsh]\n"
	if err := os.WriteFile(filepath.Join(path, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Theme != "mono" || cfg.StopTimeout != 3*time.Second || cfg.MaxStatStreams != 8 {
		t.Errorf("values must be read: %+v", cfg)
	}
	if !cfg.ReadOnly || len(cfg.Shell) != 1 || cfg.Shell[0] != "/bin/zsh" {
		t.Errorf("values must be read: %+v", cfg)
	}
	// Unset fields keep their defaults.
	if cfg.LogTail != 500 {
		t.Errorf("log_tail default: got %d", cfg.LogTail)
	}
}

func TestLoadRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "hublot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hublot", "config.yaml"), []byte("theme: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A broken file is reported rather than ignored: settings that do nothing
	// are worse than an error at startup.
	if _, err := Load(); err == nil {
		t.Fatal("a malformed config file must be an error")
	}
}

func TestNormalisedRepairsNonsense(t *testing.T) {
	cfg := Config{StopTimeout: -1, MaxStatStreams: 0, LogTail: -5}.normalised()
	d := Default()
	if cfg.StopTimeout != d.StopTimeout || cfg.MaxStatStreams != d.MaxStatStreams || cfg.LogTail != d.LogTail {
		t.Errorf("nonsense values fall back to defaults: %+v", cfg)
	}
}
