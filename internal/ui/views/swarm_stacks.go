package views

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/swarm"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
)

// stackFileNames are looked for, in this order, in a directory given as the
// place a stack lives.
var stackFileNames = []string{
	"stack.yml", "stack.yaml", "docker-stack.yml", "docker-stack.yaml",
	"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml",
}

// errNoStackFile means the path holds nothing to deploy, which is the moment
// to offer writing a file rather than to refuse.
var errNoStackFile = errors.New("no stack file there")

// stackFileAt resolves a typed path to a stack file.
func stackFileAt(path string) (string, error) {
	resolved, err := filepath.Abs(state.HomePath(path))
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return resolved, errNoStackFile
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", resolved, err)
	}
	if !info.IsDir() {
		return resolved, nil
	}
	for _, name := range stackFileNames {
		candidate := filepath.Join(resolved, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return filepath.Join(resolved, "stack.yml"), errNoStackFile
}

var stackNameJunk = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// stackName turns a directory name into a name docker accepts for a stack.
func stackName(dir string) string {
	name := strings.Trim(stackNameJunk.ReplaceAllString(filepath.Base(dir), "-"), "-_")
	if name == "" {
		return "stack"
	}
	return strings.ToLower(name)
}

// withStackFile runs an action on the files a stack was deployed from, asking
// for them first when hublot was not the one that deployed it.
func (v *Swarm) withStackFile(st swarm.Stack, then func(files []string) tea.Cmd) tea.Cmd {
	if st.Loose() {
		return v.info("these services belong to no stack",
			"They were created one by one, not deployed from a file: there is no file to use.")
	}
	files := v.deps.Store.StackFiles[st.Name]
	if len(files) > 0 && allExist(files) {
		return then(files)
	}

	body := []string{
		"docker stack deploy records nowhere which file a stack came from, so hublot",
		"only knows the ones it deployed itself. Where is the file for " + st.Name + "?",
		"A directory works too: a stack.yml or compose file in it is used.",
	}
	if len(files) > 0 {
		body = append(body, "", "hublot remembered "+strings.Join(files, ", ")+", which is gone.")
	}
	return request(PromptRequest{
		Title: "The file of " + st.Name,
		Body:  body,
		Label: "path: ",
		Run: func(path string) tea.Cmd {
			file, err := stackFileAt(path)
			if err != nil {
				return v.info("that path cannot be used", err.Error())
			}
			found := []string{file}
			return tea.Batch(request(RememberStackRequest{Stack: st.Name, Files: found}), then(found))
		},
	})
}

func allExist(files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return false
		}
	}
	return true
}

// needsCLI explains an action the docker CLI is needed for, when it is not
// there, rather than failing once the key is pressed.
func (v *Swarm) needsCLI() tea.Cmd {
	if v.deps.SwarmCLI.Available {
		return nil
	}
	return v.info("the docker CLI is unavailable", v.deps.SwarmCLI.Reason)
}

// deployRequest is a deploy of the files given, with the preflight for
// variables nothing sets.
func deployRequest(stack string, files []string, prune bool) SwarmRunRequest {
	title := "deploy " + stack
	if prune {
		title += " --prune"
	}
	return SwarmRunRequest{
		Title:     title,
		Args:      swarm.DeployArgs(stack, files, prune),
		Dir:       filepath.Dir(files[0]),
		Stack:     stack,
		Files:     files,
		Preflight: true,
	}
}

// deploy brings the stack in line with its file. Pruning also removes the
// services the file no longer names, which is destructive and says so.
func (v *Swarm) deploy(st swarm.Stack, prune bool) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if cmd := v.needsCLI(); cmd != nil {
		return cmd
	}
	return v.withStackFile(st, func(files []string) tea.Cmd {
		req := deployRequest(st.Name, files, prune)
		if !prune {
			return request(req)
		}
		return request(ConfirmRequest{
			Severity: components.SevDanger,
			Title:    "Deploy " + st.Name + " and prune it",
			Body: []string{
				"Services of " + st.Name + " that " + filepath.Base(files[0]) + " no longer names are removed,",
				"with every one of their tasks. Today the stack has:",
				"  " + serviceNames(st.Services),
				"",
				v.deps.SwarmCLI.Display(req.Args...),
			},
			Run: func() tea.Cmd { return request(req) },
		})
	})
}

func serviceNames(svcs []docker.Service) string {
	var names []string
	for _, svc := range svcs {
		names = append(names, svc.Name)
	}
	if len(names) == 0 {
		return "(no services)"
	}
	return strings.Join(names, ", ")
}

