package components

import (
	"fmt"
	"testing"
)

// Output arriving while someone is reading must not yank the panel back to the
// end. This is what made the task output feel broken: a command printing every
// few milliseconds took the screen back on every line.
func TestTaskOutputStaysWhereItWasScrolledTo(t *testing.T) {
	p := NewTaskPanel()
	p.SetSize(80, 30)
	p.Start("task-1", "compose up", "docker compose up -d")
	for i := 0; i < 200; i++ {
		p.Append("task-1", fmt.Sprintf("line %d", i))
	}

	p.sync()

	// Following, so the newest line is on screen.
	if !p.viewport.AtBottom() {
		t.Fatal("a panel nobody scrolled follows the output")
	}

	p.Wheel(true)
	away := p.viewport.YOffset
	if p.follow {
		t.Error("scrolling away means reading, not following")
	}

	for i := 200; i < 260; i++ {
		p.Append("task-1", fmt.Sprintf("line %d", i))
	}
	p.sync()
	if p.viewport.YOffset != away {
		t.Errorf("offset moved from %d to %d while output arrived", away, p.viewport.YOffset)
	}

	// Scrolling back to the end picks the output up again.
	for i := 0; i < 100; i++ {
		p.Wheel(false)
	}
	if !p.follow {
		t.Error("back at the bottom is following again")
	}
	before := p.viewport.YOffset
	p.Append("task-1", "one more")
	p.sync()
	if p.viewport.YOffset == before {
		t.Error("a following panel moves with the output")
	}
}

// Reopening the panel shows the end of the output, whatever was being read
// when it was closed.
func TestTaskPanelOpensOnTheNewestOutput(t *testing.T) {
	p := NewTaskPanel()
	p.SetSize(80, 30)
	p.Start("task-1", "compose up", "docker compose up -d")
	for i := 0; i < 200; i++ {
		p.Append("task-1", fmt.Sprintf("line %d", i))
	}

	p.sync()
	p.Wheel(true)
	p.Toggle() // open
	if !p.follow || !p.viewport.AtBottom() {
		t.Error("opening the panel shows what just happened")
	}
}
