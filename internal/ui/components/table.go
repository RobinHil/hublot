// Package components holds the reusable widgets the views are assembled from.
package components

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// gutter is the space between two columns.
const gutter = 2

// Column describes one column of the generic table. Every view parameterises
// the same component rather than growing its own (AGENTS.md section 9.1).
type Column struct {
	// Title is the header text.
	Title string
	// SortKey names the comparison the owning view applies when this column is
	// the sort column. An empty key makes the column unsortable.
	SortKey string
	// Width is a fixed width in cells. Zero means the column flexes.
	Width int
	// MinWidth is the narrowest the column may be squeezed to when flexing.
	MinWidth int
	// Priority decides what is dropped first on a narrow terminal: higher goes
	// first. Priority 0 columns are never dropped.
	Priority int
	// Right aligns the cell content to the right, for sizes and percentages.
	Right bool
}

// Cell is one rendered value. Styling is kept separate from the text so column
// widths can be computed without counting escape sequences.
type Cell struct {
	Text  string
	Style *lipgloss.Style
}

// Txt is a plain cell.
func Txt(s string) Cell { return Cell{Text: s} }

// Styled is a cell rendered in the given style.
func Styled(s string, st lipgloss.Style) Cell { return Cell{Text: s, Style: &st} }

// Row is one line of the table.
type Row struct {
	// ID identifies the underlying object, and is what marking records, so
	// selection survives a refresh that reorders the list.
	ID    string
	Cells []Cell
	// Group, when set, prints a heading above the row. Consecutive rows
	// sharing a group print it once. This is what lets the Compose view show
	// services under their project without a second table component.
	Group string
	// Warn styles the whole row as a warning, used for Compose-owned objects
	// in prune previews (AGENTS.md section 10.3).
	Warn bool
	// Dim styles the row as inactive, used for stopped containers.
	Dim bool
}

// Table is a sortable, filterable, multi-selectable list with a fixed header.
type Table struct {
	columns []Column
	rows    []Row

	cursor int
	offset int
	marked map[string]bool

	sortIndex int
	sortAsc   bool

	filtering bool
	input     textinput.Model

	width  int
	height int

	// Empty is shown when there is nothing to list.
	Empty string
}

// NewTable builds a table for the given columns.
func NewTable(columns []Column) Table {
	in := textinput.New()
	in.Prompt = "filter: "
	in.CharLimit = 80

	return Table{
		columns: columns,
		marked:  map[string]bool{},
		sortAsc: true,
		input:   in,
		Empty:   "nothing here",
	}
}

// SetRows replaces the visible rows, keeping the cursor on the same object when
// it is still there, so a background refresh does not move the selection.
func (t *Table) SetRows(rows []Row) {
	var focused string
	if r, ok := t.Current(); ok {
		focused = r.ID
	}

	t.rows = rows

	if focused != "" {
		for i, r := range rows {
			if r.ID == focused {
				t.cursor = i
				t.clampCursor()
				return
			}
		}
	}
	t.clampCursor()
}

// SetSize records the space the table may use.
func (t *Table) SetSize(width, height int) {
	t.width, t.height = width, height
	t.clampCursor()
}

// Rows returns the rows currently displayed.
func (t *Table) Rows() []Row { return t.rows }

// Current returns the row under the cursor.
func (t *Table) Current() (Row, bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return Row{}, false
	}
	return t.rows[t.cursor], true
}

// CurrentID returns the id under the cursor, or "".
func (t *Table) CurrentID() string {
	r, ok := t.Current()
	if !ok {
		return ""
	}
	return r.ID
}

// Targets returns what an action applies to: every marked row, or the row under
// the cursor when nothing is marked.
func (t *Table) Targets() []string {
	var out []string
	for _, r := range t.rows {
		if t.marked[r.ID] {
			out = append(out, r.ID)
		}
	}
	if len(out) > 0 {
		return out
	}
	if id := t.CurrentID(); id != "" {
		return []string{id}
	}
	return nil
}

