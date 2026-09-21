// Package state holds the model's data and the pure functions that transform
// it. No IO, no Docker calls, no UI (AGENTS.md section 4).
package state

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RobinHil/hublot/internal/compose"
	"github.com/RobinHil/hublot/internal/docker"
)

// Category is one prunable object class. The UI offers each one separately, and
// the global prune is the union of them all.
type Category int

const (
	// CatContainers mirrors `docker container prune`.
	CatContainers Category = iota
	// CatImages mirrors `docker image prune`.
	CatImages
	// CatImagesAll mirrors `docker image prune -a`.
	CatImagesAll
	// CatVolumes mirrors `docker volume prune`.
	CatVolumes
	// CatVolumesAll mirrors `docker volume prune --all`.
	CatVolumesAll
	// CatNetworks mirrors `docker network prune`.
	CatNetworks
	// CatBuildCache mirrors `docker builder prune`.
	CatBuildCache
	// CatBuildCacheAll mirrors `docker builder prune --all`.
	CatBuildCacheAll
)

// String names the category for modals and headers.
func (c Category) String() string {
	switch c {
	case CatContainers:
		return "stopped containers"
	case CatImages:
		return "dangling images"
	case CatImagesAll:
		return "unused images"
	case CatVolumes:
		return "unused volumes"
	case CatVolumesAll:
		return "unused volumes (named included)"
	case CatNetworks:
		return "unused networks"
	case CatBuildCache:
		return "unused build cache"
	case CatBuildCacheAll:
		return "all build cache"
	}
	return "unknown"
}

// Destructive reports whether the category can destroy data the user cannot
// recreate. Volumes are the only ones that hold state.
func (c Category) Destructive() bool { return c == CatVolumes || c == CatVolumesAll }

// Item is one object a prune would destroy.
type Item struct {
	ID   string
	Name string
	// Size is the space the daemon would reclaim, or -1 when unknown. Volume
	// sizes are only known after a `system df`.
	Size int64
	// Project is the Compose project owning the object, empty when genuinely
	// orphaned. Compose-owned objects are shown first and styled as a warning
	// (AGENTS.md section 10.3).
	Project string
	Service string
	// Detail is a short human explanation, such as "exited 3 days ago".
	Detail string
}

// Preview is what a prune would do, reconstructed object by object. The Engine
// API has no dry-run: prune endpoints delete first and report afterwards, so
// this is computed from the same filters the daemon applies
// (AGENTS.md section 10.2).
type Preview struct {
	Category Category
	// Items are sorted with Compose-owned objects first.
	Items []Item
	// Reclaimable sums the known sizes. Unknown sizes are excluded and counted
	// in Unknown instead.
	Reclaimable int64
	Unknown     int
	// ComposeOwned is how many items belong to a Compose project.
	ComposeOwned int
}

// Empty reports whether the prune would do nothing.
func (p Preview) Empty() bool { return len(p.Items) == 0 }

// Summary is the one-line headline of a confirmation modal.
func (p Preview) Summary() string {
	if p.Empty() {
		return "nothing to remove"
	}
	s := fmt.Sprintf("%d %s, %s reclaimed", len(p.Items), p.Category, FormatBytes(p.Reclaimable))
	if p.Unknown > 0 {
		s += fmt.Sprintf(" (+%d of unknown size)", p.Unknown)
	}
	if p.ComposeOwned > 0 {
		s += fmt.Sprintf(", %d belonging to compose projects", p.ComposeOwned)
	}
	return s
}

// Snapshot is the object set a preview is computed against. Passing it in
// keeps the computation pure and testable against fabricated data.
type Snapshot struct {
	Containers []docker.Container
	Images     []docker.Image
	Volumes    []docker.Volume
	Networks   []docker.Network
	BuildCache []docker.BuildCacheRecord
	// Now anchors relative filters; zero means time.Now.
	Now time.Time
	// Until, when set, restricts the prune to objects created before Now-Until,
	// the way `--filter until=24h` does.
	Until time.Duration
	// VolumePruneAllSupported reflects the negotiated API version. Below 1.42
	// the daemon has no `all` filter and prunes every unused volume, named ones
	// included (AGENTS.md section 10.2).
	VolumePruneAllSupported bool
}

