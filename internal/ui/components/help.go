package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// helpCell is the width of one binding entry. Entries are truncated to it
// rather than wrapped: a binding split across two lines is unreadable.
const helpCell = 26

// HelpOverlay renders every binding, grouped, scrolled to offset. It is
// generated from the keys package, so a binding can never be documented
// differently from what it does (AGENTS.md section 9.2).
//
// The second return value is how far the overlay can be scrolled, which the
// app clamps its offset to.
func HelpOverlay(k keys.Map, readOnly bool, width, height, offset int) (string, int) {
	s := theme.Current()

	disabled := map[string]bool{}
	if readOnly {
		for _, b := range k.Destructive() {
			disabled[b.Help().Key] = true
		}
	}

	inner := width - 8
	if inner < 40 {
		inner = 40
	}
	if inner > 110 {
		inner = 110
	}

	perLine := inner / helpCell
	if perLine < 1 {
		perLine = 1
	}

	lines := helpLines(k, disabled, perLine)

	// Two lines for the title, two for the border, one for the footer.
	visible := height - 6
	if visible < 3 {
		visible = 3
	}

	maxOffset := len(lines) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + visible
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	b.WriteString(s.Title.Render("hublot keys"))
	if readOnly {
		b.WriteString("  " + s.Badge.Render("read-only: disabled keys are marked"))
	}
	b.WriteString("\n\n")
	b.WriteString(strings.Join(lines[offset:end], "\n"))
	b.WriteString("\n")

	footer := "esc or ? to close"
	if maxOffset > 0 {
		footer = "up/down to scroll   " + footer
	}
	b.WriteString(s.Help.Render(footer))

	return s.Modal.Width(inner).Render(b.String()), maxOffset
}

// helpLines lays every section out into fixed-width columns.
func helpLines(k keys.Map, disabled map[string]bool, perLine int) []string {
	s := theme.Current()

	var lines []string
	for i, section := range k.Sections() {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, s.Accent.Render(section.Title))

		var row []string
		for _, bind := range section.Bindings {
			row = append(row, helpEntry(bind, disabled))
			if len(row) == perLine {
				lines = append(lines, strings.Join(row, ""))
				row = nil
			}
		}
		if len(row) > 0 {
			lines = append(lines, strings.Join(row, ""))
		}
	}
	return lines
}

// helpEntry renders one binding, truncated to the cell width so nothing wraps.
func helpEntry(b key.Binding, disabled map[string]bool) string {
	s := theme.Current()
	h := b.Help()

	desc := h.Desc
	if disabled[h.Key] {
		desc += " (off)"
		return lipgloss.NewStyle().Width(helpCell).Render(
			s.Dim.Render(fit(h.Key+"  "+desc, helpCell-1)))
	}

	// The key is styled, so the text is fitted before the style is applied to
	// keep the column width exact.
	room := helpCell - 1 - lipgloss.Width(h.Key) - 2
	return lipgloss.NewStyle().Width(helpCell).Render(
		s.Key.Render(h.Key) + "  " + s.Help.Render(fit(desc, room)))
}

// fit truncates to a display width without wrapping.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "."
	}
	return string(runes[:width-1]) + "."
}

// HintBar renders the one-line key reminder at the bottom of a view.
func HintBar(bindings []key.Binding, width int) string {
	s := theme.Current()

	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, s.Key.Render(h.Key)+s.Help.Render(":"+h.Desc))
	}
	return truncateToWidth(strings.Join(parts, s.Dim.Render("  ")), width)
}
