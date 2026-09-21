package ui

import "strconv"

// How the screen is divided, decided in one place and tested without a
// terminal. Everything the frame draws asks this rather than doing its own
// arithmetic, which is what kept the hint bar pinned to the bottom before and
// what keeps the panel honest now.

// PanelShare is how much of the screen the side panel takes when it is open,
// as a percentage. It is a percentage rather than a handful of named fractions
// because the divider can be dragged: the key cycles through the useful stops,
// the mouse lands wherever it likes, and both end up in the same field.
type PanelShare int

const (
	// ShareThird leaves the list most of the screen.
	ShareThird PanelShare = 33
	// ShareHalf splits it evenly.
	ShareHalf PanelShare = 50
	// ShareTwoThirds is for reading, with the list kept for context.
	ShareTwoThirds PanelShare = 66
	// ShareFull hides the list entirely.
	ShareFull PanelShare = 100
	// shareMin is as small as dragging may make the panel before it is not
	// worth drawing; the floors below decide the rest.
	shareMin PanelShare = 10
	// shareDragMax stops short of full: dragging the divider off the edge
	// would leave no list and no divider to drag back. W still goes full.
	shareDragMax PanelShare = 90
)

// Next cycles to the following stop, wrapping round.
func (s PanelShare) Next() PanelShare {
	switch {
	case s < ShareThird:
		return ShareThird
	case s < ShareHalf:
		return ShareHalf
	case s < ShareTwoThirds:
		return ShareTwoThirds
	case s < ShareFull:
		return ShareFull
	default:
		return ShareThird
	}
}

// Clamp keeps a dragged share inside what can actually be drawn.
func (s PanelShare) Clamp() PanelShare {
	if s < shareMin {
		return shareMin
	}
	if s > ShareFull {
		return ShareFull
	}
	return s
}

// Label names the share for the status bar. The familiar fractions keep their
// names, since those are what the key cycles through.
func (s PanelShare) Label() string {
	switch s {
	case ShareThird:
		return "1/3"
	case ShareHalf:
		return "1/2"
	case ShareTwoThirds:
		return "2/3"
	case ShareFull:
		return "full"
	default:
		return strconv.Itoa(int(s)) + "%"
	}
}

// Minimums below which the interface says so rather than drawing nonsense.
const (
	minWidth  = 40
	minHeight = 9
	// sideBySideWidth is the narrowest screen where a list and a panel can both
	// hold useful columns; under it the panel goes below the list instead.
	sideBySideWidth = 96
	// stackedHeight is the shortest screen where stacking still leaves rows
	// worth reading in both halves.
	stackedHeight = 20
	// listFloor is the narrowest a list may be squeezed to beside a panel.
	listFloor = 34
	// panelFloor is the narrowest a panel is worth drawing.
	panelFloor = 30
)

// Layout is the resolved geometry of one frame.
type Layout struct {
	Width, Height int

	// TooSmall means nothing is drawn but an explanation.
	TooSmall bool

	// HeaderRows is how much of the dashboard fits: three with a blank row
	// between them, two with the history graphs, or one with the essentials.
	HeaderRows int
	// Compact drops the tab titles down to their numbers and shortens labels:
	// a narrow screen spends its width on data.
	Compact bool
	// ShowTabs and ShowMessage come off first when the screen is short.
	ShowTabs    bool
	ShowMessage bool
	// HeaderGap puts a blank row above and below the tab bar. Six labels, two
	// rows of figures and a table heading stacked with nothing between them
	// read as one block, and the eye has to work out where each part ends.
	// Rows are only spent on this when there are rows to spare.
	HeaderGap bool

	// ListWidth and ListHeight are the space the active view gets.
	ListWidth, ListHeight int

	// PanelOpen is whether the panel is drawn at all, which is not the same as
	// whether the user opened it: a screen can be too small for both.
	PanelOpen bool
	// PanelStacked puts the panel under the list rather than beside it.
	PanelStacked            bool
	PanelWidth, PanelHeight int
	ListHidden              bool
}

