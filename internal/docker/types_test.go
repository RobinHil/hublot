package docker

import "testing"

// Ranging over a map gives a different order on every call. The attached
// column is rebuilt on every refresh, once a second, so an unsorted list
// reorders itself under the cursor while nothing about the host has changed.
func TestNetworkAttachedIsStable(t *testing.T) {
	n := Network{Containers: map[string]string{
		"c3": "blog-web-1",
		"c1": "blog-api-1",
		"c4": "blog-worker-1",
		"c2": "blog-db-1",
	}}

	want := []string{"blog-api-1", "blog-db-1", "blog-web-1", "blog-worker-1"}

	// Many times over, because one call agreeing with one order proves nothing
	// about a map.
	for i := 0; i < 50; i++ {
		var got []string
		for _, a := range n.Attached() {
			got = append(got, a.Name)
		}
		if len(got) != len(want) {
			t.Fatalf("got %d attachments, want %d", len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("call %d: got %v, want %v", i, got, want)
			}
		}
	}
}

// Two containers can carry the same name on different networks only in odd
// cases, but the order still has to be decided by something.
func TestNetworkAttachedBreaksTiesById(t *testing.T) {
	n := Network{Containers: map[string]string{"b": "same", "a": "same"}}
	got := n.Attached()
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("got %v, want the ids to decide", got)
	}
}

// An empty network has nothing attached and must not pretend otherwise.
func TestNetworkAttachedWhenEmpty(t *testing.T) {
	if got := (Network{}).Attached(); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}
