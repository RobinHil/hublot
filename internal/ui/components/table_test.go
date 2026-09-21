package components

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/ui/keys"
)

func testColumns() []Column {
	return []Column{
		{Title: "NAME", SortKey: "name", MinWidth: 10},
		{Title: "IMAGE", SortKey: "image", MinWidth: 10, Priority: 2},
		{Title: "CPU", SortKey: "cpu", Width: 6, Right: true, Priority: 1},
		{Title: "PORTS", Width: 10, Priority: 3},
	}
}

func testRows(names ...string) []Row {
	rows := make([]Row, 0, len(names))
	for _, n := range names {
		rows = append(rows, Row{ID: n, Cells: []Cell{Txt(n), Txt("img"), Txt("1%"), Txt(":80")}})
	}
	return rows
}

func press(t *Table, k keys.Map, keyType tea.KeyType, runes string) {
	t.Helper2()
	msg := tea.KeyMsg{Type: keyType}
	if runes != "" {
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(runes)}
	}
	t.Update(msg, k)
}

// Helper2 exists so press can take *Table without importing testing state.
func (t *Table) Helper2() {}

func TestTableNavigationAndScrolling(t *testing.T) {
	k := keys.Default()
	tb := NewTable(testColumns())
	tb.SetSize(100, 6) // header plus five body rows
	tb.SetRows(testRows("a", "b", "c", "d", "e", "f", "g", "h"))

	if tb.CurrentID() != "a" {
		t.Fatalf("cursor starts on the first row, got %q", tb.CurrentID())
	}

	for i := 0; i < 6; i++ {
		press(&tb, k, tea.KeyDown, "")
	}
	if tb.CurrentID() != "g" {
		t.Errorf("after six moves down: got %q, want g", tb.CurrentID())
	}
	// The viewport must have scrolled to keep the cursor visible.
	if tb.offset == 0 {
		t.Error("the viewport must follow the cursor")
	}

	press(&tb, k, tea.KeyRunes, "G")
	if tb.CurrentID() != "h" {
		t.Errorf("G goes to the last row, got %q", tb.CurrentID())
	}
	press(&tb, k, tea.KeyRunes, "g")
	if tb.CurrentID() != "a" {
		t.Errorf("g goes to the first row, got %q", tb.CurrentID())
	}

	// Moving past either end stays in range rather than panicking.
	for i := 0; i < 50; i++ {
		press(&tb, k, tea.KeyUp, "")
	}
	if tb.CurrentID() != "a" {
		t.Errorf("cursor clamps at the top, got %q", tb.CurrentID())
	}
}

func TestTableKeepsCursorOnObjectAcrossRefresh(t *testing.T) {
	k := keys.Default()
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	tb.SetRows(testRows("a", "b", "c"))
	press(&tb, k, tea.KeyDown, "")
	if tb.CurrentID() != "b" {
		t.Fatalf("cursor: got %q", tb.CurrentID())
	}

	// A refresh that reorders the list must not move the selection.
	tb.SetRows(testRows("c", "b", "a"))
	if tb.CurrentID() != "b" {
		t.Errorf("cursor must follow the object, got %q", tb.CurrentID())
	}

	// When the object disappears, the cursor stays in range.
	tb.SetRows(testRows("x", "y"))
	if id := tb.CurrentID(); id != "x" && id != "y" {
		t.Errorf("cursor must stay in range, got %q", id)
	}

	tb.SetRows(nil)
	if tb.CurrentID() != "" {
		t.Errorf("an empty table has no current row, got %q", tb.CurrentID())
	}
}

