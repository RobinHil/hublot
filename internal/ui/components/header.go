package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Dashboard is what the head of the screen shows: the two figures that matter
// with their history, then what the host is made of. It is one line on a short
// screen and two when there is room, because the history is worth a line.
type Dashboard struct {
	Version string
	View    string

	// CPUPercent is the containers' share of the machine, CPUHistory the same
	// figure over the last few minutes.
	CPUPercent float64
	CPUHistory []float64
	MemUsage   int64
	MemTotal   int64
	MemHistory []float64

	Running, Paused, Stopped int
	Projects, Drifted        int

	Stale      bool
	ReadOnly   bool
	Marked     int
	Position   string
	Panel      string
	Tasks      int
	TaskFailed bool

	// Summary is the active view's own line, such as what a list adds up to.
	Summary string
}

// HeaderLines renders the header in the number of rows it is given: two for
// the full dashboard, one for the essentials.
func HeaderLines(d Dashboard, width, rows int) []string {
	switch {
	case rows >= 3:
		// A blank row between what the session is and what it is doing: two
		// dense lines stacked read as one.
		return []string{titleLine(d, width), "", gaugeLine(d, width)}
	case rows == 2:
		return []string{titleLine(d, width), gaugeLine(d, width)}
	}
	return []string{compactLine(d, width)}
}

// titleLine is the identity of the session and its warnings.
func titleLine(d Dashboard, width int) string {
	s := theme.Current()

	left := []string{s.Title.Render("hublot")}
	if d.Version != "" {
		left = append(left, s.Faint.Render("docker "+d.Version))
	}
	if d.Summary != "" {
		left = append(left, s.Dim.Render(d.Summary))
	}

	return join(strings.Join(left, s.Faint.Render(" · ")), badges(d), width)
}

// gaugeLine is the dashboard proper: two meters with their history, then the
// state of the host in dots. The graphs take whatever the rest of the line
// does not, which is what a window is for.
func gaugeLine(d Dashboard, width int) string {
	s := theme.Current()

	var memLevel float64
	if d.MemTotal > 0 {
		memLevel = float64(d.MemUsage) / float64(d.MemTotal) * 100
	}

	cpuTail := " " + Meter(d.CPUPercent, 10) + " " +
		theme.LevelStyle(d.CPUPercent).Render(fmt.Sprintf("%5s", percent(d.CPUPercent)))
	memTail := " " + Meter(memLevel, 10) + " " + s.Text.Render(humanBytes(d.MemUsage))
	if d.MemTotal > 0 {
		memTail += s.Faint.Render("/" + humanBytes(d.MemTotal))
	}

	right := states(d)
	spark := sparkWidth(width, lipgloss.Width("cpu ")+lipgloss.Width(cpuTail)+
		lipgloss.Width("mem ")+lipgloss.Width(memTail)+
		lipgloss.Width(right)+betweenGauges+besideStates)

	// The tails carry the space that separates them from the graph, so a line
	// too narrow for one must not keep the gap where it would have been.
	if spark == 0 {
		cpuTail, memTail = strings.TrimPrefix(cpuTail, " "), strings.TrimPrefix(memTail, " ")
	}

	cpu := s.Faint.Render("cpu ") + ScaledSparkline(d.CPUHistory, spark, 100, d.CPUPercent) + cpuTail
	mem := s.Faint.Render("mem ") +
		ScaledSparkline(d.MemHistory, spark, float64(d.MemTotal), memLevel) + memTail

	left := cpu + strings.Repeat(" ", betweenGauges) + mem
	return join(left, right, width)
}

const (
	// betweenGauges separates the two gauges, besideStates keeps the pair of
	// them off the dots. That one is much the wider, and deliberately: it is
	// the seam between two different things rather than between two readings
	// of the same kind, and it is the only gap the graphs grow into. Every two
	// columns given to it cost one from each graph, which is a trade worth
	// making: a graph a little shorter reads the same, a dashboard with no air
	// in it reads as one block.
	betweenGauges = 3
	besideStates  = 8
	// minSpark is the shortest graph worth drawing; under it the line is
	// better off without one.
	minSpark = 8
	// maxSpark matches the samples the store keeps, since a graph wider than
	// its history is padding.
	maxSpark = 160
)

// sparkWidth splits what the line has left between the two graphs.
func sparkWidth(width, fixed int) int {
	spark := (width - fixed) / 2
	if spark < minSpark {
		return 0
	}
	if spark > maxSpark {
		return maxSpark
	}
	return spark
}

// compactLine is everything that still fits when the screen is short.
func compactLine(d Dashboard, width int) string {
	s := theme.Current()

	var memLevel float64
	if d.MemTotal > 0 {
		memLevel = float64(d.MemUsage) / float64(d.MemTotal) * 100
	}

	left := strings.Join([]string{
		s.Title.Render("hublot"),
		s.Faint.Render("cpu ") + Meter(d.CPUPercent, 6) + " " +
			theme.LevelStyle(d.CPUPercent).Render(percent(d.CPUPercent)),
		s.Faint.Render("mem ") + Meter(memLevel, 6) + " " + humanBytes(d.MemUsage),
		states(d),
	}, s.Faint.Render("  "))

	return join(left, badges(d), width)
}

// states is the container breakdown as coloured dots, which reads faster than
// three labelled numbers.
func states(d Dashboard) string {
	s := theme.Current()

	parts := []string{
		s.Running.Render("●") + s.Dim.Render(fmt.Sprintf(" %d up", d.Running)),
	}
	if d.Paused > 0 {
		parts = append(parts, s.Warning.Render("●")+s.Dim.Render(fmt.Sprintf(" %d paused", d.Paused)))
	}
	if d.Stopped > 0 {
		parts = append(parts, s.Faint.Render("●")+s.Dim.Render(fmt.Sprintf(" %d stopped", d.Stopped)))
	}
	if d.Drifted > 0 {
		parts = append(parts, s.Warning.Render("→")+s.Dim.Render(fmt.Sprintf(" %d drifted", d.Drifted)))
	}
	return strings.Join(parts, s.Faint.Render("  "))
}

// badges are the states of the session itself, which are exceptions rather
// than figures: they only appear when they apply.
func badges(d Dashboard) string {
	s := theme.Current()

	var out []string
	if d.ReadOnly {
		out = append(out, s.BadgeGood.Render("read-only"))
	}
	if d.Stale {
		out = append(out, s.Danger.Render("daemon unreachable"))
	}
	if d.Marked > 0 {
		out = append(out, s.Accent.Render(fmt.Sprintf("%d selected", d.Marked)))
	}
	if d.Tasks > 0 {
		label := fmt.Sprintf("%d running", d.Tasks)
		if d.TaskFailed {
			out = append(out, s.Danger.Render(label))
		} else {
			out = append(out, s.Warning.Render(label))
		}
	}
	if d.Panel != "" {
		out = append(out, s.Faint.Render("panel "+d.Panel))
	}
	if d.Position != "" {
		out = append(out, s.Dim.Render(d.Position))
	}
	return strings.Join(out, s.Faint.Render("  "))
}

// percent keeps small numbers readable without giving big ones decimals they
// do not deserve.
func percent(v float64) string {
	if v >= 10 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

// join places left and right on one line, padding between them.
func join(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, width, "")
	}
	return left + strings.Repeat(" ", gap) + right
}

// humanBytes is a compact size, since a header has no room for decimals it
// does not need.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTP"[exp])
}
