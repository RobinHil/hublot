// Command hublot is a full-screen terminal UI for a local Docker daemon.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/config"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/ui"
)

// version is injected at build time with
// -ldflags "-X main.version=$(git describe --tags)".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hublot: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		showVersion = flag.Bool("version", false, "print the version and exit")
		readOnly    = flag.Bool("read-only", false, "disable every action that changes anything")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		// A broken config file is worth saying out loud, but it must not stop
		// hublot from opening with the defaults.
		fmt.Fprintln(os.Stderr, "hublot: "+err.Error())
	}
	if *readOnly {
		cfg.ReadOnly = true
	}

	// Signals cancel the root context, which is what every stream hangs off.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := docker.Connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	cli := compose.Detect(ctx)

	app := ui.New(ctx, client, cfg, cli, cfg.ReadOnly)

	program := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("running the interface: %w", err)
	}
	return nil
}

func usage() {
	_, _ = fmt.Fprintf(flag.CommandLine.Output(), `hublot %s - a terminal UI for a local Docker daemon

usage: hublot [flags]

flags:
  -read-only   disable every action that changes anything
  -version     print the version and exit

hublot talks to the local daemon socket only. Configuration, all of it
optional, lives in ~/.config/hublot/config.yaml.
`, version)
}
