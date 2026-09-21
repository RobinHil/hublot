package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

// DefaultSocket is the only transport hublot supports. Remote hosts are out of
// scope on purpose (AGENTS.md section 2), so there is no host option anywhere.
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
// error it returns is meant to be shown to a human as-is (AGENTS.md section 11).
func Connect(ctx context.Context) (*Client, error) {
	socket := DefaultSocket
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		socket = strings.TrimPrefix(h, "unix://")
	}

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

// connectionError turns a dial failure into something actionable: which socket
// was tried, and the permission hint that explains nine failures out of ten.
func connectionError(socket string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file"):
		return fmt.Errorf("no docker daemon at %s: is docker running?", socket)
	case errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied"):
		return fmt.Errorf("permission denied on %s: add your user to the 'docker' group, or run hublot as root", socket)
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
