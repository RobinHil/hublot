// Package keys holds every key binding. Views never declare bindings inline, so
// the help overlay is generated from this one source (AGENTS.md section 9.2).
//
// The convention throughout: lowercase is safe, uppercase is destructive or
// forced.
package keys

import "github.com/charmbracelet/bubbles/key"

// Global bindings are active in every view. The left and right arrows switch
// view rather than scrolling: no table here scrolls sideways, it drops
// low-priority columns instead (AGENTS.md section 9.1).
type Global struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	NextView  key.Binding
	PrevView  key.Binding
	View1     key.Binding
	View2     key.Binding
	View3     key.Binding
	View4     key.Binding
	View5     key.Binding
	View6     key.Binding
	Filter    key.Binding
	Sort      key.Binding
	SortRev   key.Binding
	Mark      key.Binding
	MarkAll   key.Binding
	Escape    key.Binding
	Help      key.Binding
	Refresh   key.Binding
	Tasks     key.Binding
	Quit      key.Binding
	ForceQuit key.Binding
}

// Containers bindings apply to the containers view.
type Containers struct {
	Detail  key.Binding
	Logs    key.Binding
	Exec    key.Binding
	Start   key.Binding
	Stop    key.Binding
	Restart key.Binding
	Pause   key.Binding
	Kill    key.Binding
	Remove  key.Binding
	New     key.Binding
	Palette key.Binding
}

// Images bindings apply to the images view.
type Images struct {
	Detail  key.Binding
	History key.Binding
	Remove  key.Binding
	Force   key.Binding
	Pull    key.Binding
	Fetch   key.Binding
	Run     key.Binding
	Palette key.Binding
}

// Objects bindings apply to the volumes and networks views.
type Objects struct {
	Detail  key.Binding
	Remove  key.Binding
	Force   key.Binding
	Palette key.Binding
}

// Compose bindings apply to the Compose view.
type Compose struct {
	New         key.Binding
	View        key.Binding
	Edit        key.Binding
	Up          key.Binding
	UpRecreate  key.Binding
	Pull        key.Binding
	Build       key.Binding
	Stop        key.Binding
	Restart     key.Binding
	Logs        key.Binding
	LogsAll     key.Binding
	ScaleUp     key.Binding
	ScaleDown   key.Binding
	Config      key.Binding
	Drift       key.Binding
	Down        key.Binding
	DownVolumes key.Binding
	Expand      key.Binding
}

// Disk bindings apply to the disk view.
type Disk struct {
	Refresh     key.Binding
	Prune       key.Binding
	PruneGlobal key.Binding
	Toggle      key.Binding
}

// Panel bindings apply to the side panel: logs, inspect output, anything read
// next to the list rather than instead of it.
type Panel struct {
	// Focus moves the keyboard between the list and the panel.
	Focus key.Binding
	// Width cycles how much of the screen the panel takes.
	Width  key.Binding
	Close  key.Binding
	Search key.Binding
	Next   key.Binding
	Prev   key.Binding
	Follow key.Binding
	Wrap   key.Binding
}

// Modal bindings apply to confirmation dialogs.
type Modal struct {
	Confirm key.Binding
	Cancel  key.Binding
	// Fix accepts the way out an error dialog offers, such as opening the file
	// that would not parse.
	Fix key.Binding
}

// Map is every binding set, built once and passed around.
type Map struct {
	Global     Global
	Containers Containers
	Images     Images
	Objects    Objects
	Compose    Compose
	Disk       Disk
	Panel      Panel
	Modal      Modal
}

