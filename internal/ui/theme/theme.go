// Package theme holds every colour and style used by the UI. It is the one
// place in the codebase allowed a package-level variable
// (AGENTS.md section 15).
//
// The palette has a job to do rather than a mood: chrome recedes, data reads
// first, and colour only ever means something. Green is healthy, amber is
// attention, red is broken or destructive, blue is where you are. Everything
// else is one of three greys.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the set of colours a theme defines. Adaptive so a light terminal
// gets ink on paper rather than the same values washed out.
type Palette struct {
	// Text is the foreground for data that matters.
	Text lipgloss.AdaptiveColor
	// Muted is for secondary data: units, ages, counts.
	Muted lipgloss.AdaptiveColor
	// Faint is for chrome: rules, inactive tabs, placeholders.
	Faint lipgloss.AdaptiveColor

	Accent    lipgloss.AdaptiveColor
	AccentDim lipgloss.AdaptiveColor

	Running lipgloss.AdaptiveColor
	Warning lipgloss.AdaptiveColor
	Danger  lipgloss.AdaptiveColor

	// Selection is the background of the row under the cursor.
	Selection lipgloss.AdaptiveColor
	// SelectionText keeps that row readable on it.
	SelectionText lipgloss.AdaptiveColor
	// Marked tints rows picked for a batch action.
	Marked lipgloss.AdaptiveColor

	Border    lipgloss.AdaptiveColor
	BorderLit lipgloss.AdaptiveColor
	Surface   lipgloss.AdaptiveColor

	// Gauge is the three-stop ramp meters and sparklines climb.
	GaugeLow  lipgloss.AdaptiveColor
	GaugeMid  lipgloss.AdaptiveColor
	GaugeHigh lipgloss.AdaptiveColor
}

// Styles are the derived lipgloss styles the views render with.
type Styles struct {
	Palette Palette

	Tab       lipgloss.Style
	TabActive lipgloss.Style
	TabKey    lipgloss.Style

	Header      lipgloss.Style
	HeaderSort  lipgloss.Style
	Row         lipgloss.Style
	RowSelected lipgloss.Style
	RowMarked   lipgloss.Style

	Text    lipgloss.Style
	Dim     lipgloss.Style
	Faint   lipgloss.Style
	Accent  lipgloss.Style
	Running lipgloss.Style
	Warning lipgloss.Style
	Danger  lipgloss.Style

	Badge       lipgloss.Style
	BadgeDanger lipgloss.Style
	BadgeGood   lipgloss.Style

	Panel        lipgloss.Style
	PanelFocused lipgloss.Style
	PanelTitle   lipgloss.Style

	Modal       lipgloss.Style
	ModalDanger lipgloss.Style
	Title       lipgloss.Style
	Key         lipgloss.Style
	Help        lipgloss.Style
	Rule        lipgloss.Style
}

var current = New(Default())

// Default is the palette used when the config names no other.
func Default() Palette {
	return Palette{
		Text:          lipgloss.AdaptiveColor{Light: "#12161b", Dark: "#e7eef6"},
		Muted:         lipgloss.AdaptiveColor{Light: "#5b6572", Dark: "#9aa6b4"},
		Faint:         lipgloss.AdaptiveColor{Light: "#98a2b0", Dark: "#5c6675"},
		Accent:        lipgloss.AdaptiveColor{Light: "#0b66d0", Dark: "#63b3ff"},
		AccentDim:     lipgloss.AdaptiveColor{Light: "#3d87e0", Dark: "#3c6ea5"},
		Running:       lipgloss.AdaptiveColor{Light: "#127a3a", Dark: "#49c96d"},
		Warning:       lipgloss.AdaptiveColor{Light: "#9a6207", Dark: "#e3a92a"},
		Danger:        lipgloss.AdaptiveColor{Light: "#c62b30", Dark: "#ff6b63"},
		Selection:     lipgloss.AdaptiveColor{Light: "#d7e7fb", Dark: "#1d3a5f"},
		SelectionText: lipgloss.AdaptiveColor{Light: "#0a1420", Dark: "#f2f7fd"},
		Marked:        lipgloss.AdaptiveColor{Light: "#eaf2ff", Dark: "#16233a"},
		Border:        lipgloss.AdaptiveColor{Light: "#d3dae3", Dark: "#2a323d"},
		BorderLit:     lipgloss.AdaptiveColor{Light: "#0b66d0", Dark: "#3c6ea5"},
		Surface:       lipgloss.AdaptiveColor{Light: "#f2f5f9", Dark: "#141a22"},
		GaugeLow:      lipgloss.AdaptiveColor{Light: "#127a3a", Dark: "#49c96d"},
		GaugeMid:      lipgloss.AdaptiveColor{Light: "#9a6207", Dark: "#e3a92a"},
		GaugeHigh:     lipgloss.AdaptiveColor{Light: "#c62b30", Dark: "#ff6b63"},
	}
}

