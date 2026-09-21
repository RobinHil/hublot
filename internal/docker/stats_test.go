package docker

import (
	"math"
	"testing"

	"github.com/docker/docker/api/types/container"
)

// sample builds a stats frame with the fields the computation reads.
func sample(total, preTotal, sys, preSys uint64, onlineCPUs uint32, percpu int) container.StatsResponse {
	s := container.StatsResponse{}
	s.CPUStats.CPUUsage.TotalUsage = total
	s.CPUStats.SystemUsage = sys
	s.CPUStats.OnlineCPUs = onlineCPUs
	s.PreCPUStats.CPUUsage.TotalUsage = preTotal
	s.PreCPUStats.SystemUsage = preSys
	if percpu > 0 {
		s.CPUStats.CPUUsage.PercpuUsage = make([]uint64, percpu)
	}
	return s
}

func TestCPUPercent(t *testing.T) {
	tests := []struct {
		name  string
		in    container.StatsResponse
		want  float64
		valid bool
	}{
		{
			// Half of one core out of four: 25% of a core, 4 cores reported.
			name:  "quarter of a four core host",
			in:    sample(2_000_000_000, 1_000_000_000, 40_000_000_000, 20_000_000_000, 4, 4),
			want:  20,
			valid: true,
		},
		{
			name:  "single core saturated",
			in:    sample(1_100_000_000, 100_000_000, 2_000_000_000, 1_000_000_000, 1, 1),
			want:  100,
			valid: true,
		},
		{
			// First frame of a stream: PreCPUStats is zeroed, so "-" not "0%".
			name:  "first sample has no previous frame",
			in:    sample(1_000_000_000, 0, 20_000_000_000, 0, 4, 4),
			valid: false,
		},
		{
			// An idle container: real zero, must render as 0% not "-".
			name:  "zero delta is a valid zero",
			in:    sample(1_000_000_000, 1_000_000_000, 40_000_000_000, 20_000_000_000, 4, 4),
			want:  0,
			valid: true,
		},
		{
			// OnlineCPUs is absent on older API versions: fall back to PercpuUsage.
			name:  "online cpus absent falls back to percpu length",
			in:    sample(2_000_000_000, 1_000_000_000, 40_000_000_000, 20_000_000_000, 0, 8),
			want:  40,
			valid: true,
		},
		{
			name:  "no cpu count at all is unusable",
			in:    sample(2_000_000_000, 1_000_000_000, 40_000_000_000, 20_000_000_000, 0, 0),
			valid: false,
		},
		{
			// A daemon restart can make the system counter go backwards.
			name:  "negative system delta is unusable",
			in:    sample(2_000_000_000, 1_000_000_000, 10_000_000_000, 20_000_000_000, 4, 4),
			want:  0,
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := cpuPercent(tt.in)
			if valid != tt.valid {
				t.Fatalf("validity: got %v, want %v", valid, tt.valid)
			}
			if valid && math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("percent: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryUsage(t *testing.T) {
	const mib = 1024 * 1024

	tests := []struct {
		name        string
		usage       uint64
		limit       uint64
		stats       map[string]uint64
		wantUsage   int64
		wantPercent float64
	}{
		{
			// cgroup v2: inactive_file is page cache and must come off.
			name:        "cgroup v2 subtracts inactive_file",
			usage:       200 * mib,
			limit:       1000 * mib,
			stats:       map[string]uint64{"inactive_file": 150 * mib, "anon": 50 * mib},
			wantUsage:   50 * mib,
			wantPercent: 5,
		},
		{
			// cgroup v1 reports the same thing under a different key.
			name:        "cgroup v1 subtracts cache",
			usage:       200 * mib,
			limit:       400 * mib,
			stats:       map[string]uint64{"cache": 100 * mib, "rss": 100 * mib},
			wantUsage:   100 * mib,
			wantPercent: 25,
		},
		{
			name:        "no stats map leaves usage untouched",
			usage:       128 * mib,
			limit:       256 * mib,
			wantUsage:   128 * mib,
			wantPercent: 50,
		},
		{
			// Guard against a nonsensical frame producing a negative figure.
			name:      "cache larger than usage is ignored",
			usage:     10 * mib,
			limit:     100 * mib,
			stats:     map[string]uint64{"inactive_file": 20 * mib},
			wantUsage: 10 * mib, wantPercent: 10,
		},
		{
			name:      "no limit means no percentage",
			usage:     64 * mib,
			limit:     0,
			wantUsage: 64 * mib,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := container.StatsResponse{}
			s.MemoryStats.Usage = tt.usage
			s.MemoryStats.Limit = tt.limit
			s.MemoryStats.Stats = tt.stats

			usage, limit, pct := memoryUsage(s)
			if usage != tt.wantUsage {
				t.Errorf("usage: got %d, want %d", usage, tt.wantUsage)
			}
			if limit != int64(tt.limit) {
				t.Errorf("limit: got %d, want %d", limit, tt.limit)
			}
			if math.Abs(pct-tt.wantPercent) > 0.001 {
				t.Errorf("percent: got %v, want %v", pct, tt.wantPercent)
			}
		})
	}
}

func TestNetworkAndBlockIO(t *testing.T) {
	s := container.StatsResponse{}
	s.Networks = map[string]container.NetworkStats{
		"eth0": {RxBytes: 100, TxBytes: 200},
		"eth1": {RxBytes: 50, TxBytes: 25},
	}
	s.BlkioStats.IoServiceBytesRecursive = []container.BlkioStatEntry{
		{Op: "read", Value: 1000},
		{Op: "Read", Value: 500},
		{Op: "write", Value: 2000},
		{Op: "sync", Value: 9999},
	}

	if rx, tx := networkIO(s); rx != 150 || tx != 225 {
		t.Errorf("network: got %d/%d, want 150/225", rx, tx)
	}
	if r, w := blockIO(s); r != 1500 || w != 2000 {
		t.Errorf("block: got %d/%d, want 1500/2000", r, w)
	}
}

func TestShortID(t *testing.T) {
	cases := map[string]string{
		"sha256:9f2b1c3d4e5f6a7b8c9d": "9f2b1c3d4e5f",
		"9f2b1c3d4e5f6a7b8c9d":        "9f2b1c3d4e5f",
		"short":                       "short",
		"":                            "",
	}
	for in, want := range cases {
		if got := ShortID(in); got != want {
			t.Errorf("ShortID(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestImageDanglingAndRef(t *testing.T) {
	tagged := Image{ID: "sha256:abcdef123456789", RepoTags: []string{"nginx:alpine"}}
	if tagged.Dangling() {
		t.Error("a tagged image is not dangling")
	}
	if got := tagged.Ref(); got != "nginx:alpine" {
		t.Errorf("Ref: got %q", got)
	}

	untagged := Image{ID: "sha256:abcdef123456789", RepoTags: []string{"<none>:<none>"}}
	if !untagged.Dangling() {
		t.Error("<none>:<none> is dangling")
	}
	if got := untagged.Ref(); got != "abcdef123456" {
		t.Errorf("Ref: got %q", got)
	}
}

func TestAnonymousVolumeDetection(t *testing.T) {
	anon := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !isAnonymousVolume(anon) {
		t.Error("64 hex chars is an anonymous volume name")
	}
	if isAnonymousVolume("pgdata") {
		t.Error("a named volume is not anonymous")
	}
	if isAnonymousVolume(anon[:63] + "z") {
		t.Error("non-hex characters mean a named volume")
	}
}

func TestCompareAPIVersion(t *testing.T) {
	if compareAPIVersion("1.42", "1.42") != 0 {
		t.Error("equal versions compare equal")
	}
	if compareAPIVersion("1.41", "1.42") != -1 {
		t.Error("1.41 is older than 1.42")
	}
	if compareAPIVersion("1.5", "1.42") != -1 {
		t.Error("versions compare numerically, not lexically")
	}
	if compareAPIVersion("1.47", "1.42") != 1 {
		t.Error("1.47 is newer than 1.42")
	}
}
