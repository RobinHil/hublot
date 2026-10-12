package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RobinHil/hublot/internal/config"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/state"
	"github.com/RobinHil/hublot/internal/ui/keys"
	"github.com/RobinHil/hublot/internal/ui/views"
)

func tabsApp(tabs config.Tabs) *App {
	deps := views.Deps{Ctx: context.Background(), Store: state.New(), Keys: keys.Default()}
	a := &App{store: deps.Store, keys: deps.Keys, tabs: tabs}
	a.catalog = []views.View{
		views.NewContainers(deps), views.NewCompose(deps), views.NewSwarm(deps),
		views.NewImages(deps), views.NewVolumes(deps), views.NewNetworks(deps), views.NewDisk(deps),
	}
	a.arrangeViews()
	return a
}

func TestTabsFollowTheChoice(t *testing.T) {
	cases := []struct {
		tabs config.Tabs
		want string
	}{
		{config.Tabs{Compose: true, Swarm: true}, "Containers Compose Swarm Images Volumes Networks Disk"},
		{config.Tabs{Compose: true}, "Containers Compose Images Volumes Networks Disk"},
		{config.Tabs{Swarm: true}, "Containers Swarm Images Volumes Networks Disk"},
		{config.Tabs{}, "Containers Images Volumes Networks Disk"},
	}
	for _, c := range cases {
		if got := strings.Join(tabsApp(c.tabs).titles(), " "); got != c.want {
			t.Errorf("%+v: tabs %q, want %q", c.tabs, got, c.want)
		}
	}
}

// Turning a tab off or on must not throw the user onto another view: the
// one on screen stays on screen, under whatever digit it now has.
func TestTabsKeepTheActiveView(t *testing.T) {
	a := tabsApp(config.Tabs{Compose: true, Swarm: true})
	a.active = 3 // Images
	a.tabs = config.Tabs{}
	a.arrangeViews()
	if a.views[a.active].Title() != "Images" || a.active != 1 {
		t.Fatalf("active is %q at %d", a.views[a.active].Title(), a.active)
	}

	// The view on screen is the one turned off: back to the first tab.
	a = tabsApp(config.Tabs{Compose: true, Swarm: true})
	a.active = 2 // Swarm
	a.tabs = config.Tabs{Compose: true}
	a.arrangeViews()
	if a.active != 0 {
		t.Fatalf("active = %d after its tab went away", a.active)
	}
}

func TestDefaultTabsFollowWhatTheMachineHas(t *testing.T) {
	if got := defaultTabs(true, docker.SwarmState{State: docker.SwarmInactive}); !got.Compose || got.Swarm {
		t.Errorf("compose installed, no swarm: %+v", got)
	}
	if got := defaultTabs(false, docker.SwarmState{State: docker.SwarmActive}); got.Compose || !got.Swarm {
		t.Errorf("no compose, in a swarm: %+v", got)
	}
	// A locked manager is still a swarm the user will want to unlock.
	if got := defaultTabs(false, docker.SwarmState{State: docker.SwarmLocked}); !got.Swarm {
		t.Errorf("locked swarm: %+v", got)
	}
}

func TestApplyTabsIsSaved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	a := tabsApp(config.Tabs{Compose: true, Swarm: true})
	a.applyTabs(map[string]string{"compose": "off", "swarm": "on"})

	if got := strings.Join(a.titles(), " "); got != "Containers Swarm Images Volumes Networks Disk" {
		t.Fatalf("tabs = %q", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "hublot", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load()
	if err != nil || saved.Tabs == nil || saved.Tabs.Compose || !saved.Tabs.Swarm {
		t.Fatalf("saved %+v, %v:\n%s", saved.Tabs, err, body)
	}
}

func TestHelpLeavesOutTabsTurnedOff(t *testing.T) {
	a := tabsApp(config.Tabs{Swarm: true})
	hidden := a.hiddenFeatures()
	if !hidden["compose"] || hidden["swarm"] {
		t.Fatalf("hidden = %v", hidden)
	}
}

// Right from Compose enters Swarm on its first section, walks its sections,
// and only then goes on to Images; left retraces the same path backwards.
func TestArrowsWalkSectionsThenTabs(t *testing.T) {
	a := tabsApp(config.Tabs{Compose: true, Swarm: true})
	a.store.Info.Swarm = docker.SwarmState{State: docker.SwarmActive, Manager: true}
	a.active = 1 // Compose

	var path []string
	for i := 0; i < 7; i++ {
		a.arrow(1)
		path = append(path, where(a))
	}
	want := "Swarm/0 Swarm/1 Swarm/2 Swarm/3 Swarm/4 Images Volumes"
	if got := strings.Join(path, " "); got != want {
		t.Fatalf("right: %s\nwant:  %s", got, want)
	}

	path = nil
	for i := 0; i < 7; i++ {
		a.arrow(-1)
		path = append(path, where(a))
	}
	want = "Images Swarm/4 Swarm/3 Swarm/2 Swarm/1 Swarm/0 Compose"
	if got := strings.Join(path, " "); got != want {
		t.Fatalf("left: %s\nwant:  %s", got, want)
	}
}

func where(a *App) string {
	v := a.views[a.active]
	if sw, ok := v.(*views.Swarm); ok {
		return fmt.Sprintf("Swarm/%d", sw.Section())
	}
	return v.Title()
}
