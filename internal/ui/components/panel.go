package components

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

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// maxPanelLines bounds the buffer: a chatty container must not grow the model
// without limit over a long session.
const maxPanelLines = 5000

// PanelKind decides the title and which keys make sense.
type PanelKind int

const (
	// PanelNone means the panel is closed.
	PanelNone PanelKind = iota
	// PanelLogs is a live stream that follows its tail.
	PanelLogs
	// PanelDetail is inspect output, read from the top.
	PanelDetail
	// PanelText is anything else read once: processes, changes, config.
	PanelText
)

// serviceColours are cycled by a stable hash of the service name, so a service
// keeps its colour across sessions (AGENTS.md section 8.6).
var serviceColours = []lipgloss.AdaptiveColor{
	{Light: "#0b66d0", Dark: "#63b3ff"},
	{Light: "#127a3a", Dark: "#49c96d"},
	{Light: "#9a6207", Dark: "#e3a92a"},
	{Light: "#7b3fd0", Dark: "#b78cff"},
	{Light: "#b03a7a", Dark: "#f487c0"},
	{Light: "#0f6f74", Dark: "#3fc7cf"},
}

// Panel is the side view: logs, inspect output, anything worth reading next to
// the list rather than instead of it. It holds lines that are already
// sanitised, because everything in it comes from the daemon.
type Panel struct {
	Kind     PanelKind
	Title    string
	Subtitle string
	// Source identifies what is being shown, so reopening the same thing is a
	// toggle rather than a reload.
	Source string

	lines    []string
	viewport viewport.Model
	follow   bool
	wrap     bool
	// rowOf maps each line to the row it starts on once wrapped, so searching
	// lands on the line and not on whatever happens to share its number.
	rowOf []int
	// dirty says lines have arrived since the last render. Laying the buffer
	// out costs a pass over every line in it, and a busy container sends more
	// lines per second than the screen has frames: the work is done once per
	// frame, when something is about to be drawn, not once per line.
	dirty bool

	search    textinput.Model
	searching bool
	query     string
	match     int

	focused bool
	edge    PanelEdge
	width   int
	height  int
}

// PanelEdge is which side the panel is separated from the list on. It follows
// from the geometry rather than from taste: a rule belongs between two things,
// so a panel that fills the screen has none.
type PanelEdge int

const (
	// EdgeLeft is the panel beside the list.
	EdgeLeft PanelEdge = iota
	// EdgeTop is the panel under the list.
	EdgeTop
	// EdgeNone is the panel alone on the screen.
	EdgeNone
)

// NewPanel builds an empty, closed panel.
func NewPanel() Panel {
	in := textinput.New()
	in.Prompt = "search: "
	in.CharLimit = 80

	return Panel{
		viewport: viewport.New(40, 10),
		follow:   true,
		wrap:     true,
		search:   in,
		match:    -1,
	}
}

// Open clears the panel and points it at something new.
func (p *Panel) Open(kind PanelKind, title, subtitle, source string) {
	p.Kind = kind
	p.Title = title
	p.Subtitle = subtitle
	p.Source = source
	p.lines = nil
	p.follow = kind == PanelLogs
	p.query = ""
	p.match = -1
	p.search.SetValue("")
	p.searching = false
	p.viewport.SetContent("")
	p.viewport.GotoTop()
}

// Close empties the panel.
func (p *Panel) Close() {
	p.Kind = PanelNone
	p.Source = ""
	p.lines = nil
	p.focused = false
	p.viewport.SetContent("")
}

// Open reports whether the panel has anything to show.
func (p *Panel) IsOpen() bool { return p.Kind != PanelNone }

// Focused reports whether keys go to the panel.
func (p *Panel) Focused() bool { return p.focused && p.IsOpen() }

// SetFocus moves the keyboard in or out of the panel.
func (p *Panel) SetFocus(focused bool) { p.focused = focused }

// SetEdge says where the rule goes, which depends on where the list is.
func (p *Panel) SetEdge(edge PanelEdge) { p.edge = edge }

// Append adds one line, trimming the buffer when it grows past the cap.
func (p *Panel) Append(line string) {
	p.lines = append(p.lines, Sanitize(line))
	p.dirty = true
}

// AppendService adds a line prefixed by the service it came from, coloured by
// a stable hash of that name.
func (p *Panel) AppendService(service, line string, stderr bool) {
	s := theme.Current()

	text := Sanitize(line)
	if stderr {
		text = s.Danger.Render(text)
	}
	if service != "" {
		text = serviceStyle(service).Render(Sanitize(service)) + s.Faint.Render(" | ") + text
	}

	p.lines = append(p.lines, text)
	p.dirty = true
}