// MarkedCount is how many rows are marked, for the status bar.
func (t *Table) MarkedCount() int {
	n := 0
	for _, r := range t.rows {
		if t.marked[r.ID] {
			n++
		}
	}
	return n
}

// ClearMarks unmarks everything.
func (t *Table) ClearMarks() { t.marked = map[string]bool{} }

// SortKey is the column the view should sort by, and the direction.
func (t *Table) SortKey() (string, bool) {
	if t.sortIndex < 0 || t.sortIndex >= len(t.columns) {
		return "", t.sortAsc
	}
	return t.columns[t.sortIndex].SortKey, t.sortAsc
}

// SetSortKey selects the sort column by key, used to restore a config default.
func (t *Table) SetSortKey(key string, asc bool) {
	for i, c := range t.columns {
		if c.SortKey == key && key != "" {
			t.sortIndex, t.sortAsc = i, asc
			return
		}
	}
}

// Filtering reports whether the filter input has focus.
func (t *Table) Filtering() bool { return t.filtering }

// Query is the active filter text.
func (t *Table) Query() string { return t.input.Value() }

// StopFiltering drops focus but keeps the query, so results stay filtered while
// the keys go back to the view.
func (t *Table) StopFiltering() {
	t.filtering = false
	t.input.Blur()
}

// ClearFilter drops the query entirely.
func (t *Table) ClearFilter() {
	t.StopFiltering()
	t.input.SetValue("")
}

// Update handles navigation, marking, sorting and the filter input. It returns
// true when it consumed the message, so the view can skip its own bindings.
func (t *Table) Update(msg tea.Msg, k keys.Map) (tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}

	// While typing a filter, only escape and enter are commands: everything
	// else is text.
	if t.filtering {
		switch {
		case key.Matches(km, k.Global.Escape):
			t.ClearFilter()
			return nil, true
		case km.Type == tea.KeyEnter:
			t.StopFiltering()
			return nil, true
		}
		var cmd tea.Cmd
		t.input, cmd = t.input.Update(msg)
		return cmd, true
	}

	switch {
	case key.Matches(km, k.Global.Up):
		t.move(-1)
	case key.Matches(km, k.Global.Down):
		t.move(1)
	case key.Matches(km, k.Global.PageUp):
		t.move(-t.visibleRows())
	case key.Matches(km, k.Global.PageDown):
		t.move(t.visibleRows())
	case key.Matches(km, k.Global.Home):
		t.cursor = 0
		t.clampCursor()
	case key.Matches(km, k.Global.End):
		t.cursor = len(t.rows) - 1
		t.clampCursor()
	case key.Matches(km, k.Global.Filter):
		t.filtering = true
		t.input.Focus()
		return textinput.Blink, true
	case key.Matches(km, k.Global.Sort):
		t.cycleSort()
	case key.Matches(km, k.Global.SortRev):
		t.sortAsc = !t.sortAsc
	case key.Matches(km, k.Global.Mark):
		if id := t.CurrentID(); id != "" {
			t.marked[id] = !t.marked[id]
			t.move(1)
		}
	case key.Matches(km, k.Global.MarkAll):
		// Marking all respects the active filter: only visible rows are hit.
		all := t.MarkedCount() == len(t.rows) && len(t.rows) > 0
		for _, r := range t.rows {
			t.marked[r.ID] = !all
		}
	default:
		return nil, false
	}
	return nil, true
}

// cycleSort moves to the next sortable column, wrapping around.
func (t *Table) cycleSort() {
	for i := 1; i <= len(t.columns); i++ {
		next := (t.sortIndex + i) % len(t.columns)
		if t.columns[next].SortKey != "" {
			t.sortIndex = next
			return
		}
	}
}

func (t *Table) move(delta int) {
	t.cursor += delta
	t.clampCursor()
}

func (t *Table) clampCursor() {
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}

	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.offset < 0 {
		t.offset = 0
	}
	// How many rows fit depends on the headings between them, so the offset is
	// advanced one row at a time until the cursor is inside the window.
	for t.offset < t.cursor && t.cursor >= t.offset+t.rowsFitting(t.offset) {
		t.offset++
	}
}

