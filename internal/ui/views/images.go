package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/cmds"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Images lists images with what uses them, which is what decides whether one is
// safe to remove.
type Images struct {
	base
}

// NewImages builds the images view.
func NewImages(d Deps) *Images {
	cols := []components.Column{
		{Title: "REPOSITORY:TAG", SortKey: "name", MinWidth: 20},
		{Title: "ID", Width: 12, Priority: 4},
		{Title: "SIZE", SortKey: "size", Width: 10, Right: true},
		{Title: "SHARED", Width: 10, Right: true, Priority: 3},
		{Title: "CREATED", SortKey: "created", Width: 9, Right: true, Priority: 2},
		{Title: "IN USE", Width: 8, Right: true, Priority: 1},
	}

	t := components.NewTable(cols)
	t.Empty = "no images on this host"
	t.SetSortKey("size", false)

	return &Images{base: base{deps: d, table: t}}
}

// Title is the tab label.
func (v *Images) Title() string { return "Images" }

// Refresh rebuilds the rows.
func (v *Images) Refresh() {
	is := state.Filter(v.deps.Store.Images, v.table.Query(), state.ImageFields)

	if col, asc := v.table.SortKey(); col != "" {
		state.SortBy(is, imageLess(col), asc)
	}

	used := v.usage()
	rows := make([]components.Row, 0, len(is))
	for _, img := range is {
		rows = append(rows, v.row(img, used[img.ID]))
	}
	v.table.SetRows(rows)
}

// usage counts the containers referencing each image. A container is matched
// both by image id and by the name it was created from, so an image pinned only
// by tag still counts as in use; counting distinct containers keeps one
// container from being tallied twice.
func (v *Images) usage() map[string]int {
	byRef := map[string][]string{}
	for _, c := range v.deps.Store.Containers {
		if c.ImageID != "" {
			byRef[c.ImageID] = append(byRef[c.ImageID], c.ID)
		}
		if c.Image != "" {
			byRef[c.Image] = append(byRef[c.Image], c.ID)
		}
	}

	out := map[string]int{}
	for _, img := range v.deps.Store.Images {
		seen := map[string]bool{}
		for _, id := range byRef[img.ID] {
			seen[id] = true
		}
		for _, tag := range img.RepoTags {
			for _, id := range byRef[tag] {
				seen[id] = true
			}
		}
		out[img.ID] = len(seen)
	}
	return out
}

func imageLess(column string) func(a, b docker.Image) bool {
	switch column {
	case "size":
		return func(a, b docker.Image) bool { return a.Size < b.Size }
	case "created":
		return func(a, b docker.Image) bool { return a.Created.Before(b.Created) }
	default:
		return func(a, b docker.Image) bool { return a.Ref() < b.Ref() }
	}
}

func (v *Images) row(img docker.Image, inUse int) components.Row {
	s := theme.Current()

	use := "-"
	if inUse > 0 {
		use = fmt.Sprintf("%d", inUse)
	}

	name := components.Txt(img.Ref())
	if img.Dangling() {
		// Dangling images are what a plain prune removes, so they are worth
		// spotting at a glance.
		name = components.Styled(img.Ref()+" (dangling)", s.Warning)
	}

	return components.Row{
		ID:  img.ID,
		Dim: inUse == 0,
		Cells: []components.Cell{
			name,
			components.Txt(truncateID(img.ID)),
			components.Txt(state.FormatBytes(img.Size)),
			components.Txt(state.FormatBytes(img.SharedSize)),
			components.Txt(formatAge(img.Created)),
			components.Txt(use),
		},
	}
}

