package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/theme"
)

// Disk is where disk space went, and the only place pruning is offered. Every
// prune shows a reconstructed preview first (AGENTS.md section 10.2).
type Disk struct {
	base
	// expanded remembers which categories show their objects.
	expanded map[state.Category]bool
	// loading is set while `system df` runs, which takes seconds.
	loading bool
}

// NewDisk builds the disk view.
func NewDisk(d Deps) *Disk {
	cols := []components.Column{
		{Title: "WHAT", MinWidth: 30},
		{Title: "COUNT", Width: 7, Right: true, Priority: 2},
		{Title: "SIZE", Width: 10, Right: true},
		{Title: "RECLAIMABLE", Width: 12, Right: true},
		{Title: "DETAIL", Width: 28, Priority: 1},
	}

	t := components.NewTable(cols)
	t.Empty = "press r to compute disk usage"

	return &Disk{base: base{deps: d, table: t}, expanded: map[state.Category]bool{}}
}

// Title is the tab label.
func (v *Disk) Title() string { return "Disk" }

// SetLoading marks the view busy while df runs.
func (v *Disk) SetLoading(loading bool) { v.loading = loading }

// SetSize keeps one line for the df age header this view prints above its
// table, so the two together are exactly as tall as the frame allows.
func (v *Disk) SetSize(width, height int) {
	v.width, v.height = width, height
	v.table.SetSize(width, height-1)
}

// pruneCategories are the categories offered, in the order the daemon
// processes them.
func (v *Disk) pruneCategories() []state.Category {
	cats := []state.Category{
		state.CatContainers,
		state.CatImages,
		state.CatImagesAll,
		state.CatVolumes,
	}
	// Below API 1.42 the plain volume prune already takes named volumes with
	// it, so offering an "all" variant separately would be a lie
	// (AGENTS.md section 10.2).
	if v.deps.Store.VolumePruneAll {
		cats = append(cats, state.CatVolumesAll)
	}
	return append(cats, state.CatNetworks, state.CatBuildCache)
}

// Refresh rebuilds the summary and the expanded previews.
func (v *Disk) Refresh() {
	var rows []components.Row

	rows = append(rows, v.summaryRows()...)

	snap := v.deps.Store.SnapshotForPrune(0)
	for _, cat := range v.pruneCategories() {
		preview := state.BuildPreview(cat, snap)
		rows = append(rows, v.categoryRow(cat, preview))
		if v.expanded[cat] {
			rows = append(rows, v.itemRows(preview)...)
		}
	}

	rows = append(rows, v.leftoverRows()...)
	v.table.SetRows(rows)
}

// summaryRows show what `system df` reported, with its age, since the figure
// is a cached snapshot rather than live (AGENTS.md section 6.6).
func (v *Disk) summaryRows() []components.Row {
	s := theme.Current()
	du := v.deps.Store.Disk

	if du == nil {
		detail := "not computed yet"
		if v.loading {
			detail = "computing, this walks every layer"
		}
		return []components.Row{{
			ID:    "summary",
			Group: "system df",
			Cells: []components.Cell{
				components.Styled("disk usage", s.Dim),
				components.Txt(""),
				components.Txt("-"),
				components.Txt("-"),
				components.Styled(detail, s.Dim),
			},
		}}
	}

	age := fmt.Sprintf("measured %s ago", formatAge(du.At))
	if v.loading {
		age = "recomputing"
	}

	var imagesSize, volumesSize, containersSize, cacheSize int64
	for _, i := range du.Images {
		imagesSize += i.Size
	}
	for _, vol := range du.Volumes {
		if vol.Size > 0 {
			volumesSize += vol.Size
		}
	}
	for _, c := range du.Containers {
		containersSize += c.SizeRW
	}
	for _, b := range du.BuildCache {
		cacheSize += b.Size
	}

	group := "system df, " + age
	return []components.Row{
		v.summaryRow(group, "images", len(du.Images), imagesSize, du.LayersSize),
		v.summaryRow(group, "containers (writable layers)", len(du.Containers), containersSize, 0),
		v.summaryRow(group, "volumes", len(du.Volumes), volumesSize, 0),
		v.summaryRow(group, "build cache", len(du.BuildCache), cacheSize, 0),
	}
}

func (v *Disk) summaryRow(group, what string, count int, size, layers int64) components.Row {
	detail := ""
	if layers > 0 {
		detail = "layers on disk: " + state.FormatBytes(layers)
	}
	return components.Row{
		ID:    "summary/" + what,
		Group: group,
		Cells: []components.Cell{
			components.Txt(what),
			components.Txt(fmt.Sprintf("%d", count)),
			components.Txt(state.FormatBytes(size)),
			components.Txt(""),
			components.Txt(detail),
		},
	}
}