// sync lays out whatever has arrived, and is called before anything that reads
// the viewport. It trims the buffer first, keeping what is on screen where it
// is: the buffer is a window on a stream, so once it is full every line that
// arrives drops one from the front, and the viewport counts rows from the
// front. Left alone the text being read creeps up a row per line received,
// which is unreadable exactly when the logs are busy enough to be worth
// reading. Following is the one case where that movement is wanted, and there
// the render goes to the bottom anyway.
func (p *Panel) sync() {
	if !p.dirty {
		return
	}
	p.dirty = false

	var removed int
	if len(p.lines) > maxPanelLines {
		dropped := len(p.lines) - maxPanelLines
		// rowOf still describes the front of the buffer, which is what is
		// being dropped: nothing has been removed from it since it was built.
		removed = p.rowAt(dropped)
		p.lines = p.lines[dropped:]

		if p.match -= dropped; p.match < 0 {
			p.match = 0
		}
	}

	offset := p.viewport.YOffset
	p.render()
	if removed > 0 && !p.follow {
		p.viewport.SetYOffset(offset - removed)
	}
}

// Set replaces the whole content at once, for anything read rather than
// followed.
func (p *Panel) Set(lines []string) {
	p.lines = p.lines[:0]
	for _, line := range lines {
		p.lines = append(p.lines, Sanitize(line))
	}
	p.render()
	p.viewport.GotoTop()
}

// Lines is how much the panel holds, for the header.
func (p *Panel) Lines() int { return len(p.lines) }

// SetSize gives the panel its drawing area. The arithmetic accounts for what
// the rule and the header cost, so View can return exactly height rows.
func (p *Panel) SetSize(width, height int) {
	p.width, p.height = width, height

	inner := width
	if p.edge == EdgeLeft {
		// A vertical rule and the space after it.
		inner = width - 2
	}
	if inner < 10 {
		inner = 10
	}

	body := p.contentRows() - 1 // the header
	if p.focused {
		body-- // the key hints
	}
	if body < 1 {
		body = 1
	}

	p.viewport.Width = inner
	p.viewport.Height = body
	p.render()
}

