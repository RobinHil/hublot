// Package ui is the Bubble Tea layer. It owns every stream, routes keys to the
// active view, and is the only place the model is mutated
// (AGENTS.md section 4).
package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/config"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
	"github.com/RobinHil/hublot/internal/ui/views"
)

// messageTTL is how long a transient footer message stays up.
const messageTTL = 6 * time.Second

// App is the root model.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	client *docker.Client
	store  *state.Store
	cfg    config.Config
	keys   keys.Map
	cli    compose.CLI

	views  []views.View
	active int
	tasks  components.TaskPanel

	// The side panel: logs, inspect output, anything worth reading next to the
	// list rather than instead of it.
	panel      components.Panel
	panelShare PanelShare
	// dragging is set while the divider is held, so the panel follows the
	// mouse until the button comes back up.
	dragging bool

	modal       *components.Modal
	modalRun    func() tea.Cmd
	picker      *components.Picker
	prompt      *components.Prompt
	form        *components.Form
	helpOpen    bool
	helpOffset  int
	helpMax     int
	panelCancel context.CancelFunc

	// stats streams, one goroutine and one cancel per container
	// (AGENTS.md section 6.3).
	statsCancels map[string]context.CancelFunc
	statsCh      chan docker.Stats
	logCh        chan docker.LogLine
	taskCh       chan taskEvent
	taskSeq      int
	// What each running compose task was doing, so a failure can offer to open
	// the file behind it and run the same command again.
	taskOf map[string]composeTask

	events <-chan docker.EventUpdate

	// What every view was given, kept because a form the app opens on a view's
	// behalf is built from the same dependencies the view has.
	deps views.Deps

	width, height int
	message       string
	messageErr    bool
	messageAt     time.Time
	quitting      bool
}

// New builds the root model. The context it is given is cancelled on quit,
// which is what stops every stream.
func New(ctx context.Context, client *docker.Client, cfg config.Config, cli compose.CLI, readOnly bool) *App {
	ctx, cancel := context.WithCancel(ctx)

	// The theme is the one package-level variable in the codebase, set once
	// here and read everywhere (AGENTS.md section 15).
	theme.Set(theme.ByName(cfg.Theme))

	store := state.New()
	store.Socket = client.Socket()
	store.APIVersion = client.APIVersion()
	store.Info = client.CachedInfo()
	store.VolumePruneAll = client.SupportsVolumePruneAll()
	store.ReadOnly = readOnly
	store.ComposeCLI = cli

	k := keys.Default()
	deps := views.Deps{
		Ctx:         ctx,
		Client:      client,
		Store:       store,
		CLI:         cli,
		Keys:        k,
		ReadOnly:    readOnly,
		StopTimeout: cfg.StopTimeout,
		Shell:       cfg.Shell,
	}

	app := &App{
		ctx:          ctx,
		deps:         deps,
		cancel:       cancel,
		client:       client,
		store:        store,
		cfg:          cfg,
		keys:         k,
		cli:          cli,
		tasks:        components.NewTaskPanel(),
		panel:        components.NewPanel(),
		panelShare:   ShareThird,
		statsCancels: map[string]context.CancelFunc{},
		statsCh:      make(chan docker.Stats, 128),
		logCh:        make(chan docker.LogLine, 512),
		taskCh:       make(chan taskEvent, 256),
		taskOf:       map[string]composeTask{},
	}

	containers := views.NewContainers(deps)
	// Compose comes second because that is how hosts are actually run: stacks
	// first, then the loose objects underneath them. The order here is the
	// order of the tabs and of the digit keys, and nothing else depends on it.
	app.views = []views.View{
		containers,
		views.NewCompose(deps),
		views.NewImages(deps),
		views.NewVolumes(deps),
		views.NewNetworks(deps),
		views.NewDisk(deps),
	}

	if !cli.Available {
		app.setMessage("compose actions disabled: "+cli.Reason, true)
	}

	return app
}

// Init starts the event stream and the first sync.
func (a *App) Init() tea.Cmd {
	a.events = a.client.StreamEvents(a.ctx)

	return tea.Batch(
		cmds.RefreshAll(a.ctx, a.client),
		cmds.LoadInfo(a.ctx, a.client),
		cmds.Tick(),
		waitForEvents(a.events),
		waitForStats(a.statsCh),
		waitForLogs(a.logCh),
		waitForTasks(a.taskCh),
	)
}

