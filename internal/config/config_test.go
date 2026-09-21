package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// write puts a config file where Load will look for it.
func write(t *testing.T, contents string) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, "hublot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hublot", "config.yaml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathFollowsXDGOnEveryPlatform(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/config")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/somewhere/config", "hublot", "config.yaml"); got != want {
		t.Errorf("path: got %q, want %q", got, want)
	}
}

func TestPathFallsBackToDotConfig(t *testing.T) {
	// Deliberately not os.UserConfigDir, which answers Library/Application
	// Support on macOS: the documented location is ~/.config on both.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")

	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/someone", ".config", "hublot", "config.yaml"); got != want {
		t.Errorf("path: got %q, want %q", got, want)
	}
}

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
	write(t, "theme: mono\nstop_timeout: 3s\nmax_stat_streams: 8\nread_only: true\nshell: [/bin/zsh]\n")

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
	write(t, "theme: [unclosed")

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

func TestSaveRoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := Default()
	cfg.Editor = "code --wait"
	cfg.Theme = "mono"

	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Saving is for remembering an answer, so what comes back has to be what
	// went in, not a file that needs hand-repair afterwards.
	back, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if back.Editor != "code --wait" || back.Theme != "mono" {
		t.Errorf("round trip: %+v", back)
	}
	if back.StopTimeout != cfg.StopTimeout || back.MaxStatStreams != cfg.MaxStatStreams {
		t.Errorf("the rest of the settings must survive: %+v", back)
	}

	path, _ := Path()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "# hublot configuration") {
		t.Errorf("the file should say what it is:\n%s", body)
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	// The first save is usually the first time the directory is needed.
	dir := filepath.Join(t.TempDir(), "nothing", "here")
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := Save(Default()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := Load(); err != nil {
		t.Errorf("load after save: %v", err)
	}
}
