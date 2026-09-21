package state

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseBytes reads a size the way the docker CLI accepts one: a number with an
// optional unit suffix, where a bare number is bytes. "0" means no limit, which
// is how the daemon clears one.
func ParseBytes(in string) (int64, error) {
	s := strings.TrimSpace(strings.ToLower(in))
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}

	units := []struct {
		suffix string
		factor int64
	}{
		{"kb", 1 << 10}, {"mb", 1 << 20}, {"gb", 1 << 30}, {"tb", 1 << 40},
		{"k", 1 << 10}, {"m", 1 << 20}, {"g", 1 << 30}, {"t", 1 << 40},
		{"b", 1},
	}

	factor := int64(1)
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			factor = u.factor
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size: use a number with an optional unit, such as 512m", in)
	}
	if value < 0 {
		return 0, fmt.Errorf("a size cannot be negative")
	}
	return int64(value * float64(factor)), nil
}

// ParseCPUs reads a CPU allowance in cores, as `docker update --cpus` takes it,
// and returns the nanoCPU figure the API wants. Zero clears the limit.
func ParseCPUs(in string) (int64, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return 0, fmt.Errorf("empty cpu count")
	}

	cores, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number of cpus: use a decimal, such as 1.5", in)
	}
	if cores < 0 {
		return 0, fmt.Errorf("a cpu allowance cannot be negative")
	}
	return int64(cores * 1e9), nil
}