// removeStack is `docker stack rm`, naming everything it takes. Volumes are
// not among them: the deploy never owned them, and they live on each node.
func (v *Swarm) removeStack(st swarm.Stack) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if st.Loose() {
		return v.info("these services belong to no stack",
			"Remove them one at a time with D on their line, or select several with space first.")
	}
	if cmd := v.needsCLI(); cmd != nil {
		return cmd
	}
	body := []string{"Removed from the swarm, with every task of the services:"}
	for _, svc := range st.Services {
		body = append(body, fmt.Sprintf("  service  %s  %d task(s)", svc.Name, svc.Running))
	}
	for _, n := range st.Networks {
		body = append(body, "  network  "+n.Name)
	}
	for _, s := range st.Secrets {
		body = append(body, "  secret   "+s.Name)
	}
	for _, c := range st.Configs {
		body = append(body, "  config   "+c.Name)
	}
	body = append(body, "",
		"Volumes are kept: each node holds its own, and the Volumes view of each one removes them.",
		"", v.deps.SwarmCLI.Display(swarm.RemoveArgs(st.Name)...))

	return request(ConfirmRequest{
		Severity: components.SevDanger,
		Title:    "Remove the stack " + st.Name,
		Body:     body,
		Run: func() tea.Cmd {
			return request(SwarmRunRequest{Title: "rm " + st.Name, Args: swarm.RemoveArgs(st.Name)})
		},
	})
}

// editStack opens the stack file, and offers to deploy what was written.
func (v *Swarm) editStack(st swarm.Stack) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	return v.withStackFile(st, func(files []string) tea.Cmd {
		return request(EditRequest{
			Path:  files[0],
			Title: st.Name,
			Then:  func() tea.Cmd { return v.offerDeploy(st.Name, files) },
		})
	})
}

// offerDeploy asks before deploying what was just written. An editor closing
// is not consent to replace running tasks.
func (v *Swarm) offerDeploy(stack string, files []string) tea.Cmd {
	req := deployRequest(stack, files, false)
	return request(ConfirmRequest{
		Severity: components.SevConfirm,
		Title:    "Deploy " + stack + "?",
		Body: []string{
			"The services whose definition changed are updated, following their",
			"update settings; the others are left as they are.",
			"",
			v.deps.SwarmCLI.Display(req.Args...),
		},
		Run: func() tea.Cmd { return request(req) },
	})
}

// readStack shows the stack file beside the stack, without an editor: reading
// a file is not editing it.
func (v *Swarm) readStack(st swarm.Stack) tea.Cmd {
	return v.withStackFile(st, func(files []string) tea.Cmd {
		return func() tea.Msg {
			var lines []string
			for i, path := range files {
				if i > 0 {
					lines = append(lines, "")
				}
				if len(files) > 1 {
					lines = append(lines, "# "+path, "")
				}
				body, err := os.ReadFile(path)
				if err != nil {
					return cmds.TextMsg{Title: st.Name + ": stack file", Err: err}
				}
				lines = append(lines, strings.Split(strings.TrimRight(string(body), "\n"), "\n")...)
			}
			return cmds.TextMsg{Title: st.Name + ": " + filepath.Base(files[0]), Lines: lines}
		}
	})
}

// stackConfig shows the file as a deploy would read it, merged and resolved.
func (v *Swarm) stackConfig(st swarm.Stack) tea.Cmd {
	if cmd := v.needsCLI(); cmd != nil {
		return cmd
	}
	return v.withStackFile(st, func(files []string) tea.Cmd {
		return cmds.ShowStackConfig(v.deps.Ctx, v.deps.SwarmCLI, st.Name, files)
	})
}

// starterStack is what a new stack starts from. Like the compose starter it
// publishes nothing: a port taken on one node fails the task there, and the
// first thing a new user would see is a scheduling error.
const starterStack = `# Written by hublot. Edit, save, and it will offer to deploy this as a stack.
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 2
      # Replace one task at a time, starting the new one before stopping the old.
      update_config:
        parallelism: 1
        order: start-first
      # placement:
      #   constraints:
      #     - node.role == worker
    # Published on every node through the routing mesh, host port first.
    # ports:
    #   - "8080:80"
`

// newStack deploys a stack from a file on disk, writing a starter first when
// there is none.
func (v *Swarm) newStack() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if cmd := v.needsCLI(); cmd != nil {
		return cmd
	}
	return request(PromptRequest{
		Title: "A stack from a file",
		Body: []string{
			"The path to a stack file, or to the directory holding one:",
			"/srv/shop, ~/stacks/shop/stack.yml.",
			"",
			"A path with no file gets one written for you, opened in your editor.",
		},
		Label: "path: ",
		Run: func(path string) tea.Cmd {
			file, err := stackFileAt(path)
			switch {
			case err == nil:
				return v.askStackName(file)
			case errors.Is(err, errNoStackFile):
				return v.writeNewStack(file)
			default:
				return v.info("that path cannot be used", err.Error())
			}
		},
	})
}

