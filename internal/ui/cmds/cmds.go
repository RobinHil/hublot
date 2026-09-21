// Package cmds turns Docker and Compose calls into Bubble Tea commands. Every
// call runs in its own goroutine and comes back as a message: nothing blocks
// Update (AGENTS.md section 4, rule 3).
package cmds

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
)

// PollInterval is the safety net behind the event stream: events are the source
// of truth, this only catches what was missed (AGENTS.md section 7).
const PollInterval = 10 * time.Second

// ContainersMsg carries a refreshed container list.
type ContainersMsg struct {
	Containers []docker.Container
	Err        error
}

// ImagesMsg carries a refreshed image list.
type ImagesMsg struct {
	Images []docker.Image
	Err    error
}

// VolumesMsg carries a refreshed volume list.
type VolumesMsg struct {
	Volumes []docker.Volume
	Err     error
}

// NetworksMsg carries a refreshed network list.
type NetworksMsg struct {
	Networks []docker.Network
	Err      error
}

// InfoMsg carries daemon info.
type InfoMsg struct {
	Info docker.Info
	Err  error
}

// DiskMsg carries a `system df` result.
type DiskMsg struct {
	Usage docker.DiskUsage
	Err   error
}

// ActionDoneMsg reports the outcome of a mutation. The daemon's own message is
// passed through untouched (AGENTS.md section 11).
type ActionDoneMsg struct {
	Label string
	Err   error
	// Refresh asks the app to resync lists, for actions events do not cover.
	Refresh bool
}

// InspectMsg carries raw inspect output for the detail pane.
type InspectMsg struct {
	Title   string
	Content string
	Err     error
}

// HistoryMsg carries an image's layer history.
type HistoryMsg struct {
	Image  string
	Layers []docker.HistoryLayer
	Err    error
}

// DriftMsg carries freshly computed Compose config hashes.
type DriftMsg struct {
	Project string
	Hashes  map[string]string
	Err     error
}

// PruneDoneMsg reports what a prune destroyed.
type PruneDoneMsg struct {
	Reports []docker.PruneReport
	Err     error
}

// TickMsg drives the safety poll and the elapsed-time redraws.
type TickMsg time.Time

// Tick schedules the next safety poll.
func Tick() tea.Cmd {
	return tea.Tick(PollInterval, func(t time.Time) tea.Msg { return TickMsg(t) })
}

// safely runs a command, turning a panic into an ordinary error message rather
// than taking the program down with the terminal in raw mode
// (AGENTS.md section 11).
func safely(label string, fn func() tea.Msg) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = ActionDoneMsg{Label: label, Err: fmt.Errorf("%s panicked: %v", label, r)}
			}
		}()
		return fn()
	}
}

// ListContainers refreshes the container list.
func ListContainers(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing containers", func() tea.Msg {
		cs, err := c.ListContainers(ctx)
		return ContainersMsg{Containers: cs, Err: err}
	})
}

// ListImages refreshes the image list.
func ListImages(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing images", func() tea.Msg {
		is, err := c.ListImages(ctx)
		return ImagesMsg{Images: is, Err: err}
	})
}

// ListVolumes refreshes the volume list.
func ListVolumes(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing volumes", func() tea.Msg {
		vs, err := c.ListVolumes(ctx)
		return VolumesMsg{Volumes: vs, Err: err}
	})
}

// ListNetworks refreshes the network list.
func ListNetworks(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("listing networks", func() tea.Msg {
		ns, err := c.ListNetworks(ctx)
		return NetworksMsg{Networks: ns, Err: err}
	})
}

// LoadInfo reads daemon info for the status bar.
func LoadInfo(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("reading daemon info", func() tea.Msg {
		info, err := c.Info(ctx)
		return InfoMsg{Info: info, Err: err}
	})
}

// RefreshAll resyncs every list, used at startup and after the event stream
// reconnects, since events were missed while it was down
// (AGENTS.md section 6.5).
func RefreshAll(ctx context.Context, c *docker.Client) tea.Cmd {
	return tea.Batch(
		ListContainers(ctx, c),
		ListImages(ctx, c),
		ListVolumes(ctx, c),
		ListNetworks(ctx, c),
	)
}

// DiskUsage computes `system df`. Slow by nature, so it is only ever run on
// demand (AGENTS.md section 6.6).
func DiskUsage(ctx context.Context, c *docker.Client) tea.Cmd {
	return safely("computing disk usage", func() tea.Msg {
		du, err := c.DiskUsage(ctx)
		return DiskMsg{Usage: du, Err: err}
	})
}

