package views

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// maxLogLines bounds the buffer: a chatty container must not grow the model
// without limit over a long session.
const maxLogLines = 5000

// serviceColours are cycled by a stable hash of the service name, so a service
// keeps its colour across sessions (AGENTS.md section 8.6).
var serviceColours = []lipgloss.AdaptiveColor{
	{Light: "#0969da", Dark: "#58a6ff"},
	{Light: "#1a7f37", Dark: "#3fb950"},
	{Light: "#9a6700", Dark: "#d29922"},
	{Light: "#8250df", Dark: "#bc8cff"},
	{Light: "#bf3989", Dark: "#f778ba"},
	{Light: "#136d75", Dark: "#39c5cf"},
}

// Logs is the full-screen log viewer, used for one container and for the
// aggregated logs of a whole Compose project.
type Logs struct {
	keys   keys.Map
	title  string
	lines  []docker.LogLine
	follow bool
	wrap   bool
	stamps bool

	viewport  viewport.Model
	search    textinput.Model
	searching bool
	query     string
	match     int

	width  int
	height int
}

// NewLogs builds an empty viewer.
func NewLogs(k keys.Map) *Logs {
	in := textinput.New()
	in.Prompt = "search: "
	in.CharLimit = 80

	return &Logs{keys: k, follow: true, wrap: true, viewport: viewport.New(80, 20), search: in}
}

// Open resets the viewer for a new source.
func (l *Logs) Open(title string) {
	l.title = title
	l.lines = nil
	l.follow = true
	l.query = ""
	l.match = -1
	l.search.SetValue("")
	l.viewport.SetContent("")
}

// Title is what the header shows.
func (l *Logs) Title() string { return l.title }

// Append adds a line, trimming the buffer when it grows past the cap.
func (l *Logs) Append(line docker.LogLine) {
	l.lines = append(l.lines, line)
	if len(l.lines) > maxLogLines {
		l.lines = l.lines[len(l.lines)-maxLogLines:]
	}
	l.render()
}

// SetSize records the drawing area.
func (l *Logs) SetSize(width, height int) {
	l.width, l.height = width, height
	l.viewport.Width = width
	// One line for the title, the rest is output.
	l.viewport.Height = height - 1
	if l.viewport.Height < 3 {
		l.viewport.Height = 3
	}
	l.render()
}

// Update handles the viewer keys, reporting whether the viewer should close.
func (l *Logs) Update(msg tea.Msg) (tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		l.viewport, cmd = l.viewport.Update(msg)
		return cmd, false
	}

	if l.searching {
		switch {
		case key.Matches(km, l.keys.Global.Escape):
			l.searching = false
			l.search.Blur()
			return nil, false
		case km.Type == tea.KeyEnter:
			l.searching = false
			l.search.Blur()
			l.query = l.search.Value()
			l.jumpToMatch(1)
			return nil, false
		}
		var cmd tea.Cmd
		l.search, cmd = l.search.Update(msg)
		return cmd, false
	}

	k := l.keys.Logs
	switch {
	case key.Matches(km, k.Close):
		return nil, true
	case key.Matches(km, k.Search):
		l.searching = true
		l.search.Focus()
		return textinput.Blink, false
	case key.Matches(km, k.Next):
		l.jumpToMatch(1)
		return nil, false
	case key.Matches(km, k.Prev):
		l.jumpToMatch(-1)
		return nil, false
	case key.Matches(km, k.Follow):
		l.follow = !l.follow
		if l.follow {
			l.viewport.GotoBottom()
		}
		return nil, false
	case key.Matches(km, k.Wrap):
		l.wrap = !l.wrap
		l.render()
		return nil, false
	case key.Matches(km, k.Timestamp):
		l.stamps = !l.stamps
		l.render()
		return nil, false
	}

	// Scrolling by hand means the user wants to read, not to follow.
	before := l.viewport.YOffset
	var cmd tea.Cmd
	l.viewport, cmd = l.viewport.Update(msg)
	if l.viewport.YOffset != before && !l.viewport.AtBottom() {
		l.follow = false
	}
	return cmd, false
}

// jumpToMatch moves to the next or previous line containing the query.
func (l *Logs) jumpToMatch(delta int) {
	if l.query == "" {
		return
	}

	needle := strings.ToLower(l.query)
	n := len(l.lines)
	if n == 0 {
		return
	}

	start := l.match
	for i := 1; i <= n; i++ {
		idx := (start + delta*i%n + n) % n
		if strings.Contains(strings.ToLower(l.lines[idx].Text), needle) {
			l.match = idx
			l.follow = false
			l.viewport.SetYOffset(idx)
			return
		}
	}
}

// render rebuilds the viewport content.
func (l *Logs) render() {
	s := theme.Current()

	var b strings.Builder
	for i, line := range l.lines {
		if i > 0 {
			b.WriteString("\n")
		}

		var prefix string
		if line.Service != "" {
			prefix = serviceStyle(line.Service).Render(line.Service) + s.Dim.Render(" | ")
		}
		if l.stamps && !line.At.IsZero() {
			prefix += s.Dim.Render(line.At.Format("15:04:05") + " ")
		}

		text := line.Text
		if line.Stream == "stderr" {
			text = s.Danger.Render(text)
		}
		if l.query != "" && strings.Contains(strings.ToLower(text), strings.ToLower(l.query)) {
			text = s.Warning.Render(line.Text)
		}

		body := prefix + text
		if !l.wrap {
			body = truncateLine(body, l.width)
		}
		b.WriteString(body)
	}

	l.viewport.SetContent(b.String())
	if l.follow {
		l.viewport.GotoBottom()
	}
}

// truncateLine cuts a rendered log line to the terminal width. Log lines carry
// the service colour and the stderr styling, so the cut counts display width
// and keeps escape sequences whole.
func truncateLine(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}

// serviceStyle picks a stable colour from the service name, so the same service
// always reads the same way in an aggregated stream.
func serviceStyle(service string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(service))
	colour := serviceColours[int(h.Sum32())%len(serviceColours)]
	return lipgloss.NewStyle().Foreground(colour).Bold(true)
}

// View renders the viewer.
func (l *Logs) View() string {
	s := theme.Current()

	status := []string{fmt.Sprintf("%d lines", len(l.lines))}
	if l.follow {
		status = append(status, s.Running.Render("following"))
	} else {
		status = append(status, s.Warning.Render("paused"))
	}
	if !l.wrap {
		status = append(status, "nowrap")
	}
	if l.stamps {
		status = append(status, "timestamps")
	}
	if l.query != "" {
		status = append(status, "search: "+l.query)
	}

	header := s.Title.Render(l.title) + "  " + s.Dim.Render(strings.Join(status, "  "))
	if l.searching {
		header = l.search.View()
	}

	return header + "\n" + l.viewport.View()
}

// Hints are the footer bindings.
func (l *Logs) Hints() []key.Binding {
	k := l.keys.Logs
	return []key.Binding{k.Search, k.Next, k.Follow, k.Wrap, k.Timestamp, k.Close}
}
