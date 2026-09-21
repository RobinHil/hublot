package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// SubmittedMsg carries the filled form back, keyed by field name.
type SubmittedMsg struct {
	Values  map[string]string
	Payload any
}

// Field is one line of a form.
type Field struct {
	// Name is how the value comes back.
	Name string
	// Label is what the user reads.
	Label string
	// Hint shows under the field while it has the focus, for the syntax that
	// would otherwise have to be guessed.
	Hint string
	// Value is what the field starts with.
	Value string
	// Options, when set, make the field a choice cycled with left and right
	// rather than typed.
	Options []string
	// Required refuses an empty value on submit.
	Required bool
}

// Form is a short list of fields filled in place. It exists because creating a
// container needs more than one answer and less than a configuration file: a
// single prompt cannot ask for a name, ports and volumes at once, and a
// settings screen would be a different program.
type Form struct {
	Title    string
	Subtitle string
	Payload  any

	fields  []Field
	inputs  []textinput.Model
	choices []int
	focus   int
	err     string
	width   int
}

// NewForm builds a form over the given fields, with the first one focused.
func NewForm(title, subtitle string, fields []Field, payload any) Form {
	f := Form{Title: title, Subtitle: subtitle, Payload: payload, fields: fields}

	for i, field := range fields {
		in := textinput.New()
		in.Prompt = ""
		in.CharLimit = 512
		in.SetValue(field.Value)
		in.CursorEnd()
		f.inputs = append(f.inputs, in)

		choice := 0
		for c, option := range field.Options {
			if option == field.Value {
				choice = c
			}
		}
		f.choices = append(f.choices, choice)

		if i == 0 && len(field.Options) == 0 {
			f.inputs[0].Focus()
		}
	}
	return f
}

// SetSize records the space available.
func (f *Form) SetSize(width, _ int) { f.width = width }

// Update moves between fields, edits them, and submits.
func (f *Form) Update(msg tea.Msg, k keys.Map) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(km, k.Global.Escape):
		return dismiss

	case km.Type == tea.KeyTab, km.Type == tea.KeyDown:
		f.move(1)
		return nil

	case km.Type == tea.KeyShiftTab, km.Type == tea.KeyUp:
		f.move(-1)
		return nil

	case km.Type == tea.KeyEnter:
		return f.submit()

	case len(f.fields[f.focus].Options) > 0:
		// A choice field is cycled rather than typed, so the values it accepts
		// cannot be got wrong.
		switch km.Type {
		case tea.KeyLeft:
			f.cycle(-1)
			return nil
		case tea.KeyRight, tea.KeyRunes:
			f.cycle(1)
			return nil
		}
		return nil
	}

	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return cmd
}

func (f *Form) move(delta int) {
	f.inputs[f.focus].Blur()
	f.focus = (f.focus + delta + len(f.fields)) % len(f.fields)
	if len(f.fields[f.focus].Options) == 0 {
		f.inputs[f.focus].Focus()
	}
}

func (f *Form) cycle(delta int) {
	options := f.fields[f.focus].Options
	f.choices[f.focus] = (f.choices[f.focus] + delta + len(options)) % len(options)
}

// submit checks what is required and hands the values back.
func (f *Form) submit() tea.Cmd {
	values := map[string]string{}
	for i, field := range f.fields {
		value := strings.TrimSpace(f.inputs[i].Value())
		if len(field.Options) > 0 {
			value = field.Options[f.choices[i]]
		}
		if field.Required && value == "" {
			f.err = field.Label + " is required"
			f.focus = i
			f.inputs[i].Focus()
			return nil
		}
		values[field.Name] = value
	}

	payload := f.Payload
	return func() tea.Msg { return SubmittedMsg{Values: values, Payload: payload} }
}

// View renders the form.
func (f *Form) View() string {
	s := theme.Current()

	inner := f.width - 8
	if inner < 40 {
		inner = 40
	}
	if inner > 76 {
		inner = 76
	}

	labelWidth := 0
	for _, field := range f.fields {
		if len(field.Label) > labelWidth {
			labelWidth = len(field.Label)
		}
	}

	var b strings.Builder
	b.WriteString(s.Title.Render(f.Title))
	if f.Subtitle != "" {
		b.WriteString("\n" + s.Dim.Render(f.Subtitle))
	}
	b.WriteString("\n\n")

	for i, field := range f.fields {
		focused := i == f.focus

		label := pad(field.Label, labelWidth, false)
		if focused {
			label = s.Accent.Render(label)
		} else {
			label = s.Dim.Render(label)
		}

		var value string
		switch {
		case len(field.Options) > 0:
			value = renderChoice(field.Options, f.choices[i], focused)
		case focused:
			value = f.inputs[i].View()
		case f.inputs[i].Value() == "":
			value = s.Faint.Render("—")
		default:
			value = s.Text.Render(SanitizeWidth(f.inputs[i].Value(), inner-labelWidth-4))
		}

		marker := "  "
		if focused {
			marker = s.Accent.Render("▸ ")
		}
		b.WriteString(marker + label + "  " + value + "\n")

		if focused && field.Hint != "" {
			// Cut rather than wrapped: a hint that runs onto a second line
			// pushes the fields under it around as the focus moves.
			indent := labelWidth + 4
			b.WriteString(strings.Repeat(" ", indent) +
				s.Faint.Render(SanitizeWidth(field.Hint, inner-indent)) + "\n")
		}
	}

	if f.err != "" {
		b.WriteString("\n" + s.Danger.Render(f.err))
	}
	b.WriteString("\n" + s.Help.Render("tab: next field   enter: run   esc: cancel"))

	return s.Modal.Width(inner).Render(b.String())
}

// renderChoice draws the options with the picked one standing out, so a field
// with a fixed set of answers never has to be typed.
func renderChoice(options []string, picked int, focused bool) string {
	s := theme.Current()

	var parts []string
	for i, option := range options {
		switch {
		case i == picked && focused:
			parts = append(parts, s.RowSelected.Render(" "+option+" "))
		case i == picked:
			parts = append(parts, s.Accent.Render(" "+option+" "))
		default:
			parts = append(parts, s.Faint.Render(" "+option+" "))
		}
	}
	return strings.Join(parts, "")
}