// StartContainers starts each target.
func StartContainers(ctx context.Context, c *docker.Client, ids []string) tea.Cmd {
	return each(ctx, "start", ids, func(id string) error { return c.StartContainer(ctx, id) })
}

// StopContainers stops each target.
func StopContainers(ctx context.Context, c *docker.Client, ids []string, timeout time.Duration) tea.Cmd {
	return each(ctx, "stop", ids, func(id string) error { return c.StopContainer(ctx, id, timeout) })
}

// RestartContainers restarts each target.
func RestartContainers(ctx context.Context, c *docker.Client, ids []string, timeout time.Duration) tea.Cmd {
	return each(ctx, "restart", ids, func(id string) error { return c.RestartContainer(ctx, id, timeout) })
}

// PauseContainer pauses or unpauses depending on the container's current state.
func PauseContainer(ctx context.Context, c *docker.Client, id string, paused bool) tea.Cmd {
	label := "pause"
	fn := c.PauseContainer
	if paused {
		label, fn = "unpause", c.UnpauseContainer
	}
	return each(ctx, label, []string{id}, func(id string) error { return fn(ctx, id) })
}

// KillContainers sends a signal to each target.
func KillContainers(ctx context.Context, c *docker.Client, ids []string, signal string) tea.Cmd {
	return each(ctx, "kill", ids, func(id string) error { return c.KillContainer(ctx, id, signal) })
}

// RemoveContainers deletes each target.
func RemoveContainers(ctx context.Context, c *docker.Client, ids []string, force, volumes bool) tea.Cmd {
	return each(ctx, "remove", ids, func(id string) error {
		return c.RemoveContainer(ctx, id, force, volumes)
	})
}

// RenameContainer changes a container's name.
func RenameContainer(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return each(ctx, "rename", []string{id}, func(id string) error {
		return c.RenameContainer(ctx, id, name)
	})
}

// RemoveImages deletes each image.
func RemoveImages(ctx context.Context, c *docker.Client, ids []string, force bool) tea.Cmd {
	return each(ctx, "remove image", ids, func(id string) error {
		_, err := c.RemoveImage(ctx, id, force, true)
		return err
	})
}

// RemoveVolumes deletes each volume. This destroys data.
func RemoveVolumes(ctx context.Context, c *docker.Client, names []string, force bool) tea.Cmd {
	return each(ctx, "remove volume", names, func(name string) error {
		return c.RemoveVolume(ctx, name, force)
	})
}

// RemoveNetworks deletes each network.
func RemoveNetworks(ctx context.Context, c *docker.Client, ids []string) tea.Cmd {
	return each(ctx, "remove network", ids, func(id string) error { return c.RemoveNetwork(ctx, id) })
}

// DisconnectNetwork detaches a container from a network.
func DisconnectNetwork(ctx context.Context, c *docker.Client, networkID, containerID string) tea.Cmd {
	return each(ctx, "disconnect", []string{containerID}, func(id string) error {
		return c.DisconnectNetwork(ctx, networkID, id, false)
	})
}

// ConnectNetwork attaches a container to a network.
func ConnectNetwork(ctx context.Context, c *docker.Client, networkID, containerID string) tea.Cmd {
	return each(ctx, "connect", []string{containerID}, func(id string) error {
		return c.ConnectNetwork(ctx, networkID, id)
	})
}

// each applies an action to every target, reporting the first failure with the
// daemon's own wording.
func each(_ context.Context, label string, ids []string, fn func(string) error) tea.Cmd {
	return safely(label, func() tea.Msg {
		for _, id := range ids {
			if err := fn(id); err != nil {
				return ActionDoneMsg{Label: label, Err: err, Refresh: true}
			}
		}
		return ActionDoneMsg{
			Label:   fmt.Sprintf("%s: %d object(s)", label, len(ids)),
			Refresh: true,
		}
	})
}

// InspectContainer fetches a container's raw inspect output.
func InspectContainer(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("inspect", func() tea.Msg {
		d, err := c.InspectContainer(ctx, id)
		if err != nil {
			return InspectMsg{Title: name, Err: err}
		}
		return InspectMsg{Title: "container " + name, Content: string(d.Raw)}
	})
}

// InspectImage fetches an image's raw inspect output.
func InspectImage(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("inspect", func() tea.Msg {
		raw, err := c.InspectImage(ctx, id)
		return InspectMsg{Title: "image " + name, Content: string(raw), Err: err}
	})
}

