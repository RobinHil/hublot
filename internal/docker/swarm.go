package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// toSwarmState narrows what `docker info` says about the swarm. Every node
// answers this, which is how a worker learns it is one before it is refused
// anything else.
func toSwarmState(s swarm.Info) SwarmState {
	out := SwarmState{
		State:    string(s.LocalNodeState),
		Manager:  s.ControlAvailable,
		NodeID:   s.NodeID,
		NodeAddr: s.NodeAddr,
		Error:    s.Error,
		Nodes:    s.Nodes,
		Managers: s.Managers,
	}
	if out.State == "" {
		out.State = SwarmInactive
	}
	for _, p := range s.RemoteManagers {
		out.RemoteManagers = append(out.RemoteManagers, p.Addr)
	}
	if s.Cluster != nil {
		out.ClusterID = s.Cluster.ID
	}
	return out
}

// InspectCluster reads the swarm's settings and join tokens. Managers only.
func (c *Client) InspectCluster(ctx context.Context) (Cluster, error) {
	sw, err := c.api.SwarmInspect(ctx)
	if err != nil {
		return Cluster{}, fmt.Errorf("inspecting the swarm: %w", err)
	}
	return toCluster(sw), nil
}

func toCluster(sw swarm.Swarm) Cluster {
	spec := sw.Spec
	out := Cluster{
		ID:               sw.ID,
		Version:          sw.Version.Index,
		CreatedAt:        sw.CreatedAt,
		UpdatedAt:        sw.UpdatedAt,
		WorkerToken:      sw.JoinTokens.Worker,
		ManagerToken:     sw.JoinTokens.Manager,
		Autolock:         spec.EncryptionConfig.AutoLockManagers,
		CertExpiry:       spec.CAConfig.NodeCertExpiry,
		Heartbeat:        spec.Dispatcher.HeartbeatPeriod,
		SnapshotInterval: spec.Raft.SnapshotInterval,
		ElectionTick:     spec.Raft.ElectionTick,
		HeartbeatTick:    spec.Raft.HeartbeatTick,
		RootRotation:     sw.RootRotationInProgress,
		DefaultAddrPool:  sw.DefaultAddrPool,
		SubnetSize:       sw.SubnetSize,
		DataPathPort:     sw.DataPathPort,
	}
	if spec.Orchestration.TaskHistoryRetentionLimit != nil {
		out.TaskHistory = *spec.Orchestration.TaskHistoryRetentionLimit
	}
	if spec.Raft.KeepOldSnapshots != nil {
		out.KeepOldSnapshots = *spec.Raft.KeepOldSnapshots
	}
	for _, ca := range spec.CAConfig.ExternalCAs {
		if ca != nil {
			out.ExternalCAs = append(out.ExternalCAs, string(ca.Protocol)+" "+ca.URL)
		}
	}
	return out
}

// InspectClusterRaw is the swarm inspect output for the detail panel, with the
// join tokens left out: they are shown on purpose, by a key that says it
// reveals them, not as a side effect of reading the settings.
func (c *Client) InspectClusterRaw(ctx context.Context) ([]byte, error) {
	sw, err := c.api.SwarmInspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspecting the swarm: %w", err)
	}
	sw.JoinTokens = swarm.JoinTokens{}
	return marshalIndent(sw)
}

// InitSwarm makes this daemon the first manager of a new swarm.
func (c *Client) InitSwarm(ctx context.Context, spec InitSpec) (string, error) {
	req := swarm.InitRequest{
		ListenAddr:       orDefault(spec.ListenAddr, "0.0.0.0:2377"),
		AdvertiseAddr:    spec.AdvertiseAddr,
		DataPathAddr:     spec.DataPathAddr,
		ForceNewCluster:  spec.ForceNewCluster,
		AutoLockManagers: spec.Autolock,
		DefaultAddrPool:  spec.DefaultAddrPool,
	}
	id, err := c.api.SwarmInit(ctx, req)
	if err != nil {
		return "", fmt.Errorf("initialising the swarm: %w", err)
	}
	return id, nil
}

