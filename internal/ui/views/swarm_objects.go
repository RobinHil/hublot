package views

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

func newSecretsTable() components.Table {
	t := components.NewTable([]components.Column{
		{Title: "SECRET", SortKey: "name", MinWidth: 18},
		{Title: "STACK", Width: 12, Priority: 2},
		{Title: "USED BY", Width: 26, Priority: 1},
		{Title: "CREATED", Width: 7},
		{Title: "DRIVER", Width: 10, Priority: 3},
	})
	t.Empty = "no secrets: n creates one"
	return t
}

func newConfigsTable() components.Table {
	t := components.NewTable([]components.Column{
		{Title: "CONFIG", SortKey: "name", MinWidth: 18},
		{Title: "STACK", Width: 12, Priority: 2},
		{Title: "USED BY", Width: 26, Priority: 1},
		{Title: "SIZE", Width: 8, Right: true},
		{Title: "CREATED", Width: 7},
	})
	t.Empty = "no configs: n creates one"
	return t
}

func usedBy(svcs []docker.Service) string {
	if len(svcs) == 0 {
		return "-"
	}
	var names []string
	for _, svc := range svcs {
		names = append(names, svc.Name)
	}
	return strings.Join(names, ", ")
}

func (v *Swarm) refreshSecrets() {
	s := theme.Current()
	t := &v.tables[secSecrets]
	var rows []components.Row
	for _, sec := range state.Filter(v.deps.Store.Secrets, t.Query(), state.SecretFields) {
		users := v.deps.Store.SecretUsers(sec.ID)
		rows = append(rows, components.Row{
			ID:  "secret:" + sec.ID,
			Dim: len(users) == 0,
			Cells: []components.Cell{
				components.Txt(sec.Name),
				components.Styled(sec.Stack(), s.Accent),
				components.Txt(usedBy(users)),
				components.Txt(formatAge(sec.CreatedAt)),
				components.Styled(orDash(sec.Driver), s.Dim),
			},
		})
	}
	t.SetRows(rows)
}

