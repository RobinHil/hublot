package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// ListImages returns every image including intermediate untagged ones, sorted
// newest first.
func (c *Client) ListImages(ctx context.Context) ([]Image, error) {
	raw, err := c.api.ImageList(ctx, image.ListOptions{All: false, SharedSize: true})
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}
	out := make([]Image, 0, len(raw))
	for _, r := range raw {
		out = append(out, toImage(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

func toImage(r image.Summary) Image {
	return Image{
		ID:          r.ID,
		RepoTags:    r.RepoTags,
		RepoDigests: r.RepoDigests,
		Created:     time.Unix(r.Created, 0),
		Size:        r.Size,
		SharedSize:  r.SharedSize,
		Containers:  r.Containers,
		Labels:      r.Labels,
	}
}

// InspectImage returns the raw inspect JSON, pretty-printed for display.
func (c *Client) InspectImage(ctx context.Context, id string) ([]byte, error) {
	var raw bytes.Buffer
	if _, err := c.api.ImageInspect(ctx, id, client.ImageInspectWithRawResponse(&raw)); err != nil {
		return nil, fmt.Errorf("inspecting image %s: %w", ShortID(id), err)
	}
	return indentJSON(raw.Bytes()), nil
}

// ImageHistory returns the layer history of an image.
func (c *Client) ImageHistory(ctx context.Context, id string) ([]HistoryLayer, error) {
	raw, err := c.api.ImageHistory(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reading history of image %s: %w", ShortID(id), err)
	}
	out := make([]HistoryLayer, 0, len(raw))
	for _, h := range raw {
		out = append(out, HistoryLayer{
			ID:        h.ID,
			Created:   time.Unix(h.Created, 0),
			CreatedBy: h.CreatedBy,
			Size:      h.Size,
			Comment:   h.Comment,
			Tags:      h.Tags,
		})
	}
	return out, nil
}

// RemoveImage deletes an image, optionally forcing removal when containers
// still reference it.
func (c *Client) RemoveImage(ctx context.Context, id string, force, pruneChildren bool) ([]string, error) {
	res, err := c.api.ImageRemove(ctx, id, image.RemoveOptions{Force: force, PruneChildren: pruneChildren})
	if err != nil {
		return nil, fmt.Errorf("removing image %s: %w", ShortID(id), err)
	}
	var deleted []string
	for _, r := range res {
		if r.Deleted != "" {
			deleted = append(deleted, r.Deleted)
		}
		if r.Untagged != "" {
			deleted = append(deleted, r.Untagged)
		}
	}
	return deleted, nil
}

// TagImage adds a tag to an existing image.
func (c *Client) TagImage(ctx context.Context, id, ref string) error {
	if err := c.api.ImageTag(ctx, id, ref); err != nil {
		return fmt.Errorf("tagging image %s as %s: %w", ShortID(id), ref, err)
	}
	return nil
}

// PullImage streams a pull, sending one progress line per message. The channel
// is closed when the pull ends; err is delivered as the final line.
func (c *Client) PullImage(ctx context.Context, ref string) (<-chan string, <-chan error) {
	lines := make(chan string, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(lines)
		defer close(errs)

		rc, err := c.api.ImagePull(ctx, ref, image.PullOptions{})
		if err != nil {
			errs <- fmt.Errorf("pulling %s: %w", ref, err)
			return
		}
		defer func() { _ = rc.Close() }()

		dec := json.NewDecoder(rc)
		for {
			var msg struct {
				Status   string `json:"status"`
				ID       string `json:"id"`
				Progress string `json:"progress"`
				Error    string `json:"error"`
			}
			if err := dec.Decode(&msg); err != nil {
				if err != io.EOF && ctx.Err() == nil {
					errs <- fmt.Errorf("reading pull progress for %s: %w", ref, err)
				}
				return
			}
			if msg.Error != "" {
				errs <- fmt.Errorf("pulling %s: %s", ref, msg.Error)
				return
			}
			line := msg.Status
			if msg.ID != "" {
				line = msg.ID + ": " + line
			}
			if msg.Progress != "" {
				line += " " + msg.Progress
			}
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	return lines, errs
}

// SaveImage writes an image tarball to path.
func (c *Client) SaveImage(ctx context.Context, ids []string, path string) error {
	rc, err := c.api.ImageSave(ctx, ids)
	if err != nil {
		return fmt.Errorf("saving image to %s: %w", path, err)
	}
	defer func() { _ = rc.Close() }()

	return writeStreamTo(rc, path)
}

// writeStreamTo drains a tarball the daemon is sending into a local file.
func writeStreamTo(r io.Reader, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}

	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}

// PruneImages removes dangling images, or every unreferenced image when all is
// set. Mirrors `docker image prune [-a]`.
func (c *Client) PruneImages(ctx context.Context, all bool, until time.Duration) (PruneReport, error) {
	args := filters.NewArgs()
	args.Add("dangling", fmt.Sprintf("%t", !all))
	if until > 0 {
		args.Add("until", fmt.Sprintf("%ds", int(until.Seconds())))
	}
	rep, err := c.api.ImagesPrune(ctx, args)
	if err != nil {
		return PruneReport{}, fmt.Errorf("pruning images: %w", err)
	}
	var deleted []string
	for _, d := range rep.ImagesDeleted {
		if d.Deleted != "" {
			deleted = append(deleted, d.Deleted)
		} else {
			deleted = append(deleted, d.Untagged)
		}
	}
	return PruneReport{Category: "images", Deleted: deleted, Reclaimed: int64(rep.SpaceReclaimed)}, nil
}

// indentJSON pretty-prints raw inspect output, falling back to the original
// bytes when the daemon sends something unexpected.
func indentJSON(raw []byte) []byte {
	var buf []byte
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return buf
}

// LoadImage reads back a tarball written by SaveImage, or produced by
// `docker save` elsewhere.
func (c *Client) LoadImage(ctx context.Context, path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	res, err := c.api.ImageLoad(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()

	// The response is the same progress stream a pull produces; the lines
	// naming what was loaded are the only part worth keeping.
	var loaded []string
	dec := json.NewDecoder(res.Body)
	for {
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return loaded, fmt.Errorf("reading the load progress of %s: %w", path, err)
		}
		if msg.Error != "" {
			return loaded, fmt.Errorf("loading %s: %s", path, msg.Error)
		}
		if line := strings.TrimSpace(msg.Stream); line != "" {
			loaded = append(loaded, line)
		}
	}
	return loaded, nil
}
