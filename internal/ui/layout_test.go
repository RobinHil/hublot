package ui

import "testing"

func TestComputeRefusesScreensTooSmallToRead(t *testing.T) {
	for _, size := range [][2]int{{39, 20}, {80, 8}, {10, 4}} {
		if l := Compute(size[0], size[1], false, ShareThird); !l.TooSmall {
			t.Errorf("%dx%d should be refused", size[0], size[1])
		}
	}
	if l := Compute(minWidth, minHeight, false, ShareThird); l.TooSmall {
		t.Error("the minimum size itself must be usable")
	}
}

func TestComputeFillsTheScreenExactly(t *testing.T) {
	// Every row is accounted for: what the chrome takes plus what the body
	// gets has to be the height, or the hint bar drifts off the bottom.
	for _, size := range [][2]int{{150, 40}, {96, 24}, {80, 14}, {60, 12}, {40, 9}} {
		l := Compute(size[0], size[1], false, ShareThird)
		chrome := l.HeaderRows + 1 // header and hints
		if l.ShowTabs {
			chrome++
		}
		if l.ShowMessage {
			chrome++
		}
		if l.HeaderGap {
			chrome += 2 // a blank row each side of the tab bar
		}
		if got := chrome + l.ListHeight; got != size[1] {
			t.Errorf("%dx%d: rows add up to %d, want %d", size[0], size[1], got, size[1])
		}
	}
}

func TestComputeDropsChromeAsTheScreenShortens(t *testing.T) {
	tall := Compute(120, 40, false, ShareThird)
	if !tall.ShowTabs || !tall.ShowMessage {
		t.Error("a tall screen keeps its tabs and its message line")
	}

	// The message line goes first: what it says also reaches the status bar.
	short := Compute(120, 13, false, ShareThird)
	if !short.ShowTabs || short.ShowMessage {
		t.Errorf("at 13 rows the message line goes, tabs stay: %+v", short)
	}

	shorter := Compute(120, 11, false, ShareThird)
	if shorter.ShowTabs {
		t.Error("at 11 rows the tab bar goes too, the numbers still switch views")
	}
}

func TestComputeGoesCompactWhenNarrow(t *testing.T) {
	if Compute(120, 30, false, ShareThird).Compact {
		t.Error("a wide screen is not compact")
	}
	if !Compute(70, 30, false, ShareThird).Compact {
		t.Error("70 columns is compact")
	}
}

func TestPanelSitsBesideTheListWhenThereIsRoom(t *testing.T) {
	l := Compute(160, 40, true, ShareThird)
	if !l.PanelOpen || l.PanelStacked || l.ListHidden {
		t.Fatalf("a wide screen puts the panel beside the list: %+v", l)
	}
	if l.ListWidth+l.PanelWidth != 160 {
		t.Errorf("the split must use the whole width: %d + %d", l.ListWidth, l.PanelWidth)
	}
	if l.PanelHeight != l.ListHeight {
		t.Errorf("side by side, both halves are the same height: %d vs %d", l.PanelHeight, l.ListHeight)
	}

	// Wider shares take more, and the list keeps its floor.
	half := Compute(160, 40, true, ShareHalf)
	if half.PanelWidth <= l.PanelWidth {
		t.Error("a half share is wider than a third")
	}
	tight := Compute(100, 40, true, ShareTwoThirds)
	if tight.ListWidth < listFloor {
		t.Errorf("the list must keep %d columns, got %d", listFloor, tight.ListWidth)
	}
}

func TestPanelStacksWhenTheScreenIsNarrow(t *testing.T) {
	l := Compute(90, 30, true, ShareThird)
	if !l.PanelStacked {
		t.Fatalf("under %d columns the panel goes below: %+v", sideBySideWidth, l)
	}
	if l.PanelWidth != 90 {
		t.Errorf("stacked, the panel spans the width: %d", l.PanelWidth)
	}
	if l.ListHeight+l.PanelHeight != Compute(90, 30, false, ShareThird).ListHeight {
		t.Error("stacking must not change how many rows the body has in total")
	}
	if l.ListHeight < 4 {
		t.Errorf("the list keeps rows worth reading: %d", l.ListHeight)
	}
}

func TestPanelTakesTheScreenWhenNothingElseFits(t *testing.T) {
	small := Compute(60, 14, true, ShareThird)
	if !small.ListHidden {
		t.Errorf("a small screen gives the panel everything: %+v", small)
	}

	// And the full share does the same at any size, because it was asked for.
	full := Compute(200, 50, true, ShareFull)
	if !full.ListHidden || full.PanelWidth != 200 {
		t.Errorf("the full share hides the list: %+v", full)
	}
}

