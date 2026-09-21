package views

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Compose groups containers by project and service, and is where stacks are
// acted on. Compose is a CLI plugin, so every mutation shells out
// (AGENTS.md section 8).
type Compose struct {
	base
	// collapsed remembers which projects are folded, by name.
	collapsed map[string]bool
}

// NewCompose builds the Compose view.
func NewCompose(d Deps) *Compose {
	cols := []components.Column{
		{Title: "SERVICE", SortKey: "name", MinWidth: 16},
		{Title: "DRIFT", Width: 5},
		{Title: "STATE", Width: 16},
		{Title: "IMAGE", Width: 24, Priority: 3},
		{Title: "CPU", Width: 8, Right: true, Priority: 1},
		{Title: "MEM", Width: 10, Right: true, Priority: 2},
		{Title: "PORTS", Width: 16, Priority: 4},
	}

	t := components.NewTable(cols)
	t.Empty = "no compose projects on this host"

	return &Compose{base: base{deps: d, table: t}, collapsed: map[string]bool{}}
}

// Title is the tab label.
func (v *Compose) Title() string { return "Compose" }

// Refresh rebuilds the project tree.
func (v *Compose) Refresh() {
	projects := state.Filter(v.deps.Store.Projects, v.table.Query(), state.ProjectFields)

	var rows []components.Row
	for _, p := range projects {
		// The project is a row of its own, above its services: it is where the
		// stack is folded and where an action means the whole stack. A service
		// line answers for that service only, which is the one rule in this
		// view worth learning.
		rows = append(rows, components.Row{
			ID:   p.Name + "/",
			Full: v.projectLine(p),
		})

		if v.collapsed[p.Name] || len(p.Services) == 0 {
			continue
		}

		for _, s := range p.Services {
			rows = append(rows, v.serviceRow(p, s))
		}
		for _, c := range p.OneOff {
			// `compose run` leftovers belong to no service and nobody ever
			// cleans them up (AGENTS.md section 8.5).
			rows = append(rows, components.Row{
				ID:  p.Name + "/oneoff/" + c.ID,
				Dim: true,
				Cells: []components.Cell{
					components.Txt(c.Name),
					components.Txt("-"),
					components.Txt(formatStatus(c)),
					components.Txt(c.Image),
					components.Txt("-"),
					components.Txt("-"),
					components.Txt("one-off"),
				},
			})
		}
	}

	v.table.SetRows(rows)
}

// projectLine is the project row, carrying the facts that decide what the user
// can do with the stack, and a marker saying whether its services are showing.
func (v *Compose) projectLine(p compose.Project) string {
	s := theme.Current()

	marker := "v"
	if v.collapsed[p.Name] {
		marker = ">"
	}

	label := s.Faint.Render(marker+" ") + s.Title.Render(p.Name) +
		s.Dim.Render(fmt.Sprintf("  %d/%d running", p.Running(), p.Total()))

	switch {
	case p.Ghost && len(p.ConfigFiles) == 0:
		// Nothing runs, so nothing carries the labels that said where the file
		// was. The file may well still be there; hublot simply has no record
		// of it any more (AGENTS.md section 8.5).
		label += s.Warning.Render("  stopped, only volumes and networks left")
	case p.Orphaned && p.Ghost:
		label += s.Danger.Render("  ghost, its compose file is gone")
	case p.Ghost:
		label += s.Warning.Render("  stopped, volumes still here")
	case p.Orphaned:
		label += s.Danger.Render("  orphaned, " + missingFiles(p))
	case p.Drifted():
		label += s.Warning.Render("  → config changed")
	}
	if p.WorkingDir != "" {
		label += s.Faint.Render("  " + p.WorkingDir)
	}
	return label
}

func missingFiles(p compose.Project) string {
	if len(p.ConfigFiles) == 0 {
		// The path lived on the containers' labels, and they are gone.
		return "nothing here records where its compose file is"
	}
	return "cannot read " + strings.Join(p.ConfigFiles, ", ")
}

