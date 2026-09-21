package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Status is everything the bottom bar shows about the session.
type Status struct {
	Socket     string
	Version    string
	Containers int
	Running    int
	// Stale is set while the event stream is disconnected: the data on screen
	// is the last known state (AGENTS.md section 6.5).
	Stale      bool
	ReadOnly   bool
	Marked     int
	Position   string
	Tasks      int
	TaskFailed bool
	Filter     string
	Message    string
	MessageErr bool
	MessageAt  time.Time
}

// StatusBar renders the one-line header shown at the top of every view.
func StatusBar(st Status, width int) string {
	s := theme.Current()

	left := []string{s.Accent.Render("hublot")}
	if st.Version != "" {
		left = append(left, s.Dim.Render("docker "+st.Version))
	}
	left = append(left, s.Dim.Render(fmt.Sprintf("%d containers, %d running", st.Containers, st.Running)))

	var right []string
	if st.ReadOnly {
		right = append(right, s.Badge.Render("read-only"))
	}
	if st.Stale {
		// The indicator says the daemon is gone, not that nothing is happening.
		right = append(right, s.Danger.Render("daemon unreachable, retrying"))
	}
	if st.Marked > 0 {
		right = append(right, s.Accent.Render(fmt.Sprintf("%d marked", st.Marked)))
	}
	if st.Tasks > 0 {
		label := fmt.Sprintf("%d running", st.Tasks)
		if st.TaskFailed {
			right = append(right, s.Danger.Render(label))
		} else {
			right = append(right, s.Warning.Render(label))
		}
	}
	if st.Position != "" {
		right = append(right, s.Dim.Render(st.Position))
	}

	return join(strings.Join(left, s.Dim.Render(" - ")), strings.Join(right, s.Dim.Render(" - ")), width)
}

// FooterMessage renders the transient line above the key hints.
func FooterMessage(st Status) string {
	if st.Message == "" {
		return ""
	}
	s := theme.Current()
	if st.MessageErr {
		return s.Danger.Render(st.Message)
	}
	return s.Dim.Render(st.Message)
}

// Tabs renders the view selector.
func Tabs(titles []string, active int, width int) string {
	s := theme.Current()

	var rendered []string
	for i, t := range titles {
		label := fmt.Sprintf("%d %s", i+1, t)
		if i == active {
			rendered = append(rendered, s.TabActive.Render(label))
		} else {
			rendered = append(rendered, s.Tab.Render(label))
		}
	}
	return truncateToWidth(strings.Join(rendered, ""), width)
}

// join places left and right on one line, padding between them.
func join(left, right string, width int) string {
	gap := width - visibleWidth(left) - visibleWidth(right)
	if gap < 1 {
		return truncateToWidth(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}
