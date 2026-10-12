package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/ui/components"
)

// The sections of the Swarm view, in the order of the segment line.
const (
	secServices = iota
	secNodes
	secSecrets
	secConfigs
	secCluster
	sectionCount
)

var sectionTitles = []string{"services", "nodes", "secrets", "configs", "cluster"}

// Swarm is the view of a swarm as its manager sees it: stacks and their
// services, the nodes, secrets, configs and the cluster settings, one table
// each behind a segment line. Off a manager, it says where this node stands
// and what can be done from there instead.
type Swarm struct {
	deps    Deps
	section int
	tables  [sectionCount]components.Table
	width   int
	height  int
	top     int
	// folded remembers folded stacks, by name; open the services and nodes
	// whose tasks are showing, by id.
	folded map[string]bool
	open   map[string]bool
}

// NewSwarm builds the Swarm view.
func NewSwarm(d Deps) *Swarm {
	v := &Swarm{deps: d, folded: map[string]bool{}, open: map[string]bool{}}
	v.tables[secServices] = newServicesTable()
	v.tables[secNodes] = newNodesTable()
	v.tables[secSecrets] = newSecretsTable()
	v.tables[secConfigs] = newConfigsTable()
	v.tables[secCluster] = newClusterTable()
	return v
}

// Title is the tab label.
func (v *Swarm) Title() string { return "Swarm" }

// managed reports whether there is a swarm to show lists of.
func (v *Swarm) managed() bool { return v.deps.Store.SwarmManaged() }

// Refresh rebuilds every section, so switching between them shows current rows.
func (v *Swarm) Refresh() {
	v.refreshServices()
	v.refreshNodes()
	v.refreshSecrets()
	v.refreshConfigs()
	v.refreshCluster()
}

// SetSize gives the tables the space under the segment line.
func (v *Swarm) SetSize(width, height int) {
	v.width, v.height = width, height
	for i := range v.tables {
		v.tables[i].SetSize(width, max(height-v.head(), 1))
	}
}

// SetOrigin records where the view starts, one row above its tables.
func (v *Swarm) SetOrigin(top int) {
	v.top = top
	for i := range v.tables {
		v.tables[i].SetOrigin(top + v.head())
	}
}

func (v *Swarm) table() *components.Table { return &v.tables[v.section] }

// head is how many rows the segment line takes above the table: its own, and
// a blank one under it when the screen can spare it. Without that gap the
// sections and the table's heading read as one block.
func (v *Swarm) head() int {
	if v.height >= 12 {
		return 2
	}
	return 1
}

// Marked is how many rows of the active section are selected.
func (v *Swarm) Marked() int {
	if !v.managed() {
		return 0
	}
	return v.table().MarkedCount()
}

// Position is the scroll indicator of the active section.
func (v *Swarm) Position() string {
	if !v.managed() {
		return ""
	}
	return v.table().Scrollbar()
}

// Filtering reports whether the active section is capturing a filter.
func (v *Swarm) Filtering() bool { return v.managed() && v.table().Filtering() }

// View renders the segment line and the active section, or the state of a
// node that has no swarm to show.
func (v *Swarm) View() string {
	if !v.managed() {
		return v.offView()
	}
	return components.Segments(sectionTitles, v.section, v.width) + strings.Repeat("\n", v.head()) + v.table().View()
}

