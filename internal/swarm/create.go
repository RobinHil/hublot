package swarm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RobinHil/hublot/internal/docker"
)

// ServiceForm is what the creation form asks for: what `docker service create`
// takes for a service a person would type out by hand. Anything more belongs
// in a stack file.
type ServiceForm struct {
	Name        string
	Image       string
	Mode        string
	Replicas    string
	Ports       []string
	Mounts      []string
	Env         []string
	Networks    []string
	Constraints []string
	Command     []string
}

// CreateArgs turns the form into a `docker service create` invocation,
// refusing what the daemon would refuse in a less readable way.
func CreateArgs(f ServiceForm) ([]string, error) {
	if strings.TrimSpace(f.Image) == "" {
		return nil, fmt.Errorf("a service needs an image")
	}
	args := []string{"service", "create", "--detach", "--with-registry-auth"}
	if f.Name != "" {
		args = append(args, "--name", f.Name)
	}

	mode := f.Mode
	if mode == "" {
		mode = "replicated"
	}
	args = append(args, "--mode", mode)
	if f.Replicas != "" {
		if mode != "replicated" && mode != "replicated-job" {
			return nil, fmt.Errorf("a %s service runs one task per node, so it takes no replica count", mode)
		}
		n, err := strconv.ParseUint(f.Replicas, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("replicas is a whole number, not %q", f.Replicas)
		}
		args = append(args, "--replicas", strconv.FormatUint(n, 10))
	}

	for _, p := range f.Ports {
		spec, err := publishSpec(p)
		if err != nil {
			return nil, err
		}
		args = append(args, "--publish", spec)
	}
	for _, m := range f.Mounts {
		spec, err := mountSpec(m)
		if err != nil {
			return nil, err
		}
		args = append(args, "--mount", spec)
	}
	for _, e := range f.Env {
		args = append(args, "--env", e)
	}
	for _, n := range f.Networks {
		args = append(args, "--network", n)
	}
	for _, c := range f.Constraints {
		constraint, err := ParseConstraint(c)
		if err != nil {
			return nil, err
		}
		args = append(args, "--constraint", constraint)
	}

	args = append(args, f.Image)
	return append(args, f.Command...), nil
}

// ParseConstraint checks a placement constraint and tidies the spaces people
// put around its operator, which the daemon accepts but a list split on
// spaces would tear apart.
func ParseConstraint(c string) (string, error) {
	for _, op := range []string{"==", "!="} {
		if left, right, ok := strings.Cut(c, op); ok {
			left, right = strings.TrimSpace(left), strings.TrimSpace(right)
			if left == "" || right == "" || strings.ContainsAny(right, "=!") {
				break
			}
			return left + op + right, nil
		}
	}
	return "", fmt.Errorf("%q is not a constraint: write node.role==worker or node.labels.zone!=eu", c)
}

// SplitConstraints reads constraints separated by commas. Not by spaces:
// "node.role == worker" is one constraint, however it is spaced.
func SplitConstraints(in string) []string {
	var out []string
	for _, c := range strings.Split(in, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// ParsePort reads 8080:80, 8080:80/udp, or 80 alone for a port the swarm
// picks.
func ParsePort(p string) (docker.ServicePort, error) {
	in := p
	proto := "tcp"
	if base, rest, ok := strings.Cut(p, "/"); ok {
		p, proto = base, rest
	}
	if proto != "tcp" && proto != "udp" && proto != "sctp" {
		return docker.ServicePort{}, fmt.Errorf("%q: the protocol is tcp, udp or sctp", in)
	}

	published, target, found := strings.Cut(p, ":")
	if !found {
		target, published = published, ""
	}
	t, err := strconv.ParseUint(target, 10, 16)
	if err != nil || t == 0 {
		return docker.ServicePort{}, fmt.Errorf("%q is not a port: write published:target, such as 8080:80", in)
	}
	port := docker.ServicePort{Target: uint32(t), Protocol: proto, Mode: "ingress"}
	if published != "" {
		pub, err := strconv.ParseUint(published, 10, 16)
		if err != nil {
			return docker.ServicePort{}, fmt.Errorf("%q is not a port: write published:target, such as 8080:80", in)
		}
		port.Published = uint32(pub)
	}
	return port, nil
}

// publishSpec writes a port in the long form the CLI takes without guessing.
func publishSpec(p string) (string, error) {
	port, err := ParsePort(p)
	if err != nil {
		return "", err
	}
	spec := fmt.Sprintf("target=%d,protocol=%s", port.Target, port.Protocol)
	if port.Published != 0 {
		spec = fmt.Sprintf("published=%d,", port.Published) + spec
	}
	return spec, nil
}

// mountSpec reads source:/path[:ro]. A source that is a path is a bind mount,
// anything else a named volume. A bind mount in a swarm is made on whichever
// node runs the task, so a relative path means nothing there and is refused.
func mountSpec(m string) (string, error) {
	parts := strings.Split(m, ":")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || !strings.HasPrefix(parts[1], "/") {
		return "", fmt.Errorf("%q is not a mount: write source:/path/inside, and ro after it if needed", m)
	}
	src, dst := parts[0], parts[1]

	kind := "volume"
	switch {
	case strings.HasPrefix(src, "/"):
		kind = "bind"
	case strings.HasPrefix(src, ".") || strings.HasPrefix(src, "~"):
		return "", fmt.Errorf("%q: a bind mount in a service needs an absolute path, present on every node that may run it", m)
	}

	spec := "type=" + kind + ",source=" + src + ",target=" + dst
	if len(parts) == 3 {
		switch parts[2] {
		case "ro":
			spec += ",readonly"
		case "rw":
		default:
			return "", fmt.Errorf("%q: the option after the path is ro or rw", m)
		}
	}
	return spec, nil
}
