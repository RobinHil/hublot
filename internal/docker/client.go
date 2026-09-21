package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

// DefaultSocket is where a daemon installed as root listens. Remote hosts are
// out of scope on purpose (AGENTS.md section 2), so there is no host option
// anywhere: the only question is which local socket to open.
const DefaultSocket = "/var/run/docker.sock"

// Client is the Engine SDK wrapper. Every method converts SDK types into the
// domain types of this package before returning.
type Client struct {
	api        *client.Client
	socket     string
	apiVersion string
	info       Info
}

// Connect dials the local daemon socket and negotiates the API version. The
// error it returns is meant to be shown to a human as-is (AGENTS.md section 12).
func Connect(ctx context.Context) (*Client, error) {
	socket := ResolveSocket(os.Getenv, func(path string) error {
		_, err := os.Stat(path)
		return err
	}, os.Getuid())

	api, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating docker client for %s: %w", socket, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := api.Ping(pingCtx); err != nil {
		_ = api.Close()
		return nil, connectionError(socket, err)
	}

	c := &Client{api: api, socket: socket, apiVersion: api.ClientVersion()}
	if info, err := c.Info(ctx); err == nil {
		c.info = info
	}
	return c, nil
}

// ResolveSocket picks the local socket to talk to, in the order a user would
// expect: what DOCKER_HOST names, then the root daemon's socket, then the one a
// rootless installation puts under the runtime directory. The lookups are
// injected so the choice can be tested without a daemon or a particular host.
//
// Rootless is not a second kind of host, it is the same local daemon installed
// differently: without this, a rootless user is told there is no daemon at a
// path their installation never uses.
func ResolveSocket(env func(string) string, stat func(string) error, uid int) string {
	if host := env("DOCKER_HOST"); strings.HasPrefix(host, "unix://") {
		return strings.TrimPrefix(host, "unix://")
	}

	candidates := []string{DefaultSocket}

	runtimeDir := env("XDG_RUNTIME_DIR")
	if runtimeDir == "" && uid > 0 {
		// The value systemd would have set, for a shell that lost it.
		runtimeDir = fmt.Sprintf("/run/user/%d", uid)
	}
	if runtimeDir != "" {
		candidates = append(candidates, filepath.Join(runtimeDir, "docker.sock"))
	}

	for _, candidate := range candidates {
		if stat(candidate) == nil {
			return candidate
		}
	}

	// Nothing is there; the first candidate is what the error should name.
	return candidates[0]
}

// connectionError turns a dial failure into something actionable: which socket
// was tried, and the permission hint that explains nine failures out of ten.
func connectionError(socket string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file"):
		return fmt.Errorf("no docker daemon at %s: is docker running? "+
			"a rootless daemon would listen under $XDG_RUNTIME_DIR instead", socket)
	case errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied"):
		return fmt.Errorf("%s", permissionAdvice(socket))
	default:
		return fmt.Errorf("cannot reach the docker daemon on %s: %w", socket, err)
	}
}

// Close releases the underlying HTTP connections.
func (c *Client) Close() error {
	if c.api == nil {
		return nil
	}
	return c.api.Close()
}

// Socket is the path this client is connected to, for display.
func (c *Client) Socket() string { return c.socket }

// APIVersion is the negotiated version. Prune semantics depend on it
// (AGENTS.md section 10.2), so it must stay reachable from the state layer.
func (c *Client) APIVersion() string { return c.apiVersion }

// CachedInfo is the daemon info read at connection time.
func (c *Client) CachedInfo() Info { return c.info }

// versionAtLeast compares the negotiated API version against a "1.NN" string.
func (c *Client) versionAtLeast(want string) bool {
	return compareAPIVersion(c.apiVersion, want) >= 0
}

// compareAPIVersion compares two dotted API versions such as "1.41".
func compareAPIVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var an, bn int
		if i < len(as) {
			an, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bn, _ = strconv.Atoi(bs[i])
		}
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
	}
	return 0
}