// Update routes everything. Nothing in here blocks: Docker calls go out as
// commands (AGENTS.md section 4, rule 3).
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.layout()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(m)

	case tea.MouseMsg:
		return a, a.handleMouse(m)

	case cmds.TickMsg:
		return a, a.tick()

	case EventMsg:
		return a, a.handleEvent(m.Update)

	case StatsMsg:
		a.store.ApplyStats(m.Sample)
		a.refreshViews()
		return a, waitForStats(a.statsCh)

	case LogMsg:
		a.panel.AppendService(m.Line.Service, m.Line.Text, m.Line.Stream == "stderr")
		return a, waitForLogs(a.logCh)

	case TaskLineMsg:
		a.tasks.Append(m.TaskID, m.Line)
		return a, waitForTasks(a.taskCh)

	case TaskDoneMsg:
		if a.tasks.Finish(m.TaskID, m.ExitCode, m.Err) {
			a.setMessage("a task failed, see the task panel", true)
			// The commonest failures have an obvious cause and a one-line fix,
			// and leaving someone to find it in sixty characters of endpoint id
			// is not help.
			if modal, ok := a.explain(a.tasks.Output(m.TaskID)); ok {
				modal.Body = append(modal.Body, "", "The whole output is in the task panel, which t opens.")
				a.offerFix(&modal, a.taskOf[m.TaskID], a.tasks.Output(m.TaskID))
				a.openModal(modal)
			}
			delete(a.taskOf, m.TaskID)
		}
		return a, tea.Batch(waitForTasks(a.taskCh), cmds.RefreshAll(a.ctx, a.client))

	case StreamClosedMsg:
		// Only the per-source channels the app owns are restarted; the event
		// stream closes only when the context is done.
		return a, a.restartStream(m.Source)

	case cmds.ContainersMsg:
		return a, a.applyContainers(m)

	case cmds.ImagesMsg:
		if m.Err != nil {
			a.setMessage(m.Err.Error(), true)
		} else {
			a.store.SetImages(m.Images)
		}
		a.refreshViews()
		return a, nil

	case cmds.VolumesMsg:
		if m.Err != nil {
			a.setMessage(m.Err.Error(), true)
		} else {
			a.store.SetVolumes(m.Volumes)
		}
		a.refreshViews()
		return a, nil

	case cmds.NetworksMsg:
		if m.Err != nil {
			a.setMessage(m.Err.Error(), true)
		} else {
			a.store.SetNetworks(m.Networks)
		}
		a.refreshViews()
		return a, nil

	case cmds.InfoMsg:
		if m.Err == nil {
			a.store.Info = m.Info
		}
		return a, nil

	case cmds.DiskMsg:
		a.diskView().SetLoading(false)
		if m.Err != nil {
			a.setMessage(m.Err.Error(), true)
		} else {
			a.store.SetDisk(m.Usage)
		}
		a.refreshViews()
		return a, nil

	case cmds.DriftMsg:
		a.store.ApplyDrift(m.Project, m.Hashes, m.Err)
		if m.Err != nil {
			// A Compose too old for `config --hash` is a degraded state, not a
			// failure (AGENTS.md section 8.4).
			a.setMessage("drift unknown for "+m.Project+": "+m.Err.Error(), true)
		}
		a.refreshViews()
		return a, nil

	case cmds.ActionDoneMsg:
		if m.Err != nil {
			// The same explanation for a direct action: running a container
			// hits the port and the name clashes just as compose does.
			if modal, ok := a.explain(m.Err.Error()); ok {
				modal.Body = append(modal.Body, "", m.Err.Error())
				a.openModal(modal)
			} else {
				a.openModal(components.NewModal(components.SevError, "action failed",
					[]string{m.Err.Error()}, nil))
			}
		} else {
			a.setMessage(m.Label, false)
		}
		if m.Refresh {
			return a, cmds.RefreshAll(a.ctx, a.client)
		}
		return a, nil

	case cmds.PruneDoneMsg:
		return a, a.applyPruneResult(m)

	case cmds.ContainerSpecMsg:
		// The form cannot be built until the daemon has said what the
		// container is made of, so the view asks and the answer opens it.
		if m.Err != nil {
			a.openModal(components.NewModal(components.SevError,
				"that container cannot be read", []string{m.Err.Error()}, nil))
			return a, nil
		}
		return a, emit(views.EditFormRequest(a.deps, m.Spec))

	case cmds.InspectMsg:
		return a, a.showInspect(m)

	case cmds.HistoryMsg:
		return a, a.showHistory(m)

	case cmds.TextMsg:
		return a, a.showText(m)

	case components.ConfirmedMsg:
		run := a.modalRun
		a.closeModal()
		if run != nil {
			return a, run()
		}
		return a, nil

	case components.ChosenMsg:
		a.picker = nil
		if run, ok := m.Payload.(func() tea.Cmd); ok && run != nil {
			return a, run()
		}
		return a, nil

	case components.AnsweredMsg:
		run, ok := m.Payload.(func(string) tea.Cmd)
		a.prompt = nil
		if ok && run != nil {
			return a, run(m.Value)
		}
		return a, nil

	case components.SubmittedMsg:
		run, ok := m.Payload.(func(map[string]string) tea.Cmd)
		a.form = nil
		if ok && run != nil {
			return a, run(m.Values)
		}
		return a, nil

	case components.DismissedMsg:
		a.closeModal()
		a.picker = nil
		a.prompt = nil
		a.form = nil
		return a, nil

	case views.ConfirmRequest:
		a.modalRun = m.Run
		a.openModal(components.NewModal(m.Severity, m.Title, m.Body, nil))
		return a, nil

	case views.PromptRequest:
		p := components.NewPrompt(m.Title, m.Body, m.Label, m.Initial, m.Run)
		p.SetSize(a.width, a.height)
		a.prompt = &p
		return a, textinput.Blink

	case views.FormRequest:
		f := components.NewForm(m.Title, m.Subtitle, m.Fields, m.Run)
		f.SetSize(a.width, a.height)
		a.form = &f
		return a, textinput.Blink

	case views.PickerRequest:
		p := components.NewPicker(m.Title, m.Choices)
		p.SetSize(a.width, a.height)
		a.picker = &p
		return a, nil

	case views.ReadOnlyMsg:
		a.setMessage("read-only session: that action is disabled", true)
		return a, nil

	case views.DetailRequest:
		return a, a.openDetail(m)

	case views.LogsRequest:
		return a, a.openContainerLogs(m.ContainerID, m.Name)

	case views.ComposeLogsRequest:
		return a, a.openProjectLogs(m.Project, m.Service)

	case views.EditRequest:
		return a, a.openEditor(m)

	case editFinishedMsg:
		return a, a.editFinished(m)

	case views.ExecRequest:
		return a, a.openExec(m.ContainerID, m.Name)

	case execFinishedMsg:
		// Without this the shell could fail to open and leave no trace at all.
		if m.err != nil {
			a.openModal(components.NewModal(components.SevError,
				"shell in "+m.name+" failed", []string{m.err.Error()}, nil))
		} else {
			a.setMessage("left the shell in "+m.name, false)
		}
		// A shell can have changed anything, and events only cover some of it.
		return a, cmds.RefreshAll(a.ctx, a.client)

	case views.ComposeRunRequest:
		return a, a.runCompose(m)

	case views.PullRequest:
		return a, a.runPull(m.Ref)

	case views.PruneRequest:
		return a, cmds.Prune(a.ctx, a.client, m.Categories, 0)
	}

	return a, nil
}

