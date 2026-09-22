package state

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RobinHil/hublot/internal/docker"
)

// EditFields are the names the container edit form uses, in the order it shows
// them. The view builds its fields from these and hands the values straight
// back, so neither side can invent a name the other does not know.
var EditFields = []string{
	"name", "restart", "cpus", "memory",
	"image", "command", "env", "ports", "mounts",
}

// EditPrefill is what the form starts with: the container as it stands, in the
// syntax the fields are typed in.
func EditPrefill(spec docker.ContainerSpec) map[string]string {
	return map[string]string{
		"name":    spec.Name,
		"restart": restartOrNo(spec.RestartPolicy),
		"cpus":    FormatCPUs(spec.NanoCPUs),
		"memory":  FormatLimit(spec.Memory),
		"image":   spec.Image,
		"command": JoinCommand(spec.Command),
		"env":     strings.Join(spec.Env, ", "),
		"ports":   strings.Join(spec.Ports, ", "),
		"mounts":  strings.Join(spec.Mounts, ", "),
	}
}

// EditFromForm turns what the form came back with into the change to make.
//
// A field still holding exactly what it was filled with is left out of the
// edit entirely. That rule is what makes editing safe: a container carries
// more than nine fields can show, some of it in a shape this syntax cannot
// express, and anything nobody typed over is never sent anywhere.
func EditFromForm(spec docker.ContainerSpec, values map[string]string) (docker.Edit, error) {
	before := EditPrefill(spec)
	var e docker.Edit

	changed := func(field string) (string, bool) {
		value := strings.TrimSpace(values[field])
		return value, value != before[field]
	}

	if name, ok := changed("name"); ok {
		if name == "" {
			return e, fmt.Errorf("a container cannot be left without a name")
		}
		e.Name = &name
	}

	if policy, ok := changed("restart"); ok {
		e.RestartPolicy = &policy
	}

	if value, ok := changed("cpus"); ok {
		cpus, err := ParseCPUs(orZero(value))
		if err != nil {
			return e, err
		}
		e.NanoCPUs = &cpus
	}

	if value, ok := changed("memory"); ok {
		memory, err := ParseBytes(orZero(value))
		if err != nil {
			return e, err
		}
		e.Memory = &memory
	}

	if image, ok := changed("image"); ok {
		if image == "" {
			return e, fmt.Errorf("a container needs an image to run")
		}
		e.Image = &image
	}

	if value, ok := changed("command"); ok {
		command, err := SplitCommand(value)
		if err != nil {
			return e, err
		}
		e.Command = &command
	}

	if value, ok := changed("env"); ok {
		env, err := ParseEnv(value)
		if err != nil {
			return e, err
		}
		e.Env = &env
	}

	if value, ok := changed("ports"); ok {
		ports := ParseList(value)
		e.Ports = &ports
	}

	if value, ok := changed("mounts"); ok {
		mounts, err := ParseMounts(value)
		if err != nil {
			return e, err
		}
		e.Mounts = &mounts
	}

	return e, nil
}

// DescribeEdit says what the change does, one line per field, for the dialog
// that asks about it. A confirmation that names what it is about to do beats
// one that asks whether you are sure (AGENTS.md section 10.1).
func DescribeEdit(spec docker.ContainerSpec, e docker.Edit) []string {
	before := EditPrefill(spec)

	var out []string
	line := func(field string, to string) {
		out = append(out, fmt.Sprintf("  %-8s %s  ->  %s", field, orNone(before[field]), orNone(to)))
	}

	if e.Name != nil {
		line("name", *e.Name)
	}
	if e.RestartPolicy != nil {
		line("restart", restartOrNo(*e.RestartPolicy))
	}
	if e.NanoCPUs != nil {
		line("cpus", FormatCPUs(*e.NanoCPUs))
	}
	if e.Memory != nil {
		line("memory", FormatLimit(*e.Memory))
	}
	if e.Image != nil {
		line("image", *e.Image)
	}
	if e.Command != nil {
		line("command", JoinCommand(*e.Command))
	}
	if e.Env != nil {
		line("env", strings.Join(*e.Env, ", "))
	}
	if e.Ports != nil {
		line("ports", strings.Join(*e.Ports, ", "))
	}
	if e.Mounts != nil {
		line("volumes", strings.Join(*e.Mounts, ", "))
	}
	return out
}

// JoinCommand renders an argument list as one line, quoting what would not
// survive being split again. SplitCommand reads back exactly what this writes.
func JoinCommand(args []string) string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t'\"") {
			out = append(out, `"`+strings.ReplaceAll(arg, `"`, `'`)+`"`)
			continue
		}
		out = append(out, arg)
	}
	return strings.Join(out, " ")
}

// FormatCPUs renders a nanoCPU figure as the number of cores a person types.
func FormatCPUs(nanoCPUs int64) string {
	if nanoCPUs <= 0 {
		return "0"
	}
	return strconv.FormatFloat(float64(nanoCPUs)/1e9, 'f', -1, 64)
}

// FormatLimit renders a limit as a size, where zero is no limit at all rather
// than a limit of nothing.
func FormatLimit(bytes int64) string {
	if bytes <= 0 {
		return "0"
	}
	return FormatBytes(bytes)
}

// restartOrNo names the policy a container with none has, which is what the
// docker CLI calls it.
func restartOrNo(policy string) string {
	if policy == "" {
		return "no"
	}
	return policy
}

// orZero reads an emptied number field as zero, since clearing a limit is what
// emptying it means.
func orZero(value string) string {
	if value == "" {
		return "0"
	}
	return value
}

func orNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(none)"
	}
	return value
}
