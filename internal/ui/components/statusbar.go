package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RobinHil/hublot/internal/ui/theme"
)

// FooterMessage renders the transient line above the key hints.
func FooterMessage(text string, isErr bool) string {
	if text == "" {
		return ""
	}
	s := theme.Current()
	if isErr {
		return s.Danger.Render(Sanitize(text))
	}
	return s.Dim.Render(Sanitize(text))
}

// Tabs renders the view selector. Compact keeps the numbers only, which is
// what a narrow terminal can spare.
func Tabs(titles []string, active int, width int, compact bool) string {
	s := theme.Current()

	var rendered []string
	for i, t := range titles {
		number := fmt.Sprintf("%d", i+1)
		label := number
		if !compact {
			label = number + " " + t
		}

		if i == active {
			rendered = append(rendered, s.TabActive.Render(label))
			continue
		}
		if compact {
			rendered = append(rendered, s.Tab.Render(label))
			continue
		}
		rendered = append(rendered, s.TabKey.Render(" "+number)+s.Tab.Render(t))
	}

	bar := strings.Join(rendered, "")
	if lipgloss.Width(bar) > width {
		return ansi.Truncate(bar, width, "")
	}
	return bar
}

// TabAt maps a column on the tab bar back to a view, so a click switches to
// what was clicked. The arithmetic mirrors what Tabs draws.
func TabAt(titles []string, x int, compact bool) (int, bool) {
	cursor := 0
	for i, t := range titles {
		width := 3 // " 1 "
		if !compact {
			width = 3 + len(t)
		}
		if x >= cursor && x < cursor+width {
			return i, true
		}
		cursor += width
	}
	return 0, false
}