// InspectVolume fetches a volume's raw inspect output.
func InspectVolume(ctx context.Context, c *docker.Client, name string) tea.Cmd {
	return safely("inspect", func() tea.Msg {
		raw, err := c.InspectVolume(ctx, name)
		return InspectMsg{Title: "volume " + name, Content: string(raw), Err: err}
	})
}

// InspectNetwork fetches a network's raw inspect output.
func InspectNetwork(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("inspect", func() tea.Msg {
		raw, err := c.InspectNetwork(ctx, id)
		return InspectMsg{Title: "network " + name, Content: string(raw), Err: err}
	})
}

// ImageHistory fetches an image's layers.
func ImageHistory(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("history", func() tea.Msg {
		layers, err := c.ImageHistory(ctx, id)
		return HistoryMsg{Image: name, Layers: layers, Err: err}
	})
}

// Prune runs the categories of a preview against the daemon. The preview was
// computed beforehand and shown to the user; this is what destroys things.
func Prune(ctx context.Context, c *docker.Client, cats []state.Category, until time.Duration) tea.Cmd {
	return safely("prune", func() tea.Msg {
		var reports []docker.PruneReport
		for _, cat := range cats {
			var (
				rep docker.PruneReport
				err error
			)
			switch cat {
			case state.CatContainers:
				rep, err = c.PruneContainers(ctx, until)
			case state.CatImages:
				rep, err = c.PruneImages(ctx, false, until)
			case state.CatImagesAll:
				rep, err = c.PruneImages(ctx, true, until)
			case state.CatVolumes:
				rep, err = c.PruneVolumes(ctx, false)
			case state.CatVolumesAll:
				rep, err = c.PruneVolumes(ctx, true)
			case state.CatNetworks:
				rep, err = c.PruneNetworks(ctx)
			case state.CatBuildCache:
				rep, err = c.PruneBuildCache(ctx, false)
			case state.CatBuildCacheAll:
				rep, err = c.PruneBuildCache(ctx, true)
			}
			if err != nil {
				return PruneDoneMsg{Reports: reports, Err: err}
			}
			reports = append(reports, rep)
		}
		return PruneDoneMsg{Reports: reports}
	})
}

// CheckDrift computes the config hashes the project's YAML resolves to now.
func CheckDrift(ctx context.Context, cli compose.CLI, p compose.Project) tea.Cmd {
	return safely("drift check", func() tea.Msg {
		hashes, err := cli.Hashes(ctx, p)
		return DriftMsg{Project: p.Name, Hashes: hashes, Err: err}
	})
}

// ShowComposeConfig resolves a project's YAML for display.
func ShowComposeConfig(ctx context.Context, cli compose.CLI, p compose.Project) tea.Cmd {
	return safely("compose config", func() tea.Msg {
		out, err := cli.Config(ctx, p)
		return InspectMsg{Title: "compose config: " + p.Name, Content: out, Err: err}
	})
}

// UpdateLimits changes a container's CPU and memory limits in place. Zero
// values leave the corresponding limit alone.
func UpdateLimits(ctx context.Context, c *docker.Client, id string, nanoCPUs, memory int64) tea.Cmd {
	return each(ctx, "update limits", []string{id}, func(id string) error {
		return c.UpdateContainer(ctx, id, nanoCPUs, memory)
	})
}

// TagImage adds a reference to an existing image.
func TagImage(ctx context.Context, c *docker.Client, id, ref string) tea.Cmd {
	return each(ctx, "tag "+ref, []string{id}, func(id string) error {
		return c.TagImage(ctx, id, ref)
	})
}

// UntagImage drops one reference from an image. The daemon removes the tag
// rather than the image as long as another reference remains.
func UntagImage(ctx context.Context, c *docker.Client, ref string) tea.Cmd {
	return each(ctx, "untag "+ref, []string{ref}, func(ref string) error {
		_, err := c.RemoveImage(ctx, ref, false, false)
		return err
	})
}

// SaveImage writes an image tarball to a local path.
func SaveImage(ctx context.Context, c *docker.Client, id, ref, path string) tea.Cmd {
	return safely("save image", func() tea.Msg {
		if err := c.SaveImage(ctx, []string{id}, path); err != nil {
			return ActionDoneMsg{Label: "save image", Err: err}
		}
		return ActionDoneMsg{Label: fmt.Sprintf("saved %s to %s", ref, path)}
	})
}

