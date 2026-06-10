package linode

import (
	"context"

	"github.com/linode/linodego/v2"
)

// Client wraps linodego.Client. Views consume this type so we can add
// pagination defaults, retries, or observability without touching view code.
type Client struct {
	api linodego.Client
}

func NewClient(token string) (*Client, error) {
	c, err := linodego.NewClient(nil)
	if err != nil {
		return nil, err
	}
	c.SetToken(token)
	return &Client{api: c}, nil
}

func (c *Client) Raw() *linodego.Client { return &c.api }

// SetBaseURL overrides the API endpoint. Intended for tests that want to
// point the client at an httptest server.
func (c *Client) SetBaseURL(url string) { c.api.SetBaseURL(url) }

func (c *Client) ListInstances(ctx context.Context) ([]linodego.Instance, error) {
	return c.api.ListInstances(ctx, nil)
}
