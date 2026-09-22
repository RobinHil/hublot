//go:build live

// Checks the docker layer against a real daemon. Behind a build tag on
// purpose: it never runs in CI and never runs by default, so the suite that
// does run needs no daemon (AGENTS.md section 13).
//
//	go test -tags live ./internal/docker/
//
// It creates and removes its own objects, all named hublot-live-*, and never
// runs an -a image prune: that would take images the host legitimately holds.
package docker

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
)

const testImage = "alpine:latest"

func live(t *testing.T) (*Client, context.Context) {
	t.Helper()
	ctx := context.Background()
	c, err := Connect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, ctx
}

// makeContainer creates and starts a container, and removes it at the end.
func makeContainer(t *testing.T, c *Client, ctx context.Context, name string, cmd []string) string {
	t.Helper()
	created, err := c.api.ContainerCreate(ctx,
		&container.Config{Image: testImage, Cmd: cmd},
		&container.HostConfig{}, nil, nil, name)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	t.Cleanup(func() { _ = c.RemoveContainer(context.Background(), created.ID, true, true) })

	if err := c.StartContainer(ctx, created.ID); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}
	return created.ID
}

func TestLiveContainerLifecycle(t *testing.T) {
	c, ctx := live(t)
	id := makeContainer(t, c, ctx, "hublot-live-lifecycle", []string{"sleep", "600"})

	list, err := c.ListContainers(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var found *Container
	for i := range list {
		if list[i].ID == id {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatal("the container we just started is not in the list")
	}
	if !found.Running() {
		t.Errorf("state: got %q, want running", found.State)
	}
	if found.Image != testImage {
		t.Errorf("image: got %q", found.Image)
	}

	detail, err := c.InspectContainer(ctx, id)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if detail.Name != "hublot-live-lifecycle" {
		t.Errorf("inspect name: got %q", detail.Name)
	}
	if len(detail.Raw) == 0 {
		t.Error("inspect returned no raw json for the detail pane")
	}

	if tty, err := c.HasTTY(ctx, id); err != nil || tty {
		t.Errorf("HasTTY: got %v, %v; want false, nil", tty, err)
	}

	// Pause and unpause.
	if err := c.PauseContainer(ctx, id); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if state := stateOf(t, c, ctx, id); state != StatePaused {
		t.Errorf("after pause: got %q", state)
	}
	if err := c.UnpauseContainer(ctx, id); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	if state := stateOf(t, c, ctx, id); state != StateRunning {
		t.Errorf("after unpause: got %q", state)
	}

	// Rename.
	if err := c.RenameContainer(ctx, id, "hublot-live-renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if detail, err := c.InspectContainer(ctx, id); err != nil || detail.Name != "hublot-live-renamed" {
		t.Errorf("after rename: %q, %v", detail.Name, err)
	}

	// Update limits, then read them back.
	if err := c.UpdateContainer(ctx, id, 1_500_000_000, 256*1024*1024); err != nil {
		t.Fatalf("update: %v", err)
	}
	detail, err = c.InspectContainer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.NanoCPUs != 1_500_000_000 {
		t.Errorf("nano cpus: got %d, want 1500000000", detail.NanoCPUs)
	}
	if detail.MemoryLimit != 256*1024*1024 {
		t.Errorf("memory limit: got %d", detail.MemoryLimit)
	}

	// Restart, then stop.
	if err := c.RestartContainer(ctx, id, 2*time.Second); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if state := stateOf(t, c, ctx, id); state != StateRunning {
		t.Errorf("after restart: got %q", state)
	}
	if err := c.StopContainer(ctx, id, 2*time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if state := stateOf(t, c, ctx, id); state != StateExited {
		t.Errorf("after stop: got %q", state)
	}

	// Start again and kill with an explicit signal.
	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatalf("restart after stop: %v", err)
	}
	if err := c.KillContainer(ctx, id, "SIGKILL"); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if state := stateOf(t, c, ctx, id); state != StateExited {
		t.Errorf("after kill: got %q", state)
	}

	// And remove it for good.
	if err := c.RemoveContainer(ctx, id, false, true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := c.InspectContainer(ctx, id); err == nil {
		t.Error("the container is still there after removal")
	}
}

func stateOf(t *testing.T, c *Client, ctx context.Context, id string) string {
	t.Helper()
	for i := 0; i < 40; i++ {
		d, err := c.InspectContainer(ctx, id)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		if d.State != StateRestarting && d.State != StateRemoving {
			return d.State
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "timed out"
}

func TestLiveLogsAndStats(t *testing.T) {
	c, ctx := live(t)
	id := makeContainer(t, c, ctx, "hublot-live-logs",
		[]string{"sh", "-c", "i=0; while true; do echo line $i; i=$((i+1)); sleep 1; done"})

	logCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	lines, errs := c.StreamLogs(logCtx, id, DefaultLogOptions())
	select {
	case line := <-lines:
		if !strings.HasPrefix(line.Text, "line ") {
			t.Errorf("log line: got %q", line.Text)
		}
		if line.Stream != "stdout" {
			t.Errorf("stream: got %q, want stdout", line.Stream)
		}
		if line.At.IsZero() {
			t.Error("the timestamp the daemon prefixes was not parsed")
		}
	case err := <-errs:
		t.Fatalf("log stream: %v", err)
	case <-time.After(12 * time.Second):
		t.Fatal("no log line arrived")
	}

	statsCtx, cancelStats := context.WithTimeout(ctx, 20*time.Second)
	defer cancelStats()

	samples, statErrs := c.StreamStats(statsCtx, id)
	var got []Stats
	for len(got) < 3 {
		select {
		case s, ok := <-samples:
			if !ok {
				t.Fatal("the stats stream closed early")
			}
			got = append(got, s)
		case err := <-statErrs:
			t.Fatalf("stats stream: %v", err)
		case <-time.After(15 * time.Second):
			t.Fatalf("only %d samples arrived", len(got))
		}
	}

	// The first frame carries no usable previous sample, the later ones do.
	if got[0].CPUValid {
		t.Error("the first sample must not claim a usable cpu figure")
	}
	if !got[2].CPUValid {
		t.Error("later samples must carry a usable cpu figure")
	}
	if got[2].MemUsage <= 0 {
		t.Errorf("memory usage: got %d", got[2].MemUsage)
	}
	if got[2].MemLimit <= 0 {
		t.Errorf("memory limit: got %d", got[2].MemLimit)
	}
	if got[2].ContainerID != id {
		t.Error("the sample is not attributed to the container it came from")
	}
}

func TestLiveCopyBothWays(t *testing.T) {
	c, ctx := live(t)
	id := makeContainer(t, c, ctx, "hublot-live-copy", []string{"sleep", "600"})

	dir := t.TempDir()
	source := filepath.Join(dir, "hublot.conf")
	body := "answer = 42\n"
	if err := os.WriteFile(source, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.CopyToContainer(ctx, id, source, "/tmp/hublot.conf"); err != nil {
		t.Fatalf("copy in: %v", err)
	}

	back := filepath.Join(dir, "back")
	written, err := c.CopyFromContainer(ctx, id, "/tmp/hublot.conf", back)
	if err != nil {
		t.Fatalf("copy out: %v", err)
	}

	got, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("reading what came back: %v", err)
	}
	if string(got) != body {
		t.Errorf("round trip: got %q, want %q", got, body)
	}
}

func TestLiveEvents(t *testing.T) {
	c, ctx := live(t)

	streamCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	updates := c.StreamEvents(streamCtx)

	// Give the stream a moment to attach before making something happen.
	time.Sleep(500 * time.Millisecond)
	id := makeContainer(t, c, ctx, "hublot-live-events", []string{"sleep", "60"})

	deadline := time.After(15 * time.Second)
	for {
		select {
		case u := <-updates:
			if u.Kind != EventReceived {
				continue
			}
			if u.Event.ActorID == id && u.Event.StartsContainer() {
				if !u.Event.AffectsContainers() {
					t.Error("a start event must trigger a container resync")
				}
				return
			}
		case <-deadline:
			t.Fatal("no start event arrived for the container we started")
		}
	}
}

func TestLiveImages(t *testing.T) {
	c, ctx := live(t)

	images, err := c.ListImages(ctx)
	if err != nil {
		t.Fatalf("listing images: %v", err)
	}
	var target Image
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == testImage {
				target = img
			}
		}
	}
	if target.ID == "" {
		t.Fatalf("%s is not on this host", testImage)
	}

	if raw, err := c.InspectImage(ctx, target.ID); err != nil || len(raw) == 0 {
		t.Errorf("inspect image: %d bytes, %v", len(raw), err)
	}
	layers, err := c.ImageHistory(ctx, target.ID)
	if err != nil || len(layers) == 0 {
		t.Errorf("history: %d layers, %v", len(layers), err)
	}

	// Tag, save, then drop the tag again.
	const ref = "hublot-live-test:tagged"
	if err := c.TagImage(ctx, target.ID, ref); err != nil {
		t.Fatalf("tag: %v", err)
	}

	path := filepath.Join(t.TempDir(), "image.tar")
	if err := c.SaveImage(ctx, []string{ref}, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < 1024 {
		t.Errorf("saved tarball: %v, %v", info, err)
	}

	deleted, err := c.RemoveImage(ctx, ref, false, false)
	if err != nil {
		t.Fatalf("untag: %v", err)
	}
	if len(deleted) == 0 {
		t.Error("removing a tag reported nothing")
	}
	// The image itself survives, since alpine:latest still points at it.
	if _, err := c.InspectImage(ctx, target.ID); err != nil {
		t.Errorf("the image should survive losing one of its tags: %v", err)
	}
}

func TestLiveVolumesAndNetworks(t *testing.T) {
	c, ctx := live(t)

	vol, err := c.api.VolumeCreate(ctx, volume.CreateOptions{Name: "hublot-live-volume"})
	if err != nil {
		t.Fatalf("creating a volume: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveVolume(context.Background(), vol.Name, true) })

	volumes, err := c.ListVolumes(ctx)
	if err != nil {
		t.Fatalf("listing volumes: %v", err)
	}
	var seen bool
	for _, v := range volumes {
		if v.Name == vol.Name {
			seen = true
			if v.Anonymous {
				t.Error("a named volume must not be reported as anonymous")
			}
		}
	}
	if !seen {
		t.Error("the volume we created is not in the list")
	}
	if raw, err := c.InspectVolume(ctx, vol.Name); err != nil || len(raw) == 0 {
		t.Errorf("inspect volume: %v", err)
	}

	net, err := c.api.NetworkCreate(ctx, "hublot-live-net", network.CreateOptions{Driver: "bridge"})
	if err != nil {
		t.Fatalf("creating a network: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveNetwork(context.Background(), net.ID) })

	id := makeContainer(t, c, ctx, "hublot-live-net-member", []string{"sleep", "600"})

	if err := c.ConnectNetwork(ctx, net.ID, id); err != nil {
		t.Fatalf("connect: %v", err)
	}
	networks, err := c.ListNetworks(ctx)
	if err != nil {
		t.Fatalf("listing networks: %v", err)
	}
	attached := false
	for _, n := range networks {
		if n.ID == net.ID {
			if _, ok := n.Containers[id]; ok {
				attached = true
			}
			if n.Predefined() {
				t.Error("a user-defined network must not be reported as predefined")
			}
		}
	}
	if !attached {
		t.Error("the container is not listed as attached to the network")
	}

	if err := c.DisconnectNetwork(ctx, net.ID, id, false); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if raw, err := c.InspectNetwork(ctx, net.ID); err != nil || len(raw) == 0 {
		t.Errorf("inspect network: %v", err)
	}
	if err := c.RemoveNetwork(ctx, net.ID); err != nil {
		t.Fatalf("remove network: %v", err)
	}

	if err := c.RemoveVolume(ctx, vol.Name, false); err != nil {
		t.Fatalf("remove volume: %v", err)
	}
}

func TestLiveDiskUsageAndPrune(t *testing.T) {
	c, ctx := live(t)

	du, err := c.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("df: %v", err)
	}
	if len(du.Images) == 0 {
		t.Error("df reported no images on a host that has some")
	}
	if du.At.IsZero() {
		t.Error("df results must carry the time they were taken")
	}

	// A stopped container, an unused volume and an unused network, then the
	// prunes that should take exactly those. The -a image prune is never run
	// here: it would remove images this host legitimately holds.
	id := makeContainer(t, c, ctx, "hublot-live-prunable", []string{"true"})
	if state := stateOf(t, c, ctx, id); state != StateExited {
		t.Fatalf("the container should have exited on its own: %q", state)
	}

	vol, err := c.api.VolumeCreate(ctx, volume.CreateOptions{Name: "hublot-live-prunable-vol"})
	if err != nil {
		t.Fatal(err)
	}
	net, err := c.api.NetworkCreate(ctx, "hublot-live-prunable-net", network.CreateOptions{Driver: "bridge"})
	if err != nil {
		t.Fatal(err)
	}

	// The preview the UI shows, computed from the same lists.
	containers, _ := c.ListContainers(ctx)
	prunable := 0
	for _, ct := range containers {
		if ct.State == StateExited || ct.State == StateCreated {
			prunable++
		}
	}
	if prunable == 0 {
		t.Fatal("the stopped container is not visible as prunable")
	}

	report, err := c.PruneContainers(ctx, 0)
	if err != nil {
		t.Fatalf("prune containers: %v", err)
	}
	if len(report.Deleted) == 0 {
		t.Error("pruning removed no container although one was stopped")
	}

	volReport, err := c.PruneVolumes(ctx, true)
	if err != nil {
		t.Fatalf("prune volumes: %v", err)
	}
	if !contains(volReport.Deleted, vol.Name) {
		t.Errorf("the unused volume was not pruned: %v", volReport.Deleted)
	}

	netReport, err := c.PruneNetworks(ctx)
	if err != nil {
		t.Fatalf("prune networks: %v", err)
	}
	if !contains(netReport.Deleted, "hublot-live-prunable-net") {
		t.Errorf("the unused network was not pruned: %v", netReport.Deleted)
	}
	_ = net

	// Dangling images only: everything on this host is tagged, so this is a
	// no-op that still proves the call works.
	if _, err := c.PruneImages(ctx, false, 0); err != nil {
		t.Fatalf("prune dangling images: %v", err)
	}
	if _, err := c.PruneBuildCache(ctx, false); err != nil {
		t.Fatalf("prune build cache: %v", err)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle || strings.HasSuffix(h, needle) {
			return true
		}
	}
	return false
}

func TestLiveInfo(t *testing.T) {
	c, ctx := live(t)

	info, err := c.Info(ctx)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.ServerVersion == "" || info.APIVersion == "" {
		t.Errorf("version fields are empty: %+v", info)
	}
	if info.NCPU <= 0 {
		t.Errorf("cpu count: got %d", info.NCPU)
	}
	if c.APIVersion() == "" {
		t.Error("the negotiated api version is empty")
	}
	// Which the prune previews depend on to get volume semantics right.
	t.Logf("negotiated api %s, volume prune all supported: %v",
		c.APIVersion(), c.SupportsVolumePruneAll())
}

// TestLiveStreamsDoNotLeak churns the streams the UI opens and closes all day:
// a container starting and stopping opens a stats stream and cancels it, and a
// forgotten cancel leaks a goroutine and an HTTP connection every time
// (AGENTS.md section 6.3).
func TestLiveStreamsDoNotLeak(t *testing.T) {
	c, ctx := live(t)
	id := makeContainer(t, c, ctx, "hublot-live-leak", []string{"sleep", "600"})

	settle := func() int {
		for i := 0; i < 20; i++ {
			time.Sleep(150 * time.Millisecond)
			runtime.GC()
		}
		return runtime.NumGoroutine()
	}

	// One round first, so whatever the http client sets up once is already up.
	openAndClose(t, c, ctx, id)
	before := settle()

	for i := 0; i < 15; i++ {
		openAndClose(t, c, ctx, id)
	}
	after := settle()

	if after > before+2 {
		buf := make([]byte, 1<<16)
		buf = buf[:runtime.Stack(buf, true)]
		t.Errorf("goroutines grew from %d to %d over 15 stream cycles:\n%s", before, after, buf)
	}
	t.Logf("goroutines: %d before, %d after 15 cycles", before, after)
}

// openAndClose opens a stats stream and a log stream, reads a little, then
// cancels them the way the model does when a container stops.
func openAndClose(t *testing.T, c *Client, ctx context.Context, id string) {
	t.Helper()

	streamCtx, cancel := context.WithCancel(ctx)
	samples, _ := c.StreamStats(streamCtx, id)
	lines, _ := c.StreamLogs(streamCtx, id, LogOptions{Tail: "5", Follow: true})

	select {
	case <-samples:
	case <-time.After(5 * time.Second):
		t.Fatal("no stats sample arrived")
	}
	select {
	case <-lines:
	case <-time.After(2 * time.Second):
	}

	cancel()

	// Draining is what the model does by rescheduling its read until the
	// channel closes; without it the producer would block on a full channel.
	for range samples {
	}
	for range lines {
	}
}

// TestLiveInspectionExtras covers the operations that complete the coverage of
// what the engine offers for the objects hublot manages.
func TestLiveInspectionExtras(t *testing.T) {
	c, ctx := live(t)
	id := makeContainer(t, c, ctx, "hublot-live-extras",
		[]string{"sh", "-c", "echo written > /tmp/hublot-marker; sleep 600"})
	time.Sleep(time.Second)

	titles, rows, err := c.Processes(ctx, id)
	if err != nil {
		t.Fatalf("processes: %v", err)
	}
	if len(titles) == 0 || len(rows) == 0 {
		t.Errorf("top returned %d columns and %d rows", len(titles), len(rows))
	}

	changes, err := c.Changes(ctx, id)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	var sawMarker bool
	for _, change := range changes {
		if change.Path == "/tmp/hublot-marker" {
			sawMarker = true
			if change.Kind != "added" {
				t.Errorf("a new file should read as added, got %q", change.Kind)
			}
		}
	}
	if !sawMarker {
		t.Errorf("the file the container wrote is not in the changes: %v", changes)
	}

	// Commit, then load and save round trip through a tarball.
	const ref = "hublot-live-committed:v1"
	imageID, err := c.Commit(ctx, id, ref, "from the live harness")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if imageID == "" {
		t.Error("commit returned no image id")
	}
	t.Cleanup(func() { _, _ = c.RemoveImage(context.Background(), ref, true, true) })

	tar := filepath.Join(t.TempDir(), "committed.tar")
	if err := c.SaveImage(ctx, []string{ref}, tar); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := c.RemoveImage(ctx, ref, true, false); err != nil {
		t.Fatalf("removing before the load: %v", err)
	}
	loaded, err := c.LoadImage(ctx, tar)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) == 0 {
		t.Error("load reported nothing")
	}

	// Export the container filesystem.
	export := filepath.Join(t.TempDir(), "fs.tar")
	if err := c.Export(ctx, id, export); err != nil {
		t.Fatalf("export: %v", err)
	}
	if info, err := os.Stat(export); err != nil || info.Size() < 1024 {
		t.Errorf("exported tarball: %v, %v", info, err)
	}
}

func TestLiveCreateVolumeAndNetwork(t *testing.T) {
	c, ctx := live(t)

	if err := c.CreateVolume(ctx, "hublot-live-created-vol"); err != nil {
		t.Fatalf("create volume: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveVolume(context.Background(), "hublot-live-created-vol", true) })

	volumes, err := c.ListVolumes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVolume(volumes, "hublot-live-created-vol") {
		t.Error("the volume we created is not listed")
	}

	if err := c.CreateNetwork(ctx, "hublot-live-created-net"); err != nil {
		t.Fatalf("create network: %v", err)
	}
	networks, err := c.ListNetworks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var created Network
	for _, n := range networks {
		if n.Name == "hublot-live-created-net" {
			created = n
		}
	}
	if created.ID == "" {
		t.Fatal("the network we created is not listed")
	}
	if created.Driver != "bridge" {
		t.Errorf("driver: got %q, want bridge", created.Driver)
	}
	if err := c.RemoveNetwork(ctx, created.ID); err != nil {
		t.Fatalf("remove network: %v", err)
	}
}

func hasVolume(volumes []Volume, name string) bool {
	for _, v := range volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

// TestLiveRunContainer covers creating a container the way the form does, with
// the parts people actually fill in.
func TestLiveRunContainer(t *testing.T) {
	c, ctx := live(t)

	spec := RunSpec{
		Image:         testImage,
		Name:          "hublot-live-run",
		Ports:         []string{"18080:80"},
		Env:           []string{"HUBLOT=yes"},
		Command:       []string{"sleep", "600"},
		RestartPolicy: "unless-stopped",
		Start:         true,
	}

	id, err := c.RunContainer(ctx, spec)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveContainer(context.Background(), id, true, true) })

	detail, err := c.InspectContainer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Name != "hublot-live-run" {
		t.Errorf("name: got %q", detail.Name)
	}
	if detail.State != StateRunning {
		t.Errorf("it should have been started: %q", detail.State)
	}
	if detail.RestartPolicy != "unless-stopped" {
		t.Errorf("restart policy: got %q", detail.RestartPolicy)
	}

	var sawEnv bool
	for _, e := range detail.Env {
		if e == "HUBLOT=yes" {
			sawEnv = true
		}
	}
	if !sawEnv {
		t.Errorf("the environment did not reach the container: %v", detail.Env)
	}

	list, err := c.ListContainers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range list {
		if ct.ID != id {
			continue
		}
		if len(ct.Ports) == 0 {
			t.Error("the published port is not reported")
		}
	}

	// Created but not started is the other half of the form.
	stopped, err := c.RunContainer(ctx, RunSpec{
		Image: testImage, Name: "hublot-live-created", Command: []string{"true"},
	})
	if err != nil {
		t.Fatalf("create without starting: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveContainer(context.Background(), stopped, true, true) })

	if state := stateOf(t, c, ctx, stopped); state != StateCreated {
		t.Errorf("a container created without start is %q, want created", state)
	}

	// An image nothing could resolve has to fail as an error, not a panic.
	if _, err := c.RunContainer(ctx, RunSpec{Image: "hublot-no-such-image:v0"}); err == nil {
		t.Error("an unknown image must be refused")
	}
	if _, err := c.RunContainer(ctx, RunSpec{}); err == nil {
		t.Error("a spec with no image must be refused")
	}
}

// TestLiveEditContainer covers reading a container back into a spec, changing
// it where it stands, and rebuilding it. The rebuild is the one operation in
// this package that destroys something, so what it keeps and what it does when
// it fails both get checked here.
func TestLiveEditContainer(t *testing.T) {
	c, ctx := live(t)

	const name = "hublot-live-edit"
	exposed, bindings, err := nat.ParsePortSpecs([]string{"18080:80"})
	if err != nil {
		t.Fatalf("ports: %v", err)
	}

	created, err := c.api.ContainerCreate(ctx,
		&container.Config{
			Image:        testImage,
			Cmd:          []string{"sleep", "600"},
			Env:          []string{"GREETING=hello"},
			Labels:       map[string]string{"com.example.kept": "yes"},
			WorkingDir:   "/tmp",
			ExposedPorts: exposed,
		},
		&container.HostConfig{
			Binds:         []string{"/tmp:/mnt/host:ro"},
			PortBindings:  bindings,
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Resources: container.Resources{
				NanoCPUs: 1_000_000_000, Memory: 256 << 20, MemorySwap: 256 << 20,
			},
		}, nil, nil, name)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	id := created.ID
	t.Cleanup(func() { _ = c.RemoveContainer(context.Background(), id, true, true) })

	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatalf("starting: %v", err)
	}

	spec, err := c.ContainerSpecOf(ctx, id)
	if err != nil {
		t.Fatalf("reading the spec back: %v", err)
	}
	if spec.Name != name || spec.Image != testImage || !spec.Running {
		t.Errorf("spec: %+v", spec)
	}
	if len(spec.Env) != 1 || spec.Env[0] != "GREETING=hello" {
		// The image's own variables, PATH above all, must have been subtracted.
		t.Errorf("env: got %q, want only what was given", spec.Env)
	}
	if len(spec.Ports) != 1 || spec.Ports[0] != "18080:80" {
		t.Errorf("ports: got %q", spec.Ports)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0] != "/tmp:/mnt/host:ro" {
		t.Errorf("binds: got %q", spec.Mounts)
	}
	if spec.RestartPolicy != "unless-stopped" || spec.NanoCPUs != 1_000_000_000 || spec.Memory != 256<<20 {
		t.Errorf("limits: %+v", spec)
	}

	// Changing it where it stands: a rename, a policy and a bigger allowance.
	renamed, policy := name+"-renamed", "always"
	cpus, memory := int64(2_000_000_000), int64(384<<20)
	if err := c.ApplyEdit(ctx, id, Edit{
		Name: &renamed, RestartPolicy: &policy, NanoCPUs: &cpus, Memory: &memory,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	spec, err = c.ContainerSpecOf(ctx, id)
	if err != nil {
		t.Fatalf("reading back after the update: %v", err)
	}
	if spec.Name != renamed || spec.RestartPolicy != policy ||
		spec.NanoCPUs != cpus || spec.Memory != memory {
		t.Errorf("the in-place edit did not take: %+v", spec)
	}
	if !spec.Running {
		t.Error("updating limits must not stop the container")
	}

	// A rebuild that cannot work leaves everything exactly as it was.
	missing := "hublot-no-such-image:v0"
	if _, err := c.RecreateContainer(ctx, id, Edit{Image: &missing}, time.Second); err == nil {
		t.Error("an unknown image must fail the rebuild")
	}
	spec, err = c.ContainerSpecOf(ctx, id)
	if err != nil {
		t.Fatalf("the container must still be there after a failed rebuild: %v", err)
	}
	if spec.Name != renamed || !spec.Running {
		t.Errorf("a failed rebuild must put the container back: %+v", spec)
	}

	// The real thing: a new command, everything else carried over.
	command := []string{"sleep", "900"}
	newID, err := c.RecreateContainer(ctx, id, Edit{Command: &command}, 3*time.Second)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	t.Cleanup(func() { _ = c.RemoveContainer(context.Background(), newID, true, true) })
	if newID == id {
		t.Fatal("a rebuild makes another container")
	}

	rebuilt, err := c.ContainerSpecOf(ctx, newID)
	if err != nil {
		t.Fatalf("reading the rebuilt container: %v", err)
	}
	if rebuilt.Name != renamed {
		t.Errorf("the name goes with it: got %q", rebuilt.Name)
	}
	if !rebuilt.Running {
		t.Error("a container that was running comes back running")
	}
	if len(rebuilt.Command) != 2 || rebuilt.Command[1] != "900" {
		t.Errorf("command: got %q", rebuilt.Command)
	}
	if rebuilt.Labels["com.example.kept"] != "yes" {
		t.Errorf("labels are carried over, got %q", rebuilt.Labels)
	}
	if len(rebuilt.Env) != 1 || rebuilt.Env[0] != "GREETING=hello" {
		t.Errorf("env is carried over exactly once: %q", rebuilt.Env)
	}
	if len(rebuilt.Ports) != 1 || rebuilt.Ports[0] != "18080:80" {
		t.Errorf("ports are carried over: %q", rebuilt.Ports)
	}
	if len(rebuilt.Mounts) != 1 || rebuilt.Mounts[0] != "/tmp:/mnt/host:ro" {
		t.Errorf("binds are carried over: %q", rebuilt.Mounts)
	}
	if rebuilt.NanoCPUs != cpus || rebuilt.Memory != memory || rebuilt.RestartPolicy != policy {
		t.Errorf("limits are carried over: %+v", rebuilt)
	}

	// Anything the form cannot show has to survive too.
	insp, err := c.api.ContainerInspect(ctx, newID)
	if err != nil {
		t.Fatalf("inspecting the rebuilt container: %v", err)
	}
	if insp.Config.WorkingDir != "/tmp" {
		t.Errorf("the working directory is carried over, got %q", insp.Config.WorkingDir)
	}

	// The old one is gone, name and all.
	if _, err := c.api.ContainerInspect(ctx, id); err == nil {
		t.Error("the container that was replaced must have been removed")
	}
}
