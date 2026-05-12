package linode

import (
	"context"

	"github.com/linode/linodego"
)

// Client wraps linodego.Client. Views consume this type so we can add
// pagination defaults, retries, or observability without touching view code.
type Client struct {
	api linodego.Client
}

func NewClient(token string) *Client {
	c := linodego.NewClient(nil)
	c.SetToken(token)
	return &Client{api: c}
}

func (c *Client) Raw() *linodego.Client { return &c.api }

// SetBaseURL overrides the API endpoint. Intended for tests that want to
// point the client at an httptest server.
func (c *Client) SetBaseURL(url string) { c.api.SetBaseURL(url) }

func (c *Client) ListInstances(ctx context.Context) ([]linodego.Instance, error) {
	return c.api.ListInstances(ctx, nil)
}
