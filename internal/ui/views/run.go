package views

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
)

// RunFormRequest builds the form that creates a container. It asks for what a
// person running one actually types, and nothing else: anything rarer belongs
// in a compose file, which is what the compose view is for.
func RunFormRequest(deps Deps, image string) FormRequest {
	return FormRequest{
		Title:    "Run a container",
		Subtitle: "what docker run takes, without the flags",
		Fields: []components.Field{
			{
				Name: "image", Label: "image", Value: image, Required: true,
				Hint: "nginx:alpine, pulled first if this host lacks it",
			},
			{
				Name: "name", Label: "name",
				Hint: "optional and unique; the daemon invents one otherwise",
			},
			{
				Name: "ports", Label: "ports",
				Hint: "host:container, comma separated: 8080:80, 9000:9000/udp",
			},
			{
				Name: "mounts", Label: "volumes",
				Hint: "source:/path[:ro]: data:/var/lib/data, ./conf:/etc/conf:ro",
			},
			{
				Name: "env", Label: "env",
				Hint: "KEY=value pairs, separated by commas",
			},
			{
				Name: "command", Label: "command",
				Hint: "optional, replaces the image's own; quotes honoured",
			},
			{
				Name: "restart", Label: "restart", Value: "no",
				Options: []string{"no", "on-failure", "unless-stopped", "always"},
				Hint:    "left and right to choose",
			},
			{
				Name: "start", Label: "start it", Value: "yes",
				Options: []string{"yes", "no"},
				Hint:    "created either way, no leaves it stopped",
			},
		},
		Run: func(values map[string]string) tea.Cmd {
			spec, err := specFromForm(values)
			if err != nil {
				return request(ConfirmRequest{
					Severity: components.SevError,
					Title:    "that form cannot be run",
					Body:     []string{err.Error()},
				})
			}

			// A reference the host does not hold is pulled first, which is
			// what docker run does and what anyone typing one expects.
			if !cmds.HasImage(deps.Ctx, deps.Client, spec.Image) {
				return tea.Batch(
					request(PullRequest{Ref: spec.Image}),
					request(ConfirmRequest{
						Severity: components.SevInfo,
						Title:    "pulling " + spec.Image + " first",
						Body: []string{
							"This host does not have that image yet, so it is being fetched.",
							"The task panel shows the progress; run the form again once it is done.",
						},
					}),
				)
			}
			return cmds.RunContainer(deps.Ctx, deps.Client, spec)
		},
	}
}

// specFromForm turns what was typed into what the daemon takes, reporting the
// first thing that does not make sense rather than letting the daemon phrase
// it.
func specFromForm(values map[string]string) (docker.RunSpec, error) {
	spec := docker.RunSpec{
		Image:         values["image"],
		Name:          values["name"],
		Ports:         state.ParseList(values["ports"]),
		RestartPolicy: values["restart"],
		Start:         values["start"] == "yes",
	}

	mounts, err := state.ParseMounts(values["mounts"])
	if err != nil {
		return spec, err
	}
	spec.Mounts = mounts

	env, err := state.ParseEnv(values["env"])
	if err != nil {
		return spec, err
	}
	spec.Env = env

	command, err := state.SplitCommand(values["command"])
	if err != nil {
		return spec, err
	}
	spec.Command = command

	return spec, nil
}
