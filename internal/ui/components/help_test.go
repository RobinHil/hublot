package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/RobinHil/hublot/internal/ui/keys"
)

// A tab turned off takes its keys out of the help: listing bindings for a
// view that is not there documents nothing.
func TestHelpOverlayLeavesOutHiddenTabs(t *testing.T) {
	out, _ := HelpOverlay(keys.Default(), false, map[string]bool{"compose": true}, 120, 400, 0)
	text := ansi.Strip(out)
	if strings.Contains(text, "compose, where") {
		t.Error("the compose section is still there")
	}
	if !strings.Contains(text, "swarm, where") {
		t.Error("the swarm section went with it")
	}
}
