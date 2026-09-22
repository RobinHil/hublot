package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
)

// EditFormRequest builds the form that changes a container that already
// exists, filled with what that container is made of.
//
// A container is two things at once, and the form says which is which: a name,
// a restart policy and limits, which the daemon will change on the container
// as it stands, and an image, a command, an environment, ports and binds,
// which it only ever reads at creation. Changing one of the second kind means
// building another container, so the form collects both and the dialog
// afterwards says which one is happening.
func EditFormRequest(deps Deps, spec docker.ContainerSpec) FormRequest {
	before := state.EditPrefill(spec)
	field := func(name, label, hint string, options ...string) components.Field {
		return components.Field{
			Name: name, Label: label, Hint: hint,
			Value: before[name], Options: options,
		}
	}

	return FormRequest{
		Title:    "Edit " + spec.Name,
		Subtitle: "the first four are applied where it stands, the rest rebuild it",
		Fields: []components.Field{
			field("name", "name", "unique on this host; the container is renamed, not replaced"),
			field("restart", "restart", "left and right to choose",
				"no", "on-failure", "unless-stopped", "always"),
			// Zero is not "no limit" to the update endpoint, it is "leave this
			// alone", so clearing one is a rebuild and the hint says so rather
			// than letting the field quietly do nothing.
			field("cpus", "cpus", "cores, such as 1.5; 0 clears the limit, which rebuilds it"),
			field("memory", "memory", "such as 512m; 0 clears the limit, which rebuilds it"),
			field("image", "image", "rebuilds the container; pull it first if this host lacks it"),
			field("command", "command", "replaces the image's own; rebuilds the container"),
			field("env", "env", "KEY=value pairs, comma separated; rebuilds the container"),
			field("ports", "ports", "host:container, comma separated; rebuilds the container"),
			field("mounts", "volumes", "source:/path[:ro], comma separated; rebuilds the container"),
		},
		Run: func(values map[string]string) tea.Cmd {
			if deps.ReadOnly {
				return denied()
			}

			edit, err := state.EditFromForm(spec, values)
			if err != nil {
				return request(ConfirmRequest{
					Severity: components.SevError,
					Title:    "that form cannot be applied",
					Body:     []string{err.Error()},
				})
			}

			switch {
			case edit.Empty():
				return announce("nothing was changed in " + spec.Name)
			case !edit.NeedsRecreate():
				return cmds.ApplyEdit(deps.Ctx, deps.Client, spec.ID, spec.Name, edit)
			}

			// The same courtesy the run form extends: a reference this host
			// does not hold is fetched rather than refused at create time.
			if edit.Image != nil && !cmds.HasImage(deps.Ctx, deps.Client, *edit.Image) {
				return tea.Batch(
					request(PullRequest{Ref: *edit.Image}),
					request(ConfirmRequest{
						Severity: components.SevInfo,
						Title:    "pulling " + *edit.Image + " first",
						Body: []string{
							"This host does not have that image yet, so it is being fetched.",
							"The task panel shows the progress; edit again once it is done.",
						},
					}),
				)
			}

			return request(ConfirmRequest{
				Severity: components.SevDanger,
				Title:    "Rebuild " + spec.Name,
				Body:     rebuildBody(spec, edit),
				Run: func() tea.Cmd {
					return cmds.RecreateContainer(deps.Ctx, deps.Client,
						spec.ID, spec.Name, edit, deps.StopTimeout)
				},
			})
		},
	}
}

// rebuildBody says exactly what a rebuild does, which is more than the changes
// themselves: what survives it, and what does not.
func rebuildBody(spec docker.ContainerSpec, edit docker.Edit) []string {
	body := []string{
		fmt.Sprintf("%s is replaced by a container built from these changes:", spec.Name),
		"",
	}
	body = append(body, state.DescribeEdit(spec, edit)...)
	body = append(body,
		"",
		"Everything the form does not show is carried over: user, working directory,",
		"entrypoint, health check, labels and the networks it is attached to.",
	)

	if spec.Running {
		body = append(body, "It is stopped, rebuilt under the same name, and started again.")
	} else {
		body = append(body, "It is rebuilt under the same name, and left stopped as it is now.")
	}

	body = append(body,
		"",
		"Its logs go with the old container, and so does anything written inside it",
		"outside a volume. Anonymous volumes are kept, no longer attached to anything.",
	)

	if project := compose.ProjectOf(spec.Labels); project != "" {
		body = append(body,
			"",
			fmt.Sprintf("%s belongs to the compose project %s: the next up rebuilds it", spec.Name, project),
			"from the file, and this change goes with it.",
		)
	}

	return body
}