func (v *Swarm) refreshConfigs() {
	s := theme.Current()
	t := &v.tables[secConfigs]
	var rows []components.Row
	for _, cfg := range state.Filter(v.deps.Store.Configs, t.Query(), state.ConfigFields) {
		users := v.deps.Store.ConfigUsers(cfg.ID)
		rows = append(rows, components.Row{
			ID:  "config:" + cfg.ID,
			Dim: len(users) == 0,
			Cells: []components.Cell{
				components.Txt(cfg.Name),
				components.Styled(cfg.Stack(), s.Accent),
				components.Txt(usedBy(users)),
				components.Txt(state.FormatBytes(int64(len(cfg.Data)))),
				components.Txt(formatAge(cfg.CreatedAt)),
			},
		})
	}
	t.SetRows(rows)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (v *Swarm) currentSecret() (docker.Secret, bool) {
	kind, id := v.currentKind()
	if kind != "secret" {
		return docker.Secret{}, false
	}
	for _, s := range v.deps.Store.Secrets {
		if s.ID == id {
			return s, true
		}
	}
	return docker.Secret{}, false
}

func (v *Swarm) currentConfig() (docker.Config, bool) {
	kind, id := v.currentKind()
	if kind != "config" {
		return docker.Config{}, false
	}
	for _, c := range v.deps.Store.Configs {
		if c.ID == id {
			return c, true
		}
	}
	return docker.Config{}, false
}

func (v *Swarm) secretsKey(km tea.KeyMsg) tea.Cmd {
	k := v.deps.Keys.Swarm
	if key.Matches(km, k.New) {
		return v.newSecret()
	}
	sec, ok := v.currentSecret()
	if !ok {
		return nil
	}
	switch {
	case key.Matches(km, k.Expand), key.Matches(km, k.View):
		return v.inspectSecret(sec)
	case key.Matches(km, k.Edit):
		return v.rotateSecret(sec)
	case key.Matches(km, k.Remove):
		return v.removeObjects(true)
	case key.Matches(km, k.Palette):
		choices := []components.Choice{
			{Label: "inspect", Detail: "its value is never shown, by the daemon itself",
				Payload: func() tea.Cmd { return v.inspectSecret(sec) }},
			{Label: "replace the value", Detail: "a new version, mounted where the old one was", Destructive: true,
				Payload: func() tea.Cmd { return v.rotateSecret(sec) }},
			{Label: "create a secret", Destructive: true, Payload: func() tea.Cmd { return v.newSecret() }},
			{Label: "remove", Destructive: true, Payload: func() tea.Cmd { return v.removeObjects(true) }},
		}
		if v.deps.ReadOnly {
			choices = keepSafe(choices)
		}
		return request(PickerRequest{Title: "Actions on " + sec.Name, Choices: choices})
	}
	return nil
}

func (v *Swarm) configsKey(km tea.KeyMsg) tea.Cmd {
	k := v.deps.Keys.Swarm
	if key.Matches(km, k.New) {
		return v.newConfig()
	}
	cfg, ok := v.currentConfig()
	if !ok {
		return nil
	}
	switch {
	case key.Matches(km, k.View):
		return cmds.ShowConfigData(cfg)
	case key.Matches(km, k.Expand):
		return cmds.InspectSwarmObject(v.deps.Ctx, "config "+cfg.Name, func(ctx context.Context) ([]byte, error) {
			return v.deps.Client.InspectConfig(ctx, cfg.ID)
		})
	case key.Matches(km, k.Edit):
		return v.editConfig(cfg)
	case key.Matches(km, k.Remove):
		return v.removeObjects(false)
	case key.Matches(km, k.Palette):
		choices := []components.Choice{
			{Label: "read", Detail: "its contents", Payload: func() tea.Cmd { return cmds.ShowConfigData(cfg) }},
			{Label: "edit", Detail: "a new version, mounted where this one was", Destructive: true,
				Payload: func() tea.Cmd { return v.editConfig(cfg) }},
			{Label: "create a config", Destructive: true, Payload: func() tea.Cmd { return v.newConfig() }},
			{Label: "remove", Destructive: true, Payload: func() tea.Cmd { return v.removeObjects(false) }},
		}
		if v.deps.ReadOnly {
			choices = keepSafe(choices)
		}
		return request(PickerRequest{Title: "Actions on " + cfg.Name, Choices: choices})
	}
	return nil
}

func (v *Swarm) inspectSecret(sec docker.Secret) tea.Cmd {
	return cmds.InspectSwarmObject(v.deps.Ctx, "secret "+sec.Name, func(ctx context.Context) ([]byte, error) {
		return v.deps.Client.InspectSecret(ctx, sec.ID)
	})
}

// newSecret asks for a name, then for the value, typed hidden or read from a
// file. Never through an editor: a secret has no business in a swap file or
// an undo history.
func (v *Swarm) newSecret() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return request(PromptRequest{
		Title: "Create a secret",
		Body:  []string{"Services mount it at /run/secrets/<name> unless told otherwise."},
		Label: "name: ",
		Run: func(name string) tea.Cmd {
			return request(PickerRequest{Title: "The value of " + name, Choices: []components.Choice{
				{Label: "type it", Detail: "hidden as it is typed", Payload: func() tea.Cmd {
					return request(PromptRequest{
						Title: "The value of " + name, Label: "value: ", Secret: true,
						Run: func(value string) tea.Cmd {
							return cmds.CreateSecret(v.deps.Ctx, v.deps.Client, name, []byte(value))
						},
					})
				}},
				{Label: "read it from a file", Detail: "a key, a certificate, anything multi-line", Payload: func() tea.Cmd {
					return request(PromptRequest{
						Title: "The file holding " + name, Label: "path: ",
						Run: func(path string) tea.Cmd {
							data, err := os.ReadFile(state.HomePath(path))
							if err != nil {
								return v.info("that file cannot be read", err.Error())
							}
							return cmds.CreateSecret(v.deps.Ctx, v.deps.Client, name, data)
						},
					})
				}},
			}})
		},
	})
}

// newConfig writes the contents in the editor, or takes them from a file.
func (v *Swarm) newConfig() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return request(PromptRequest{
		Title: "Create a config",
		Body:  []string{"Services mount it as a file, at /<name> unless told otherwise."},
		Label: "name: ",
		Run: func(name string) tea.Cmd {
			return request(PickerRequest{Title: "The contents of " + name, Choices: []components.Choice{
				{Label: "write it", Detail: "in your editor", Payload: func() tea.Cmd {
					return v.editNewConfig(name, nil, func(path string) tea.Cmd {
						return cmds.CreateConfig(v.deps.Ctx, v.deps.Client, name, path)
					})
				}},
				{Label: "read it from a file", Payload: func() tea.Cmd {
					return request(PromptRequest{
						Title: "The file holding " + name, Label: "path: ",
						Run: func(path string) tea.Cmd {
							return cmds.CreateConfig(v.deps.Ctx, v.deps.Client, name, state.HomePath(path))
						},
					})
				}},
			}})
		},
	})
}

// editNewConfig hands contents to the editor in a private temporary file and
// runs then on the path once something was written. The file goes either way.
func (v *Swarm) editNewConfig(name string, initial []byte, then func(path string) tea.Cmd) tea.Cmd {
	dir, err := os.MkdirTemp("", "hublot-config-")
	if err != nil {
		return v.info("no file to edit in", err.Error())
	}
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return v.info("no file to edit in", err.Error())
	}
	cleanup := func() tea.Msg { _ = os.RemoveAll(dir); return nil }
	return request(EditRequest{
		Path:  path,
		Title: name,
		Then: func() tea.Cmd {
			return tea.Sequence(then(path), cleanup)
		},
		Unchanged: func() tea.Cmd {
			return tea.Batch(cleanup, announce("nothing written, so nothing was created"))
		},
	})
}

