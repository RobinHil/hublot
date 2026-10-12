package state

import (
	"strings"
	"testing"
)

// Every message below was captured from Docker 29.9 in a swarm, through the
// API as hublot wraps it or through the CLI, whichever path hublot takes for
// that action.
func TestDiagnoseSwarm(t *testing.T) {
	cases := []struct {
		name, output, title string
	}{
		{
			"ingress port taken by another service",
			`Error response from daemon: rpc error: code = InvalidArgument desc = port '18080' is already in use by service 'cap-a' (g0n9wi2scgsgkj8zc3iphyet4) as an ingress port`,
			"Port 18080 is already published by the service cap-a",
		},
		{
			"service name taken",
			`Error response from daemon: rpc error: code = AlreadyExists desc = name conflicts with an existing object: service cap-a already exists`,
			"A service named cap-a already exists",
		},
		{
			"not in a swarm",
			`Error response from daemon: This node is not a swarm manager. Use "docker swarm init" or "docker swarm join" to connect this node to swarm and try again.`,
			"This daemon is not part of a swarm",
		},
		{
			"a worker asked for the swarm",
			`Error response from daemon: This node is not a swarm manager. Worker nodes can't be used to view or modify cluster state. Please run this command on a manager node or promote the current node to a manager.`,
			"This node is a worker",
		},
		{
			"locked after a restart",
			`Error response from daemon: Swarm is encrypted and needs to be unlocked before it can be used. Please use "docker swarm unlock" to unlock it.`,
			"This manager is locked",
		},
		{
			"wrong unlock key",
			`unlocking the swarm: Error response from daemon: swarm could not be unlocked: invalid key provided`,
			"That is not the unlock key",
		},
		{
			"secret still mounted",
			`removing secret rfsvmoibi56q: Error response from daemon: rpc error: code = InvalidArgument desc = secret 'hublot-live-secret' is in use by the following service: hublot-live-web`,
			"The secret hublot-live-secret is still mounted",
		},
		{
			"config still mounted",
			`Error response from daemon: rpc error: code = InvalidArgument desc = config 'cap-c' is in use by the following service: cap-a`,
			"The config cap-c is still mounted",
		},
		{
			"swarm network still used",
			`Error response from daemon: rpc error: code = FailedPrecondition desc = network 4919ucp0m8q03a323q5py61dn is in use by service g0n9wi2scgsgkj8zc3iphyet4`,
			"A service is still attached to that network",
		},
		{
			"edited against an old version",
			`updating service hublot-live-web: Error response from daemon: rpc error: code = Unknown desc = update out of sequence`,
			"It changed while you were editing it",
		},
		{
			"demoting the last manager",
			`updating node charlunix: Error response from daemon: rpc error: code = FailedPrecondition desc = attempting to demote the last manager of the swarm`,
			"That is the last manager",
		},
		{
			"removing a manager",
			`removing node m9olf6jide09: Error response from daemon: rpc error: code = FailedPrecondition desc = node m9olf6jide09p1dlv7rzwy3v5 is a cluster manager and is a member of the raft cluster. It must be demoted to worker before removal`,
			"A manager cannot be removed as it is",
		},
		{
			"a stack deployed with a secret since rotated",
			"service web: secret not found: db_pw",
			"The stack file names a secret that does not exist: db_pw",
		},
		{
			"joining while in a swarm",
			`Error response from daemon: This node is already part of a swarm. Use "docker swarm leave" to leave this swarm and join another one.`,
			"This node is already in a swarm",
		},
	}
	for _, c := range cases {
		d, ok := Diagnose(c.output)
		if !ok {
			t.Errorf("%s: not recognised", c.name)
			continue
		}
		if d.Title != c.title {
			t.Errorf("%s: title %q, want %q", c.name, d.Title, c.title)
		}
		if len(d.Lines) == 0 {
			t.Errorf("%s: no explanation", c.name)
		}
	}
}

// A port published on the routing mesh is not the single-host clash, whose
// advice about the host side of a binding would be wrong here.
func TestDiagnoseIngressPortIsNotAHostPort(t *testing.T) {
	d, _ := Diagnose(`rpc error: code = InvalidArgument desc = port '18080' is already in use by service 'cap-a' (g0n9wi2scgsgkj8zc3iphyet4) as an ingress port`)
	if d.Port != "" || strings.Contains(strings.Join(d.Lines, " "), "host port") {
		t.Fatalf("read as a host port clash: %+v", d)
	}
}

// The task error a pull fails with is already recognised as an image that
// cannot be fetched, swarm or not.
func TestDiagnoseTaskPullFailure(t *testing.T) {
	d, ok := Diagnose(`pull access denied for nonexistent-hublot/nope, repository does not exist or may require 'docker login'`)
	if !ok || d.Image != "nonexistent-hublot/nope" {
		t.Fatalf("got %+v, %v", d, ok)
	}
}