// handleKey dispatches a key press through the overlays, then the active view.
func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, whatever is on screen.
	if key.Matches(msg, a.keys.Global.ForceQuit) {
		return a, a.quit()
	}

	// Overlays get the key first, innermost last opened.
	if a.form != nil {
		return a, a.form.Update(msg, a.keys)
	}
	if a.prompt != nil {
		return a, a.prompt.Update(msg, a.keys)
	}
	if a.picker != nil {
		return a, a.picker.Update(msg, a.keys)
	}
	if a.modal != nil {
		return a, a.modal.Update(msg, a.keys)
	}
	if a.helpOpen {
		switch {
		case key.Matches(msg, a.keys.Global.Escape), key.Matches(msg, a.keys.Global.Help),
			key.Matches(msg, a.keys.Global.Quit):
			a.helpOpen = false
			a.helpOffset = 0
		case key.Matches(msg, a.keys.Global.Down):
			a.scrollHelp(1)
		case key.Matches(msg, a.keys.Global.Up):
			a.scrollHelp(-1)
		case key.Matches(msg, a.keys.Global.PageDown):
			a.scrollHelp(10)
		case key.Matches(msg, a.keys.Global.PageUp):
			a.scrollHelp(-10)
		}
		return a, nil
	}
	if a.tasks.Visible {
		if key.Matches(msg, a.keys.Global.Tasks) || key.Matches(msg, a.keys.Global.Escape) {
			a.tasks.Visible = false
			return a, nil
		}
		return a, a.tasks.Update(msg)
	}

	// The panel takes the keys while it has the focus, so reading and
	// searching in it never fights with the list underneath.
	if a.panel.Focused() {
		switch {
		case key.Matches(msg, a.keys.Panel.Focus):
			a.panel.SetFocus(false)
			return a, nil
		case key.Matches(msg, a.keys.Panel.Close):
			a.closePanel()
			return a, nil
		case key.Matches(msg, a.keys.Panel.Width):
			a.cyclePanelWidth()
			return a, nil
		}
		return a, a.panel.Update(msg, a.keys)
	}

	// Global keys, unless the active view is capturing text for its filter.
	if !a.filtering() {
		switch {
		case key.Matches(msg, a.keys.Panel.Focus):
			if a.panel.IsOpen() {
				a.panel.SetFocus(true)
			}
			return a, nil
		case key.Matches(msg, a.keys.Panel.Width):
			if a.panel.IsOpen() {
				a.cyclePanelWidth()
			}
			return a, nil
		case key.Matches(msg, a.keys.Panel.Close):
			if a.panel.IsOpen() {
				a.closePanel()
				return a, nil
			}
		case key.Matches(msg, a.keys.Global.Quit):
			return a, a.quit()
		case key.Matches(msg, a.keys.Global.Help):
			a.helpOpen = true
			return a, nil
		case key.Matches(msg, a.keys.Global.Tasks):
			a.tasks.Toggle()
			return a, nil
		case key.Matches(msg, a.keys.Global.Refresh):
			return a, a.forceRefresh()
		case key.Matches(msg, a.keys.Global.NextView):
			a.switchView(a.active + 1)
			return a, a.enterView()
		case key.Matches(msg, a.keys.Global.PrevView):
			a.switchView(a.active - 1)
			return a, a.enterView()
		}
		if n, ok := viewNumber(msg, a.keys.Global); ok {
			a.switchView(n)
			return a, a.enterView()
		}
	}

	before := a.views[a.active].Marked()
	cmd := a.views[a.active].Update(msg)

	// Selecting is the one thing in a list that does nothing visible on its
	// own, so the first time it happens the interface says what it is for.
	if after := a.views[a.active].Marked(); after > 0 && before == 0 {
		a.setMessage("selected: the next action applies to every selected row, esc clears", false)
	}

	// Scrolling changes which containers are visible, which changes which ones
	// are worth streaming when the cap is reached.
	a.syncStatsStreams()
	return a, cmd
}