// editConfig makes a new version of a config from its current contents. A
// config cannot change in place: the new one is created, every service using
// the old one is moved to it at the same path, and the old one is removed.
func (v *Swarm) editConfig(cfg docker.Config) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return v.editNewConfig(cfg.Name, cfg.Data, func(path string) tea.Cmd {
		data, err := os.ReadFile(path)
		if err != nil {
			return v.info("the edit cannot be read", err.Error())
		}
		var taken []string
		for _, c := range v.deps.Store.Configs {
			taken = append(taken, c.Name)
		}
		return v.confirmReplace(cmds.Replacement{
			OldID: cfg.ID, OldName: cfg.Name,
			NewName: state.NextVersionName(cfg.Name, taken),
			Data:    data,
			Users:   v.deps.Store.ConfigUsers(cfg.ID),
		})
	})
}

// rotateSecret replaces a secret's value the same way.
func (v *Swarm) rotateSecret(sec docker.Secret) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return request(PromptRequest{
		Title: "A new value for " + sec.Name,
		Body: []string{
			"Secrets cannot change in place: a new version is created and every service",
			"using this one is moved to it, mounted at the same path.",
		},
		Label:  "value: ",
		Secret: true,
		Run: func(value string) tea.Cmd {
			var taken []string
			for _, s := range v.deps.Store.Secrets {
				taken = append(taken, s.Name)
			}
			return v.confirmReplace(cmds.Replacement{
				Secret: true, OldID: sec.ID, OldName: sec.Name,
				NewName: state.NextVersionName(sec.Name, taken),
				Data:    []byte(value),
				Users:   v.deps.Store.SecretUsers(sec.ID),
			})
		},
	})
}

func (v *Swarm) confirmReplace(r cmds.Replacement) tea.Cmd {
	body := []string{fmt.Sprintf("%s is created, and %s removed once nothing uses it.", r.NewName, r.OldName)}
	if len(r.Users) > 0 {
		body = append(body, "", "These services move to it, and their tasks are replaced:")
		for _, svc := range r.Users {
			body = append(body, "  "+svc.Name)
		}
		body = append(body, "", "If one of them refuses, the others are moved back and nothing is removed.")
	}
	for _, svc := range r.Users {
		if svc.Stack() != "" {
			body = append(body, "", "A stack file still naming "+r.OldName+" needs "+r.NewName+" instead before its",
				"next deploy, which would otherwise create the old one again or fail without it.")
			break
		}
	}
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Replace " + r.OldName,
		Body:     body,
		Run:      func() tea.Cmd { return cmds.Replace(v.deps.Ctx, v.deps.Client, r) },
	})
}

// removeObjects removes the selected secrets or configs, naming the services
// that still use one: the daemon refuses those, and saying so first spares a
// failure dialog.
func (v *Swarm) removeObjects(secrets bool) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	kind, noun := "config", "config"
	if secrets {
		kind, noun = "secret", "secret"
	}
	ids := v.targetsOf(kind)
	if len(ids) == 0 {
		return nil
	}

	var lines, inUse []string
	for _, id := range ids {
		name, users := v.objectUsers(secrets, id)
		lines = append(lines, "  "+name)
		if len(users) > 0 {
			inUse = append(inUse, fmt.Sprintf("  %s, by %s", name, usedBy(users)))
		}
	}
	body := append([]string{fmt.Sprintf("These %s(s) are removed from the swarm:", noun)}, lines...)
	if len(inUse) > 0 {
		body = append(body, "", "Still mounted, so the daemon will refuse them:")
		body = append(body, inUse...)
	}
	sev := components.SevConfirm
	if len(ids) > 1 {
		sev = components.SevDanger
	}
	return request(ConfirmRequest{
		Severity: sev,
		Title:    fmt.Sprintf("Remove %s", plural(len(ids), noun)),
		Body:     body,
		Run: func() tea.Cmd {
			if secrets {
				return cmds.RemoveSecrets(v.deps.Ctx, v.deps.Client, ids)
			}
			return cmds.RemoveConfigs(v.deps.Ctx, v.deps.Client, ids)
		},
	})
}

func (v *Swarm) objectUsers(secret bool, id string) (string, []docker.Service) {
	store := v.deps.Store
	if secret {
		for _, s := range store.Secrets {
			if s.ID == id {
				return s.Name, store.SecretUsers(id)
			}
		}
		return docker.ShortID(id), nil
	}
	for _, c := range store.Configs {
		if c.ID == id {
			return c.Name, store.ConfigUsers(id)
		}
	}
	return docker.ShortID(id), nil
}