// categoryRow is one prunable category with what pruning it would free.
func (v *Disk) categoryRow(cat state.Category, p state.Preview) components.Row {
	s := theme.Current()

	detail := ""
	if p.ComposeOwned > 0 {
		detail = fmt.Sprintf("%d belong to compose projects", p.ComposeOwned)
	}
	if p.Unknown > 0 {
		if detail != "" {
			detail += ", "
		}
		detail += fmt.Sprintf("%d of unknown size", p.Unknown)
	}

	marker := "  "
	if v.expanded[cat] {
		marker = "- "
	} else if !p.Empty() {
		marker = "+ "
	}

	name := components.Txt(marker + cat.String())
	if p.ComposeOwned > 0 {
		name = components.Styled(marker+cat.String(), s.Warning)
	}

	return components.Row{
		ID:   categoryID(cat),
		Dim:  p.Empty(),
		Warn: p.ComposeOwned > 0,
		Cells: []components.Cell{
			name,
			components.Txt(fmt.Sprintf("%d", len(p.Items))),
			components.Txt(""),
			components.Txt(state.FormatBytes(p.Reclaimable)),
			components.Txt(detail),
		},
	}
}

// itemRows list exactly what a prune would destroy, Compose-owned objects
// first and styled as a warning (AGENTS.md section 10.3).
func (v *Disk) itemRows(p state.Preview) []components.Row {
	s := theme.Current()

	rows := make([]components.Row, 0, len(p.Items))
	for _, it := range p.Items {
		owner := it.Project
		if owner == "" {
			owner = "no compose project"
		} else if it.Service != "" {
			owner += " / " + it.Service
		}

		rows = append(rows, components.Row{
			ID:   categoryID(p.Category) + "/" + it.ID,
			Warn: it.Project != "",
			Cells: []components.Cell{
				components.Txt("    " + it.Name),
				components.Txt(""),
				components.Txt(state.FormatBytes(it.Size)),
				components.Txt(""),
				components.Styled(owner+"  "+it.Detail, s.Dim),
			},
		})
	}
	return rows
}

// leftoverRows surface what nobody cleans up: one-off containers from
// `compose run`, and stacks whose volumes outlived them
// (AGENTS.md section 8.5).
func (v *Disk) leftoverRows() []components.Row {
	s := theme.Current()

	var rows []components.Row
	const group = "compose leftovers"

	for _, p := range v.deps.Store.Projects {
		for _, c := range p.OneOff {
			rows = append(rows, components.Row{
				ID:    "oneoff/" + c.ID,
				Group: group,
				Warn:  true,
				Cells: []components.Cell{
					components.Txt("  " + c.Name),
					components.Txt(""),
					components.Txt(state.FormatBytes(c.SizeRW)),
					components.Txt(""),
					components.Styled(p.Name+"  from compose run, "+c.Status, s.Dim),
				},
			})
		}

		if p.Ghost {
			var size int64
			for _, vol := range p.Volumes {
				if vol.Size > 0 {
					size += vol.Size
				}
			}
			rows = append(rows, components.Row{
				ID:    "ghost/" + p.Name,
				Group: group,
				Warn:  true,
				Cells: []components.Cell{
					components.Txt("  " + p.Name),
					components.Txt(fmt.Sprintf("%d", len(p.Volumes))),
					components.Txt(state.FormatBytes(size)),
					components.Txt(""),
					components.Styled("stopped stack, volumes and networks still here", s.Dim),
				},
			})
		}
	}
	return rows
}

func categoryID(c state.Category) string { return "cat/" + c.String() }