func (v *Compose) serviceRow(p compose.Project, s compose.Service) components.Row {
	st := theme.Current()

	var cpu, mem, image, ports string
	var running int
	for _, c := range s.Containers {
		if c.Running() {
			running++
		}
		if image == "" {
			image = c.Image
		}
		if pl := formatPorts(c.Ports); pl != "" && ports == "" {
			ports = pl
		}
		if sample, ok := v.deps.Store.Stats[c.ID]; ok && sample.CPUValid {
			cpu = fmt.Sprintf("%.1f%%", sample.CPUPercent)
			mem = state.FormatBytes(sample.MemUsage)
		}
	}
	if cpu == "" {
		cpu, mem = "-", "-"
	}

	drift := components.Styled(s.Drift.Marker(), driftStyle(s.Drift))

	return components.Row{
		ID:  p.Name + "/" + s.Name,
		Dim: running == 0,
		Cells: []components.Cell{
			components.Txt("  " + s.Name),
			drift,
			components.Styled(s.State(), st.Dim),
			components.Txt(image),
			components.Txt(cpu),
			components.Txt(mem),
			components.Txt(ports),
		},
	}
}

// driftStyle colours the per-service marker (AGENTS.md section 8.4).
func driftStyle(d compose.Drift) lipgloss.Style {
	s := theme.Current()
	switch d {
	case compose.DriftNone:
		return s.Running
	case compose.DriftChanged:
		return s.Warning
	case compose.DriftStopped:
		return s.Dim
	default:
		return s.Danger
	}
}

// Update handles the Compose bindings.
func (v *Compose) Update(msg tea.Msg) tea.Cmd {
	if cmd, handled := v.table.Update(msg, v.deps.Keys); handled {
		v.Refresh()
		return cmd
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	k := v.deps.Keys.Compose
	p, service, ok := v.currentTarget()
	if !ok {
		return nil
	}

	if !v.deps.CLI.Available && needsCLI(km, k) {
		// Compose actions were disabled at startup rather than failing here
		// (AGENTS.md section 8.2).
		return v.info("compose actions are unavailable", []string{v.deps.CLI.Reason})
	}

	switch {
	case key.Matches(km, k.Expand):
		// Only from the project line. Folding a stack from one of its services
		// moves that service out from under the cursor, which reads as the
		// list jumping about for no reason.
		if !v.onProject() {
			return announce("fold " + p.Name + " from its project line, above its services")
		}
		v.collapsed[p.Name] = !v.collapsed[p.Name]
		v.Refresh()
		return nil

	case key.Matches(km, k.Drift):
		return cmds.CheckDrift(v.deps.Ctx, v.deps.CLI, p)

	case key.Matches(km, k.Config):
		return cmds.ShowComposeConfig(v.deps.Ctx, v.deps.CLI, p)

	case key.Matches(km, k.Logs):
		// The row under the cursor, because that is what reading logs usually
		// means: one service, not the whole stack at once.
		if service.Name != "" {
			return request(ComposeLogsRequest{Project: p, Service: service.Name})
		}
		return request(ComposeLogsRequest{Project: p})

	case key.Matches(km, k.LogsAll):
		return request(ComposeLogsRequest{Project: p})

	case key.Matches(km, k.New):
		if v.deps.ReadOnly {
			return denied()
		}
		return v.startFromFile()

	case key.Matches(km, k.View):
		return v.read(p)

	case key.Matches(km, k.Edit):
		if v.deps.ReadOnly {
			return denied()
		}
		return v.edit(p)

	case key.Matches(km, k.Up):
		// Scoped to the row, like the logs: a stack is usually acted on one
		// service at a time, and the whole stack is one keypress away by
		// collapsing it first.
		return v.run(p, scopedTitle("up -d", service.Name), scopedArgs(service.Name, "up", "-d"))

	case key.Matches(km, k.UpRecreate):
		return v.confirmRun(p, "up -d --force-recreate",
			[]string{"up", "-d", "--force-recreate"},
			[]string{
				fmt.Sprintf("Every container of %s is recreated, even those whose config did not change.", p.Name),
				"Anonymous volumes attached to them are recreated empty; named volumes are kept.",
			}, components.SevDanger)

	case key.Matches(km, k.Pull):
		return v.run(p, "pull", []string{"pull"})

	case key.Matches(km, k.Build):
		return v.run(p, "build", []string{"build"})

	case key.Matches(km, k.Stop):
		return v.run(p, scopedTitle("stop", service.Name), scopedArgs(service.Name, "stop"))

	case key.Matches(km, k.Restart):
		return v.run(p, scopedTitle("restart", service.Name), scopedArgs(service.Name, "restart"))

	case key.Matches(km, k.ScaleUp):
		return v.scale(p, service, 1)

	case key.Matches(km, k.ScaleDown):
		return v.scale(p, service, -1)

	case key.Matches(km, k.Down):
		return v.downConfirm(p, false)

	case key.Matches(km, k.DownVolumes):
		return v.downConfirm(p, true)
	}
	return nil
}

// needsCLI reports whether a key would shell out to the Compose binary, so an
// unavailable CLI is explained once rather than per action. The bindings come
// from the keys package like everywhere else, so adding one here cannot drift
// from what the view dispatches on.
func needsCLI(km tea.KeyMsg, k keys.Compose) bool {
	for _, b := range []key.Binding{
		k.Up, k.UpRecreate, k.Pull, k.Build, k.Stop, k.Restart,
		k.ScaleUp, k.ScaleDown, k.Config, k.Drift, k.Down, k.DownVolumes,
	} {
		if key.Matches(km, b) {
			return true
		}
	}
	return false
}

// currentTarget resolves the project, and the service when the cursor is on one.
// onProject says whether the cursor is on a project line rather than on one of
// its services. The project line is where the stack is folded and where an
// action means all of it.
func (v *Compose) onProject() bool {
	_, rest, _ := strings.Cut(v.table.CurrentID(), "/")
	return rest == ""
}

func (v *Compose) currentTarget() (compose.Project, compose.Service, bool) {
	id := v.table.CurrentID()
	if id == "" {
		return compose.Project{}, compose.Service{}, false
	}

	projectName, rest, _ := strings.Cut(id, "/")
	p, ok := v.deps.Store.ProjectByName(projectName)
	if !ok {
		return compose.Project{}, compose.Service{}, false
	}

	for _, s := range p.Services {
		if s.Name == rest {
			return p, s, true
		}
	}
	return p, compose.Service{}, true
}

// run asks the app to execute a Compose command as a task, with the preflight
// every mutation needs.
func (v *Compose) run(p compose.Project, title string, args []string) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if !p.Actionable() {
		return v.orphanExplanation(p)
	}
	return request(ComposeRunRequest{Project: p, Title: title, Args: args, Preflight: true})
}

