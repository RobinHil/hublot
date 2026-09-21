package docker

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/events"
)

// Backoff bounds for event stream reconnection.
const (
	eventBackoffMin = 500 * time.Millisecond
	eventBackoffMax = 30 * time.Second
)

// EventUpdateKind distinguishes what a message off the event stream means.
type EventUpdateKind int

const (
	// EventReceived carries a daemon event.
	EventReceived EventUpdateKind = iota
	// EventDisconnected means the stream died; the UI marks its data stale.
	EventDisconnected
	// EventReconnected means the stream is back. Events were missed during the
	// gap, so every list must be resynced (AGENTS.md section 6.5).
	EventReconnected
)

// EventUpdate is one message off the event stream.
type EventUpdate struct {
	Kind  EventUpdateKind
	Event Event
	Err   error
}

// StreamEvents follows the daemon event stream until ctx is cancelled,
// reconnecting with exponential backoff when the daemon goes away. The channel
// is closed only when ctx is done.
func (c *Client) StreamEvents(ctx context.Context) <-chan EventUpdate {
	out := make(chan EventUpdate, 64)

	go func() {
		defer close(out)

		backoff := eventBackoffMin
		connected := true

		for ctx.Err() == nil {
			msgs, errs := c.api.Events(ctx, events.ListOptions{})
			alive := true

			for alive {
				select {
				case <-ctx.Done():
					return

				case msg, ok := <-msgs:
					if !ok {
						alive = false
						break
					}
					// A successful read means the daemon is healthy again.
					backoff = eventBackoffMin
					if !connected {
						connected = true
						send(ctx, out, EventUpdate{Kind: EventReconnected})
					}
					send(ctx, out, EventUpdate{Kind: EventReceived, Event: toEvent(msg)})

				case err, ok := <-errs:
					if !ok {
						alive = false
						break
					}
					if ctx.Err() != nil {
						return
					}
					if connected {
						connected = false
						send(ctx, out, EventUpdate{Kind: EventDisconnected, Err: err})
					}
					alive = false
				}
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > eventBackoffMax {
				backoff = eventBackoffMax
			}
		}
	}()

	return out
}

// send publishes an update unless the caller has gone away.
func send(ctx context.Context, ch chan<- EventUpdate, u EventUpdate) {
	select {
	case ch <- u:
	case <-ctx.Done():
	}
}

func toEvent(m events.Message) Event {
	e := Event{
		Type:       string(m.Type),
		Action:     string(m.Action),
		ActorID:    m.Actor.ID,
		Attributes: m.Actor.Attributes,
		At:         time.Unix(0, m.TimeNano),
	}
	if m.Actor.Attributes != nil {
		e.ActorName = m.Actor.Attributes["name"]
	}
	return e
}

// AffectsContainers reports whether an event should trigger a container list
// resync. Attribute-only events such as `exec_start` do not.
func (e Event) AffectsContainers() bool {
	if e.Type != "container" {
		return false
	}
	switch e.Action {
	case "create", "destroy", "die", "kill", "pause", "unpause", "rename",
		"restart", "start", "stop", "update", "health_status", "oom":
		return true
	}
	return false
}

// AffectsImages reports whether the image list must be resynced.
func (e Event) AffectsImages() bool {
	if e.Type != "image" {
		return false
	}
	switch e.Action {
	case "delete", "import", "load", "pull", "push", "tag", "untag", "save":
		return true
	}
	return false
}

// AffectsVolumes reports whether the volume list must be resynced.
func (e Event) AffectsVolumes() bool {
	return e.Type == "volume" && (e.Action == "create" || e.Action == "destroy")
}

// AffectsNetworks reports whether the network list must be resynced.
func (e Event) AffectsNetworks() bool {
	if e.Type != "network" {
		return false
	}
	switch e.Action {
	case "create", "destroy", "connect", "disconnect", "remove":
		return true
	}
	return false
}

// StartsContainer reports whether the event means a container began running, so
// a stats stream must be opened for it (AGENTS.md section 6.3).
func (e Event) StartsContainer() bool {
	return e.Type == "container" && (e.Action == "start" || e.Action == "unpause" || e.Action == "restart")
}

// StopsContainer reports whether the event means a container stopped running,
// so its stats goroutine must be cancelled.
func (e Event) StopsContainer() bool {
	if e.Type != "container" {
		return false
	}
	switch e.Action {
	case "die", "stop", "kill", "pause", "destroy":
		return true
	}
	return false
}