// JoinSwarm adds this daemon to an existing swarm. The token decides whether
// it joins as a worker or as a manager.
func (c *Client) JoinSwarm(ctx context.Context, spec JoinSpec) error {
	req := swarm.JoinRequest{
		ListenAddr:    orDefault(spec.ListenAddr, "0.0.0.0:2377"),
		AdvertiseAddr: spec.AdvertiseAddr,
		DataPathAddr:  spec.DataPathAddr,
		RemoteAddrs:   []string{spec.RemoteAddr},
		JoinToken:     spec.Token,
	}
	if err := c.api.SwarmJoin(ctx, req); err != nil {
		return fmt.Errorf("joining the swarm at %s: %w", spec.RemoteAddr, err)
	}
	return nil
}

// LeaveSwarm takes this daemon out of the swarm. A manager needs force, and
// leaving with it takes the node's share of the quorum along.
func (c *Client) LeaveSwarm(ctx context.Context, force bool) error {
	if err := c.api.SwarmLeave(ctx, force); err != nil {
		return fmt.Errorf("leaving the swarm: %w", err)
	}
	return nil
}

// UnlockSwarm hands a locked manager the key it needs to decrypt its raft log
// after a restart.
func (c *Client) UnlockSwarm(ctx context.Context, unlockKey string) error {
	if err := c.api.SwarmUnlock(ctx, swarm.UnlockRequest{UnlockKey: strings.TrimSpace(unlockKey)}); err != nil {
		return fmt.Errorf("unlocking the swarm: %w", err)
	}
	return nil
}

// UnlockKey reads the current key of an autolocked swarm.
func (c *Client) UnlockKey(ctx context.Context) (string, error) {
	resp, err := c.api.SwarmGetUnlockKey(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the unlock key: %w", err)
	}
	return resp.UnlockKey, nil
}

// UpdateCluster applies a settings change against the version just read, so
// a change made meanwhile by another manager is refused rather than undone.
func (c *Client) UpdateCluster(ctx context.Context, change ClusterChange) error {
	sw, err := c.api.SwarmInspect(ctx)
	if err != nil {
		return fmt.Errorf("inspecting the swarm: %w", err)
	}

	spec := sw.Spec
	applyClusterChange(&spec, change)

	flags := swarm.UpdateFlags{
		RotateWorkerToken:      change.RotateWorkerToken,
		RotateManagerToken:     change.RotateManagerToken,
		RotateManagerUnlockKey: change.RotateUnlockKey,
	}
	if err := c.api.SwarmUpdate(ctx, sw.Version, spec, flags); err != nil {
		return fmt.Errorf("updating the swarm: %w", err)
	}
	return nil
}

// applyClusterChange writes the requested settings into a spec. Kept apart
// from the call so each field is tested without a daemon.
func applyClusterChange(spec *swarm.Spec, change ClusterChange) {
	if change.TaskHistory != nil {
		v := *change.TaskHistory
		spec.Orchestration.TaskHistoryRetentionLimit = &v
	}
	if change.CertExpiry != nil {
		spec.CAConfig.NodeCertExpiry = *change.CertExpiry
	}
	if change.Heartbeat != nil {
		spec.Dispatcher.HeartbeatPeriod = *change.Heartbeat
	}
	if change.SnapshotInterval != nil {
		spec.Raft.SnapshotInterval = *change.SnapshotInterval
	}
	if change.KeepOldSnapshots != nil {
		v := *change.KeepOldSnapshots
		spec.Raft.KeepOldSnapshots = &v
	}
	if change.Autolock != nil {
		spec.EncryptionConfig.AutoLockManagers = *change.Autolock
	}
	if change.RotateCA {
		// The counter is the whole mechanism: any change to it asks the
		// managers for a new root, which is what `docker swarm ca --rotate`
		// does.
		spec.CAConfig.ForceRotate++
	}
}