// confirmRun runs a command behind a confirmation naming what it destroys.
func (v *Compose) confirmRun(p compose.Project, title string, args, body []string, sev components.Severity) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if !p.Actionable() {
		return v.orphanExplanation(p)
	}
	return request(ConfirmRequest{
		Severity: sev,
		Title:    fmt.Sprintf("%s on %s", title, p.Name),
		Body:     append(body, "", v.deps.CLI.DisplayCommand(p, args...)),
		Run: func() tea.Cmd {
			return request(ComposeRunRequest{Project: p, Title: title, Args: args, Preflight: true})
		},
	})
}

// orphanExplanation says why a stack cannot be acted on through the CLI, and
// what is left (AGENTS.md section 8.5).
func (v *Compose) orphanExplanation(p compose.Project) tea.Cmd {
	return v.info("compose file missing for "+p.Name, []string{
		missingFiles(p),
		"",
		"Compose needs the file it was started from, so up, down and build cannot run.",
		"The containers, volumes and networks of this project can still be stopped and removed",
		"from the other views, which go through the engine API directly.",
	})
}

// scale changes a service's replica count.
func (v *Compose) scale(p compose.Project, s compose.Service, delta int) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if s.Name == "" {
		return v.info("no service selected", []string{
			"Scaling applies to one service: put the cursor on a service row first.",
		})
	}
	if !p.Actionable() {
		return v.orphanExplanation(p)
	}

	want := s.Replicas() + delta
	if want < 0 {
		want = 0
	}
	if want == s.Replicas() {
		return nil
	}

	title := fmt.Sprintf("scale %s=%d", s.Name, want)
	args := []string{"up", "-d", "--no-deps", "--scale", fmt.Sprintf("%s=%d", s.Name, want), s.Name}

	if delta < 0 {
		return v.confirmRun(p, title, args, []string{
			fmt.Sprintf("%s goes from %d to %d replica(s); the extra containers are removed.",
				s.Name, s.Replicas(), want),
		}, components.SevConfirm)
	}
	return request(ComposeRunRequest{Project: p, Title: title, Args: args, Preflight: true})
}