func (s Snapshot) now() time.Time {
	if s.Now.IsZero() {
		return time.Now()
	}
	return s.Now
}

// olderThanUntil applies the `until` filter: objects created at or after the
// cutoff are kept.
func (s Snapshot) olderThanUntil(created time.Time) bool {
	if s.Until <= 0 {
		return true
	}
	return created.Before(s.now().Add(-s.Until))
}

// BuildPreview computes what the given category would destroy.
func BuildPreview(cat Category, snap Snapshot) Preview {
	p := Preview{Category: cat}

	switch cat {
	case CatContainers:
		p.Items = prunableContainers(snap)
	case CatImages:
		p.Items = prunableImages(snap, false)
	case CatImagesAll:
		p.Items = prunableImages(snap, true)
	case CatVolumes:
		p.Items = prunableVolumes(snap, false)
	case CatVolumesAll:
		p.Items = prunableVolumes(snap, true)
	case CatNetworks:
		p.Items = prunableNetworks(snap)
	case CatBuildCache:
		p.Items = prunableBuildCache(snap, false)
	case CatBuildCacheAll:
		p.Items = prunableBuildCache(snap, true)
	}

	finalize(&p)
	return p
}

// GlobalPreview is the equivalent of `docker system prune -a --volumes`: every
// category at once, in the order the daemon processes them.
func GlobalPreview(snap Snapshot, includeVolumes, allImages bool) []Preview {
	imgCat := CatImages
	if allImages {
		imgCat = CatImagesAll
	}

	cats := []Category{CatContainers, imgCat, CatNetworks, CatBuildCache}
	if includeVolumes {
		// Named volumes go too: this is the case that silently destroys the
		// data of every stopped Compose stack (AGENTS.md section 10.3).
		cats = append(cats, CatVolumesAll)
	}

	out := make([]Preview, 0, len(cats))
	for _, c := range cats {
		out = append(out, BuildPreview(c, snap))
	}
	return out
}