func TestShareCycles(t *testing.T) {
	share := ShareThird
	seen := []string{share.Label()}
	for i := 0; i < 3; i++ {
		share = share.Next()
		seen = append(seen, share.Label())
	}
	want := []string{"1/3", "1/2", "2/3", "full"}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("share %d: got %q, want %q", i, seen[i], want[i])
		}
	}
	if share.Next() != ShareThird {
		t.Error("the cycle wraps round to a third")
	}
}

// The rows spent on breathing room are spent only where there are rows to
// spare, and the tab bar has to be where the renderer drew it or a click
// selects the wrong view.
func TestHeaderGapOnlyOnAScreenWithRoom(t *testing.T) {
	roomy := Compute(150, 40, false, ShareThird)
	if !roomy.HeaderGap {
		t.Error("a 40 row terminal has rows to spare")
	}
	if roomy.TabRow() != roomy.HeaderRows+1 {
		t.Errorf("tab row = %d, want one below the header", roomy.TabRow())
	}

	tight := Compute(150, 20, false, ShareThird)
	if tight.HeaderGap {
		t.Error("20 rows go to the table, not to whitespace")
	}
	if tight.TabRow() != tight.HeaderRows {
		t.Errorf("tab row = %d, want right under the header", tight.TabRow())
	}
	if !tight.ShowTabs {
		t.Error("the tab bar itself survives at 20 rows")
	}
}

// Dragging the divider puts it where the mouse is, and never so far that the
// list disappears along with the divider itself.
func TestDividerFollowsTheMouse(t *testing.T) {
	l := Compute(150, 40, true, ShareThird)

	// The divider reads back as where it is drawn, to the column. It is not
	// exact because a third of 150 columns is not a whole number, and only a
	// drag changes the share, so the rounding never accumulates.
	if got := l.ShareAtColumn(l.ListWidth); got < ShareThird-2 || got > ShareThird+2 {
		t.Errorf("share at the divider = %d, want about %d", got, ShareThird)
	}

	// Dragging left grows the panel.
	wide := l.ShareAtColumn(50)
	if wide <= ShareThird {
		t.Errorf("dragging left grows the panel: %d", wide)
	}

	// Dragging past the edge stops short of hiding the list.
	if got := l.ShareAtColumn(0); got != shareDragMax {
		t.Errorf("share at the edge = %d, want %d", got, shareDragMax)
	}
	if got := l.ShareAtColumn(l.Width); got != shareMin {
		t.Errorf("share at the far edge = %d, want %d", got, shareMin)
	}

	// A panel dragged to any percentage still leaves both halves readable.
	for _, share := range []PanelShare{shareMin, 20, 37, 64, shareDragMax} {
		got := Compute(150, 40, true, share)
		if got.ListWidth < listFloor {
			t.Errorf("share %d leaves the list at %d columns", share, got.ListWidth)
		}
		if got.PanelWidth < panelFloor {
			t.Errorf("share %d leaves the panel at %d columns", share, got.PanelWidth)
		}
	}
}

// Stacked, the divider is measured inside the body: the header and the hint
// bar are not the panel's to take.
func TestDividerFollowsTheMouseStacked(t *testing.T) {
	l := Compute(80, 40, true, ShareThird)
	if !l.PanelStacked {
		t.Fatal("80 columns stacks the panel")
	}

	top := l.ContentRow() + l.ListHeight
	if got := l.ShareAtRow(top); got < ShareThird-3 || got > ShareThird+3 {
		t.Errorf("share at the divider = %d, want about %d", got, ShareThird)
	}
	if up, down := l.ShareAtRow(top-5), l.ShareAtRow(top+5); up <= down {
		t.Errorf("dragging up grows the panel: %d up against %d down", up, down)
	}
}

// The key cycles through the stops it always did, wherever a drag left it.
func TestShareCyclesFromAnyDraggedValue(t *testing.T) {
	if got := PanelShare(37).Next(); got != ShareHalf {
		t.Errorf("37%% cycles to %d, want %d", got, ShareHalf)
	}
	if got := ShareFull.Next(); got != ShareThird {
		t.Errorf("full cycles to %d, want %d", got, ShareThird)
	}
	if got := PanelShare(37).Label(); got != "37%" {
		t.Errorf("label = %q, want the percentage", got)
	}
}