func TestTableMarkingAndTargets(t *testing.T) {
	k := keys.Default()
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	tb.SetRows(testRows("a", "b", "c"))

	// With nothing marked, an action targets the row under the cursor.
	if got := tb.Targets(); len(got) != 1 || got[0] != "a" {
		t.Errorf("targets: got %v, want [a]", got)
	}

	press(&tb, k, tea.KeyRunes, " ")
	if tb.MarkedCount() != 1 {
		t.Errorf("marked: got %d, want 1", tb.MarkedCount())
	}
	// Marking advances, so a run of rows can be marked by repeating space.
	if tb.CurrentID() != "b" {
		t.Errorf("marking advances the cursor, got %q", tb.CurrentID())
	}
	press(&tb, k, tea.KeyRunes, " ")
	if got := tb.Targets(); len(got) != 2 {
		t.Errorf("targets: got %v, want two marked rows", got)
	}

	press(&tb, k, tea.KeyRunes, "a")
	if tb.MarkedCount() != 3 {
		t.Errorf("mark all: got %d, want 3", tb.MarkedCount())
	}
	press(&tb, k, tea.KeyRunes, "a")
	if tb.MarkedCount() != 0 {
		t.Errorf("mark all again unmarks everything: got %d", tb.MarkedCount())
	}

	tb.marked["a"] = true
	tb.ClearMarks()
	if tb.MarkedCount() != 0 {
		t.Error("ClearMarks unmarks everything")
	}
}

func TestTableMarkAllRespectsFilter(t *testing.T) {
	k := keys.Default()
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	// The owning view filters the data; the table only ever sees what is left.
	tb.SetRows(testRows("web-1", "web-2"))
	press(&tb, k, tea.KeyRunes, "a")

	if tb.MarkedCount() != 2 {
		t.Fatalf("marked: got %d, want 2", tb.MarkedCount())
	}
	// A row outside the filter was never marked.
	tb.SetRows(testRows("web-1", "web-2", "db-1"))
	if tb.marked["db-1"] {
		t.Error("mark all only touches rows the filter left visible")
	}
}

func TestTableFilterInputSwallowsKeys(t *testing.T) {
	k := keys.Default()
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	tb.SetRows(testRows("a", "b", "c"))

	press(&tb, k, tea.KeyRunes, "/")
	if !tb.Filtering() {
		t.Fatal("slash starts filtering")
	}

	// While typing, "j" is text, not a cursor move.
	press(&tb, k, tea.KeyRunes, "j")
	if tb.CurrentID() != "a" {
		t.Error("navigation keys are text while filtering")
	}
	if tb.Query() != "j" {
		t.Errorf("query: got %q, want j", tb.Query())
	}

	press(&tb, k, tea.KeyEnter, "")
	if tb.Filtering() {
		t.Error("enter leaves the input but keeps the query")
	}
	if tb.Query() != "j" {
		t.Errorf("query after enter: got %q", tb.Query())
	}

	press(&tb, k, tea.KeyRunes, "/")
	press(&tb, k, tea.KeyEsc, "")
	if tb.Query() != "" || tb.Filtering() {
		t.Errorf("escape clears the filter: query=%q filtering=%v", tb.Query(), tb.Filtering())
	}
}

func TestTableSortCycling(t *testing.T) {
	k := keys.Default()
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	tb.SetRows(testRows("a"))

	if col, asc := tb.SortKey(); col != "name" || !asc {
		t.Fatalf("initial sort: got %q asc=%v", col, asc)
	}

	press(&tb, k, tea.KeyRunes, "s")
	if col, _ := tb.SortKey(); col != "image" {
		t.Errorf("after one cycle: got %q, want image", col)
	}
	press(&tb, k, tea.KeyRunes, "s")
	if col, _ := tb.SortKey(); col != "cpu" {
		t.Errorf("after two cycles: got %q, want cpu", col)
	}
	// PORTS has no sort key, so cycling skips it and wraps to NAME.
	press(&tb, k, tea.KeyRunes, "s")
	if col, _ := tb.SortKey(); col != "name" {
		t.Errorf("cycling skips unsortable columns: got %q", col)
	}

	press(&tb, k, tea.KeyRunes, "A")
	if _, asc := tb.SortKey(); asc {
		t.Error("A reverses the direction")
	}

	tb.SetSortKey("cpu", false)
	if col, asc := tb.SortKey(); col != "cpu" || asc {
		t.Errorf("SetSortKey: got %q asc=%v", col, asc)
	}
	tb.SetSortKey("nonexistent", true)
	if col, _ := tb.SortKey(); col != "cpu" {
		t.Error("an unknown sort key leaves the current one alone")
	}
}

