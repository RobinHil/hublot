package state

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Diagnosis is a plain explanation of a failure, with what to do about it.
// The daemon's own message is informative and stays on screen, but it is
// written for someone who already knows what went wrong.
type Diagnosis struct {
	// Title is the failure in one line.
	Title string
	// Lines explain it and say what to do.
	Lines []string
	// Port is set when the trouble is a host port, so the caller can say what
	// is holding it.
	Port string
	// Name is set when the trouble is a container name already taken.
	Name string
	// Image is set when an image could not be found or pulled.
	Image string
	// File is set when the failure is in a file, which the caller can then
	// offer to open rather than leaving the user to find it.
	File string
}

var (
	portInUse  = regexp.MustCompile(`(?i)(?:address already in use|port is already allocated)`)
	nameTaken  = regexp.MustCompile(`(?i)container name "/?([^"]+)" is already in use`)
	noImage    = regexp.MustCompile(`(?i)(?:manifest unknown|manifest for ([^\s]+) not found|pull access denied for ([^\s,]+)|no such image:? ?([^\s]*))`)
	missingVar = regexp.MustCompile(`(?i)required variable ([A-Za-z_][A-Za-z0-9_]*) is missing`)
	lowPort    = regexp.MustCompile(`(?i)permission denied.*bind`)
	pausedCtr  = regexp.MustCompile(`(?i)cannot (?:start|kill) a paused container`)

	// Swarm, in the wording of the daemon and of swarmkit behind it.
	ingressPort    = regexp.MustCompile(`port '(\d{1,5})' is already in use by service '([^']+)'`)
	objectTaken    = regexp.MustCompile(`name conflicts with an existing object: (\w+) (\S+) already exists`)
	notManager     = regexp.MustCompile(`This node is not a swarm manager`)
	workerRefused  = regexp.MustCompile(`Worker nodes can't be used to view or modify cluster state`)
	swarmLocked    = regexp.MustCompile(`Swarm is encrypted and needs to be unlocked`)
	badUnlockKey   = regexp.MustCompile(`swarm could not be unlocked: invalid key`)
	inUseBy        = regexp.MustCompile(`(secret|config) '([^']+)' is in use by the following services?: (.+)`)
	networkInUse   = regexp.MustCompile(`network \S+ is in use by service`)
	outOfSequence  = regexp.MustCompile(`update out of sequence`)
	lastManager    = regexp.MustCompile(`attempting to demote the last manager of the swarm`)
	raftMember     = regexp.MustCompile(`is a cluster manager and is a member of the raft cluster`)
	alreadyInSwarm = regexp.MustCompile(`This node is already part of a swarm`)
	refMissing     = regexp.MustCompile(`service (\S+): (secret|config) not found: (\S+)`)

	// Compose reports a schema problem as "validating <path>: <what>", and a
	// syntax one through the yaml parser, which names no file at all.
	validating = regexp.MustCompile(`validating ([^\s:]+): (.+)`)
	badYAML    = regexp.MustCompile(`(?i)(?:go-yaml|yaml:) .*(?:error|did not find|could not find|found character)`)
	badSchema  = regexp.MustCompile(`(?i)(must be a (?:mapping|array|string|number|boolean)|additional properties .* not allowed|has neither an image nor a build context)`)

	// Tried in order: the more specific the pattern, the more it is trusted.
	portPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}:(\d{1,5})`),
		regexp.MustCompile(`\[::[0-9a-fA-F:]*\]:(\d{1,5})`),
		regexp.MustCompile(`:::(\d{1,5})`),
		regexp.MustCompile(`(?i)\bport:? (\d{1,5})\b`),
	}
)

// Diagnose reads the output of a failed command and recognises the handful of
// mistakes that account for most failures. It returns false when it has
// nothing useful to add, in which case the daemon's own words are better than
// anything invented here.
func Diagnose(output string) (Diagnosis, bool) {
	if d, ok := diagnoseSwarm(output); ok {
		return d, true
	}
	switch {
	case portInUse.MatchString(output):
		d := Diagnosis{
			Title: "That host port is already taken",
			Lines: []string{
				"Something else on this machine is already listening on it, so the",
				"container could not be published.",
			},
		}
		if port := extractPort(output); port != "" {
			d.Port = port
			d.Title = "Host port " + port + " is already taken"
		}
		d.Lines = append(d.Lines,
			"",
			"Either publish it on another host port, the number on the left of the",
			"colon, or stop whatever is holding this one.",
		)
		return d, true

	case nameTaken.MatchString(output):
		name := firstGroup(nameTaken, output)
		return Diagnosis{
			Title: "The name " + name + " is already taken",
			Name:  name,
			Lines: []string{
				"A container with that name exists on this host, running or not.",
				"",
				"Give this one another name, or remove the old one first: it is in the",
				"containers view, and D removes it.",
			},
		}, true

	case noImage.MatchString(output):
		image := firstGroup(noImage, output)
		d := Diagnosis{
			Title: "That image could not be fetched",
			Image: image,
			Lines: []string{
				"The reference does not exist on this host or in the registry, or it",
				"needs credentials this machine does not have.",
				"",
				"Check the spelling and the tag. A private registry needs a docker",
				"login, which hublot deliberately does not do.",
			},
		}
		if image != "" {
			d.Title = "Cannot fetch " + image
		}
		return d, true

	case missingVar.MatchString(output):
		name := firstGroup(missingVar, output)
		return Diagnosis{
			Title: "The variable " + name + " has no value",
			Lines: []string{
				"Compose resolves variables when it runs, and records nothing about",
				"where they came from. This stack was probably first started from a",
				"shell that had " + name + " exported.",
				"",
				"Put it in a .env file next to the compose file, which compose reads by",
				"itself, or give it a default in the yaml with ${" + name + ":-value}.",
			},
		}, true

	case lowPort.MatchString(output):
		return Diagnosis{
			Title: "Ports below 1024 need privileges",
			Lines: []string{
				"The daemon was refused permission to bind that port.",
				"",
				"Publish on a high port instead, such as 8080:80, and put whatever",
				"needs the low one in front of it.",
			},
		}, true

	case pausedCtr.MatchString(output):
		return Diagnosis{
			Title: "That container is paused, not stopped",
			Lines: []string{
				"A paused container is frozen where it stands, still in memory. It",
				"cannot be started, because it never stopped.",
				"",
				"P lets it run again. It is in the containers view, and the key bar",
				"says unpause whenever the cursor is on a paused one.",
			},
		}, true

	// The config cases come before the rest: a file that does not parse is a
	// file to open, and saying so beats anything the failure that followed it
	// could be read as.
	case validating.MatchString(output):
		m := validating.FindStringSubmatch(output)
		return Diagnosis{
			Title: "The compose file does not describe a valid stack",
			File:  m[1],
			Lines: []string{
				strings.TrimSpace(m[2]),
				"",
				"Compose checked the file against its schema and refused it, so",
				"nothing was created or changed.",
			},
		}, true

	case badYAML.MatchString(output):
		return Diagnosis{
			Title: "The compose file is not valid YAML",
			Lines: []string{
				firstLine(output),
				"",
				"YAML is indentation, and this is almost always a line indented by a",
				"different amount than the ones around it, or a missing colon.",
			},
		}, true

	case badSchema.MatchString(output):
		return Diagnosis{
			Title: "The compose file does not describe a valid stack",
			Lines: []string{
				firstLine(output),
				"",
				"The syntax is fine, so this is a key in the wrong place, a value of",
				"the wrong shape, or a service missing what every service needs.",
			},
		}, true

	case strings.Contains(strings.ToLower(output), "no space left on device"):
		return Diagnosis{
			Title: "The disk is full",
			Lines: []string{
				"There is no room left where docker keeps its data.",
				"",
				"The disk view shows where it went and what a prune would give",
				"back before removing anything.",
			},
		}, true
	}

	return Diagnosis{}, false
}

// extractPort pulls the host port out of a binding error, which names it in
// several shapes depending on which layer complained.
func extractPort(output string) string {
	// The binding is written as an address: 0.0.0.0:8080, [::]:8080, :::8080.
	// Anchoring on that is what keeps a container id out of the answer, since
	// "starting container 14dde66ee0cc" offers a perfectly good "14" to
	// anything looking for the first number after a space.
	for _, re := range portPatterns {
		for _, m := range re.FindAllStringSubmatch(output, -1) {
			if p := validPort(m[1]); p != "" {
				return p
			}
		}
	}
	return ""
}

// validPort keeps only what could actually be a TCP port, so a hexadecimal id
// that happens to read as digits is discarded rather than shown.
func validPort(s string) string {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return ""
	}
	return s
}

// firstLine is the one line of output worth quoting back: compose puts the
// actual complaint first and the noise after it.
func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// firstGroup returns the first non-empty capture, since several alternatives
// in one expression each have their own group.
func firstGroup(re *regexp.Regexp, output string) string {
	m := re.FindStringSubmatch(output)
	for _, group := range m[1:] {
		if group != "" {
			return strings.TrimSuffix(group, ":")
		}
	}
	return ""
}

// PortHolder names what is publishing a host port, so an explanation can say
// which container to look at rather than leaving it as an exercise.
func PortHolder(containers []Container, port string) string {
	for _, c := range containers {
		for _, p := range c.Ports {
			if fmt.Sprintf("%d", p.Public) == port {
				return c.Name
			}
		}
	}
	return ""
}

// Container and Port are the little the diagnosis needs to know about the
// running objects, kept here so this file depends on nothing.
type Container struct {
	Name  string
	Ports []Port
}

// Port is a published port.
type Port struct {
	Public uint16
}

// diagnoseSwarm recognises the swarm failures. They come first because some
// of them would otherwise be read as their single-host cousins: a port taken
// on the routing mesh is not one taken on this machine.
func diagnoseSwarm(output string) (Diagnosis, bool) {
	switch {
	case ingressPort.MatchString(output):
		m := ingressPort.FindStringSubmatch(output)
		return Diagnosis{
			Title: "Port " + m[1] + " is already published by the service " + m[2],
			Lines: []string{
				"A port published through the routing mesh is opened on every node of",
				"the swarm, so two services cannot share it, wherever their tasks run.",
				"",
				"Publish this one on another port, or unpublish it from " + m[2] + " first.",
			},
		}, true

	case objectTaken.MatchString(output):
		m := objectTaken.FindStringSubmatch(output)
		return Diagnosis{
			Title: "A " + m[1] + " named " + m[2] + " already exists",
			Lines: []string{
				"Names are unique across the swarm for each kind of object.",
				"",
				"Pick another name, or change the existing one rather than making a",
				"second: it is in the Swarm view.",
			},
		}, true

	case workerRefused.MatchString(output):
		return Diagnosis{
			Title: "This node is a worker",
			Lines: []string{
				"Only a manager answers for the swarm: its services, nodes, secrets and",
				"configs. Open hublot on a manager, or promote this node from one.",
			},
		}, true

	case notManager.MatchString(output):
		return Diagnosis{
			Title: "This daemon is not part of a swarm",
			Lines: []string{
				"Services, secrets, configs and overlay networks only exist in one.",
				"",
				"The Swarm view creates one with I or joins one with J; , turns it on.",
			},
		}, true

	case swarmLocked.MatchString(output):
		return Diagnosis{
			Title: "This manager is locked",
			Lines: []string{
				"Autolock is on and the daemon restarted, so it cannot read the swarm's",
				"state until it is given the unlock key.",
				"",
				"u in the Swarm view asks for it. Another manager shows it with",
				"docker swarm unlock-key.",
			},
		}, true

	case badUnlockKey.MatchString(output):
		return Diagnosis{
			Title: "That is not the unlock key",
			Lines: []string{
				"The key starts with SWMKEY-1-, and rotating it on another manager",
				"makes the old one useless. docker swarm unlock-key, on a manager that",
				"is unlocked, shows the current one.",
			},
		}, true

	case inUseBy.MatchString(output):
		m := inUseBy.FindStringSubmatch(output)
		return Diagnosis{
			Title: "The " + m[1] + " " + m[2] + " is still mounted",
			Lines: []string{
				"These services use it: " + strings.TrimSpace(m[3]) + ".",
				"",
				"Detach it from them first, from the palette on each service, or give",
				"them a new version with E so the old one is free to go.",
			},
		}, true

	case networkInUse.MatchString(output):
		return Diagnosis{
			Title: "A service is still attached to that network",
			Lines: []string{
				"A swarm network belongs to the services using it, wherever their",
				"tasks run, so the containers on this node say nothing about it.",
				"",
				"The network column of the Networks view names the services; disconnect",
				"them, or remove them, first.",
			},
		}, true

	case outOfSequence.MatchString(output):
		return Diagnosis{
			Title: "It changed while you were editing it",
			Lines: []string{
				"Something else updated this object after hublot read it, and the swarm",
				"refuses a change made against an older version rather than undo that one.",
				"",
				"Make the change again: it starts from what is there now.",
			},
		}, true

	case lastManager.MatchString(output):
		return Diagnosis{
			Title: "That is the last manager",
			Lines: []string{
				"Without a manager nothing could change the swarm again. Promote another",
				"node first, then demote this one.",
			},
		}, true

	case raftMember.MatchString(output):
		return Diagnosis{
			Title: "A manager cannot be removed as it is",
			Lines: []string{
				"It holds a vote in the quorum. Demote it to a worker first, from the",
				"palette on its line, then remove it.",
			},
		}, true

	case refMissing.MatchString(output):
		m := refMissing.FindStringSubmatch(output)
		return Diagnosis{
			Title: "The stack file names a " + m[2] + " that does not exist: " + m[3],
			Lines: []string{
				"The service " + m[1] + " mounts " + m[3] + ", declared external, so the deploy",
				"expects to find it already in the swarm. It may have been rotated to a",
				"new version, which takes a new name.",
				"",
				"Point the file at the " + m[2] + " that exists now, in the " + m[2] + "s section",
				"of the Swarm view, or create one under that name.",
			},
		}, true

	case alreadyInSwarm.MatchString(output):
		return Diagnosis{
			Title: "This node is already in a swarm",
			Lines: []string{
				"A node belongs to one swarm at a time. Leave the current one first,",
				"from the palette in the Swarm view.",
			},
		}, true
	}
	return Diagnosis{}, false
}
