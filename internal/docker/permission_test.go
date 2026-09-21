package docker

import (
	"strings"
	"testing"
)

func TestGroupAdvice(t *testing.T) {
	const socket = "/var/run/docker.sock"

	tests := []struct {
		name        string
		inProcess   bool
		inDatabase  bool
		wantPhrases []string
		avoid       string
	}{
		{
			// The case that wastes the most time: usermod ran, the session did
			// not restart, and every guide says to add yourself to the group
			// you are already in.
			name:       "member but the session predates it",
			inProcess:  false,
			inDatabase: true,
			wantPhrases: []string{
				"you are in the 'docker' group",
				"session started before that",
				"newgrp docker",
			},
			avoid: "usermod",
		},
		{
			name:       "not a member at all",
			inProcess:  false,
			inDatabase: false,
			wantPhrases: []string{
				"add yourself to the 'docker' group",
				"usermod -aG docker robin",
				"log out and back in",
			},
			avoid: "newgrp",
		},
		{
			// Already a member and the process carries it, so the group is not
			// the problem and saying otherwise sends the user in circles.
			name:       "member and still refused",
			inProcess:  true,
			inDatabase: true,
			wantPhrases: []string{
				"although you are in the 'docker' group",
				"check the socket's permissions",
			},
			avoid: "usermod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupAdvice(socket, "docker", "robin", tt.inProcess, tt.inDatabase)

			if !strings.Contains(got, socket) {
				t.Errorf("the advice must name the socket: %q", got)
			}
			for _, phrase := range tt.wantPhrases {
				if !strings.Contains(got, phrase) {
					t.Errorf("advice is missing %q:\n%s", phrase, got)
				}
			}
			if tt.avoid != "" && strings.Contains(got, tt.avoid) {
				t.Errorf("advice should not mention %q:\n%s", tt.avoid, got)
			}
		})
	}
}

func TestGroupAdviceWorksForAnyGroupName(t *testing.T) {
	// The socket is not always owned by a group called docker: a rootless
	// setup, or a repackaged daemon, can differ.
	got := groupAdvice("/run/user/1000/docker.sock", "dockerroot", "someone", false, false)
	if !strings.Contains(got, "'dockerroot'") || !strings.Contains(got, "usermod -aG dockerroot someone") {
		t.Errorf("the advice must use the socket's own group: %s", got)
	}
}

func TestHasGroup(t *testing.T) {
	groups := []int{1000, 998, 941}
	if !hasGroup(groups, 941) {
		t.Error("941 is in the list")
	}
	if hasGroup(groups, 42) {
		t.Error("42 is not in the list")
	}
	if hasGroup(nil, 941) {
		t.Error("nothing is in an empty list")
	}
}