// downConfirm is the graded confirmation for tearing a stack down. With
// volumes it is the data-loss case, so it asks for a typed word.
func (v *Compose) downConfirm(p compose.Project, withVolumes bool) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if !p.Actionable() {
		return v.orphanExplanation(p)
	}

	args := []string{"down"}
	title := "down"
	sev := components.SevDanger
	body := []string{
		fmt.Sprintf("Every container and network of %s is removed. Named volumes are kept.", p.Name),
	}

	if withVolumes {
		args = append(args, "-v", "--remove-orphans")
		title = "down -v --remove-orphans"
		sev = components.SevTyped
		body = []string{
			fmt.Sprintf("Every container, network and named volume of %s is destroyed.", p.Name),
			"",
			"These volumes and their contents go:",
		}
		if len(p.Volumes) == 0 {
			body = append(body, "  (none recorded on this host)")
		}
		for _, vol := range p.Volumes {
			body = append(body, fmt.Sprintf("  %s  %s", vol.Name, state.FormatBytes(vol.Size)))
		}
		body = append(body, "", "There is no undo.")
	}

	body = append(body, "", v.deps.CLI.DisplayCommand(p, args...))

	return request(ConfirmRequest{
		Severity: sev,
		Title:    fmt.Sprintf("%s on %s", title, p.Name),
		Body:     body,
		Run: func() tea.Cmd {
			return request(ComposeRunRequest{Project: p, Title: title, Args: args, Preflight: true})
		},
	})
}

// edit opens the project's compose file, and offers to apply what was written
// when the editor closes: a file changed and not applied is exactly the drift
// this view exists to show.
// read shows the compose file in the panel, beside the stack it describes.
// Reading a file is not editing it, and handing someone an editor to answer
// "what does this stack actually say" is how a file gets changed by accident.
func (v *Compose) read(p compose.Project) tea.Cmd {
	if len(p.ConfigFiles) == 0 {
		return v.info("nothing to read for "+p.Name, []string{
			missingFiles(p),
			"",
			"c shows the configuration compose resolves from the running stack,",
			"which is the closest thing left when the file is gone.",
		})
	}

	return func() tea.Msg {
		var lines []string
		for i, path := range p.ConfigFiles {
			if i > 0 {
				lines = append(lines, "")
			}
			// Several files means overrides, and which one a line came from
			// decides what it does, so each is named.
			if len(p.ConfigFiles) > 1 {
				lines = append(lines, "# "+path, "")
			}

			body, err := os.ReadFile(path)
			if err != nil {
				return cmds.TextMsg{Title: p.Name + ": compose file", Err: err}
			}
			lines = append(lines, strings.Split(strings.TrimRight(string(body), "\n"), "\n")...)
		}

		title := p.Name + ": " + filepath.Base(p.ConfigFiles[0])
		if len(p.ConfigFiles) > 1 {
			title = p.Name + ": compose files"
		}
		return cmds.TextMsg{Title: title, Lines: lines}
	}
}

func (v *Compose) edit(p compose.Project) tea.Cmd {
	if len(p.ConfigFiles) == 0 {
		return v.info("nothing to edit for "+p.Name, []string{
			missingFiles(p),
			"",
			"Use n to point hublot at a compose file, or to write a new one.",
		})
	}

	// A project can be built from several files; the first is the one it was
	// created with, and the others override it.
	path := p.ConfigFiles[0]
	return request(EditRequest{
		Path:  path,
		Title: p.Name,
		Then: func() tea.Cmd {
			return request(ConfirmRequest{
				Severity: components.SevConfirm,
				Title:    "Apply the changes to " + p.Name + "?",
				Body: []string{
					"Recreating the services whose configuration changed, and leaving the",
					"others as they are. This is what compose itself does on up.",
					"",
					v.deps.CLI.DisplayCommand(p, "up", "-d"),
				},
				Run: func() tea.Cmd {
					return request(ComposeRunRequest{
						Project: p, Title: "up -d", Args: []string{"up", "-d"}, Preflight: true,
					})
				},
			})
		},
	})
}

