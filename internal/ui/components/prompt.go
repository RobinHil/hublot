package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// AnsweredMsg carries what the user typed, with whatever the opening view
// attached so the app knows what to do with it.
type AnsweredMsg struct {
	Value   string
	Payload any
}

// Prompt asks for one value: a new name, an image reference, a path. The
// confirmation modals cannot do this, they only ever answer yes or no.
type Prompt struct {
	Title string
	// Body explains what the value is for, one entry per line.
	Body    []string
	Payload any

	input textinput.Model
	width int
}

// NewPrompt builds a single-line prompt.
func NewPrompt(title string, body []string, label, initial string, payload any) Prompt {
	in := textinput.New()
	in.Prompt = label
	in.CharLimit = 512
	in.SetValue(initial)
	in.CursorEnd()
	in.Focus()

	return Prompt{Title: title, Body: body, Payload: payload, input: in}
}

// SetSize records the space available.
func (p *Prompt) SetSize(width, _ int) { p.width = width }

// Update handles typing, submission and cancellation.
func (p *Prompt) Update(msg tea.Msg, k keys.Map) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(km, k.Global.Escape):
		return dismiss
	case km.Type == tea.KeyEnter:
		value := strings.TrimSpace(p.input.Value())
		if value == "" {
			// An empty answer is not an answer: the dialog stays open rather
			// than running an action with nothing in it.
			return nil
		}
		payload := p.Payload
		return func() tea.Msg { return AnsweredMsg{Value: value, Payload: payload} }
	}

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

// View renders the prompt.
func (p *Prompt) View() string {
	s := theme.Current()

	inner := p.width - 8
	if inner < 30 {
		inner = 30
	}
	if inner > 90 {
		inner = 90
	}

	var b strings.Builder
	b.WriteString(s.Title.Render(p.Title))
	b.WriteString("\n\n")
	for _, line := range p.Body {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(p.input.View())
	b.WriteString("\n\n")
	b.WriteString(s.Help.Render("enter: confirm   esc: cancel"))

	return s.Modal.Width(inner).Render(b.String())
}