// CopyIntoContainer copies a local file or directory into a container.
func CopyIntoContainer(ctx context.Context, c *docker.Client, id, local, remote string) tea.Cmd {
	return safely("copy into container", func() tea.Msg {
		if err := c.CopyToContainer(ctx, id, local, remote); err != nil {
			return ActionDoneMsg{Label: "copy into container", Err: err}
		}
		return ActionDoneMsg{Label: fmt.Sprintf("copied %s to %s", local, remote)}
	})
}

// CopyOutOfContainer extracts a path from a container into a local directory.
func CopyOutOfContainer(ctx context.Context, c *docker.Client, id, remote, localDir string) tea.Cmd {
	return safely("copy out of container", func() tea.Msg {
		written, err := c.CopyFromContainer(ctx, id, remote, localDir)
		if err != nil {
			return ActionDoneMsg{Label: "copy out of container", Err: err}
		}
		return ActionDoneMsg{Label: "wrote " + written}
	})
}

// TextMsg carries a block of text the app shows in its pager: process lists,
// filesystem changes, resolved configuration.
type TextMsg struct {
	Title string
	Lines []string
	Err   error
}

// Processes lists what runs inside a container.
func Processes(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("processes", func() tea.Msg {
		titles, rows, err := c.Processes(ctx, id)
		if err != nil {
			return TextMsg{Title: "processes in " + name, Err: err}
		}
		lines := []string{strings.Join(titles, "  ")}
		for _, row := range rows {
			lines = append(lines, strings.Join(row, "  "))
		}
		return TextMsg{Title: "processes in " + name, Lines: lines}
	})
}

// Changes lists what a container has written since it started.
func Changes(ctx context.Context, c *docker.Client, id, name string) tea.Cmd {
	return safely("filesystem changes", func() tea.Msg {
		changes, err := c.Changes(ctx, id)
		if err != nil {
			return TextMsg{Title: "changes in " + name, Err: err}
		}
		if len(changes) == 0 {
			return TextMsg{
				Title: "changes in " + name,
				Lines: []string{"nothing has been written since this container started"},
			}
		}
		lines := make([]string, 0, len(changes))
		for _, change := range changes {
			lines = append(lines, fmt.Sprintf("%-8s %s", change.Kind, change.Path))
		}
		return TextMsg{Title: "changes in " + name, Lines: lines}
	})
}

// Commit turns a container into an image.
func Commit(ctx context.Context, c *docker.Client, id, name, reference string) tea.Cmd {
	return safely("commit", func() tea.Msg {
		imageID, err := c.Commit(ctx, id, reference, "committed from "+name+" by hublot")
		if err != nil {
			return ActionDoneMsg{Label: "commit", Err: err}
		}
		return ActionDoneMsg{
			Label:   fmt.Sprintf("committed %s as %s (%s)", name, reference, docker.ShortID(imageID)),
			Refresh: true,
		}
	})
}

// Export writes a container's filesystem to a tarball.
func Export(ctx context.Context, c *docker.Client, id, name, path string) tea.Cmd {
	return safely("export", func() tea.Msg {
		if err := c.Export(ctx, id, path); err != nil {
			return ActionDoneMsg{Label: "export", Err: err}
		}
		return ActionDoneMsg{Label: fmt.Sprintf("exported %s to %s", name, path)}
	})
}

// LoadImage reads a tarball back into the daemon.
func LoadImage(ctx context.Context, c *docker.Client, path string) tea.Cmd {
	return safely("load image", func() tea.Msg {
		loaded, err := c.LoadImage(ctx, path)
		if err != nil {
			return ActionDoneMsg{Label: "load image", Err: err}
		}
		label := "loaded " + path
		if len(loaded) > 0 {
			label = strings.Join(loaded, "; ")
		}
		return ActionDoneMsg{Label: label, Refresh: true}
	})
}

// CreateVolume makes a named volume.
func CreateVolume(ctx context.Context, c *docker.Client, name string) tea.Cmd {
	return each(ctx, "create volume "+name, []string{name}, func(name string) error {
		return c.CreateVolume(ctx, name)
	})
}

// CreateNetwork makes a user-defined bridge network.
func CreateNetwork(ctx context.Context, c *docker.Client, name string) tea.Cmd {
	return each(ctx, "create network "+name, []string{name}, func(name string) error {
		return c.CreateNetwork(ctx, name)
	})
}