// Update handles the disk bindings.
func (v *Disk) Update(msg tea.Msg) tea.Cmd {
	if cmd, handled := v.table.Update(msg, v.deps.Keys); handled {
		v.Refresh()
		return cmd
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	k := v.deps.Keys.Disk
	switch {
	case key.Matches(km, k.Toggle):
		if cat, ok := v.currentCategory(); ok {
			v.expanded[cat] = !v.expanded[cat]
			v.Refresh()
		}
		return nil

	case key.Matches(km, k.Prune):
		return v.pruneCategory()

	case key.Matches(km, k.PruneGlobal):
		return v.pruneEverything()
	}
	return nil
}

// currentCategory resolves the category the cursor is on, whether it sits on
// the category row or one of its items.
func (v *Disk) currentCategory() (state.Category, bool) {
	id := v.table.CurrentID()
	if !strings.HasPrefix(id, "cat/") {
		return 0, false
	}
	name := strings.SplitN(strings.TrimPrefix(id, "cat/"), "/", 2)[0]
	for _, cat := range v.pruneCategories() {
		if cat.String() == name {
			return cat, true
		}
	}
	return 0, false
}

// pruneCategory confirms with the preview, object by object.
func (v *Disk) pruneCategory() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}
	cat, ok := v.currentCategory()
	if !ok {
		return v.info("nothing to prune here", []string{
			"Put the cursor on one of the prune categories, or press P for the system-wide prune.",
		})
	}

	preview := state.BuildPreview(cat, v.deps.Store.SnapshotForPrune(0))
	if preview.Empty() {
		return v.info("nothing to prune", []string{
			"No object matches what a " + cat.String() + " prune would remove.",
		})
	}

	sev := components.SevDanger
	if cat.Destructive() && preview.ComposeOwned > 0 {
		// Destroying the data of a stack that is merely stopped deserves the
		// strongest gate there is (AGENTS.md section 10.3).
		sev = components.SevTyped
	}

	return request(ConfirmRequest{
		Severity: sev,
		Title:    "Prune " + cat.String(),
		Body:     previewBody(preview),
		Run:      func() tea.Cmd { return request(PruneRequest{Categories: []state.Category{cat}}) },
	})
}

// pruneEverything is the `system prune -a --volumes` equivalent, and always
// asks for a typed word.
func (v *Disk) pruneEverything() tea.Cmd {
	if v.deps.ReadOnly {
		return denied()
	}

	snap := v.deps.Store.SnapshotForPrune(0)
	previews := state.GlobalPreview(snap, true, true)

	var body []string
	var cats []state.Category
	var total int64
	composeOwned := 0

	for _, p := range previews {
		cats = append(cats, p.Category)
		total += p.Reclaimable
		composeOwned += p.ComposeOwned
		body = append(body, p.Summary())
		body = append(body, previewItems(p, 8)...)
		body = append(body, "")
	}

	head := []string{
		fmt.Sprintf("This is the equivalent of docker system prune -a --volumes: %s would be freed.",
			state.FormatBytes(total)),
	}
	if composeOwned > 0 {
		head = append(head,
			fmt.Sprintf("%d of the objects below belong to compose projects, and their volumes hold data.",
				composeOwned))
	}
	head = append(head, "")

	return request(ConfirmRequest{
		Severity: components.SevTyped,
		Title:    "Prune everything unused on this host",
		Body:     append(head, body...),
		Run:      func() tea.Cmd { return request(PruneRequest{Categories: cats, Global: true}) },
	})
}

// previewBody renders a preview for a modal: the headline, then the objects,
// Compose-owned ones first.
func previewBody(p state.Preview) []string {
	body := []string{p.Summary(), ""}
	return append(body, previewItems(p, 20)...)
}

// previewItems lists at most limit objects, naming the project each belongs to.
func previewItems(p state.Preview, limit int) []string {
	var out []string
	for i, it := range p.Items {
		if i == limit {
			out = append(out, fmt.Sprintf("  ... and %d more", len(p.Items)-limit))
			break
		}
		line := "  " + it.Name + "  " + state.FormatBytes(it.Size)
		if it.Project != "" {
			line += "  [compose: " + it.Project
			if it.Service != "" {
				line += "/" + it.Service
			}
			line += "]"
		}
		out = append(out, line)
	}
	return out
}

func (v *Disk) info(title string, body []string) tea.Cmd {
	return request(ConfirmRequest{Severity: components.SevInfo, Title: title, Body: body})
}

// View renders the table with a spinner line while df is running.
func (v *Disk) View() string {
	s := theme.Current()

	header := ""
	switch {
	case v.loading:
		header = s.Warning.Render("computing disk usage, the daemon is walking every layer...")
	case v.deps.Store.Disk == nil:
		header = s.Dim.Render("disk usage has not been computed yet: press r")
	default:
		age, _ := v.deps.Store.DiskAge()
		header = s.Dim.Render("last measured " + fmtAgo(age) + ", press r to recompute")
	}

	return header + "\n" + v.table.View()
}

func fmtAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
}

// Hints are the footer bindings.
func (v *Disk) Hints() []key.Binding {
	k := v.deps.Keys
	return []key.Binding{
		k.Global.Help, k.Disk.Refresh, k.Disk.Toggle,
		k.Disk.Prune, k.Disk.PruneGlobal, k.Global.Quit,
	}
}

// Filtering reports whether the filter input has focus.
func (v *Disk) Filtering() bool { return v.table.Filtering() }