// scopedArgs narrows a compose command to one service when the cursor is on
// one. Compose takes the service names after the command, so this is the same
// invocation with a name appended.
func scopedArgs(service string, command ...string) []string {
	if service == "" {
		return command
	}
	return append(command, service)
}

// scopedTitle says what the task panel should call it.
func scopedTitle(title, service string) string {
	if service == "" {
		return title
	}
	return title + " " + service
}

// startFromFile brings up a stack that is not running yet, which is the one
// thing the label-based discovery cannot show: a compose file on disk with
// nothing created from it.
func (v *Compose) startFromFile() tea.Cmd {
	if !v.deps.CLI.Available {
		return v.info("compose actions are unavailable", []string{v.deps.CLI.Reason})
	}

	return request(PromptRequest{
		Title: "A stack from a file",
		Body: []string{
			"The path to a compose file, or to the directory holding one:",
			"/srv/blog, ~/stacks/blog, or ./stack next to where hublot was started.",
			"",
			"A directory with no compose file in it gets one written for you,",
			"opened in your editor. The project is named after that directory,",
			"as compose itself would name it.",
		},
		// Deliberately empty: prefilling a relative path makes typing an
		// absolute one produce a broken join.
		Label: "path: ",
		Run: func(path string) tea.Cmd {
			project, err := projectFromPath(path)
			if err == nil {
				return request(ComposeRunRequest{
					Project:   project,
					Title:     "up -d",
					Args:      []string{"up", "-d"},
					Preflight: true,
				})
			}
			if !errors.Is(err, errNoComposeFile) {
				return v.info("that path cannot be used", []string{err.Error()})
			}
			return v.writeNewStack(path)
		},
	})
}

// starterCompose is what a new stack starts from: enough to be valid, and
// commented enough that the next line is obvious. Nobody wants an empty file
// and a blinking cursor.
// Nothing here binds a host port. A template that published one would fail to
// start the moment a second stack was made from it, or whenever anything else
// already held that port, and the first thing a new user would see is a
// networking error that has nothing to do with what they wrote.
const starterCompose = `# Written by hublot. Edit, save, and it will offer to bring this up.
services:
  web:
    image: nginx:alpine
    # Publish it when you want to reach it from outside, host port first.
    # Pick one nothing else is using.
    # ports:
    #   - "8080:80"
    # volumes:
    #   - ./site:/usr/share/nginx/html:ro

  # A second service finds the first one by its name, here "web".
  # worker:
  #   image: alpine:latest
  #   command: sh -c "while true; do echo working; sleep 5; done"

# volumes:
#   data:
`

// writeNewStack creates the directory and the file, then opens the editor on
// it and offers to bring the result up.
func (v *Compose) writeNewStack(path string) tea.Cmd {
	dir, err := filepath.Abs(state.HomePath(path))
	if err != nil {
		return v.info("that path cannot be used", []string{err.Error()})
	}

	// A path that names a file gets that file; a directory gets a compose.yml
	// inside it.
	file := filepath.Join(dir, "compose.yml")
	if ext := filepath.Ext(dir); ext == ".yml" || ext == ".yaml" {
		file, dir = dir, filepath.Dir(dir)
	}

	// What hublot creates here is remembered, so quitting the editor without
	// writing can undo exactly that and nothing else.
	_, dirErr := os.Stat(dir)
	createdDir := errors.Is(dirErr, os.ErrNotExist)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return v.info("that directory cannot be created", []string{err.Error()})
	}

	createdFile := false
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(file, []byte(starterCompose), 0o644); err != nil {
			return v.info("that file cannot be written", []string{err.Error()})
		}
		createdFile = true
	}

	project := compose.Project{
		Name:        filepath.Base(dir),
		WorkingDir:  dir,
		ConfigFiles: []string{file},
	}

	return request(EditRequest{
		Path:  file,
		Title: project.Name,
		Then: func() tea.Cmd {
			return request(ConfirmRequest{
				Severity: components.SevConfirm,
				Title:    "Bring up " + project.Name + "?",
				Body: []string{
					"Starting the services in " + file + ".",
					"",
					v.deps.CLI.DisplayCommand(project, "up", "-d"),
				},
				Run: func() tea.Cmd {
					return request(ComposeRunRequest{
						Project: project, Title: "up -d",
						Args: []string{"up", "-d"}, Preflight: true,
					})
				},
			})
		},
		// Quitting without writing means the stack was never wanted: the
		// starter file and the directory go back out, and nothing else is
		// touched. Said out loud, or a directory quietly disappearing looks
		// like a failure.
		Unchanged: func() tea.Cmd {
			return tea.Batch(
				undoNewStack(file, dir, createdFile, createdDir),
				announce("nothing written, so "+file+" was not kept"),
			)
		},
	})
}

