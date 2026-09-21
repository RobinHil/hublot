package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
)

// StreamStats follows the stats endpoint for one container, emitting one
// computed sample per frame (roughly one per second). The channel closes when
// the container stops or ctx is cancelled.
//
// The caller owns the cancel func and must call it when the container stops:
// forgetting it leaks a goroutine and an HTTP connection on every restart
// (AGENTS.md section 6.3).
func (c *Client) StreamStats(ctx context.Context, id string) (<-chan Stats, <-chan error) {
	out := make(chan Stats, 8)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		res, err := c.api.ContainerStats(ctx, id, true)
		if err != nil {
			if ctx.Err() == nil {
				errs <- fmt.Errorf("streaming stats for %s: %w", ShortID(id), err)
			}
			return
		}
		defer func() { _ = res.Body.Close() }()

		dec := json.NewDecoder(res.Body)
		for {
			var raw container.StatsResponse
			if err := dec.Decode(&raw); err != nil {
				if err != io.EOF && ctx.Err() == nil {
					errs <- fmt.Errorf("reading stats for %s: %w", ShortID(id), err)
				}
				return
			}
			select {
			case out <- ConvertStats(id, raw):
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, errs
}

// ConvertStats derives the displayable figures from one raw frame. Exported so
// the computation can be tested against recorded samples without a daemon.
func ConvertStats(id string, s container.StatsResponse) Stats {
	cpu, ok := cpuPercent(s)
	usage, limit, pct := memoryUsage(s)
	rx, tx := networkIO(s)
	read, write := blockIO(s)

	at := s.Read
	if at.IsZero() {
		at = time.Now()
	}

	return Stats{
		ContainerID: id,
		At:          at,
		CPUValid:    ok,
		CPUPercent:  cpu,
		MemUsage:    usage,
		MemLimit:    limit,
		MemPercent:  pct,
		NetRx:       rx,
		NetTx:       tx,
		BlockRead:   read,
		BlockWrite:  write,
		PIDs:        int64(s.PidsStats.Current),
	}
}

// cpuPercent turns the cumulative counters into a percentage the way the docker
// CLI does. The second return value is false when the frame carries no usable
// previous sample, in which case the UI shows "-", never "0%"
// (AGENTS.md section 6.1).
func cpuPercent(s container.StatsResponse) (float64, bool) {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)

	// The first frame of a stream carries zeroed PreCPUStats.
	if s.PreCPUStats.SystemUsage == 0 || s.PreCPUStats.CPUUsage.TotalUsage == 0 {
		return 0, false
	}

	// OnlineCPUs is absent on older API versions; fall back to the per-core
	// array length, which older daemons do populate.
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpus == 0 {
		return 0, false
	}

	if sysDelta > 0 && cpuDelta > 0 {
		return (cpuDelta / sysDelta) * cpus * 100.0, true
	}
	// A genuine zero-delta sample: the container simply did nothing.
	return 0, true
}

// memoryUsage subtracts page cache from the reported usage so the figure
// matches `docker stats`. Which key to subtract depends on the cgroup version,
// detected by probing the stats map rather than the host
// (AGENTS.md section 6.2).
func memoryUsage(s container.StatsResponse) (usage, limit int64, percent float64) {
	usage = int64(s.MemoryStats.Usage)
	limit = int64(s.MemoryStats.Limit)

	if v, ok := s.MemoryStats.Stats["inactive_file"]; ok {
		// cgroup v2
		if int64(v) < usage {
			usage -= int64(v)
		}
	} else if v, ok := s.MemoryStats.Stats["cache"]; ok {
		// cgroup v1
		if int64(v) < usage {
			usage -= int64(v)
		}
	}

	if limit > 0 {
		percent = float64(usage) / float64(limit) * 100.0
	}
	return usage, limit, percent
}

func networkIO(s container.StatsResponse) (rx, tx int64) {
	for _, n := range s.Networks {
		rx += int64(n.RxBytes)
		tx += int64(n.TxBytes)
	}
	return rx, tx
}

// blockIO sums the per-device counters. Values are absent on cgroup v2 hosts
// without io accounting, in which case both come back zero.
func blockIO(s container.StatsResponse) (read, write int64) {
	for _, e := range s.BlkioStats.IoServiceBytesRecursive {
		switch e.Op {
		case "read", "Read":
			read += int64(e.Value)
		case "write", "Write":
			write += int64(e.Value)
		}
	}
	return read, write
}