// contentRows is how many rows the panel draws inside its rule.
func (p *Panel) contentRows() int {
	rows := p.height
	if p.edge == EdgeTop {
		rows-- // the horizontal rule
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// Update handles the panel's own keys while it has focus.
func (p *Panel) Update(msg tea.Msg, k keys.Map) tea.Cmd {
	p.sync()

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		return cmd
	}

	if p.searching {
		switch {
		case key.Matches(km, k.Global.Escape):
			p.searching = false
			p.search.Blur()
			return nil
		case km.Type == tea.KeyEnter:
			p.searching = false
			p.search.Blur()
			p.query = p.search.Value()
			p.render()
			p.jump(1)
			return nil
		}
		var cmd tea.Cmd
		p.search, cmd = p.search.Update(msg)
		return cmd
	}

	switch {
	case key.Matches(km, k.Panel.Search):
		p.searching = true
		p.search.Focus()
		return textinput.Blink
	case key.Matches(km, k.Panel.Next):
		p.jump(1)
	case key.Matches(km, k.Panel.Prev):
		p.jump(-1)
	case key.Matches(km, k.Panel.Follow):
		p.follow = !p.follow
		if p.follow {
			p.viewport.GotoBottom()
		}
	case key.Matches(km, k.Panel.Wrap):
		p.wrap = !p.wrap
		p.render()
	case key.Matches(km, k.Global.Home):
		p.follow = false
		p.viewport.GotoTop()
	case key.Matches(km, k.Global.End):
		p.viewport.GotoBottom()
	default:
		before := p.viewport.YOffset
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		// Scrolling by hand means the user wants to read, not to follow.
		if p.viewport.YOffset != before && !p.viewport.AtBottom() {
			p.follow = false
		}
		return cmd
	}
	return nil
}

// jump moves to the next or previous line containing the query.
func (p *Panel) jump(delta int) {
	if p.query == "" || len(p.lines) == 0 {
		return
	}

	needle := strings.ToLower(p.query)
	n := len(p.lines)
	for i := 1; i <= n; i++ {
		idx := ((p.match+delta*i)%n + n) % n
		if strings.Contains(strings.ToLower(ansi.Strip(p.lines[idx])), needle) {
			p.match = idx
			p.follow = false
			// The viewport counts rows on screen, not lines of text, and a
			// wrapped line is several rows: without this the search lands
			// further and further off as the buffer wraps more.
			p.viewport.SetYOffset(p.rowAt(idx))
			return
		}
	}
}

// continuation marks a line that carries on from the one above it.
const continuation = "  "

// rowAt is the screen row a line starts on, once wrapping is taken into
// account. It falls back to the line number, which is right when nothing wraps.
func (p *Panel) rowAt(line int) int {
	if line >= 0 && line < len(p.rowOf) {
		return p.rowOf[line]
	}
	return line
}

// render rebuilds the viewport content at the current width.
func (p *Panel) render() {
	s := theme.Current()
	width := p.viewport.Width
	if width <= 0 {
		width = 40
	}

	var b strings.Builder
	p.rowOf = make([]int, len(p.lines))
	row := 0

	for i, line := range p.lines {
		if i > 0 {
			b.WriteString("\n")
		}
		p.rowOf[i] = row

		text := line
		if p.query != "" && strings.Contains(strings.ToLower(ansi.Strip(text)), strings.ToLower(p.query)) {
			text = s.Warning.Render(ansi.Strip(text))
		}

		if !p.wrap {
			b.WriteString(ansi.Truncate(text, width, ""))
			row++
			continue
		}

		// Continuations are indented, and the indent is the point. A wrapped
		// line otherwise looks exactly like the next one, and scrolling lands
		// on half a sentence with nothing saying it is half a sentence.
		wrapped := strings.Split(ansi.Wrap(text, width-len(continuation), ""), "\n")
		for j, part := range wrapped {
			if j > 0 {
				b.WriteString("\n")
				b.WriteString(s.Faint.Render(continuation))
			}
			b.WriteString(part)
		}
		row += len(wrapped)
	}

	p.viewport.SetContent(b.String())
	if p.follow {
		p.viewport.GotoBottom()
	}
}

// View renders the panel, rule included, in exactly the height it was given.
func (p *Panel) View() string {
	p.sync()
	s := theme.Current()

	lines := []string{p.header()}
	lines = append(lines, strings.Split(p.viewport.View(), "\n")...)
	if footer := p.footer(); footer != "" {
		lines = append(lines, footer)
	}

	// Exactly the rows promised: a panel that overflows would push the hint
	// bar off the screen, and one that falls short would leave a gap.
	rows := p.contentRows()
	for len(lines) < rows {
		lines = append(lines, "")
	}
	lines = lines[:rows]
	content := strings.Join(lines, "\n")

	switch p.edge {
	case EdgeTop:
		rule := s.Rule.Render(strings.Repeat("\u2500", max(p.width, 0)))
		if p.focused {
			rule = lipgloss.NewStyle().Foreground(s.Palette.BorderLit).
				Render(strings.Repeat("\u2500", max(p.width, 0)))
		}
		return rule + "\n" + content

	case EdgeNone:
		return content

	default:
		frame := s.Panel
		if p.focused {
			frame = s.PanelFocused
		}
		return frame.Width(p.width - 1).Render(content)
	}
}

func (p *Panel) header() string {
	s := theme.Current()

	title := s.PanelTitle.Render(SanitizeWidth(p.Title, p.viewport.Width))
	if p.searching {
		return p.search.View()
	}

	var facts []string
	if p.Kind == PanelLogs {
		facts = append(facts, fmt.Sprintf("%d lines", len(p.lines)))
		if p.follow {
			facts = append(facts, s.Running.Render("following"))
		} else {
			facts = append(facts, s.Warning.Render("paused"))
		}
	} else if p.Subtitle != "" {
		facts = append(facts, p.Subtitle)
	}
	if p.query != "" {
		facts = append(facts, "/"+p.query)
	}

	right := s.Faint.Render(strings.Join(facts, "  "))
	gap := p.viewport.Width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(title, p.viewport.Width, "")
	}
	return title + strings.Repeat(" ", gap) + right
}

// footer shows the panel's own keys, but only when it has the focus: the list
// keys are what matter otherwise.
func (p *Panel) footer() string {
	if !p.focused {
		return ""
	}
	s := theme.Current()

	hints := []string{"/ search", "n next", "w wrap", "esc close"}
	if p.Kind == PanelLogs {
		hints = append([]string{"f follow"}, hints...)
	}
	return s.Faint.Render(ansi.Truncate(strings.Join(hints, "   "), p.viewport.Width, ""))
}

// serviceStyle picks a stable colour from the service name, so the same
// service always reads the same way in an aggregated stream.
func serviceStyle(service string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(service))
	return lipgloss.NewStyle().
		Foreground(serviceColours[int(h.Sum32())%len(serviceColours)]).
		Bold(true)
}