// askStackName asks what to call the stack, offering the directory's name.
func (v *Swarm) askStackName(file string) tea.Cmd {
	return request(PromptRequest{
		Title: "Name the stack",
		Body: []string{
			"Every service, network, secret and config it creates is named after it,",
			"and deploying again under the same name updates it in place.",
		},
		Label:   "name: ",
		Initial: stackName(filepath.Dir(file)),
		Run: func(name string) tea.Cmd {
			return request(deployRequest(stackName(name), []string{file}, false))
		},
	})
}

// writeNewStack creates the file, opens it, and offers to deploy what was
// written; quitting without writing takes back exactly what was created.
func (v *Swarm) writeNewStack(file string) tea.Cmd {
	dir := filepath.Dir(file)
	_, dirErr := os.Stat(dir)
	createdDir := errors.Is(dirErr, os.ErrNotExist)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return v.info("that directory cannot be created", err.Error())
	}
	if err := os.WriteFile(file, []byte(starterStack), 0o644); err != nil {
		return v.info("that file cannot be written", err.Error())
	}
	return request(EditRequest{
		Path:  file,
		Title: stackName(dir),
		Then:  func() tea.Cmd { return v.askStackName(file) },
		Unchanged: func() tea.Cmd {
			return tea.Batch(
				undoNewStack(file, dir, true, createdDir),
				announce("nothing written, so "+file+" was not kept"),
			)
		},
	})
}

// newService is the form `docker service create` deserves: what a person
// types for one service, and nothing a stack file would say better.
func (v *Swarm) newService() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	if cmd := v.needsCLI(); cmd != nil {
		return cmd
	}
	return request(FormRequest{
		Title:    "Create a service",
		Subtitle: "what docker service create takes, without the flags",
		Fields: []components.Field{
			{Name: "image", Label: "image", Required: true, Hint: "nginx:alpine, pinned to a digest for every node"},
			{Name: "name", Label: "name", Hint: "optional; the swarm invents one otherwise"},
			{Name: "mode", Label: "mode", Value: docker.ModeReplicated,
				Options: []string{docker.ModeReplicated, docker.ModeGlobal, docker.ModeReplicatedJob, docker.ModeGlobalJob},
				Hint:    "replicated: a count; global: one per node"},
			{Name: "replicas", Label: "replicas", Value: "1", Hint: "for replicated services and jobs"},
			{Name: "ports", Label: "ports", Hint: "published:target, such as 8080:80, 53/udp"},
			{Name: "mounts", Label: "volumes", Hint: "volume:/path or /host/path:/path, :ro after"},
			{Name: "env", Label: "env", Hint: "KEY=value pairs, separated by commas"},
			{Name: "networks", Label: "networks", Hint: v.overlayHint()},
			{Name: "constraints", Label: "constraints", Hint: "comma separated: node.role==worker, node.labels.zone==eu"},
			{Name: "command", Label: "command", Hint: "optional, replaces the image's own"},
		},
		Run: func(values map[string]string) tea.Cmd {
			args, err := serviceArgs(values)
			if err != nil {
				return request(ConfirmRequest{
					Severity: components.SevError, Title: "that service cannot be created", Body: []string{err.Error()},
				})
			}
			return request(SwarmRunRequest{Title: "create " + orImage(values), Args: args})
		},
	})
}

func orImage(values map[string]string) string {
	if values["name"] != "" {
		return values["name"]
	}
	return values["image"]
}

// overlayHint names the networks a service can join, which are the swarm
// scoped ones: a bridge network exists on one machine only.
func (v *Swarm) overlayHint() string {
	var names []string
	for _, n := range v.deps.Store.Networks {
		if n.Scope == "swarm" && n.Name != "ingress" {
			names = append(names, n.Name)
		}
	}
	if len(names) == 0 {
		return "none exist yet: the Networks view creates one"
	}
	return strings.Join(names, ", ")
}

func serviceArgs(values map[string]string) ([]string, error) {
	env, err := state.ParseEnv(values["env"])
	if err != nil {
		return nil, err
	}
	command, err := state.SplitCommand(values["command"])
	if err != nil {
		return nil, err
	}
	replicas := values["replicas"]
	if values["mode"] != docker.ModeReplicated && values["mode"] != docker.ModeReplicatedJob {
		replicas = ""
	}
	return swarm.CreateArgs(swarm.ServiceForm{
		Name:        values["name"],
		Image:       values["image"],
		Mode:        values["mode"],
		Replicas:    replicas,
		Ports:       state.ParseList(values["ports"]),
		Mounts:      state.ParseList(values["mounts"]),
		Env:         env,
		Networks:    state.ParseList(values["networks"]),
		Constraints: swarm.SplitConstraints(values["constraints"]),
		Command:     command,
	})
}
