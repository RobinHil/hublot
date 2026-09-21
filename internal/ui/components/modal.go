package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Severity grades a confirmation, following the table in AGENTS.md section
// 10.1: reversible actions never get here at all.
type Severity int

const (
	// SevInfo just shows text, dismissed with any of the close keys.
	SevInfo Severity = iota
	// SevConfirm is a y/n dialog naming a single object.
	SevConfirm
	// SevDanger is a y/n dialog listing everything a batch action destroys.
	SevDanger
	// SevTyped requires typing a word, for the system-wide prune.
	SevTyped
	// SevError shows the daemon's own message verbatim.
	SevError
)

// ConfirmedMsg is emitted when the user accepts a modal. Payload is whatever
// the opening view attached, so the app knows what to run.
type ConfirmedMsg struct {
	Payload any
}

// DismissedMsg is emitted when a modal is closed without confirming.
type DismissedMsg struct{}

// Modal is a centred dialog. It states exactly what will happen, never
// "are you sure?" (AGENTS.md section 10.1).
type Modal struct {
	Severity Severity
	Title    string
	// Body is the explanation, one entry per line.
	Body []string
	// Word is what SevTyped requires the user to type.
	Word string
	// Payload travels back in ConfirmedMsg.
	Payload any

	input textinput.Model
	width int
}

// NewModal builds a dialog.
func NewModal(sev Severity, title string, body []string, payload any) Modal {
	m := Modal{Severity: sev, Title: title, Body: body, Payload: payload}
	if sev == SevTyped {
		m.Word = "delete"
		m.input = textinput.New()
		m.input.Prompt = "type " + m.Word + " to confirm: "
		m.input.CharLimit = 20
		m.input.Focus()
	}
	return m
}

// SetSize records the space available, so long bodies wrap sensibly.
func (m *Modal) SetSize(width, _ int) { m.width = width }

// Update handles the dialog keys, returning a command carrying the outcome.
func (m *Modal) Update(msg tea.Msg, k keys.Map) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	if m.Severity == SevTyped {
		switch {
		case key.Matches(km, k.Global.Escape):
			return dismiss
		case km.Type == tea.KeyEnter:
			if strings.TrimSpace(m.input.Value()) == m.Word {
				return m.confirm()
			}
			// A wrong word is not an error to explain: the prompt already says
			// what to type, and the dialog stays open.
			m.input.SetValue("")
			return nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}

	switch m.Severity {
	case SevInfo, SevError:
		// Any acknowledgement closes an informational dialog.
		if key.Matches(km, k.Modal.Confirm) || key.Matches(km, k.Modal.Cancel) ||
			key.Matches(km, k.Global.Quit) {
			return dismiss
		}
	default:
		switch {
		case key.Matches(km, k.Modal.Confirm):
			return m.confirm()
		case key.Matches(km, k.Modal.Cancel):
			return dismiss
		}
	}
	return nil
}

func (m *Modal) confirm() tea.Cmd {
	payload := m.Payload
	return func() tea.Msg { return ConfirmedMsg{Payload: payload} }
}

func dismiss() tea.Msg { return DismissedMsg{} }

// View renders the dialog, ready to be overlaid.
func (m *Modal) View() string {
	s := theme.Current()

	style := s.Modal
	if m.Severity == SevDanger || m.Severity == SevTyped || m.Severity == SevError {
		style = s.ModalDanger
	}

	inner := m.width - 8
	if inner < 20 {
		inner = 20
	}
	if inner > 100 {
		inner = 100
	}

	var b strings.Builder
	b.WriteString(s.Title.Render(m.Title))
	b.WriteString("\n\n")

	for _, line := range m.Body {
		b.WriteString(lipgloss.NewStyle().Width(inner).Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	switch m.Severity {
	case SevInfo, SevError:
		b.WriteString(s.Help.Render("esc: close"))
	case SevTyped:
		b.WriteString(m.input.View())
		b.WriteString("\n")
		b.WriteString(s.Help.Render("enter: confirm   esc: cancel"))
	default:
		b.WriteString(s.Key.Render("y") + s.Help.Render(": confirm   ") +
			s.Key.Render("n") + s.Help.Render(": cancel"))
	}

	return style.Width(inner).Render(b.String())
}

// Overlay centres a box over the background, which stays visible around it.
func Overlay(background, box string, width, height int) string {
	if width <= 0 || height <= 0 {
		return box
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}