// undoNewStack removes what writeNewStack created, and only that: a directory
// that already held something is left alone, and so is a file that was already
// there.
func undoNewStack(file, dir string, createdFile, createdDir bool) tea.Cmd {
	if !createdFile {
		return nil
	}
	if err := os.Remove(file); err != nil {
		return request(ConfirmRequest{
			Severity: components.SevError,
			Title:    "the file could not be removed",
			Body:     []string{err.Error()},
		})
	}

	if createdDir {
		// Only when empty: anything else in there is not ours to delete.
		_ = os.Remove(dir)
	}
	return nil
}

// errNoComposeFile means the path exists but holds nothing compose can read,
// which is the moment to offer writing one rather than to refuse.
var errNoComposeFile = errors.New("no compose file there")

// projectFromPath resolves what was typed into the project compose would make
// of it: the file, the directory it sits in, and that directory's name.
func projectFromPath(path string) (compose.Project, error) {
	resolved, err := filepath.Abs(state.HomePath(path))
	if err != nil {
		return compose.Project{}, fmt.Errorf("resolving %s: %w", path, err)
	}

	info, err := os.Stat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		// Nothing there yet: the caller offers to write one.
		return compose.Project{}, errNoComposeFile
	}
	if err != nil {
		return compose.Project{}, fmt.Errorf("reading %s: %w", resolved, err)
	}

	dir, file := resolved, ""
	if info.IsDir() {
		for _, candidate := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
			if _, err := os.Stat(filepath.Join(resolved, candidate)); err == nil {
				file = filepath.Join(resolved, candidate)
				break
			}
		}
		if file == "" {
			return compose.Project{}, errNoComposeFile
		}
	} else {
		dir = filepath.Dir(resolved)
		file = resolved
	}

	return compose.Project{
		Name:        filepath.Base(dir),
		WorkingDir:  dir,
		ConfigFiles: []string{file},
	}, nil
}

func (v *Compose) info(title string, body []string) tea.Cmd {
	return request(ConfirmRequest{Severity: components.SevInfo, Title: title, Body: body})
}

// Hints are the footer bindings.
func (v *Compose) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Compose.Logs, k.Compose.Up, k.Compose.Stop, k.Compose.View,
		k.Compose.Edit, k.Compose.New, k.Compose.Down, k.Compose.Drift,
		k.Compose.LogsAll, k.Compose.Expand, k.Global.Help, k.Global.Quit,
	}
}

// Filtering reports whether the filter input has focus.
func (v *Compose) Filtering() bool { return v.table.Filtering() }

// Summary is the state of every stack in one line: what runs, what drifted,
// and what is left behind.
func (v *Compose) Summary() string {
	var services, running, drifted, ghosts, orphans, oneoff int
	for _, p := range v.deps.Store.Projects {
		for _, svc := range p.Services {
			services++
			if svc.Running() > 0 {
				running++
			}
			if svc.Drift == compose.DriftChanged {
				drifted++
			}
		}
		if p.Ghost {
			ghosts++
		}
		if p.Orphaned {
			orphans++
		}
		oneoff += len(p.OneOff)
	}

	parts := []string{
		plural(len(v.deps.Store.Projects), "project"),
		fmt.Sprintf("%d/%d services up", running, services),
	}
	if drifted > 0 {
		parts = append(parts, fmt.Sprintf("%d drifted", drifted))
	}
	if ghosts > 0 {
		parts = append(parts, fmt.Sprintf("%d stopped with volumes left", ghosts))
	}
	if orphans > 0 {
		parts = append(parts, fmt.Sprintf("%d orphaned", orphans))
	}
	if oneoff > 0 {
		parts = append(parts, plural(oneoff, "one-off leftover"))
	}
	return summaryOf(parts...)
}
