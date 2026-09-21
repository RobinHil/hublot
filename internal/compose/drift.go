package compose

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Hashes returns the config hash the current YAML resolves to, per service.
// This is the same hash Compose itself compares to decide whether a container
// needs recreating (AGENTS.md section 8.4).
//
// Older Compose versions do not support `config --hash`; the error is returned
// so the caller can degrade to DriftUnknown rather than showing a wrong marker.
func (c CLI) Hashes(ctx context.Context, p Project) (map[string]string, error) {
	if !c.Available {
		return nil, errors.New(c.Reason)
	}
	if !p.Actionable() {
		return nil, fmt.Errorf("project %s has no readable compose file", p.Name)
	}

	out, err := c.Command(ctx, p, "config", "--hash=*").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("hashing config for %s: %s", p.Name, trimOutput(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("hashing config for %s: %w", p.Name, err)
	}
	return ParseHashes(string(out)), nil
}

// ParseHashes reads the "service hash" lines `config --hash` prints.
func ParseHashes(out string) map[string]string {
	hashes := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		hashes[fields[0]] = fields[1]
	}
	return hashes
}

// ApplyDrift compares the hash recorded on each running container against what
// the file resolves to now, and fills in the per-service marker. A service with
// nothing running stays DriftStopped: its config cannot have drifted, there is
// simply nothing to compare.
func ApplyDrift(p *Project, hashes map[string]string, hashErr error) {
	for i := range p.Services {
		s := &p.Services[i]
		s.FileHash = hashes[s.Name]

		switch {
		case s.Running() == 0:
			s.Drift = DriftStopped
		case hashErr != nil, s.ConfigHash == "", s.FileHash == "":
			s.Drift = DriftUnknown
		case s.ConfigHash == s.FileHash:
			s.Drift = DriftNone
		default:
			s.Drift = DriftChanged
		}
	}
}

// Drifted reports whether any service of the project needs recreating.
func (p Project) Drifted() bool {
	for _, s := range p.Services {
		if s.Drift == DriftChanged {
			return true
		}
	}
	return false
}