func TestTableDropsLowPriorityColumnsWhenNarrow(t *testing.T) {
	tb := NewTable(testColumns())
	tb.SetRows(testRows("a"))

	tb.SetSize(120, 10)
	wide := tb.layout()
	for i, w := range wide {
		if w == 0 {
			t.Errorf("column %d must be shown on a wide terminal", i)
		}
	}

	tb.SetSize(40, 10)
	narrow := tb.layout()
	if narrow[0] == 0 {
		t.Error("a priority 0 column is never dropped")
	}
	if narrow[3] != 0 {
		t.Error("the lowest priority column goes first")
	}

	// At 20 columns NAME and CPU still fit together, so only the two higher
	// priorities were dropped.
	tb.SetSize(20, 10)
	tighter := tb.layout()
	if tighter[0] == 0 || tighter[2] == 0 {
		t.Errorf("what fits is kept: %v", tighter)
	}
	if tighter[1] != 0 || tighter[3] != 0 {
		t.Errorf("priorities 2 and 3 go first: %v", tighter)
	}

	// Narrower still, and everything droppable goes.
	tb.SetSize(14, 10)
	minimal := tb.layout()
	if minimal[0] == 0 {
		t.Error("the mandatory column survives at any width")
	}
	if minimal[1] != 0 || minimal[2] != 0 || minimal[3] != 0 {
		t.Errorf("everything droppable goes at 14 columns: %v", minimal)
	}
}

func TestTableRendersHeaderRowsAndGroups(t *testing.T) {
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	tb.SetRows([]Row{
		{ID: "1", Group: "blog", Cells: []Cell{Txt("blog-web"), Txt("nginx"), Txt("2%"), Txt(":80")}},
		{ID: "2", Group: "blog", Cells: []Cell{Txt("blog-db"), Txt("pg"), Txt("1%"), Txt("")}},
		{ID: "3", Group: "shop", Cells: []Cell{Txt("shop-api"), Txt("go"), Txt("5%"), Txt("")}},
	})

	out := tb.View()
	if !strings.Contains(out, "NAME") {
		t.Error("the header is always rendered")
	}
	if strings.Count(out, "blog") < 1 || !strings.Contains(out, "shop") {
		t.Error("group headings are rendered")
	}
	// The heading prints once for consecutive rows of the same group.
	if got := strings.Count(out, "blog\n"); got != 1 {
		t.Errorf("group heading printed %d times, want once", got)
	}
}

func TestTableEmptyMessage(t *testing.T) {
	tb := NewTable(testColumns())
	tb.SetSize(100, 10)
	tb.Empty = "no containers"
	tb.SetRows(nil)

	if !strings.Contains(tb.View(), "no containers") {
		t.Error("an empty table explains itself")
	}
	if tb.Scrollbar() != "" {
		t.Error("an empty table has no position indicator")
	}
}

func TestPadTruncatesRatherThanOverflowing(t *testing.T) {
	if got := pad("abc", 6, false); got != "abc   " {
		t.Errorf("pad left: %q", got)
	}
	if got := pad("abc", 6, true); got != "   abc" {
		t.Errorf("pad right: %q", got)
	}
	if got := pad("abcdefgh", 4, false); got != "abc." {
		t.Errorf("pad truncates with a marker: %q", got)
	}
	if got := pad("abc", 0, false); got != "" {
		t.Errorf("zero width renders nothing: %q", got)
	}
	// A multi-byte value must be cut on runes, not bytes.
	if got := pad("aeiouyaeiouy", 5, false); len([]rune(got)) != 5 {
		t.Errorf("truncation counts runes: %q", got)
	}
}

func TestTableNeverRendersTallerThanItsHeight(t *testing.T) {
	tb := NewTable(testColumns())
	tb.SetSize(100, 8)
	tb.SetRows(testRows("a", "b", "c", "d", "e", "f", "g", "h", "i", "j"))

	if got := strings.Count(tb.View(), "\n") + 1; got > 8 {
		t.Errorf("the table rendered %d lines into a height of 8", got)
	}
}