// Default builds the standard binding set.
func Default() Map {
	return Map{
		Global: Global{
			Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "up")),
			Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "down")),
			PageUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
			PageDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
			Home:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "first")),
			End:       key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "last")),
			NextView:  key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab/right", "next view")),
			PrevView:  key.NewBinding(key.WithKeys("shift+tab", "left"), key.WithHelp("shift+tab/left", "previous view")),
			View1:     key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "containers")),
			View2:     key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "compose")),
			View3:     key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "images")),
			View4:     key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "volumes")),
			View5:     key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "networks")),
			View6:     key.NewBinding(key.WithKeys("6"), key.WithHelp("6", "disk")),
			Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			Sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "cycle sort")),
			SortRev:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "reverse sort")),
			Mark:      key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select row")),
			MarkAll:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
			Escape:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear selection")),
			Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
			Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Tasks:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tasks")),
			Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
			ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		},
		Containers: Containers{
			Detail: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			Logs:   key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
			Exec:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "shell")),
			// Lowercase, because starting something breaks nothing. It is the
			// same letter compose uses to bring a stack up.
			Start:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "start")),
			Stop:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stop")),
			Restart: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart")),
			Pause:   key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "pause/unpause")),
			Kill:    key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "kill")),
			Remove:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove")),
			New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new container")),
			Palette: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "more")),
		},
		Images: Images{
			Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			History: key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "history")),
			Remove:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove")),
			Force:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "force remove")),
			Pull:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull again")),
			Fetch:   key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "pull by name")),
			Run:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "run a container")),
			Palette: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "more")),
		},
		Objects: Objects{
			Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			Remove:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove")),
			Force:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "force remove")),
			Palette: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "actions")),
		},
		Compose: Compose{
			New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new or existing stack")),
			View:        key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "read the compose file")),
			Edit:        key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit the compose file")),
			Up:          key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "up -d")),
			UpRecreate:  key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "up -d --force-recreate")),
			Pull:        key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
			Build:       key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "build")),
			Stop:        key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stop stack")),
			Restart:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart stack")),
			Logs:        key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs of this service")),
			LogsAll:     key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "logs of the whole stack")),
			ScaleUp:     key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "scale up")),
			ScaleDown:   key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "scale down")),
			Config:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "resolved config")),
			Drift:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "check drift")),
			Down:        key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "down")),
			DownVolumes: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "down -v --remove-orphans")),
			Expand:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "fold the stack, from its project line")),
		},
		Disk: Disk{
			Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "recompute df")),
			Prune:       key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prune this category")),
			PruneGlobal: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune everything")),
			Toggle:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand")),
		},
		Panel: Panel{
			Focus:  key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "focus panel")),
			Width:  key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "panel width")),
			Close:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close panel")),
			Search: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
			Next:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
			Prev:   key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous match")),
			Follow: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow")),
			Wrap:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "wrap lines")),
		},
		Modal: Modal{
			Confirm: key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y", "confirm")),
			Cancel:  key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "cancel")),
			Fix:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "fix it")),
		},
	}
}

// Section is a named group of bindings, used to render the help overlay.
type Section struct {
	Title    string
	Bindings []key.Binding
	// Notes are the sentences a section needs that are not bindings: a rule
	// worth stating once, above the keys it governs. They are laid out across
	// the width rather than into a column, because a rule cut off at
	// twenty-six characters is not a rule anybody can follow.
	Notes []string
}

