package views

import (
	"fmt"
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
		group := v.groupLabel(p)

		if v.collapsed[p.Name] {
			rows = append(rows, components.Row{
				ID:    p.Name + "/",
				Group: group,
				Cells: []components.Cell{
					components.Txt(fmt.Sprintf("%d service(s), collapsed", len(p.Services))),
				},
			})
			continue
		}

		if len(p.Services) == 0 {
			rows = append(rows, components.Row{
				ID:    p.Name + "/",
				Group: group,
				Warn:  true,
				Cells: []components.Cell{components.Txt("no containers left, only volumes and networks")},
			})
			continue
		}

		for _, s := range p.Services {
			rows = append(rows, v.serviceRow(p, s))
		}
		for _, c := range p.OneOff {
			// `compose run` leftovers belong to no service and nobody ever
			// cleans them up (AGENTS.md section 8.5).
			rows = append(rows, components.Row{
				ID:    p.Name + "/oneoff/" + c.ID,
				Group: group,
				Dim:   true,
				Cells: []components.Cell{
					components.Txt(c.Name),
					components.Txt("-"),
					components.Txt(c.Status),
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

// groupLabel is the project heading, carrying the facts that decide what the
// user can do with it.
func (v *Compose) groupLabel(p compose.Project) string {
	s := theme.Current()

	label := fmt.Sprintf("%s  %d/%d running", p.Name, p.Running(), p.Total())
	switch {
	case p.Orphaned && p.Ghost:
		label += s.Danger.Render("  ghost: compose file gone, only volumes and networks left")
	case p.Ghost:
		label += s.Warning.Render("  stopped, volumes and networks still on disk")
	case p.Orphaned:
		label += s.Danger.Render("  orphaned: " + missingFiles(p))
	case p.Drifted():
		label += s.Warning.Render("  config changed since these containers were created")
	}
	if p.WorkingDir != "" {
		label += s.Dim.Render("  " + p.WorkingDir)
	}
	return label
}

func missingFiles(p compose.Project) string {
	if len(p.ConfigFiles) == 0 {
		return "no compose file recorded on the containers"
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
		ID:    p.Name + "/" + s.Name,
		Group: v.groupLabel(p),
		Dim:   running == 0,
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
		v.collapsed[p.Name] = !v.collapsed[p.Name]
		v.Refresh()
		return nil

	case key.Matches(km, k.Drift):
		return cmds.CheckDrift(v.deps.Ctx, v.deps.CLI, p)

	case key.Matches(km, k.Config):
		return cmds.ShowComposeConfig(v.deps.Ctx, v.deps.CLI, p)

	case key.Matches(km, k.Logs):
		return request(ComposeLogsRequest{Project: p})

	case key.Matches(km, k.Up):
		return v.run(p, "up -d", []string{"up", "-d"})

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
		return v.run(p, "stop", []string{"stop"})

	case key.Matches(km, k.Restart):
		return v.run(p, "restart", []string{"restart"})

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

func (v *Compose) info(title string, body []string) tea.Cmd {
	return request(ConfirmRequest{Severity: components.SevInfo, Title: title, Body: body})
}

// Hints are the footer bindings.
func (v *Compose) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Global.Help, k.Compose.Expand, k.Compose.Up, k.Compose.Pull,
		k.Compose.Build, k.Compose.Logs, k.Compose.Drift, k.Compose.Config,
		k.Compose.Down, k.Global.Quit,
	}
}

// Filtering reports whether the filter input has focus.
func (v *Compose) Filtering() bool { return v.table.Filtering() }
