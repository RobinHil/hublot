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
	Stop    key.Binding
	Restart key.Binding
	Pause   key.Binding
	Kill    key.Binding
	Remove  key.Binding
	Palette key.Binding
}

// Images bindings apply to the images view.
type Images struct {
	Detail  key.Binding
	History key.Binding
	Remove  key.Binding
	Force   key.Binding
	Pull    key.Binding
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
	Up          key.Binding
	UpRecreate  key.Binding
	Pull        key.Binding
	Build       key.Binding
	Stop        key.Binding
	Restart     key.Binding
	Logs        key.Binding
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

// Logs bindings apply to the log viewer.
type Logs struct {
	Search    key.Binding
	Next      key.Binding
	Prev      key.Binding
	Follow    key.Binding
	Wrap      key.Binding
	Timestamp key.Binding
	Close     key.Binding
}

// Modal bindings apply to confirmation dialogs.
type Modal struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// Map is every binding set, built once and passed around.
type Map struct {
	Global     Global
	Containers Containers
	Images     Images
	Objects    Objects
	Compose    Compose
	Disk       Disk
	Logs       Logs
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
			View2:     key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "images")),
			View3:     key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "volumes")),
			View4:     key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "networks")),
			View5:     key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "compose")),
			View6:     key.NewBinding(key.WithKeys("6"), key.WithHelp("6", "disk")),
			Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			Sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "cycle sort")),
			SortRev:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "reverse sort")),
			Mark:      key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "mark")),
			MarkAll:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "mark all")),
			Escape:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear / close")),
			Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
			Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Tasks:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tasks")),
			Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
			ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		},
		Containers: Containers{
			Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			Logs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
			Exec:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "exec shell")),
			Stop:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stop")),
			Restart: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart")),
			Pause:   key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "pause/unpause")),
			Kill:    key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "kill")),
			Remove:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove")),
			Palette: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "actions")),
		},
		Images: Images{
			Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			History: key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "history")),
			Remove:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove")),
			Force:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "force remove")),
			Pull:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull again")),
			Palette: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "actions")),
		},
		Objects: Objects{
			Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			Remove:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "remove")),
			Force:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "force remove")),
			Palette: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "actions")),
		},
		Compose: Compose{
			Up:          key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "up -d")),
			UpRecreate:  key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "up -d --force-recreate")),
			Pull:        key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
			Build:       key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "build")),
			Stop:        key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stop stack")),
			Restart:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart stack")),
			Logs:        key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "aggregated logs")),
			ScaleUp:     key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "scale up")),
			ScaleDown:   key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "scale down")),
			Config:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "resolved config")),
			Drift:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "check drift")),
			Down:        key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "down")),
			DownVolumes: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "down -v --remove-orphans")),
			Expand:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand/collapse")),
		},
		Disk: Disk{
			Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "recompute df")),
			Prune:       key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prune this category")),
			PruneGlobal: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "prune everything")),
			Toggle:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand")),
		},
		Logs: Logs{
			Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
			Next:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
			Prev:      key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "previous match")),
			Follow:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow")),
			Wrap:      key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "wrap lines")),
			Timestamp: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "timestamps")),
			Close:     key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "close")),
		},
		Modal: Modal{
			Confirm: key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y", "confirm")),
			Cancel:  key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "cancel")),
		},
	}
}

// Section is a named group of bindings, used to render the help overlay.
type Section struct {
	Title    string
	Bindings []key.Binding
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
		{Title: "lists", Bindings: []key.Binding{
			g.Filter, g.Sort, g.SortRev, g.Mark, g.MarkAll, g.Escape, g.Refresh,
		}},
		{Title: "containers", Bindings: []key.Binding{
			m.Containers.Detail, m.Containers.Logs, m.Containers.Exec,
			m.Containers.Stop, m.Containers.Restart, m.Containers.Pause,
			m.Containers.Kill, m.Containers.Remove, m.Containers.Palette,
		}},
		{Title: "images", Bindings: []key.Binding{
			m.Images.History, m.Images.Pull, m.Images.Remove, m.Images.Force,
		}},
		{Title: "volumes and networks", Bindings: []key.Binding{
			m.Objects.Detail, m.Objects.Remove, m.Objects.Force, m.Objects.Palette,
		}},
		{Title: "compose", Bindings: []key.Binding{
			m.Compose.Expand, m.Compose.Up, m.Compose.UpRecreate, m.Compose.Pull,
			m.Compose.Build, m.Compose.Stop, m.Compose.Restart, m.Compose.Logs,
			m.Compose.ScaleUp, m.Compose.ScaleDown, m.Compose.Config,
			m.Compose.Drift, m.Compose.Down, m.Compose.DownVolumes,
		}},
		{Title: "disk", Bindings: []key.Binding{
			m.Disk.Refresh, m.Disk.Toggle, m.Disk.Prune, m.Disk.PruneGlobal,
		}},
		{Title: "logs", Bindings: []key.Binding{
			m.Logs.Search, m.Logs.Next, m.Logs.Prev, m.Logs.Follow,
			m.Logs.Wrap, m.Logs.Timestamp, m.Logs.Close,
		}},
		{Title: "session", Bindings: []key.Binding{
			g.Tasks, g.Help, g.Quit, g.ForceQuit,
		}},
	}
}

// Destructive lists the bindings a read-only session disables, so the help
// overlay and the status bar can grey exactly those (AGENTS.md section 10.4).
func (m Map) Destructive() []key.Binding {
	return []key.Binding{
		m.Containers.Stop, m.Containers.Restart, m.Containers.Pause,
		m.Containers.Kill, m.Containers.Remove, m.Containers.Exec,
		m.Images.Remove, m.Images.Force, m.Images.Pull,
		m.Objects.Remove, m.Objects.Force,
		m.Compose.Up, m.Compose.UpRecreate, m.Compose.Pull, m.Compose.Build,
		m.Compose.Stop, m.Compose.Restart, m.Compose.ScaleUp, m.Compose.ScaleDown,
		m.Compose.Down, m.Compose.DownVolumes,
		m.Disk.Prune, m.Disk.PruneGlobal,
	}
}