func TestTableGroupHeadingsCountAgainstTheHeight(t *testing.T) {
	grouped := func(group, id string) Row {
		return Row{ID: id, Group: group, Cells: []Cell{Txt(id), Txt("img"), Txt("1%"), Txt("")}}
	}

	tb := NewTable(testColumns())
	// One line for the column header, six for the body.
	tb.SetSize(100, 7)
	tb.SetRows([]Row{
		grouped("blog", "blog-web"),
		grouped("blog", "blog-db"),
		grouped("shop", "shop-api"),
		grouped("shop", "shop-worker"),
		grouped("wiki", "wiki-web"),
		grouped("wiki", "wiki-db"),
	})

	// Six rows plus three headings do not fit in six lines, so fewer rows show.
	if got := tb.rowsFitting(0); got != 4 {
		t.Errorf("rows fitting: got %d, want 4 (two headings take two lines)", got)
	}
	if got := strings.Count(tb.View(), "\n") + 1; got > 7 {
		t.Errorf("a grouped table rendered %d lines into a height of 7", got)
	}
}

func TestTableScrollsThroughAGroupedList(t *testing.T) {
	k := keys.Default()
	grouped := func(group, id string) Row {
		return Row{ID: id, Group: group, Cells: []Cell{Txt(id), Txt("img"), Txt("1%"), Txt("")}}
	}

	tb := NewTable(testColumns())
	tb.SetSize(100, 5)
	tb.SetRows([]Row{
		grouped("blog", "blog-web"),
		grouped("blog", "blog-db"),
		grouped("shop", "shop-api"),
		grouped("shop", "shop-worker"),
		grouped("wiki", "wiki-web"),
	})

	// Walking to the last row must scroll it into view rather than leave the
	// cursor below the window.
	for i := 0; i < 4; i++ {
		press(&tb, k, tea.KeyDown, "")
	}
	if tb.CurrentID() != "wiki-web" {
		t.Fatalf("cursor: got %q, want wiki-web", tb.CurrentID())
	}

	visible := tb.VisibleIDs()
	found := false
	for _, id := range visible {
		if id == "wiki-web" {
			found = true
		}
	}
	if !found {
		t.Errorf("the cursor row must be inside the window, window is %v", visible)
	}
	if got := strings.Count(tb.View(), "\n") + 1; got > 5 {
		t.Errorf("scrolled grouped table rendered %d lines into a height of 5", got)
	}
}

func TestPadMeasuresStyledTextByItsDisplayWidth(t *testing.T) {
	// Styling is invisible but counts as bytes: cutting it as plain text would
	// stop far too early and could split an escape sequence. pad is where
	// every cell of every table passes, so it is where this has to hold.
	styled := lipgloss.NewStyle().Bold(true).Render("abcdefghij")

	got := pad(styled, 6, false)
	if w := lipgloss.Width(got); w != 6 {
		t.Errorf("display width: got %d, want 6 (%q)", w, got)
	}
	if !strings.Contains(got, "abcde") {
		t.Errorf("the visible text must survive: %q", got)
	}

	// And a cell that fits is left exactly as it was.
	short := lipgloss.NewStyle().Bold(true).Render("ab")
	if got := pad(short, 4, false); !strings.Contains(got, "ab") {
		t.Errorf("a short styled cell must survive: %q", got)
	}
}

func TestPadSanitisesWhatTheDaemonReported(t *testing.T) {
	// Container names, image references and statuses all come from the daemon
	// and all arrive here. A cursor movement in one would redraw the table.
	got := pad("na\x1b[2Jme", 10, false)
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("an escape reached a table cell: %q", got)
	}
	if !strings.Contains(got, "name") {
		t.Errorf("the text itself must survive: %q", got)
	}
}