// bodyBudget is how many lines the rows may occupy: the height the frame gave
// the table, less the column header and the filter line when it shows.
func (t *Table) bodyBudget() int {
	n := t.height - 1
	if t.filtering || t.input.Value() != "" {
		n--
	}
	if n < 0 {
		return 0
	}
	return n
}

// rowsFitting is how many rows render from start without overflowing the
// budget. Group headings take a line of their own, so a grouped list shows
// fewer rows than a flat one and the frame stays exactly as tall as the
// terminal.
func (t *Table) rowsFitting(start int) int {
	budget := t.bodyBudget()
	if start < 0 || start >= len(t.rows) {
		return 0
	}

	lastGroup := ""
	if start > 0 && t.rows[start].Group != "" {
		// The heading the viewport scrolled past is repeated at the top.
		budget--
		lastGroup = t.rows[start].Group
	}

	n := 0
	for i := start; i < len(t.rows) && budget > 0; i++ {
		if g := t.rows[i].Group; g != "" && g != lastGroup {
			budget--
			lastGroup = g
			if budget <= 0 {
				break
			}
		}
		budget--
		n++
	}
	return n
}

// visibleRows is how many rows are on screen right now, used for paging.
func (t *Table) visibleRows() int {
	if n := t.rowsFitting(t.offset); n > 0 {
		return n
	}
	return 1
}

// View renders header, rows and group headings.
func (t *Table) View() string {
	s := theme.Current()
	widths := t.layout()

	var b strings.Builder

	if t.filtering || t.input.Value() != "" {
		b.WriteString(s.Accent.Render(t.input.View()))
		b.WriteString("\n")
	}

	b.WriteString(s.Header.Render(t.header(widths)))

	if len(t.rows) == 0 {
		b.WriteString("\n")
		b.WriteString(s.Dim.Render("  " + t.Empty))
		return b.String()
	}

	end := t.offset + t.rowsFitting(t.offset)
	if end > len(t.rows) {
		end = len(t.rows)
	}

	lastGroup := ""
	if t.offset > 0 {
		// Repeat the group heading the viewport scrolled past, so a row is
		// never shown without knowing which project it belongs to.
		lastGroup = t.rows[t.offset].Group
		if lastGroup != "" {
			b.WriteString("\n")
			b.WriteString(s.Title.Render(lastGroup) + s.Dim.Render(" (continued)"))
		}
	}

	for i := t.offset; i < end; i++ {
		r := t.rows[i]
		if r.Group != "" && r.Group != lastGroup {
			b.WriteString("\n")
			b.WriteString(s.Title.Render(r.Group))
			lastGroup = r.Group
		}
		b.WriteString("\n")
		b.WriteString(t.renderRow(r, i == t.cursor, widths))
	}

	return b.String()
}

// Scrollbar renders the position indicator for the status bar.
func (t *Table) Scrollbar() string {
	if len(t.rows) == 0 {
		return ""
	}
	return strconv.Itoa(t.cursor+1) + "/" + strconv.Itoa(len(t.rows))
}

func (t *Table) header(widths []int) string {
	cells := make([]string, 0, len(widths))
	for i, w := range widths {
		if w == 0 {
			continue
		}
		title := t.columns[i].Title
		if i == t.sortIndex && t.columns[i].SortKey != "" {
			marker := " ^"
			if !t.sortAsc {
				marker = " v"
			}
			title += marker
		}
		cells = append(cells, pad(title, w, t.columns[i].Right))
	}
	return "  " + strings.Join(cells, strings.Repeat(" ", gutter))
}