// Mono is for terminals where colour is unwanted or unreadable.
func Mono() Palette {
	ink := lipgloss.AdaptiveColor{Light: "#000000", Dark: "#ffffff"}
	mid := lipgloss.AdaptiveColor{Light: "#555555", Dark: "#aaaaaa"}
	low := lipgloss.AdaptiveColor{Light: "#888888", Dark: "#777777"}
	sel := lipgloss.AdaptiveColor{Light: "#dddddd", Dark: "#333333"}

	return Palette{
		Text: ink, Muted: mid, Faint: low,
		Accent: ink, AccentDim: mid,
		Running: ink, Warning: mid, Danger: ink,
		Selection: sel, SelectionText: ink, Marked: sel,
		Border: low, BorderLit: mid, Surface: sel,
		GaugeLow: low, GaugeMid: mid, GaugeHigh: ink,
	}
}

// ByName returns a palette by config name, falling back to the default.
func ByName(name string) Palette {
	if name == "mono" {
		return Mono()
	}
	return Default()
}

// New derives the styles of a palette.
func New(p Palette) Styles {
	return Styles{
		Palette: p,

		Tab:       lipgloss.NewStyle().Foreground(p.Faint).Padding(0, 1),
		TabActive: lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Padding(0, 1),
		TabKey:    lipgloss.NewStyle().Foreground(p.AccentDim),

		Header:      lipgloss.NewStyle().Foreground(p.Muted).Bold(true),
		HeaderSort:  lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Row:         lipgloss.NewStyle().Foreground(p.Text),
		RowSelected: lipgloss.NewStyle().Foreground(p.SelectionText).Background(p.Selection),
		RowMarked:   lipgloss.NewStyle().Foreground(p.Text).Background(p.Marked),

		Text:    lipgloss.NewStyle().Foreground(p.Text),
		Dim:     lipgloss.NewStyle().Foreground(p.Muted),
		Faint:   lipgloss.NewStyle().Foreground(p.Faint),
		Accent:  lipgloss.NewStyle().Foreground(p.Accent),
		Running: lipgloss.NewStyle().Foreground(p.Running),
		Warning: lipgloss.NewStyle().Foreground(p.Warning),
		Danger:  lipgloss.NewStyle().Foreground(p.Danger),

		Badge:       lipgloss.NewStyle().Foreground(p.Muted).Background(p.Surface).Padding(0, 1),
		BadgeDanger: lipgloss.NewStyle().Foreground(p.Danger).Bold(true).Padding(0, 1),
		BadgeGood:   lipgloss.NewStyle().Foreground(p.Running).Padding(0, 1),

		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(p.Border),
		PanelFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(p.BorderLit),
		PanelTitle: lipgloss.NewStyle().Foreground(p.Accent).Bold(true),

		Modal: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.BorderLit).
			Padding(1, 2),
		ModalDanger: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Danger).
			Padding(1, 2),
		Title: lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Key:   lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Help:  lipgloss.NewStyle().Foreground(p.Muted),
		Rule:  lipgloss.NewStyle().Foreground(p.Border),
	}
}

// Current returns the active styles.
func Current() Styles { return current }

// Set swaps the active theme, called once at startup from the config.
func Set(p Palette) { current = New(p) }

// StateStyle picks the colour a container state is rendered in.
func StateStyle(state string) lipgloss.Style {
	s := Current()
	switch state {
	case "running":
		return s.Running
	case "paused", "restarting", "created":
		return s.Warning
	case "dead":
		return s.Danger
	default:
		return s.Faint
	}
}

// LevelStyle colours a figure by how loaded it is: quiet reads calm, busy
// reads hot, so a glance at a column is enough.
func LevelStyle(percent float64) lipgloss.Style {
	s := Current()
	switch {
	case percent >= 80:
		return lipgloss.NewStyle().Foreground(s.Palette.GaugeHigh)
	case percent >= 40:
		return lipgloss.NewStyle().Foreground(s.Palette.GaugeMid)
	default:
		return lipgloss.NewStyle().Foreground(s.Palette.GaugeLow)
	}
}
