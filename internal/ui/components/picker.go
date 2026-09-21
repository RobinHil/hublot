package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Choice is one entry of a picker.
type Choice struct {
	// Label is what the user reads and types against.
	Label string
	// Detail is the dim explanation shown next to the label.
	Detail string
	// Payload travels back in ChosenMsg.
	Payload any
	// Destructive greys the entry out in a read-only session.
	Destructive bool
}

// ChosenMsg is emitted when an entry is picked.
type ChosenMsg struct {
	Payload any
}

// Picker is the searchable list behind the `x` action palette and the kill
// signal chooser. It is the escape valve for everything too rare to deserve a
// dedicated key (AGENTS.md section 9.2).
type Picker struct {
	Title string

	all      []Choice
	filtered []Choice
	cursor   int
	input    textinput.Model
	width    int
	height   int
}

// NewPicker builds a palette over the given choices.
func NewPicker(title string, choices []Choice) Picker {
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 60
	in.Focus()

	p := Picker{Title: title, all: choices, input: in}
	p.refilter()
	return p
}

// SetSize records the space available.
func (p *Picker) SetSize(width, height int) { p.width, p.height = width, height }

// Update handles typing and selection.
func (p *Picker) Update(msg tea.Msg, k keys.Map) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(km, k.Global.Escape):
		return dismiss
	case km.Type == tea.KeyEnter:
		if c, ok := p.Current(); ok {
			payload := c.Payload
			return func() tea.Msg { return ChosenMsg{Payload: payload} }
		}
		return nil
	case key.Matches(km, k.Global.Up):
		p.move(-1)
		return nil
	case key.Matches(km, k.Global.Down):
		p.move(1)
		return nil
	}

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refilter()
	return cmd
}

// Current is the highlighted choice.
func (p *Picker) Current() (Choice, bool) {
	if p.cursor < 0 || p.cursor >= len(p.filtered) {
		return Choice{}, false
	}
	return p.filtered[p.cursor], true
}

func (p *Picker) move(delta int) {
	p.cursor += delta
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// refilter narrows the list to entries matching every typed term.
func (p *Picker) refilter() {
	terms := strings.Fields(strings.ToLower(p.input.Value()))
	p.filtered = p.filtered[:0]

	for _, c := range p.all {
		hay := strings.ToLower(c.Label + " " + c.Detail)
		matched := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				matched = false
				break
			}
		}
		if matched {
			p.filtered = append(p.filtered, c)
		}
	}
	p.move(0)
}

// View renders the palette.
func (p *Picker) View() string {
	s := theme.Current()

	width := p.width - 8
	if width < 30 {
		width = 30
	}
	if width > 80 {
		width = 80
	}

	var b strings.Builder
	b.WriteString(s.Title.Render(p.Title))
	b.WriteString("\n")
	b.WriteString(p.input.View())
	b.WriteString("\n")

	if len(p.filtered) == 0 {
		b.WriteString(s.Dim.Render("no match"))
		return s.Modal.Width(width).Render(b.String())
	}

	max := p.height - 8
	if max < 3 {
		max = 3
	}
	if max > 12 {
		max = 12
	}

	// Keep the cursor inside the window without a second scroll offset.
	start := 0
	if p.cursor >= max {
		start = p.cursor - max + 1
	}
	end := start + max
	if end > len(p.filtered) {
		end = len(p.filtered)
	}

	for i := start; i < end; i++ {
		c := p.filtered[i]
		line := "  " + c.Label
		if c.Detail != "" {
			line += "  " + s.Dim.Render(c.Detail)
		}
		if i == p.cursor {
			line = s.Accent.Render("> " + c.Label)
			if c.Detail != "" {
				line += "  " + s.Dim.Render(c.Detail)
			}
		}
		b.WriteString("\n" + line)
	}

	if end < len(p.filtered) {
		b.WriteString("\n" + s.Dim.Render("  ..."))
	}

	return s.Modal.Width(width).Render(b.String())
}