func (t *Table) renderRow(r Row, selected bool, widths []int) string {
	s := theme.Current()

	cells := make([]string, 0, len(widths))
	for i, w := range widths {
		if w == 0 || i >= len(r.Cells) {
			if w > 0 {
				cells = append(cells, strings.Repeat(" ", w))
			}
			continue
		}
		c := r.Cells[i]
		text := pad(c.Text, w, t.columns[i].Right)
		// A selected row is painted whole, so per-cell colours are dropped to
		// keep the highlight readable.
		if c.Style != nil && !selected {
			text = c.Style.Render(text)
		}
		cells = append(cells, text)
	}

	prefix := "  "
	if t.marked[r.ID] {
		prefix = s.Accent.Render("* ")
	}
	if selected {
		prefix = s.Accent.Render("> ")
	}

	line := strings.Join(cells, strings.Repeat(" ", gutter))
	switch {
	case selected:
		line = s.RowSelected.Render(line)
	case r.Warn:
		line = s.Warning.Render(line)
	case r.Dim:
		line = s.Dim.Render(line)
	}
	return prefix + line
}

// layout computes per-column widths for the current terminal width, dropping
// low-priority columns when there is not enough room. A dropped column gets
// width 0 (AGENTS.md section 9.1).
func (t *Table) layout() []int {
	// Two cells for the cursor prefix.
	avail := t.width - 2
	if avail < 0 {
		avail = 0
	}

	dropped := make([]bool, len(t.columns))
	for {
		widths, total := t.tryLayout(dropped, avail)
		if total <= avail || !t.dropOne(dropped) {
			return widths
		}
	}
}

// tryLayout sizes the columns that survive, sharing spare room between the
// flexible ones.
func (t *Table) tryLayout(dropped []bool, avail int) ([]int, int) {
	widths := make([]int, len(t.columns))

	fixed, flex, count := 0, 0, 0
	for i, c := range t.columns {
		if dropped[i] {
			continue
		}
		count++
		if c.Width > 0 {
			widths[i] = c.Width
			fixed += c.Width
		} else {
			flex++
		}
	}
	if count == 0 {
		return widths, 0
	}

	gutters := (count - 1) * gutter
	spare := avail - fixed - gutters

	if flex > 0 {
		each := spare / flex
		for i, c := range t.columns {
			if dropped[i] || c.Width > 0 {
				continue
			}
			w := each
			if w < c.MinWidth {
				w = c.MinWidth
			}
			widths[i] = w
		}
	}

	total := gutters
	for _, w := range widths {
		total += w
	}
	return widths, total
}

// dropOne removes the least important column still standing, reporting whether
// anything could be dropped.
func (t *Table) dropOne(dropped []bool) bool {
	worst, worstPriority := -1, 0
	for i, c := range t.columns {
		if dropped[i] || c.Priority == 0 {
			continue
		}
		if c.Priority > worstPriority {
			worst, worstPriority = i, c.Priority
		}
	}
	if worst < 0 {
		return false
	}
	dropped[worst] = true
	return true
}

// pad truncates or pads a value to an exact width.
func pad(s string, w int, right bool) string {
	if w <= 0 {
		return ""
	}
	l := lipgloss.Width(s)
	if l > w {
		if w == 1 {
			return "."
		}
		return truncate(s, w-1) + "."
	}
	fill := strings.Repeat(" ", w-l)
	if right {
		return fill + s
	}
	return s + fill
}

// truncate cuts a string to a display width, counting runes rather than bytes.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	return string(runes[:w])
}

// visibleWidth measures a rendered string, ignoring escape sequences.
func visibleWidth(s string) int { return lipgloss.Width(s) }

// truncateToWidth shortens a rendered string that would wrap. It measures and
// cuts by display width rather than by runes: these strings carry styling, and
// cutting them as plain text both stops too early and can leave half an escape
// sequence on screen.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}

// VisibleIDs are the rows currently inside the viewport. The app streams stats
// for these first when the concurrent stream cap is reached
// (AGENTS.md section 6.3).
func (t *Table) VisibleIDs() []string {
	if len(t.rows) == 0 {
		return nil
	}
	end := t.offset + t.rowsFitting(t.offset)
	if end > len(t.rows) {
		end = len(t.rows)
	}

	out := make([]string, 0, end-t.offset)
	for i := t.offset; i < end; i++ {
		out = append(out, t.rows[i].ID)
	}
	return out
}
