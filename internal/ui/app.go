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

// minWidth is the narrowest terminal hublot renders in. Below it the tables
// would be unreadable, so it says so instead (AGENTS.md section 9.1).
const minWidth = 80

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
	logs   *views.Logs
	tasks  components.TaskPanel

	modal      *components.Modal
	modalRun   func() tea.Cmd
	picker     *components.Picker
	prompt     *components.Prompt
	helpOpen   bool
	helpOffset int
	helpMax    int
	logsOpen   bool
	logsCancel context.CancelFunc

	// stats streams, one goroutine and one cancel per container
	// (AGENTS.md section 6.3).
	statsCancels map[string]context.CancelFunc
	statsCh      chan docker.Stats
	logCh        chan docker.LogLine
	taskCh       chan taskEvent
	taskSeq      int

	events <-chan docker.EventUpdate

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
		cancel:       cancel,
		client:       client,
		store:        store,
		cfg:          cfg,
		keys:         k,
		cli:          cli,
		logs:         views.NewLogs(k),
		tasks:        components.NewTaskPanel(),
		statsCancels: map[string]context.CancelFunc{},
		statsCh:      make(chan docker.Stats, 128),
		logCh:        make(chan docker.LogLine, 512),
		taskCh:       make(chan taskEvent, 256),
	}

	containers := views.NewContainers(deps)
	app.views = []views.View{
		containers,
		views.NewImages(deps),
		views.NewVolumes(deps),
		views.NewNetworks(deps),
		views.NewCompose(deps),
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

	case cmds.TickMsg:
		return a, a.tick()

	case EventMsg:
		return a, a.handleEvent(m.Update)

	case StatsMsg:
		a.store.ApplyStats(m.Sample)
		a.refreshViews()
		return a, waitForStats(a.statsCh)

	case LogMsg:
		a.logs.Append(m.Line)
		return a, waitForLogs(a.logCh)

	case TaskLineMsg:
		a.tasks.Append(m.TaskID, m.Line)
		return a, waitForTasks(a.taskCh)

	case TaskDoneMsg:
		if a.tasks.Finish(m.TaskID, m.ExitCode, m.Err) {
			a.setMessage("a task failed, see the task panel", true)
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
			a.openModal(components.NewModal(components.SevError, "action failed",
				[]string{m.Err.Error()}, nil))
		} else {
			a.setMessage(m.Label, false)
		}
		if m.Refresh {
			return a, cmds.RefreshAll(a.ctx, a.client)
		}
		return a, nil

	case cmds.PruneDoneMsg:
		return a, a.applyPruneResult(m)

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

	case components.DismissedMsg:
		a.closeModal()
		a.picker = nil
		a.prompt = nil
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

	case views.PickerRequest:
		p := components.NewPicker(m.Title, m.Choices)
		p.SetSize(a.width, a.height)
		a.picker = &p
		return a, nil

	case views.ReadOnlyMsg:
		a.setMessage("read-only session: that action is disabled", true)
		return a, nil

	case views.LogsRequest:
		return a, a.openContainerLogs(m.ContainerID, m.Name)

	case views.ComposeLogsRequest:
		return a, a.openProjectLogs(m.Project)

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
	if a.logsOpen {
		cmd, closed := a.logs.Update(msg)
		if closed {
			a.closeLogs()
		}
		return a, cmd
	}
	if a.tasks.Visible {
		if key.Matches(msg, a.keys.Global.Tasks) || key.Matches(msg, a.keys.Global.Escape) {
			a.tasks.Visible = false
			return a, nil
		}
		return a, a.tasks.Update(msg)
	}

	// Global keys, unless the active view is capturing text for its filter.
	if !a.filtering() {
		switch {
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

	cmd := a.views[a.active].Update(msg)
	// Scrolling changes which containers are visible, which changes which ones
	// are worth streaming when the cap is reached.
	a.syncStatsStreams()
	return a, cmd
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

// layout hands each pane the space the frame will give it. The arithmetic is
// the same as in frame(): status bar, message line and hint line are fixed, and
// the tabbed views lose one more line to the tab bar.
func (a *App) layout() {
	paneHeight := a.height - 3
	if paneHeight < 1 {
		paneHeight = 1
	}

	viewHeight := paneHeight - 1
	if viewHeight < 3 {
		viewHeight = 3
	}
	for _, v := range a.views {
		v.SetSize(a.width, viewHeight)
	}
	a.logs.SetSize(a.width, paneHeight)
	a.tasks.SetSize(a.width, paneHeight)
	if a.modal != nil {
		a.modal.SetSize(a.width, a.height)
	}
	if a.picker != nil {
		a.picker.SetSize(a.width, a.height)
	}
	if a.prompt != nil {
		a.prompt.SetSize(a.width, a.height)
	}
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
	a.closeLogs()
	for id, cancel := range a.statsCancels {
		cancel()
		delete(a.statsCancels, id)
	}
	a.cancel()
	return tea.Quit
}