// handleMouse routes the wheel and clicks. A click on the tab bar switches
// view, a click in the panel focuses it, and anything else belongs to whatever
// is under the pointer.
func (a *App) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if a.modal != nil || a.picker != nil || a.prompt != nil || a.form != nil || a.helpOpen {
		return nil
	}

	l := a.geometry()
	if l.TooSmall {
		return nil
	}

	clicked := msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress

	// The divider is dragged rather than cycled through fixed fractions. Once
	// held it keeps the mouse until the button is released, wherever it goes,
	// which is what makes it feel like a divider and not a button.
	if a.dragging {
		switch msg.Action {
		case tea.MouseActionRelease:
			a.dragging = false
		case tea.MouseActionMotion:
			a.dragPanel(l, msg.X, msg.Y)
		}
		return nil
	}
	if clicked && a.onDivider(l, msg.X, msg.Y) {
		a.dragging = true
		return nil
	}

	// The task panel is drawn instead of the list, so it takes the wheel too.
	// Without this the wheel scrolls a list nobody can see.
	if a.tasks.Visible {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			a.tasks.Wheel(true)
		case tea.MouseButtonWheelDown:
			a.tasks.Wheel(false)
		}
		return nil
	}

	if l.ShowTabs && msg.Y == l.TabRow() && clicked {
		if n, ok := components.TabAt(a.titles(), msg.X, l.Compact); ok {
			a.switchView(n)
			return a.enterView()
		}
		return nil
	}

	// Beside the list, the panel owns everything to the right of the split;
	// stacked, everything below it.
	if l.PanelOpen && !l.ListHidden {
		inPanel := (!l.PanelStacked && msg.X >= l.ListWidth) ||
			(l.PanelStacked && msg.Y >= a.panelTop(l))
		if inPanel {
			if clicked {
				a.panel.SetFocus(true)
			}
			return a.panel.Update(msg, a.keys)
		}
		if clicked {
			a.panel.SetFocus(false)
		}
	} else if l.ListHidden {
		return a.panel.Update(msg, a.keys)
	}

	return a.views[a.active].Update(msg)
}