// Update handles the image bindings.
func (v *Images) Update(msg tea.Msg) tea.Cmd {
	if cmd, handled := v.table.Update(msg, v.deps.Keys); handled {
		v.Refresh()
		return cmd
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	k := v.deps.Keys.Images
	img, hasCurrent := v.current()

	switch {
	case key.Matches(km, k.Detail):
		if !hasCurrent {
			return nil
		}
		return cmds.InspectImage(v.deps.Ctx, v.deps.Client, img.ID, img.Ref())

	case key.Matches(km, k.History):
		if !hasCurrent {
			return nil
		}
		return cmds.ImageHistory(v.deps.Ctx, v.deps.Client, img.ID, img.Ref())

	case key.Matches(km, k.Pull):
		if !hasCurrent {
			return nil
		}
		if v.deps.ReadOnly {
			return denied()
		}
		if img.Dangling() {
			return request(ConfirmRequest{
				Severity: components.SevInfo,
				Title:    "nothing to pull",
				Body:     []string{"This image has no tag, so there is no reference to pull again."},
			})
		}
		return request(PullRequest{Ref: img.Ref()})

	case key.Matches(km, k.Remove):
		return v.removeConfirm(false)

	case key.Matches(km, k.Force):
		return v.removeConfirm(true)

	case key.Matches(km, k.Run):
		if v.deps.ReadOnly {
			return denied()
		}
		reference := ""
		if hasCurrent && !img.Dangling() {
			reference = img.Ref()
		}
		return request(RunFormRequest(v.deps, reference))

	case key.Matches(km, k.Fetch):
		if v.deps.ReadOnly {
			return denied()
		}
		return request(PromptRequest{
			Title: "Pull an image",
			Body: []string{
				"A reference such as nginx:alpine or ghcr.io/owner/name:tag.",
				"Progress goes to the task panel, which t opens.",
			},
			Label: "reference: ",
			Run: func(reference string) tea.Cmd {
				return request(PullRequest{Ref: reference})
			},
		})

	case key.Matches(km, k.Palette):
		if !hasCurrent {
			return nil
		}
		return v.palette(img)
	}
	return nil
}

// palette holds what is too rare for a key of its own: tagging, untagging and
// saving an image to a file (AGENTS.md section 9.2).
func (v *Images) palette(img docker.Image) tea.Cmd {
	choices := []components.Choice{
		{
			Label: "inspect", Detail: "raw inspect output",
			Payload: func() tea.Cmd {
				return cmds.InspectImage(v.deps.Ctx, v.deps.Client, img.ID, img.Ref())
			},
		},
		{
			Label: "history", Detail: "the layers and what built them",
			Payload: func() tea.Cmd {
				return cmds.ImageHistory(v.deps.Ctx, v.deps.Client, img.ID, img.Ref())
			},
		},
		{
			Label: "tag", Detail: "add another reference to this image", Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title: "Tag " + img.Ref(),
					Body:  []string{"A reference, such as registry/name:tag. The image keeps its other tags."},
					Label: "reference: ",
					Run: func(ref string) tea.Cmd {
						return cmds.TagImage(v.deps.Ctx, v.deps.Client, img.ID, ref)
					},
				})
			},
		},
		{
			Label: "load from a file", Detail: "read back a tarball docker save wrote",
			Destructive: true,
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title:   "Load an image",
					Body:    []string{"A tarball written by docker save, or by the entry above."},
					Label:   "path: ",
					Initial: "./",
					Run: func(path string) tea.Cmd {
						return cmds.LoadImage(v.deps.Ctx, v.deps.Client, state.HomePath(path))
					},
				})
			},
		},
		{
			Label: "save to a file", Detail: "the tarball docker load reads back",
			Payload: func() tea.Cmd {
				return request(PromptRequest{
					Title:   "Save " + img.Ref(),
					Body:    []string{"Where to write the tarball. It can be loaded again with docker load."},
					Label:   "path: ",
					Initial: defaultSavePath(img),
					Run: func(path string) tea.Cmd {
						return cmds.SaveImage(v.deps.Ctx, v.deps.Client, img.ID, img.Ref(),
							state.HomePath(path))
					},
				})
			},
		},
	}

	for _, tag := range img.RepoTags {
		if tag == "" || tag == "<none>:<none>" {
			continue
		}
		reference := tag
		choices = append(choices, components.Choice{
			Label: "untag " + reference, Detail: "drop this reference, keep the image",
			Destructive: true,
			Payload: func() tea.Cmd {
				// With another tag left the daemon only removes the reference;
				// with none left it removes the image, which is worth saying.
				body := []string{"The image keeps its other references."}
				if len(img.RepoTags) == 1 {
					body = []string{
						"This is the only reference to the image, so removing it removes the image itself.",
					}
				}
				return request(ConfirmRequest{
					Severity: components.SevConfirm,
					Title:    "Untag " + reference,
					Body:     body,
					Run: func() tea.Cmd {
						return cmds.UntagImage(v.deps.Ctx, v.deps.Client, reference)
					},
				})
			},
		})
	}

	if v.deps.ReadOnly {
		choices = keepSafe(choices)
	}
	return request(PickerRequest{Title: "Actions on " + img.Ref(), Choices: choices})
}

