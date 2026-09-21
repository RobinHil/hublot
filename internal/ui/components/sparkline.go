package components

import "strings"

// blocks are the eight vertical bar glyphs, from lowest to highest.
var blocks = []rune{'_', '.', ':', '-', '=', '+', '*', '#'}

// Sparkline renders a series in one line of the given width, oldest sample
// first. An empty series renders as blanks so columns stay aligned.
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

	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}

	var b strings.Builder
	// Pad on the left so a short history grows into the column rather than
	// stretching across it.
	for i := 0; i < width-len(values); i++ {
		b.WriteRune(' ')
	}

	for _, v := range values {
		b.WriteRune(block(v, max))
	}
	return b.String()
}

// block picks a glyph for one sample. A flat series of zeroes stays at the
// lowest glyph rather than showing a full bar.
func block(v, max float64) rune {
	if max <= 0 || v <= 0 {
		return blocks[0]
	}
	idx := int(v / max * float64(len(blocks)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(blocks) {
		idx = len(blocks) - 1
	}
	return blocks[idx]
}

// Bar renders a percentage as a fixed-width meter, used for the CPU column.
func Bar(percent float64, width int) string {
	if width <= 0 {
		return ""
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("#", filled) + strings.Repeat(".", width-filled)
}
