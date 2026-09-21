package docker

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
)

// anonymousNameLen is the length of the hex name the daemon gives a volume
// created without an explicit name. Prune semantics differ for those
// (AGENTS.md section 10.2).
const anonymousNameLen = 64

// ListVolumes returns every volume, sorted by name. Size and RefCount are only
// known after a `system df`, so they come back as -1 here.
func (c *Client) ListVolumes(ctx context.Context) ([]Volume, error) {
	res, err := c.api.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing volumes: %w", err)
	}
	out := make([]Volume, 0, len(res.Volumes))
	for _, v := range res.Volumes {
		if v == nil {
			continue
		}
		out = append(out, toVolume(*v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func toVolume(v volume.Volume) Volume {
	created, _ := time.Parse(time.RFC3339, v.CreatedAt)
	out := Volume{
		Name:       v.Name,
		Driver:     v.Driver,
		Mountpoint: v.Mountpoint,
		CreatedAt:  created,
		Labels:     v.Labels,
		Scope:      v.Scope,
		Size:       -1,
		RefCount:   -1,
		Anonymous:  isAnonymousVolume(v.Name),
	}
	if v.UsageData != nil {
		out.Size = v.UsageData.Size
		out.RefCount = v.UsageData.RefCount
	}
	return out
}

// isAnonymousVolume reports whether the name looks like one the daemon
// generated: 64 lowercase hex characters.
func isAnonymousVolume(name string) bool {
	if len(name) != anonymousNameLen {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// InspectVolume returns the raw inspect JSON for display.
func (c *Client) InspectVolume(ctx context.Context, name string) ([]byte, error) {
	_, raw, err := c.api.VolumeInspectWithRaw(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("inspecting volume %s: %w", name, err)
	}
	return indentJSON(raw), nil
}

// RemoveVolume deletes a volume. This destroys data and has no undo.
func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	if err := c.api.VolumeRemove(ctx, name, force); err != nil {
		return fmt.Errorf("removing volume %s: %w", name, err)
	}
	return nil
}

// PruneVolumes removes unused volumes. Before API 1.42 the daemon skipped named
// volumes entirely; from 1.42 the `all` filter opts into them, and without it
// only anonymous volumes go (AGENTS.md section 10.2).
func (c *Client) PruneVolumes(ctx context.Context, all bool) (PruneReport, error) {
	args := filters.NewArgs()
	if all && c.SupportsVolumePruneAll() {
		args.Add("all", "true")
	}
	rep, err := c.api.VolumesPrune(ctx, args)
	if err != nil {
		return PruneReport{}, fmt.Errorf("pruning volumes: %w", err)
	}
	return PruneReport{Category: "volumes", Deleted: rep.VolumesDeleted, Reclaimed: int64(rep.SpaceReclaimed)}, nil
}

// SupportsVolumePruneAll reports whether the negotiated API understands the
// `all` filter on volume prune. The state layer needs this to build a preview
// that matches what the daemon will actually do.
func (c *Client) SupportsVolumePruneAll() bool { return c.versionAtLeast("1.42") }

// CreateVolume makes a named volume with the default driver.
func (c *Client) CreateVolume(ctx context.Context, name string) error {
	if _, err := c.api.VolumeCreate(ctx, volume.CreateOptions{Name: name}); err != nil {
		return fmt.Errorf("creating volume %s: %w", name, err)
	}
	return nil
}
