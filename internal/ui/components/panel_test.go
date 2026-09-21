package components

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The buffer is a window on a stream: once it is full, a line arriving drops
// one from the front. The viewport counts rows from the front, so without an
// adjustment the text being read creeps up a row per line received. That is
// what made reading busy logs feel like the panel was sliding.
func TestPanelKeepsWhatIsBeingReadStillWhileLinesArrive(t *testing.T) {
	p := NewPanel()
	p.SetSize(60, 20)
	p.Open(PanelLogs, "logs", "", "src")

	for i := 0; i < maxPanelLines; i++ {
		p.Append(fmt.Sprintf("line %d", i))
	}

	p.sync()

	// Scroll away from the end: now reading, not following.
	p.follow = false
	p.viewport.SetYOffset(maxPanelLines / 2)
	at := p.visibleTop()

	for i := 0; i < 40; i++ {
		p.Append(fmt.Sprintf("newcomer %d", i))
		p.sync()
	}

	if now := p.visibleTop(); now != at {
		t.Errorf("the top line moved from %q to %q while lines arrived", at, now)
	}
}

// Following is the one case where the newest line should pull the view along.
func TestPanelFollowingStillMovesWithTheOutput(t *testing.T) {
	p := NewPanel()
	p.SetSize(60, 20)
	p.Open(PanelLogs, "logs", "", "src")

	for i := 0; i < maxPanelLines+10; i++ {
		p.Append(fmt.Sprintf("line %d", i))
	}
	p.sync()
	if !strings.Contains(p.visibleBottom(), fmt.Sprintf("line %d", maxPanelLines+9)) {
		t.Errorf("a following panel ends on the newest line, got %q", p.visibleBottom())
	}
}

// A wrapped line is several rows, and search counts lines. Landing on the row
// with the same number as the line puts the match further off the more the
// buffer wraps.
func TestPanelSearchLandsOnTheLineNotTheRow(t *testing.T) {
	p := NewPanel()
	p.SetSize(40, 20)
	p.Open(PanelText, "text", "", "src")

	long := strings.Repeat("mot ", 30)
	lines := make([]string, 0, 60)
	for i := 0; i < 30; i++ {
		lines = append(lines, long, fmt.Sprintf("court %d", i))
	}
	lines = append(lines, "AIGUILLE")
	p.Set(lines)

	p.query = "aiguille"
	p.follow = false
	p.jump(1)

	// The match is the last line, so the viewport cannot put it at the top
	// without scrolling past the end: what matters is that it is on screen.
	if !strings.Contains(p.viewport.View(), "AIGUILLE") {
		t.Errorf("search did not bring the match on screen, top is %q", p.visibleTop())
	}
}

// visibleTop and visibleBottom are what the panel is showing, for the tests.
func (p *Panel) visibleTop() string {
	rows := strings.Split(p.viewport.View(), "\n")
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			return strings.TrimSpace(row)
		}
	}
	return ""
}

func (p *Panel) visibleBottom() string {
	rows := strings.Split(p.viewport.View(), "\n")
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.TrimSpace(rows[i]) != "" {
			return strings.TrimSpace(rows[i])
		}
	}
	return ""
}

// Laying the buffer out is a pass over every line in it, so it happens once
// before something is drawn, not once per line received. A container printing
// faster than the screen refreshes must not cost a full layout per line.
func TestPanelLaysOutOncePerFrameNotOncePerLine(t *testing.T) {
	p := NewPanel()
	p.SetSize(60, 20)
	p.Open(PanelLogs, "logs", "", "src")

	start := time.Now()
	for i := 0; i < maxPanelLines*2; i++ {
		p.Append(fmt.Sprintf("line %d with enough text on it to be worth wrapping somewhere", i))
	}
	p.sync()
	took := time.Since(start)

	// Ten thousand lines through a five thousand line buffer: a layout per
	// line is quadratic and takes tens of seconds, one at the end is instant.
	if took > 2*time.Second {
		t.Errorf("filling the buffer took %s, which means it is laying out per line", took)
	}
}
