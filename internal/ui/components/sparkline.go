package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/ui/theme"
)

// The eight block heights, which is what makes a sparkline readable at one
// character per sample. Terminals that cannot draw them are rare enough that
// the alternative, a row of hashes, is not worth carrying.
var blocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// meterFull and meterEmpty draw a bar that reads as a bar rather than as text.
const (
	meterFull  = '█'
	meterHalf  = '▌'
	meterEmpty = '░'
)

// flatEnough is the share of its own value a series has to move before it is
// drawn as movement rather than as noise.
const flatEnough = 0.05

// Sparkline renders a series in one line, oldest sample first. It shows the
// shape of the window rather than its absolute height, which is what a narrow
// column can carry, with one guard: a series that barely moves is drawn flat.
// Without it, a container holding a steady 400KB would come out as a row of
// full blocks and read as a container about to fall over.
func Sparkline(values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return strings.Repeat(" ", width)
	}

	if len(values) > width {
		values = values[len(values)-width:]
	}

	low, high := values[0], values[0]
	for _, v := range values {
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
	}

	var b strings.Builder
	// Pad on the left so a short history grows into the column rather than
	// stretching across it.
	b.WriteString(strings.Repeat(" ", width-len(values)))

	if high <= 0 || high-low <= high*flatEnough {
		b.WriteString(strings.Repeat(string(blocks[0]), len(values)))
		return b.String()
	}

	for _, v := range values {
		b.WriteRune(block(v-low, high-low))
	}
	return b.String()
}

// ColouredSparkline draws the series and tints it by where it ends up, so a
// column of them reads as a heat map without anyone having to compare numbers.
func ColouredSparkline(values []float64, width int, level float64) string {
	line := Sparkline(values, width)
	if strings.TrimSpace(line) == "" {
		return line
	}
	return theme.LevelStyle(level).Render(line)
}

// ScaledSparkline draws the series against a fixed ceiling rather than against
// its own maximum. The header uses it: a machine doing nothing should read as
// doing nothing, and a graph rescaled to its own noise reads as load.
func ScaledSparkline(values []float64, width int, ceiling, level float64) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 || ceiling <= 0 {
		return strings.Repeat(" ", width)
	}

	if len(values) > width {
		values = values[len(values)-width:]
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", width-len(values)))
	for _, v := range values {
		b.WriteRune(block(v, ceiling))
	}
	return theme.LevelStyle(level).Render(b.String())
}

// block picks a glyph for one sample. A flat series of zeroes stays at the
// lowest glyph rather than showing a full bar.
func block(v, high float64) rune {
	if high <= 0 || v <= 0 {
		return blocks[0]
	}
	idx := int(v / high * float64(len(blocks)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(blocks) {
		idx = len(blocks) - 1
	}
	return blocks[idx]
}

// Meter renders a percentage as a fixed-width bar, coloured by its level with
// the unfilled part left faint: a screen of idle containers should read as
// quiet, not as a wall of green.
func Meter(percent float64, width int) string {
	s := theme.Current()
	filled, empty := meterParts(percent, width)
	return theme.LevelStyle(percent).Render(filled) + s.Faint.Render(empty)
}

// PlainMeter is the same bar without colour, for callers that tint the whole
// cell themselves.
func PlainMeter(percent float64, width int) string {
	filled, empty := meterParts(percent, width)
	return filled + empty
}

// meterParts splits a bar into what is filled and what is not, half blocks
// included so a short meter still resolves small differences.
func meterParts(percent float64, width int) (string, string) {
	if width <= 0 {
		return "", ""
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	exact := percent / 100 * float64(width)
	full := min(int(exact), width)
	remainder := exact - float64(int(exact))

	filled := strings.Repeat(string(meterFull), full)
	if full < width && remainder >= 0.5 {
		filled += string(meterHalf)
		full++
	}
	return filled, strings.Repeat(string(meterEmpty), width-full)
}

// Ratio draws a two-part bar: how much of a whole is used, with the rest
// faint. Used by the disk view, where the question is always what share of the
// total one category holds.
func Ratio(part, whole int64, width int) string {
	s := theme.Current()
	if width <= 0 || whole <= 0 {
		return strings.Repeat(" ", max(width, 0))
	}

	percent := float64(part) / float64(whole) * 100
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled == 0 && part > 0 {
		filled = 1
	}

	return theme.LevelStyle(percent).Render(strings.Repeat(string(meterFull), filled)) +
		s.Faint.Render(strings.Repeat(string(meterEmpty), width-filled))
}

// Dot is a small state light, for columns too narrow for a word.
func Dot(style lipgloss.Style) string { return style.Render("●") }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