// Update routes a message to the active section.
func (v *Swarm) Update(msg tea.Msg) tea.Cmd {
	if !v.managed() {
		return v.updateOff(msg)
	}

	if mouse, ok := msg.(tea.MouseMsg); ok && mouse.Y == v.top {
		if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress {
			if n, ok := components.SegmentAt(sectionTitles, mouse.X); ok {
				v.section = n
			}
		}
		return nil
	}

	if cmd, handled := v.table().Update(msg, v.deps.Keys); handled {
		v.Refresh()
		return cmd
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch v.section {
	case secServices:
		return v.servicesKey(km)
	case secNodes:
		return v.nodesKey(km)
	case secSecrets:
		return v.secretsKey(km)
	case secConfigs:
		return v.configsKey(km)
	default:
		return v.clusterKey(km)
	}
}

// Hints name what the row under the cursor needs.
func (v *Swarm) Hints() []key.Binding {
	g, k := v.deps.Keys.Global, v.deps.Keys.Swarm
	if !v.managed() {
		return append(v.offHints(), g.Help, g.Quit)
	}
	var hints []key.Binding
	switch v.section {
	case secServices:
		hints = v.servicesHints()
	case secNodes:
		hints = v.nodesHints()
	case secSecrets:
		hints = []key.Binding{rename(k.Expand, "detail"), k.New, rename(k.Edit, "new value"), k.Remove, k.Palette}
	case secConfigs:
		hints = []key.Binding{rename(k.Expand, "detail"), k.View, k.New, rename(k.Edit, "new version"), k.Remove, k.Palette}
	default:
		hints = []key.Binding{rename(k.Expand, "change"), rename(k.View, "join commands"), k.Palette}
	}
	return append([]key.Binding{k.Sections}, append(hints, g.Filter, g.Help)...)
}

// Section is the index of the section on screen.
func (v *Swarm) Section() int { return v.section }

// Step moves to the next or previous section, the way the arrows move through
// tabs, and reports false at either end so the arrow goes on to the tab beyond.
// Off a manager there are no sections to walk.
func (v *Swarm) Step(delta int) bool {
	next := v.section + delta
	if !v.managed() || next < 0 || next >= sectionCount {
		return false
	}
	v.section = next
	return true
}

// EnterFrom places the view on the section an arrow arriving from that side
// meets first: the first one from the left, the last one from the right, so
// walking back from the next tab retraces the sections in order.
func (v *Swarm) EnterFrom(delta int) {
	if delta > 0 {
		v.section = 0
	} else {
		v.section = sectionCount - 1
	}
}

// rename gives a binding the words the row under the cursor needs, the way
// the containers view says unpause in front of a paused container.
func rename(b key.Binding, what string) key.Binding {
	return key.NewBinding(key.WithKeys(b.Keys()...), key.WithHelp(b.Help().Key, what))
}

// Summary is what the swarm amounts to, in one line.
func (v *Swarm) Summary() string {
	st := v.deps.Store.SwarmState()
	switch {
	case st.State == docker.SwarmInactive:
		return "not part of a swarm"
	case st.State == docker.SwarmLocked:
		return "swarm locked: this manager needs its unlock key"
	case !st.Active():
		return "swarm " + st.State
	case !st.Manager:
		return fmt.Sprintf("worker of a swarm, %s", plural(len(v.deps.Store.TaskContainers()), "task container"))
	}

	store := v.deps.Store
	if store.SwarmErr != nil {
		// The lists on screen are the last ones that could be read, and the
		// refusal is the reason they may be out of date.
		return "the swarm could not be listed: " + store.SwarmErr.Error()
	}
	var running, desired uint64
	degraded := 0
	stacks := 0
	for _, s := range store.Stacks {
		running += s.Running()
		desired += s.Desired()
		if s.Degraded() {
			degraded++
		}
		if !s.Loose() {
			stacks++
		}
	}
	short := ""
	if degraded > 0 {
		short = fmt.Sprintf("%d short of tasks", degraded)
	}
	total, reachable := store.Managers()
	return summaryOf(
		plural(stacks, "stack"),
		plural(len(store.Services), "service"),
		fmt.Sprintf("%d/%d tasks", running, desired),
		plural(len(store.Nodes), "node"),
		fmt.Sprintf("%d/%d managers up", reachable, total),
		short,
	)
}

// currentKind splits a row id into its kind and the rest.
func (v *Swarm) currentKind() (string, string) {
	kind, rest, _ := strings.Cut(v.table().CurrentID(), ":")
	return kind, rest
}

// targetsOf returns the ids of the selected rows of one kind, or the row
// under the cursor when it is of that kind and nothing is selected.
func (v *Swarm) targetsOf(kind string) []string {
	var out []string
	for _, id := range v.table().Targets() {
		if k, rest, _ := strings.Cut(id, ":"); k == kind {
			out = append(out, rest)
		}
	}
	return out
}

// info opens a dialog that only explains.
func (v *Swarm) info(title string, body ...string) tea.Cmd {
	return request(ConfirmRequest{Severity: components.SevInfo, Title: title, Body: body})
}