// onDivider says whether a click landed on the line between the list and the
// panel, which is the only part of the screen that resizes them.
func (a *App) onDivider(l Layout, x, y int) bool {
	if !l.PanelOpen || l.ListHidden {
		return false
	}
	// One column is a hard thing to hit with a mouse, so the row or column
	// just inside the panel counts as the divider too. The tolerance is taken
	// from the panel's side: a click there would otherwise only focus it,
	// while a click on the list's last column selects a row.
	if l.PanelStacked {
		return y == a.panelTop(l) || y == a.panelTop(l)+1
	}
	return x == l.ListWidth || x == l.ListWidth+1
}

// dragPanel moves the divider to where the mouse is.
func (a *App) dragPanel(l Layout, x, y int) {
	if l.PanelStacked {
		a.panelShare = l.ShareAtRow(y)
	} else {
		a.panelShare = l.ShareAtColumn(x)
	}
	a.layout()
}

// panelTop is the first screen row the stacked panel occupies.
func (a *App) panelTop(l Layout) int {
	return l.ContentRow() + l.ListHeight
}

// viewNumber maps the digit keys to a tab index.
func viewNumber(msg tea.KeyMsg, g keys.Global) (int, bool) {
	for i, b := range []key.Binding{g.View1, g.View2, g.View3, g.View4, g.View5, g.View6} {
		if key.Matches(msg, b) {
			return i, true
		}
	}
	return 0, false
}

// filtering reports whether the active view is capturing text.
func (a *App) filtering() bool {
	type filterer interface{ Filtering() bool }
	if f, ok := a.views[a.active].(filterer); ok {
		return f.Filtering()
	}
	return false
}

// scrollHelp moves the help overlay, clamped to what the last render reported
// as scrollable.
func (a *App) scrollHelp(delta int) {
	a.helpOffset += delta
	if a.helpOffset > a.helpMax {
		a.helpOffset = a.helpMax
	}
	if a.helpOffset < 0 {
		a.helpOffset = 0
	}
}

func (a *App) switchView(n int) {
	if n < 0 {
		n = len(a.views) - 1
	}
	a.active = n % len(a.views)
	a.layout()
}

// enterView runs what a tab needs on arrival. The Compose view checks drift
// then, and the Disk view is left alone: df is too slow to trigger by
// navigation (AGENTS.md sections 6.6 and 7).
func (a *App) enterView() tea.Cmd {
	if _, ok := a.views[a.active].(*views.Compose); !ok {
		return nil
	}
	if !a.cli.Available {
		return nil
	}

	var batch []tea.Cmd
	for _, p := range a.store.Projects {
		if p.Actionable() {
			batch = append(batch, cmds.CheckDrift(a.ctx, a.cli, p))
		}
	}
	return tea.Batch(batch...)
}

