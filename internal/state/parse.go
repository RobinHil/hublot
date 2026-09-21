package state

import (
	"fmt"
	"os"
	"path/filepath"
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

// ParseList splits a comma or space separated list the way a person types one,
// dropping the empty pieces a trailing comma leaves behind.
func ParseList(in string) []string {
	fields := strings.FieldsFunc(in, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})

	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// ParseEnv reads KEY=value pairs, which is what the daemon wants and what
// people type. A pair without a value is refused rather than sent as a name
// with an empty string, since the two mean different things to a program.
func ParseEnv(in string) ([]string, error) {
	var out []string
	for _, pair := range ParseList(in) {
		name, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%q is not a KEY=value pair", pair)
		}
		out = append(out, strings.TrimSpace(name)+"="+value)
	}
	return out, nil
}

// ParseMounts reads the source:destination[:options] bindings docker takes,
// checking the shape here so the error names the entry rather than coming back
// from the daemon as a sentence about a mount spec.
func ParseMounts(in string) ([]string, error) {
	var out []string
	for _, mount := range ParseList(in) {
		parts := strings.Split(mount, ":")
		if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("%q is not a mount: write source:/path/inside, and ro or rw after it", mount)
		}
		if !strings.HasPrefix(parts[1], "/") {
			return nil, fmt.Errorf("%q must mount at an absolute path inside the container", mount)
		}
		if len(parts) == 3 && parts[2] != "ro" && parts[2] != "rw" {
			return nil, fmt.Errorf("%q: the option after the path is ro or rw", mount)
		}
		out = append(out, mount)
	}
	return out, nil
}

// SplitCommand splits a command line, honouring single and double quotes so an
// argument with a space in it survives. It is not a shell: there is no
// expansion, no pipes and no substitution, which is what running a container
// wants.
func SplitCommand(in string) ([]string, error) {
	var (
		out     []string
		current strings.Builder
		quote   rune
		started bool
	)

	for _, r := range in {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			if started {
				out = append(out, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unbalanced %c quote", quote)
	}
	if started {
		out = append(out, current.String())
	}
	return out, nil
}

// ExpandPath resolves a path the way someone typing it expects: a leading
// tilde becomes the home directory, and $VARIABLES are substituted.
//
// The shell does this before a program ever sees its arguments, so a path
// typed into a text field inside a program does not get it for free. Without
// this, "~/stacks/blog" creates a directory actually named "~" wherever the
// program happens to be running.
func ExpandPath(path, home string) string {
	path = strings.TrimSpace(os.ExpandEnv(path))
	if path == "" {
		return path
	}

	switch {
	case path == "~":
		if home != "" {
			return home
		}
	case strings.HasPrefix(path, "~/"):
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// HomePath expands against the user's own home directory.
func HomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return ExpandPath(path, home)
}
