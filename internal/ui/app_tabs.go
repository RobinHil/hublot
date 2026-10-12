package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobinHil/hublot/internal/config"
	"github.com/RobinHil/hublot/internal/docker"
	"github.com/RobinHil/hublot/internal/ui/components"
	"github.com/RobinHil/hublot/internal/ui/views"
)

// The optional tabs, by the name the configuration and the help use.
const (
	featureCompose = "compose"
	featureSwarm   = "swarm"
)

// defaultTabs is what the first-run question starts from: a tab is offered
// on when this machine already has a use for it. Compose when its plugin is
// installed, swarm when the daemon is in one.
func defaultTabs(composeInstalled bool, sw docker.SwarmState) config.Tabs {
	return config.Tabs{
		Compose: composeInstalled,
		Swarm:   sw.State != docker.SwarmInactive && sw.State != "",
	}
}

// feature names the optional tab a view is, or "" for one always shown.
func feature(v views.View) string {
	switch v.(type) {
	case *views.Compose:
		return featureCompose
	case *views.Swarm:
		return featureSwarm
	}
	return ""
}

// shown reports whether a view is among the tabs on screen.
func (a *App) shown(v views.View) bool {
	switch feature(v) {
	case featureCompose:
		return a.tabs.Compose
	case featureSwarm:
		return a.tabs.Swarm
	}
	return true
}

// hiddenFeatures are the optional tabs turned off, for the help overlay.
func (a *App) hiddenFeatures() map[string]bool {
	return map[string]bool{featureCompose: !a.tabs.Compose, featureSwarm: !a.tabs.Swarm}
}

// arrangeViews rebuilds the tab list from the catalog, keeping the cursor on
// the view it was on when that one is still there. The digits follow what is
// shown, so they are recomputed with it.
func (a *App) arrangeViews() {
	var current views.View
	if a.active < len(a.views) {
		current = a.views[a.active]
	}

	a.views = a.views[:0]
	for _, v := range a.catalog {
		if a.shown(v) {
			a.views = append(a.views, v)
		}
	}

	a.active = 0
	for i, v := range a.views {
		if v == current {
			a.active = i
		}
	}
	a.layout()
	a.refreshViews()
}

// openTabsForm asks which optional tabs to show: on the first run, and again
// whenever the settings key is pressed, filled with the current choice. The
// form names that key, which is what makes asking only once acceptable.
func (a *App) openTabsForm() {
	subtitle := "containers and the rest are always there; , reopens this"
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	f := components.NewForm("Which tabs", subtitle, []components.Field{
		{Name: featureCompose, Label: "compose", Value: onOff(a.tabs.Compose), Options: []string{"on", "off"},
			Hint: "stacks started with docker compose"},
		{Name: featureSwarm, Label: "swarm", Value: onOff(a.tabs.Swarm), Options: []string{"on", "off"},
			Hint: "services and nodes of a swarm"},
	}, func(values map[string]string) tea.Cmd { return a.applyTabs(values) })
	f.SetSize(a.width, a.height)
	a.form = &f
}

// applyTabs records the choice, shows it, and writes it down so the question
// is not asked again.
func (a *App) applyTabs(values map[string]string) tea.Cmd {
	a.tabs = config.Tabs{Compose: values[featureCompose] == "on", Swarm: values[featureSwarm] == "on"}
	chosen := a.tabs
	a.cfg.Tabs = &chosen
	a.arrangeViews()

	if err := config.Save(a.cfg); err != nil {
		a.setMessage("tabs changed, but the choice could not be saved: "+err.Error(), true)
		return nil
	}
	a.setMessage("tabs saved; , changes them", false)
	return a.enterView()
}

// sectioned is a view with sections of its own, which the arrows walk through
// as if they were tabs between this view and the next.
type sectioned interface {
	Step(delta int) bool
	EnterFrom(delta int)
}

// arrow moves one step right or left: through the active view's sections
// while it has any left that way, otherwise on to the neighbouring tab,
// entering its sections from the side the arrow came from. tab and shift+tab
// skip the sections and only ever change tab.
func (a *App) arrow(delta int) tea.Cmd {
	if s, ok := a.views[a.active].(sectioned); ok && s.Step(delta) {
		return nil
	}
	a.switchView(a.active + delta)
	if s, ok := a.views[a.active].(sectioned); ok {
		s.EnterFrom(delta)
	}
	return a.enterView()
}
