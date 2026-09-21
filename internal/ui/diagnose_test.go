package ui

import (
	"strings"
	"testing"

	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
)

// The failure a beginner meets most often is a port already taken, and the
// daemon's own wording never says so in as many words. What the user reads has
// to name the port, and the container sitting on it.
func TestExplainNamesThePortAndWhoHoldsIt(t *testing.T) {
	a := &App{store: &state.Store{Containers: []docker.Container{
		{Name: "blog-web-1", Ports: []docker.Port{{PublicPort: 8080, PrivatePort: 80}}},
		{Name: "shop-storefront-1", Ports: []docker.Port{{PublicPort: 8081, PrivatePort: 80}}},
	}}}

	output := `conflit-web-1  Error
Error response from daemon: failed to set up container networking: driver failed programming external connectivity on endpoint conflit-web-1 (2b1c): failed to bind host port for 0.0.0.0:8080:172.18.0.2:80/tcp: address already in use`

	modal, ok := a.explain(output)
	if !ok {
		t.Fatal("a port clash must be explained")
	}
	body := modal.Title + "\n" + strings.Join(modal.Body, "\n")
	if !strings.Contains(body, "8080") {
		t.Errorf("the port has to be in there:\n%s", body)
	}
	if !strings.Contains(body, "blog-web-1") {
		t.Errorf("the container holding it has to be named:\n%s", body)
	}
	if strings.Contains(body, "shop-storefront-1") {
		t.Errorf("only the container on that port, not every published one:\n%s", body)
	}
}

// Nothing is claimed about a failure nobody recognises: the daemon's message
// is shown as it came instead.
func TestExplainStaysQuietOnAnythingElse(t *testing.T) {
	a := &App{store: &state.Store{}}
	if _, ok := a.explain("Error response from daemon: something nobody has seen before"); ok {
		t.Error("an unknown failure must not be dressed up as a diagnosis")
	}
}

// A clash with something outside docker still deserves the explanation, minus
// the container name, which would be a lie.
func TestExplainWithoutAContainerOnThePort(t *testing.T) {
	a := &App{store: &state.Store{Containers: []docker.Container{
		{Name: "blog-web-1", Ports: []docker.Port{{PublicPort: 8080}}},
	}}}

	modal, ok := a.explain("Error response from daemon: Ports are not available: exposing port TCP 0.0.0.0:5432 -> 0.0.0.0:0: listen tcp 0.0.0.0:5432: bind: address already in use")
	if !ok {
		t.Fatal("a port clash must be explained")
	}
	body := modal.Title + "\n" + strings.Join(modal.Body, "\n")
	if !strings.Contains(body, "5432") {
		t.Errorf("the port has to be in there:\n%s", body)
	}
	if strings.Contains(body, "blog-web-1") {
		t.Errorf("no container holds 5432, so none may be named:\n%s", body)
	}
}

// The container id in front of a run failure is not a port number, however
// much it looks like one at a glance.
func TestExplainIgnoresTheContainerIdInFrontOfTheError(t *testing.T) {
	a := &App{store: &state.Store{Containers: []docker.Container{
		{Name: "blog-web-1", Ports: []docker.Port{{PublicPort: 8080}}},
	}}}

	modal, ok := a.explain("starting container 14dde66ee0cc: Error response from daemon: " +
		"failed to set up container networking: driver failed programming external " +
		"connectivity on endpoint essai-conflit (1316e886dad4): Bind for 0.0.0.0:8080 " +
		"failed: port is already allocated")
	if !ok {
		t.Fatal("a port clash must be explained")
	}
	if !strings.Contains(modal.Title, "8080") {
		t.Errorf("title = %q, want the real port in it", modal.Title)
	}
	if !strings.Contains(strings.Join(modal.Body, "\n"), "blog-web-1") {
		t.Error("the container holding 8080 has to be named")
	}
}
