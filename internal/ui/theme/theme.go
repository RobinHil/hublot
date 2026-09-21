// Package theme holds every colour and style used by the UI. It is the one
// place in the codebase allowed a package-level variable
// (AGENTS.md section 14).
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the set of colours a theme defines. Terminal-default adaptive
// colours are used so hublot looks right on light and dark backgrounds alike.
type Palette struct {
	Text      lipgloss.AdaptiveColor
	Dim       lipgloss.AdaptiveColor
	Accent    lipgloss.AdaptiveColor
	Running   lipgloss.AdaptiveColor
	Stopped   lipgloss.AdaptiveColor
	Warning   lipgloss.AdaptiveColor
	Danger    lipgloss.AdaptiveColor
	Selection lipgloss.AdaptiveColor
	Border    lipgloss.AdaptiveColor
	Surface   lipgloss.AdaptiveColor
}

// Styles are the derived lipgloss styles the views render with.
type Styles struct {
	Palette Palette

	Tab         lipgloss.Style
	TabActive   lipgloss.Style
	TabBar      lipgloss.Style
	Header      lipgloss.Style
	Row         lipgloss.Style
	RowSelected lipgloss.Style
	RowMarked   lipgloss.Style
	Dim         lipgloss.Style
	Accent      lipgloss.Style
	Running     lipgloss.Style
	Stopped     lipgloss.Style
	Warning     lipgloss.Style
	Danger      lipgloss.Style
	Badge       lipgloss.Style
	BadgeDanger lipgloss.Style
	StatusBar   lipgloss.Style
	Modal       lipgloss.Style
	ModalDanger lipgloss.Style
	Title       lipgloss.Style
	Key         lipgloss.Style
	Help        lipgloss.Style
}

// current is the active theme. Views read it through Current.
var current = New(Default())

// Default is the palette used when the config names no other.
func Default() Palette {
	return Palette{
		Text:      lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#e6edf3"},
		Dim:       lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#8b949e"},
		Accent:    lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"},
		Running:   lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"},
		Stopped:   lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#6e7681"},
		Warning:   lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"},
		Danger:    lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"},
		Selection: lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f3a5f"},
		Border:    lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#30363d"},
		Surface:   lipgloss.AdaptiveColor{Light: "#f6f8fa", Dark: "#161b22"},
	}
}

// Mono is a palette for terminals where colour is unwanted or unreadable.
func Mono() Palette {
	white := lipgloss.AdaptiveColor{Light: "#000000", Dark: "#ffffff"}
	grey := lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}
	return Palette{
		Text: white, Dim: grey, Accent: white, Running: white, Stopped: grey,
		Warning: white, Danger: white,
		Selection: lipgloss.AdaptiveColor{Light: "#dddddd", Dark: "#333333"},
		Border:    grey,
		Surface:   lipgloss.AdaptiveColor{Light: "#eeeeee", Dark: "#222222"},
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
		Palette:     p,
		Tab:         lipgloss.NewStyle().Foreground(p.Dim).Padding(0, 1),
		TabActive:   lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Underline(true).Padding(0, 1),
		TabBar:      lipgloss.NewStyle().Foreground(p.Dim),
		Header:      lipgloss.NewStyle().Foreground(p.Dim).Bold(true),
		Row:         lipgloss.NewStyle().Foreground(p.Text),
		RowSelected: lipgloss.NewStyle().Foreground(p.Text).Background(p.Selection).Bold(true),
		RowMarked:   lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Dim:         lipgloss.NewStyle().Foreground(p.Dim),
		Accent:      lipgloss.NewStyle().Foreground(p.Accent),
		Running:     lipgloss.NewStyle().Foreground(p.Running),
		Stopped:     lipgloss.NewStyle().Foreground(p.Stopped),
		Warning:     lipgloss.NewStyle().Foreground(p.Warning),
		Danger:      lipgloss.NewStyle().Foreground(p.Danger),
		Badge:       lipgloss.NewStyle().Foreground(p.Text).Background(p.Surface).Padding(0, 1),
		BadgeDanger: lipgloss.NewStyle().Foreground(p.Danger).Bold(true).Padding(0, 1),
		StatusBar:   lipgloss.NewStyle().Foreground(p.Dim),
		Modal: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Accent).
			Padding(1, 2),
		ModalDanger: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Danger).
			Padding(1, 2),
		Title: lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Key:   lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Help:  lipgloss.NewStyle().Foreground(p.Dim),
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
	case "paused", "restarting":
		return s.Warning
	case "dead":
		return s.Danger
	default:
		return s.Stopped
	}
}
