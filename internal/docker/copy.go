package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// CopyToContainer writes a local file or directory into a container, the way
// `docker cp` does. The API takes a tar stream, so the source is packed on the
// fly rather than written to a temporary file.
func (c *Client) CopyToContainer(ctx context.Context, id, localPath, remotePath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", localPath, err)
	}

	// The daemon unpacks into the parent directory, so the archive holds paths
	// relative to it: copying /etc/hosts to /tmp lands at /tmp/hosts.
	dstDir := remotePath
	if !strings.HasSuffix(remotePath, "/") {
		dstDir = filepath.Dir(remotePath)
	}
	rename := ""
	if !strings.HasSuffix(remotePath, "/") && filepath.Base(remotePath) != info.Name() {
		rename = filepath.Base(remotePath)
	}

	pr, pw := io.Pipe()
	go func() {
		// The pipe carries the packing error to the daemon side, which then
		// fails the request rather than sending a truncated archive.
		_ = pw.CloseWithError(tarPath(pw, localPath, rename))
	}()
	defer func() { _ = pr.Close() }()

	err = c.api.CopyToContainer(ctx, id, dstDir, pr, container.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("copying %s into %s:%s: %w", localPath, ShortID(id), remotePath, err)
	}
	return nil
}

// CopyFromContainer extracts a path out of a container into a local directory.
// It returns what it wrote, so the UI can say where the file went.
func (c *Client) CopyFromContainer(ctx context.Context, id, remotePath, localDir string) (string, error) {
	rc, stat, err := c.api.CopyFromContainer(ctx, id, remotePath)
	if err != nil {
		return "", fmt.Errorf("copying %s:%s: %w", ShortID(id), remotePath, err)
	}
	defer func() { _ = rc.Close() }()

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", localDir, err)
	}

	written, err := untarInto(rc, localDir)
	if err != nil {
		return "", fmt.Errorf("extracting %s:%s into %s: %w", ShortID(id), remotePath, localDir, err)
	}
	if written == "" {
		written = filepath.Join(localDir, stat.Name)
	}
	return written, nil
}

// tarPath packs a file or a directory tree into w. An empty rename keeps the
// source name.
func tarPath(w io.Writer, root, rename string) error {
	tw := tar.NewWriter(w)

	base := filepath.Base(root)
	if rename != "" {
		base = rename
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Symlinks are followed by Walk's caller only for the root; inside the
		// tree they are copied as links, which is what docker cp does too.
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return fmt.Errorf("reading link %s: %w", path, err)
			}
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fmt.Errorf("describing %s: %w", path, err)
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("locating %s under %s: %w", path, root, err)
		}
		header.Name = base
		if rel != "." {
			header.Name = filepath.ToSlash(filepath.Join(base, rel))
		}

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing header for %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("copying %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Closing is what writes the trailer: skipping it produces an archive the
	// daemon reads as truncated.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("finishing the archive of %s: %w", root, err)
	}
	return nil
}

// untarInto unpacks a tar stream under dir, returning the first path written so
// the caller can report it. Entries escaping dir are refused: the archive comes
// from the daemon, which is external input.
func untarInto(r io.Reader, dir string) (string, error) {
	// Resolved first: the destination is whatever the user typed, often "." or
	// another relative path, and comparing a joined path against a relative
	// prefix rejects entries that are perfectly legitimate.
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", dir, err)
	}

	tr := tar.NewReader(r)
	first := ""

	for {
		header, err := tr.Next()
		if err == io.EOF {
			return first, nil
		}
		if err != nil {
			return first, fmt.Errorf("reading archive: %w", err)
		}

		target, err := safeJoin(root, header.Name)
		if err != nil {
			return first, err
		}
		if first == "" {
			first = target
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return first, fmt.Errorf("creating %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return first, fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return first, fmt.Errorf("creating %s: %w", target, err)
			}
			//nolint:gosec // the size is the daemon's own report of its file
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return first, fmt.Errorf("writing %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return first, fmt.Errorf("closing %s: %w", target, err)
			}
		case tar.TypeSymlink:
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return first, fmt.Errorf("linking %s: %w", target, err)
			}
		}
	}
}

// safeJoin refuses archive entries that would write outside dir, which is the
// tar traversal a malicious or broken archive relies on.
func safeJoin(dir, name string) (string, error) {
	target := filepath.Join(dir, filepath.Clean("/"+name))
	if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) &&
		target != filepath.Clean(dir) {
		return "", fmt.Errorf("archive entry %q would escape %s", name, dir)
	}
	return target, nil
}
