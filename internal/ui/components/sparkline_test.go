package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSparklineDrawsAFlatSeriesFlat(t *testing.T) {
	// A container holding a steady 400KB used to come out as a row of full
	// blocks, which reads as a container about to fall over.
	flat := []float64{400, 400, 401, 400, 399, 400}
	got := ansi.Strip(Sparkline(flat, 6))

	if strings.Trim(got, string(blocks[0])) != "" {
		t.Errorf("a steady series must read as quiet, got %q", got)
	}
}

func TestSparklineShowsRealMovement(t *testing.T) {
	rising := []float64{10, 30, 50, 70, 90, 110}
	got := ansi.Strip(Sparkline(rising, 6))

	runes := []rune(got)
	if runes[0] != blocks[0] {
		t.Errorf("the start of a rise sits at the bottom: %q", got)
	}
	if runes[len(runes)-1] != blocks[len(blocks)-1] {
		t.Errorf("the end of a rise reaches the top: %q", got)
	}
}

func TestSparklinePadsShortHistories(t *testing.T) {
	got := ansi.Strip(Sparkline([]float64{1, 2}, 6))
	if len([]rune(got)) != 6 {
		t.Errorf("width: got %d runes, want 6 (%q)", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "    ") {
		t.Errorf("a short history grows in from the right: %q", got)
	}
}

func TestScaledSparklineUsesTheCeilingItIsGiven(t *testing.T) {
	// The header plots a share of the machine: at 2% of a host the line must
	// sit at the bottom however much it wobbles.
	quiet := ansi.Strip(ScaledSparkline([]float64{1, 2, 1, 2}, 4, 100, 2))
	if strings.Trim(quiet, string(blocks[0])) != "" {
		t.Errorf("2%% of a host is quiet, got %q", quiet)
	}

	busy := ansi.Strip(ScaledSparkline([]float64{95, 98, 100, 97}, 4, 100, 98))
	if !strings.Contains(busy, string(blocks[len(blocks)-1])) {
		t.Errorf("a saturated host reaches the top, got %q", busy)
	}
}

func TestMeterFillsProportionally(t *testing.T) {
	cases := map[float64]int{0: 0, 50: 5, 100: 10}
	for percent, wantFull := range cases {
		got := ansi.Strip(Meter(percent, 10))
		if n := strings.Count(got, string(meterFull)); n != wantFull {
			t.Errorf("Meter(%v): %d full blocks, want %d (%q)", percent, n, wantFull, got)
		}
		if len([]rune(got)) != 10 {
			t.Errorf("Meter(%v) is %d wide, want 10", percent, len([]rune(got)))
		}
	}

	// Out of range values are clamped rather than overflowing the column.
	if got := ansi.Strip(Meter(250, 6)); len([]rune(got)) != 6 {
		t.Errorf("an impossible percentage must still fit: %q", got)
	}
}

func TestRatioShowsTheShareOfAWhole(t *testing.T) {
	got := ansi.Strip(Ratio(25, 100, 8))
	if n := strings.Count(got, string(meterFull)); n != 2 {
		t.Errorf("a quarter of eight cells is two, got %d (%q)", n, got)
	}

	// Something present but tiny still gets a cell, or it reads as nothing.
	tiny := ansi.Strip(Ratio(1, 1000, 8))
	if !strings.Contains(tiny, string(meterFull)) {
		t.Errorf("a small share must still be visible: %q", tiny)
	}
	if empty := ansi.Strip(Ratio(0, 1000, 8)); strings.Contains(empty, string(meterFull)) {
		t.Errorf("nothing is nothing: %q", empty)
	}
}