// The wheel moves the window, not the cursor. A list that sits still until the
// cursor reaches an edge and then jumps is what reads as broken scrolling.
func TestWheelMovesTheWindowNotTheCursor(t *testing.T) {
	tbl := NewTable([]Column{{Title: "NAME", MinWidth: 10}})
	tbl.SetSize(40, 10)

	rows := make([]Row, 50)
	for i := range rows {
		rows[i] = Row{ID: strconv.Itoa(i), Cells: []Cell{Txt(strconv.Itoa(i))}}
	}
	tbl.SetRows(rows)

	tbl.scroll(3)
	if tbl.offset != 3 {
		t.Errorf("offset = %d, want the window moved by 3", tbl.offset)
	}
	// The cursor was on row 0, which the window has left behind, so it comes
	// along to the first visible row and no further.
	if tbl.cursor != 3 {
		t.Errorf("cursor = %d, want it dragged to the top of the window", tbl.cursor)
	}

	// Scrolling back up leaves the cursor where it is: it is still visible.
	tbl.scroll(-1)
	if tbl.offset != 2 {
		t.Errorf("offset = %d, want 2", tbl.offset)
	}
	if tbl.cursor != 3 {
		t.Errorf("cursor = %d, want it left alone while it is on screen", tbl.cursor)
	}
}

// Scrolling stops at the end rather than running off into blank space.
func TestWheelStopsOnTheLastRow(t *testing.T) {
	tbl := NewTable([]Column{{Title: "NAME", MinWidth: 10}})
	tbl.SetSize(40, 10)

	rows := make([]Row, 20)
	for i := range rows {
		rows[i] = Row{ID: strconv.Itoa(i), Cells: []Cell{Txt(strconv.Itoa(i))}}
	}
	tbl.SetRows(rows)

	tbl.scroll(500)
	if tbl.offset != tbl.maxOffset() {
		t.Errorf("offset = %d, want it pinned to %d", tbl.offset, tbl.maxOffset())
	}
	if last := tbl.offset + tbl.rowsFitting(tbl.offset); last < len(rows) {
		t.Errorf("the last row has to be on screen: window ends at %d of %d", last, len(rows))
	}
	if tbl.cursor >= len(rows) {
		t.Errorf("cursor = %d, off the end of %d rows", tbl.cursor, len(rows))
	}
}

// An empty list is scrolled like any other, and must not index into nothing.
func TestWheelOnAnEmptyList(t *testing.T) {
	tbl := NewTable([]Column{{Title: "NAME", MinWidth: 10}})
	tbl.SetSize(40, 10)
	tbl.scroll(3)
	tbl.scroll(-3)
}

// When everything fits there is no window to move, and a wheel that does
// nothing reads as a wheel that is broken: it moves the selection instead.
func TestWheelMovesTheSelectionWhenNothingCanScroll(t *testing.T) {
	tbl := NewTable([]Column{{Title: "NAME", MinWidth: 10}})
	tbl.SetSize(40, 20)

	rows := make([]Row, 5)
	for i := range rows {
		rows[i] = Row{ID: strconv.Itoa(i), Cells: []Cell{Txt(strconv.Itoa(i))}}
	}
	tbl.SetRows(rows)

	if tbl.maxOffset() != 0 {
		t.Fatal("five rows in twenty fit, so there is nothing to scroll")
	}
	tbl.scroll(3)
	if tbl.cursor != 3 {
		t.Errorf("cursor = %d, want the wheel to have moved it", tbl.cursor)
	}
	if tbl.offset != 0 {
		t.Errorf("offset = %d, want the window left alone", tbl.offset)
	}
}

// The cursor keeps a few rows of context ahead of it, so a long list starts
// moving before the cursor reaches the edge rather than lurching at the end.
func TestCursorKeepsContextAhead(t *testing.T) {
	tbl := NewTable([]Column{{Title: "NAME", MinWidth: 10}})
	tbl.SetSize(40, 12)

	rows := make([]Row, 100)
	for i := range rows {
		rows[i] = Row{ID: strconv.Itoa(i), Cells: []Cell{Txt(strconv.Itoa(i))}}
	}
	tbl.SetRows(rows)

	window := tbl.rowsFitting(0)
	for i := 0; i < window; i++ {
		tbl.move(1)
	}

	// The window moved before the cursor could reach the last row.
	if tbl.offset == 0 {
		t.Error("the list has to start moving before the cursor hits the edge")
	}
	if gap := tbl.offset + tbl.rowsFitting(tbl.offset) - 1 - tbl.cursor; gap < 1 {
		t.Errorf("only %d rows of context past the cursor", gap)
	}
}