// Compute resolves the geometry for a size, whether the panel is open and how
// much of the screen it was asked to take.
func Compute(width, height int, panelOpen bool, share PanelShare) Layout {
	l := Layout{Width: width, Height: height}

	if width < minWidth || height < minHeight {
		l.TooSmall = true
		return l
	}

	l.Compact = width < 72
	// The second header row carries the history graphs, which are the first
	// thing worth giving up when rows are scarce.
	l.HeaderRows = 1
	if height >= 18 && width >= 80 {
		l.HeaderRows = 2
	}
	// The tab bar is the first thing to go: the numbers still switch views and
	// the status bar says where you are.
	l.ShowTabs = height >= 12
	// Then the transient message line, whose content also reaches the status
	// bar when something fails.
	l.ShowMessage = height >= 14
	l.HeaderGap = l.ShowTabs && height >= 24
	// The same room buys a blank row inside the header itself, between what
	// the session is and what it is doing.
	if l.HeaderGap && l.HeaderRows == 2 {
		l.HeaderRows = 3
	}

	chrome := l.HeaderRows
	if l.ShowTabs {
		chrome++
	}
	if l.HeaderGap {
		chrome += 2
	}
	if l.ShowMessage {
		chrome++
	}
	chrome++ // hint bar

	body := height - chrome
	if body < 3 {
		body = 3
	}

	l.ListWidth, l.ListHeight = width, body

	if !panelOpen {
		return l
	}
	l.PanelOpen = true

	if share >= ShareFull || width < sideBySideWidth && height < stackedHeight {
		// Nothing useful can share this screen: the panel takes it.
		l.ListHidden = true
		l.PanelWidth, l.PanelHeight = width, body
		return l
	}

	if width < sideBySideWidth {
		// Beside would leave two useless columns, so it goes below.
		l.PanelStacked = true
		l.PanelHeight = panelRows(body, share)
		l.ListHeight = body - l.PanelHeight
		l.PanelWidth = width
		return l
	}

	l.PanelWidth = panelColumns(width, share)
	l.ListWidth = width - l.PanelWidth
	l.PanelHeight = body
	return l
}

// TabRow is the screen row the tab bar sits on, and ContentRow the first row
// under it. A click has to land on the row the renderer used, so both read
// this rather than each adding up the chrome again.
func (l Layout) TabRow() int {
	if l.HeaderGap {
		return l.HeaderRows + 1
	}
	return l.HeaderRows
}

// ContentRow is the first row the active view draws on.
func (l Layout) ContentRow() int {
	row := l.HeaderRows
	if l.HeaderGap {
		row += 2
	}
	if l.ShowTabs {
		row++
	}
	return row
}

// ShareAtColumn is the share that puts the divider under this column, and
// ShareAtRow the same for a stacked panel. The drag reads these rather than
// doing the arithmetic where the mouse is handled.
func (l Layout) ShareAtColumn(x int) PanelShare {
	if l.Width <= 0 {
		return ShareThird
	}
	return dragShare((l.Width - x) * 100 / l.Width)
}

// ShareAtRow is the stacked equivalent, measured inside the body rather than
// the screen: the header and the hint bar are not the panel's to take.
func (l Layout) ShareAtRow(y int) PanelShare {
	body := l.ListHeight + l.PanelHeight
	if body <= 0 {
		return ShareThird
	}
	return dragShare((body - (y - l.ContentRow())) * 100 / body)
}

func dragShare(percent int) PanelShare {
	s := PanelShare(percent).Clamp()
	if s > shareDragMax {
		return shareDragMax
	}
	return s
}

// panelColumns splits the width, keeping both sides above their floor.
func panelColumns(width int, share PanelShare) int {
	if share >= ShareFull {
		return width
	}
	panel := width * int(share.Clamp()) / 100

	if panel < panelFloor {
		panel = panelFloor
	}
	if width-panel < listFloor {
		panel = width - listFloor
	}
	if panel < 0 {
		panel = 0
	}
	return panel
}

// panelRows splits the height when stacked, leaving the list at least three
// rows and a header.
func panelRows(body int, share PanelShare) int {
	if share >= ShareFull {
		return body
	}
	panel := body * int(share.Clamp()) / 100

	if panel < 5 {
		panel = 5
	}
	if body-panel < 4 {
		panel = body - 4
	}
	if panel < 0 {
		panel = 0
	}
	return panel
}