// forceRefresh resyncs everything, and recomputes df when the Disk view asks.
func (a *App) forceRefresh() tea.Cmd {
	if _, ok := a.views[a.active].(*views.Disk); ok {
		a.diskView().SetLoading(true)
		a.refreshViews()
		return tea.Batch(cmds.RefreshAll(a.ctx, a.client), cmds.DiskUsage(a.ctx, a.client))
	}
	return tea.Batch(cmds.RefreshAll(a.ctx, a.client), cmds.LoadInfo(a.ctx, a.client))
}

func (a *App) diskView() *views.Disk {
	for _, v := range a.views {
		if d, ok := v.(*views.Disk); ok {
			return d
		}
	}
	return nil
}

// tick is the safety poll behind the event stream, plus the clock that expires
// the footer message.
func (a *App) tick() tea.Cmd {
	if a.message != "" && time.Since(a.messageAt) > messageTTL {
		a.message = ""
	}
	return tea.Batch(cmds.RefreshAll(a.ctx, a.client), cmds.Tick())
}

// handleEvent applies one daemon event: events are the source of truth for
// state changes (AGENTS.md section 7).
func (a *App) handleEvent(u docker.EventUpdate) tea.Cmd {
	next := waitForEvents(a.events)

	switch u.Kind {
	case docker.EventDisconnected:
		a.store.Stale = true
		a.store.StaleErr = u.Err
		return next

	case docker.EventReconnected:
		// Events were missed while the stream was down, so nothing on screen
		// can be trusted until every list is resynced (AGENTS.md section 6.5).
		a.store.Stale = false
		a.store.StaleErr = nil
		a.setMessage("daemon back, resyncing", false)
		return tea.Batch(next, cmds.RefreshAll(a.ctx, a.client), cmds.LoadInfo(a.ctx, a.client))
	}

	e := u.Event
	var batch []tea.Cmd

	switch {
	case e.AffectsContainers():
		batch = append(batch, cmds.ListContainers(a.ctx, a.client))
	case e.AffectsImages():
		batch = append(batch, cmds.ListImages(a.ctx, a.client))
	case e.AffectsVolumes():
		batch = append(batch, cmds.ListVolumes(a.ctx, a.client))
	case e.AffectsNetworks():
		batch = append(batch, cmds.ListNetworks(a.ctx, a.client))
	}

	if e.StopsContainer() {
		a.stopStats(e.ActorID)
	}

	return tea.Batch(append(batch, next)...)
}

// applyContainers records a refreshed list and reconciles the stats streams
// with it.
func (a *App) applyContainers(m cmds.ContainersMsg) tea.Cmd {
	if m.Err != nil {
		// Keep showing the last known state rather than blanking the screen
		// (AGENTS.md section 12).
		a.store.Stale = true
		a.store.StaleErr = m.Err
		a.setMessage(m.Err.Error(), true)
		return nil
	}

	a.store.Stale = false
	a.store.SetContainers(m.Containers)
	a.refreshViews()
	a.syncStatsStreams()
	return nil
}

// syncStatsStreams starts a stream for every running container and cancels the
// ones that are no longer wanted. Above the configured cap, only containers
// visible in the viewport are streamed (AGENTS.md section 6.3).
func (a *App) syncStatsStreams() {
	running := a.store.RunningContainers()

	want := map[string]bool{}
	if len(running) <= a.cfg.MaxStatStreams {
		for _, c := range running {
			want[c.ID] = true
		}
	} else {
		for _, id := range a.visibleContainerIDs() {
			if len(want) >= a.cfg.MaxStatStreams {
				break
			}
			if c, ok := a.store.ContainerByID(id); ok && c.Running() {
				want[id] = true
			}
		}
		for _, c := range running {
			if len(want) >= a.cfg.MaxStatStreams {
				break
			}
			want[c.ID] = true
		}
	}

	for id, cancel := range a.statsCancels {
		if !want[id] {
			cancel()
			delete(a.statsCancels, id)
			a.store.DropStats(id)
		}
	}

	for id := range want {
		if _, running := a.statsCancels[id]; !running {
			a.startStats(id)
		}
	}
}

