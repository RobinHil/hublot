package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// LogOptions controls a log stream.
type LogOptions struct {
	// Tail is the number of trailing lines to fetch first; "all" for everything.
	Tail string
	// Follow keeps the stream open for new output.
	Follow bool
	// Since limits output to lines newer than this instant.
	Since time.Time
	// Service labels every line, used by aggregated Compose logs.
	Service string
}

// DefaultLogOptions is what the logs view opens with.
func DefaultLogOptions() LogOptions { return LogOptions{Tail: "500", Follow: true} }

// StreamLogs follows a container's output. Without a TTY the stream is
// multiplexed with an 8-byte frame header, so it goes through stdcopy;
// reading it raw produces garbage every few lines (AGENTS.md section 6.4).
func (c *Client) StreamLogs(ctx context.Context, id string, opts LogOptions) (<-chan LogLine, <-chan error) {
	out := make(chan LogLine, 256)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		tty, err := c.HasTTY(ctx, id)
		if err != nil {
			if ctx.Err() == nil {
				errs <- err
			}
			return
		}

		apiOpts := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     opts.Follow,
			Timestamps: true,
			Tail:       orDefault(opts.Tail, "all"),
		}
		if !opts.Since.IsZero() {
			apiOpts.Since = opts.Since.Format(time.RFC3339Nano)
		}

		rc, err := c.api.ContainerLogs(ctx, id, apiOpts)
		if err != nil {
			if ctx.Err() == nil {
				errs <- fmt.Errorf("streaming logs for %s: %w", ShortID(id), err)
			}
			return
		}
		defer func() { _ = rc.Close() }()

		if tty {
			// A TTY stream is already a single plain byte stream.
			scanLines(ctx, rc, id, opts.Service, "stdout", out)
			return
		}

		stdoutR, stdoutW := io.Pipe()
		stderrR, stderrW := io.Pipe()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); scanLines(ctx, stdoutR, id, opts.Service, "stdout", out) }()
		go func() { defer wg.Done(); scanLines(ctx, stderrR, id, opts.Service, "stderr", out) }()

		_, copyErr := stdcopy.StdCopy(stdoutW, stderrW, rc)
		_ = stdoutW.Close()
		_ = stderrW.Close()
		wg.Wait()

		if copyErr != nil && ctx.Err() == nil && copyErr != io.EOF {
			errs <- fmt.Errorf("demultiplexing logs for %s: %w", ShortID(id), copyErr)
		}
	}()

	return out, errs
}

// scanLines splits a stream into lines and publishes them, parsing the RFC3339
// timestamp the daemon prefixes when Timestamps is set.
func scanLines(ctx context.Context, r io.Reader, id, service, stream string, out chan<- LogLine) {
	sc := bufio.NewScanner(r)
	// Container lines can be long; the default 64KiB limit truncates JSON logs.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		ts, text := splitTimestamp(sc.Text())
		line := LogLine{ContainerID: id, Service: service, Stream: stream, At: ts, Text: text}
		select {
		case out <- line:
		case <-ctx.Done():
			return
		}
	}
}

// splitTimestamp peels off the leading timestamp the daemon adds. Lines that do
// not carry one are returned untouched.
func splitTimestamp(line string) (time.Time, string) {
	head, rest, found := strings.Cut(line, " ")
	if !found {
		return time.Time{}, line
	}
	ts, err := time.Parse(time.RFC3339Nano, head)
	if err != nil {
		return time.Time{}, line
	}
	return ts, rest
}