// Sections lists every binding, grouped, for the help overlay. Adding a binding
// above and forgetting it here is the one duplication this package cannot
// prevent, so every set is spelled out in full.
func (m Map) Sections() []Section {
	g := m.Global
	return []Section{
		{Title: "navigation", Bindings: []key.Binding{
			g.Up, g.Down, g.PageUp, g.PageDown, g.Home, g.End,
			g.NextView, g.PrevView, g.View1, g.View2, g.View3, g.View4, g.View5, g.View6,
		}},
		// Selecting is the one thing in here that is not obvious, so it says
		// what it is for rather than what it is called.
		{Title: "lists, and acting on several rows at once", Bindings: []key.Binding{
			g.Filter, g.Sort, g.SortRev, g.Refresh,
			key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select the row under the cursor")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select everything the filter left")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear the selection")),
		}, Notes: []string{
			"Once rows are selected, S, R, D and the rest act on all of them at once.",
		}},
		{Title: "containers", Bindings: []key.Binding{
			m.Containers.Detail, m.Containers.Logs, m.Containers.Exec,
			m.Containers.Start, m.Containers.Stop, m.Containers.Restart,
			m.Containers.Pause, m.Containers.Kill, m.Containers.Remove,
			m.Containers.New, m.Containers.Palette,
		}},
		{Title: "images", Bindings: []key.Binding{
			m.Images.Detail, m.Images.History, m.Images.Run, m.Images.Pull,
			m.Images.Fetch, m.Images.Remove, m.Images.Force, m.Images.Palette,
		}},
		{Title: "volumes and networks", Bindings: []key.Binding{
			m.Objects.Detail, m.Objects.Remove, m.Objects.Force, m.Objects.Palette,
		}},
		{Title: "compose, where actions apply to the row under the cursor", Notes: []string{
			"The project line folds the stack and acts on all of it; a service line acts on that service.",
			"A stack whose compose file is gone cannot be brought up, but S, R, D and X still stop and " +
				"remove what it left behind, through the engine API.",
		}, Bindings: []key.Binding{
			m.Compose.Expand, m.Compose.New, m.Compose.View, m.Compose.Edit, m.Compose.Up, m.Compose.UpRecreate, m.Compose.Pull,
			m.Compose.Build, m.Compose.Stop, m.Compose.Restart,
			m.Compose.Logs, m.Compose.LogsAll,
			m.Compose.ScaleUp, m.Compose.ScaleDown, m.Compose.Config,
			m.Compose.Drift, m.Compose.Down, m.Compose.DownVolumes,
		}},
		{Title: "disk", Bindings: []key.Binding{
			m.Disk.Refresh, m.Disk.Toggle, m.Disk.Prune, m.Disk.PruneGlobal,
		}},
		{Title: "side panel", Bindings: []key.Binding{
			m.Panel.Focus, m.Panel.Width, m.Panel.Close, m.Panel.Search,
			m.Panel.Next, m.Panel.Prev, m.Panel.Follow, m.Panel.Wrap,
		}},
		{Title: "session", Bindings: []key.Binding{
			g.Tasks, g.Help, g.Quit, g.ForceQuit,
		}},
		{Title: "mouse", Bindings: []key.Binding{
			key.NewBinding(key.WithKeys("wheel"), key.WithHelp("wheel", "scroll")),
			key.NewBinding(key.WithKeys("click"), key.WithHelp("click", "select a row")),
			key.NewBinding(key.WithKeys("click"), key.WithHelp("click tab", "switch view")),
			key.NewBinding(key.WithKeys("click"), key.WithHelp("click panel", "focus it")),
		}},
	}
}

// Destructive lists the bindings a read-only session disables, so the help
// overlay and the status bar can grey exactly those (AGENTS.md section 10.4).
func (m Map) Destructive() []key.Binding {
	return []key.Binding{
		m.Containers.Start, m.Containers.Stop, m.Containers.Restart,
		m.Containers.Pause, m.Containers.Kill, m.Containers.Remove,
		m.Containers.New, m.Containers.Exec,
		m.Images.Remove, m.Images.Force, m.Images.Pull,
		m.Objects.Remove, m.Objects.Force,
		m.Compose.Up, m.Compose.UpRecreate, m.Compose.Pull, m.Compose.Build,
		m.Compose.Stop, m.Compose.Restart, m.Compose.ScaleUp, m.Compose.ScaleDown,
		m.Compose.Down, m.Compose.DownVolumes, m.Compose.New, m.Compose.Edit,
		m.Images.Fetch, m.Images.Run, m.Containers.New, m.Containers.Start,
		m.Disk.Prune, m.Disk.PruneGlobal,
	}
}