// visibleContainerIDs asks the containers view what is on screen.
func (a *App) visibleContainerIDs() []string {
	for _, v := range a.views {
		if c, ok := v.(*views.Containers); ok {
			return c.VisibleIDs()
		}
	}
	return nil
}

// startStats opens one stream, forwarding its samples into the shared channel.
func (a *App) startStats(id string) {
	ctx, cancel := context.WithCancel(a.ctx)
	a.statsCancels[id] = cancel

	samples, errs := a.client.StreamStats(ctx, id)

	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case s, ok := <-samples:
				if !ok {
					return
				}
				select {
				case a.statsCh <- s:
				case <-ctx.Done():
					return
				}
			case <-errs:
				// A stream that dies takes its goroutine with it; the next
				// container list reconciliation will start it again if the
				// container is still running.
				return
			}
		}
	}()
}

// stopStats cancels one stream, which is what keeps goroutines and HTTP
// connections from piling up over restarts.
func (a *App) stopStats(id string) {
	if cancel, ok := a.statsCancels[id]; ok {
		cancel()
		delete(a.statsCancels, id)
	}
	a.store.DropStats(id)
}

// restartStream re-arms a reader whose channel the app owns. The event stream
// is not restarted here: it closes only when the context is done.
func (a *App) restartStream(source string) tea.Cmd {
	switch source {
	case "stats":
		return waitForStats(a.statsCh)
	case "logs":
		return waitForLogs(a.logCh)
	case "tasks":
		return waitForTasks(a.taskCh)
	}
	return nil
}

func (a *App) refreshViews() {
	for _, v := range a.views {
		v.Refresh()
	}
}

// layout hands each pane the space Compute decided it gets.
func (a *App) layout() {
	l := a.geometry()
	if l.TooSmall {
		return
	}

	top := l.HeaderRows
	if l.ShowTabs {
		top++
	}
	for _, v := range a.views {
		v.SetSize(l.ListWidth, l.ListHeight)
		if placeable, ok := v.(interface{ SetOrigin(int) }); ok {
			placeable.SetOrigin(top)
		}
	}
	if l.PanelOpen {
		switch {
		case l.ListHidden:
			a.panel.SetEdge(components.EdgeNone)
		case l.PanelStacked:
			a.panel.SetEdge(components.EdgeTop)
		default:
			a.panel.SetEdge(components.EdgeLeft)
		}
		a.panel.SetSize(l.PanelWidth, l.PanelHeight)
	}
	a.tasks.SetSize(l.Width, l.ListHeight+1)
	if a.modal != nil {
		a.modal.SetSize(a.width, a.height)
	}
	if a.picker != nil {
		a.picker.SetSize(a.width, a.height)
	}
	if a.prompt != nil {
		a.prompt.SetSize(a.width, a.height)
	}
	if a.form != nil {
		a.form.SetSize(a.width, a.height)
	}
}

// geometry resolves the current frame, which the renderer and the sizing both
// read rather than each doing the arithmetic.
func (a *App) geometry() Layout {
	return Compute(a.width, a.height, a.panel.IsOpen(), a.panelShare)
}

// cyclePanelWidth steps through the shares, skipping straight past any that
// this screen cannot honour.
func (a *App) cyclePanelWidth() {
	a.panelShare = a.panelShare.Next()
	a.layout()
}

// closePanel stops whatever was feeding the panel and puts the keys back on
// the list.
func (a *App) closePanel() {
	if a.panelCancel != nil {
		a.panelCancel()
		a.panelCancel = nil
	}
	a.panel.Close()
	a.layout()
}

func (a *App) setMessage(text string, isErr bool) {
	a.message = text
	a.messageErr = isErr
	a.messageAt = time.Now()
}

func (a *App) openModal(m components.Modal) {
	m.SetSize(a.width, a.height)
	a.modal = &m
}

func (a *App) closeModal() {
	a.modal = nil
	a.modalRun = nil
}

// quit cancels every stream and lets Bubble Tea restore the terminal.
func (a *App) quit() tea.Cmd {
	a.quitting = true
	a.closePanel()
	for id, cancel := range a.statsCancels {
		cancel()
		delete(a.statsCancels, id)
	}
	a.cancel()
	return tea.Quit
}
