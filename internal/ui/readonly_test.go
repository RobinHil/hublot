package ui

import (
	"testing"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/views"
)

// Read-only holds at the point the change would happen, not only in the views
// that ask for it. A guard in each caller holds only as long as every caller
// remembers, and the dialog offering to fix a compose file was a caller that
// did not (AGENTS.md section 10.4).
func TestReadOnlyRefusesWhereTheChangeHappens(t *testing.T) {
	a := &App{store: &state.Store{ReadOnly: true}, taskOf: map[string]composeTask{}}

	if cmd := a.runCompose(views.ComposeRunRequest{
		Project: compose.Project{Name: "blog"},
		Args:    []string{"up", "-d"},
	}); cmd == nil {
		t.Fatal("a refusal has to say so")
	} else if _, ok := cmd().(views.ReadOnlyMsg); !ok {
		t.Error("compose runs are refused in read-only")
	}
	if len(a.taskOf) != 0 {
		t.Error("nothing was started, so nothing was recorded")
	}

	if cmd := a.openEditor(views.EditRequest{Path: "/tmp/compose.yml"}); cmd == nil {
		t.Fatal("a refusal has to say so")
	} else if _, ok := cmd().(views.ReadOnlyMsg); !ok {
		t.Error("editors are refused in read-only")
	}

	modal := components.NewModal(components.SevError, "up -d failed", nil, nil)
	a.offerFix(&modal, composeTask{
		project: compose.Project{Name: "blog", ConfigFiles: []string{"/etc/hostname"}},
		args:    []string{"up", "-d"},
	}, "validating /etc/hostname: services.web additional properties 'portz' not allowed")
	if modal.Action != "" {
		t.Errorf("read-only offers no way to edit the file, got %q", modal.Action)
	}
}