// defaultSavePath suggests a file name built from the reference, so the prompt
// opens on something usable.
func defaultSavePath(img docker.Image) string {
	name := img.Ref()
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return "./" + name + ".tar"
}

// PullRequest asks the app to pull an image as a task.
type PullRequest struct {
	Ref string
}

func (v *Images) current() (docker.Image, bool) {
	id := v.table.CurrentID()
	for _, img := range v.deps.Store.Images {
		if img.ID == id {
			return img, true
		}
	}
	return docker.Image{}, false
}

func (v *Images) targets() []docker.Image {
	want := map[string]bool{}
	for _, id := range v.table.Targets() {
		want[id] = true
	}
	var out []docker.Image
	for _, img := range v.deps.Store.Images {
		if want[img.ID] {
			out = append(out, img)
		}
	}
	return out
}

// removeConfirm names every image and the space it holds, and says plainly when
// one is still referenced.
func (v *Images) removeConfirm(force bool) tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	targets := v.targets()
	if len(targets) == 0 {
		return nil
	}

	used := v.usage()
	var total int64
	var lines, inUse []string
	imageIDs := make([]string, 0, len(targets))

	for _, img := range targets {
		total += img.Size
		imageIDs = append(imageIDs, img.ID)
		lines = append(lines, fmt.Sprintf("  %s  %s", img.Ref(), state.FormatBytes(img.Size)))
		if used[img.ID] > 0 {
			inUse = append(inUse, fmt.Sprintf("%s (%d container(s))", img.Ref(), used[img.ID]))
		}
	}

	body := []string{fmt.Sprintf("These %d image(s) will be removed, freeing %s:",
		len(targets), state.FormatBytes(total))}
	body = append(body, lines...)

	if len(inUse) > 0 {
		if !force {
			body = append(body, "",
				"Still referenced by containers, so the daemon will refuse: "+strings.Join(inUse, ", "),
				"Use F to remove them anyway.")
		} else {
			body = append(body, "",
				"Forced: these are still referenced and their containers will keep running from a deleted image: "+
					strings.Join(inUse, ", "))
		}
	}

	title := fmt.Sprintf("Remove %d image(s)", len(targets))
	sev := components.SevConfirm
	if len(targets) > 1 || force {
		sev = components.SevDanger
	}

	return request(ConfirmRequest{
		Severity: sev,
		Title:    title,
		Body:     body,
		Run: func() tea.Cmd {
			return cmds.RemoveImages(v.deps.Ctx, v.deps.Client, imageIDs, force)
		},
	})
}

// Hints are the footer bindings.
func (v *Images) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Images.Run, k.Images.Fetch, k.Images.Detail, k.Images.History,
		k.Images.Pull, k.Images.Remove, k.Images.Palette,
		k.Global.Filter, k.Global.Help, k.Global.Quit,
	}
}

// Filtering reports whether the filter input has focus.
func (v *Images) Filtering() bool { return v.table.Filtering() }

// Summary is what the images cost and how much of that is unused.
func (v *Images) Summary() string {
	used := v.usage()

	var total, reclaimable int64
	var unused, dangling int
	for _, img := range v.deps.Store.Images {
		total += img.Size
		if used[img.ID] == 0 {
			unused++
			reclaimable += img.Size
		}
		if img.Dangling() {
			dangling++
		}
	}

	unusedPart := ""
	if unused > 0 {
		unusedPart = fmt.Sprintf("%d unused, %s", unused, state.FormatBytes(reclaimable))
	}
	danglingPart := ""
	if dangling > 0 {
		danglingPart = fmt.Sprintf("%d dangling", dangling)
	}

	return summaryOf(
		plural(len(v.deps.Store.Images), "image"),
		state.FormatBytes(total),
		unusedPart,
		danglingPart,
	)
}