// finalize sorts Compose-owned objects first and totals the sizes.
func finalize(p *Preview) {
	for _, it := range p.Items {
		if it.Project != "" {
			p.ComposeOwned++
		}
		if it.Size < 0 {
			p.Unknown++
			continue
		}
		p.Reclaimable += it.Size
	}

	sort.SliceStable(p.Items, func(i, j int) bool {
		a, b := p.Items[i], p.Items[j]
		if (a.Project != "") != (b.Project != "") {
			return a.Project != ""
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		return a.Name < b.Name
	})
}

// prunableContainers replicates `docker container prune`: containers in state
// exited or created, honouring the until filter.
func prunableContainers(snap Snapshot) []Item {
	var out []Item
	for _, c := range snap.Containers {
		if c.State != docker.StateExited && c.State != docker.StateCreated {
			continue
		}
		if !snap.olderThanUntil(c.Created) {
			continue
		}
		out = append(out, Item{
			ID:      c.ID,
			Name:    c.Name,
			Size:    c.SizeRW,
			Project: compose.ProjectOf(c.Labels),
			Service: compose.ServiceOf(c.Labels),
			Detail:  c.Status,
		})
	}
	return out
}

// prunableImages replicates `docker image prune`, and with all set, `-a`:
// dangling images by default, every unreferenced image otherwise. An image a
// container still points at is never touched, running or not.
func prunableImages(snap Snapshot, all bool) []Item {
	used := usedImages(snap.Containers)

	var out []Item
	for _, img := range snap.Images {
		if used[img.ID] {
			continue
		}
		if !all && !img.Dangling() {
			continue
		}
		if !snap.olderThanUntil(img.Created) {
			continue
		}
		// Tags are matched too: a container listing "nginx:alpine" pins that
		// image even when the daemon reports a different ID for it.
		if usedByTag(used, img.RepoTags) {
			continue
		}
		out = append(out, Item{
			ID:      img.ID,
			Name:    img.Ref(),
			Size:    img.Size,
			Project: "",
			Detail:  age(snap.now(), img.Created),
		})
	}
	return out
}

// usedImages indexes every image reference held by a container, by ID and by
// the name the container was created from.
func usedImages(cs []docker.Container) map[string]bool {
	used := map[string]bool{}
	for _, c := range cs {
		if c.ImageID != "" {
			used[c.ImageID] = true
		}
		if c.Image != "" {
			used[c.Image] = true
		}
	}
	return used
}

func usedByTag(used map[string]bool, tags []string) bool {
	for _, t := range tags {
		if used[t] {
			return true
		}
	}
	return false
}

// prunableVolumes replicates `docker volume prune`. Which volumes go depends on
// the negotiated API version: before 1.42 every unused volume is removed, from
// 1.42 only anonymous ones unless the all filter is set
// (AGENTS.md section 10.2).
func prunableVolumes(snap Snapshot, all bool) []Item {
	used := usedVolumes(snap.Containers)

	// Below 1.42 the daemon has no `all` filter, so the default prune already
	// takes named volumes with it.
	includeNamed := all || !snap.VolumePruneAllSupported

	var out []Item
	for _, v := range snap.Volumes {
		if used[v.Name] {
			continue
		}
		if v.RefCount > 0 {
			continue
		}
		if !v.Anonymous && !includeNamed {
			continue
		}
		detail := "named"
		if v.Anonymous {
			detail = "anonymous"
		}
		out = append(out, Item{
			ID:      v.Name,
			Name:    v.Name,
			Size:    v.Size,
			Project: v.Labels[compose.LabelProject],
			Service: v.Labels[compose.LabelService],
			Detail:  detail,
		})
	}
	return out
}

// usedVolumes indexes every volume a container mounts, running or not.
func usedVolumes(cs []docker.Container) map[string]bool {
	used := map[string]bool{}
	for _, c := range cs {
		for _, m := range c.Mounts {
			if m.Name != "" {
				used[m.Name] = true
			}
		}
	}
	return used
}

// prunableNetworks replicates `docker network prune`: user-defined networks
// with nothing attached. The three predefined networks are never candidates.
func prunableNetworks(snap Snapshot) []Item {
	attached := map[string]bool{}
	for _, c := range snap.Containers {
		for _, n := range c.Networks {
			attached[n] = true
		}
	}

	var out []Item
	for _, n := range snap.Networks {
		if n.Predefined() {
			continue
		}
		if len(n.Containers) > 0 || attached[n.Name] {
			continue
		}
		if !snap.olderThanUntil(n.Created) {
			continue
		}
		out = append(out, Item{
			ID:      n.ID,
			Name:    n.Name,
			Size:    0,
			Project: n.Labels[compose.LabelProject],
			Detail:  n.Driver,
		})
	}
	return out
}

// prunableBuildCache replicates `docker builder prune`: records not in use, or
// every record when all is set.
func prunableBuildCache(snap Snapshot, all bool) []Item {
	var out []Item
	for _, r := range snap.BuildCache {
		if r.InUse && !all {
			continue
		}
		if !snap.olderThanUntil(r.CreatedAt) {
			continue
		}
		name := r.Description
		if name == "" {
			name = docker.ShortID(r.ID)
		}
		out = append(out, Item{
			ID:     r.ID,
			Name:   name,
			Size:   r.Size,
			Detail: r.Type,
		})
	}
	return out
}

// age renders a duration the way the docker CLI does, coarsely.
func age(now, then time.Time) string {
	if then.IsZero() {
		return "unknown age"
	}
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	default:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
}

// plural renders a count with its unit, singular when there is one of them.
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// FormatBytes renders a size in the units the docker CLI uses.
func FormatBytes(n int64) string {
	if n < 0 {
		return "-"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	v := strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/float64(div)), ".0")
	return fmt.Sprintf("%s%cB", v, "KMGTP"[exp])
}
