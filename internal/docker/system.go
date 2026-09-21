package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
)

// Info reads daemon info and version in one go.
func (c *Client) Info(ctx context.Context) (Info, error) {
	info, err := c.api.Info(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("reading daemon info: %w", err)
	}
	ver, err := c.api.ServerVersion(ctx)
	if err != nil {
		return Info{}, fmt.Errorf("reading daemon version: %w", err)
	}

	cgroup := info.CgroupVersion
	if cgroup == "" {
		cgroup = "1"
	}

	return Info{
		Name:          info.Name,
		ServerVersion: ver.Version,
		APIVersion:    ver.APIVersion,
		OS:            info.OperatingSystem,
		Arch:          info.Architecture,
		KernelVersion: info.KernelVersion,
		NCPU:          info.NCPU,
		MemTotal:      info.MemTotal,
		StorageDriver: info.Driver,
		CgroupVersion: cgroup,
		Containers:    info.Containers,
		Running:       info.ContainersRunning,
		Paused:        info.ContainersPaused,
		Stopped:       info.ContainersStopped,
		Images:        info.Images,
		DockerRootDir: info.DockerRootDir,
	}, nil
}

// DiskUsage runs `system df`. The daemon walks every layer, so this takes
// seconds on a loaded host: call it on demand only, never on a timer
// (AGENTS.md section 6.6).
func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	du, err := c.api.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return DiskUsage{}, fmt.Errorf("computing disk usage: %w", err)
	}

	out := DiskUsage{
		LayersSize:  du.LayersSize,
		BuilderSize: du.BuilderSize,
		At:          time.Now(),
	}
	for _, i := range du.Images {
		if i != nil {
			out.Images = append(out.Images, toImage(*i))
		}
	}
	for _, ct := range du.Containers {
		if ct != nil {
			out.Containers = append(out.Containers, toContainer(*ct))
		}
	}
	for _, v := range du.Volumes {
		if v != nil {
			out.Volumes = append(out.Volumes, toVolume(*v))
		}
	}
	for _, b := range du.BuildCache {
		if b == nil {
			continue
		}
		out.BuildCache = append(out.BuildCache, BuildCacheRecord{
			ID:          b.ID,
			Parent:      b.Parent, //nolint:staticcheck // Parents is not populated by every daemon version
			Type:        string(b.Type),
			Description: b.Description,
			InUse:       b.InUse,
			Shared:      b.Shared,
			Size:        b.Size,
			CreatedAt:   b.CreatedAt,
			LastUsedAt:  derefTime(b.LastUsedAt),
			UsageCount:  int64(b.UsageCount),
		})
	}
	return out, nil
}

// PruneBuildCache drops build cache records. `all` also removes cache that is
// still in use by an image.
func (c *Client) PruneBuildCache(ctx context.Context, all bool) (PruneReport, error) {
	rep, err := c.api.BuildCachePrune(ctx, build.CachePruneOptions{All: all})
	if err != nil {
		return PruneReport{}, fmt.Errorf("pruning build cache: %w", err)
	}
	if rep == nil {
		return PruneReport{Category: "build cache"}, nil
	}
	return PruneReport{Category: "build cache", Deleted: rep.CachesDeleted, Reclaimed: int64(rep.SpaceReclaimed)}, nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func marshalIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("formatting inspect output: %w", err)
	}
	return b, nil
}
