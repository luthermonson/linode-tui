package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("nodebalancers", []string{"nb", "nbs", "balancer", "balancers"}, newNodeBalancers)
}

func newNodeBalancers(d Deps) View {
	return newListView(listOpts[linodego.NodeBalancer]{
		Deps:  d,
		Title: "NodeBalancers",
		Columns: []Col{
			{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
			{Title: "LABEL", Width: 28, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "REGION", Width: 14, MinWidth: 8, Priority: PriMed},
			{Title: "HOSTNAME", Width: 40, MinWidth: 16, Priority: PriLow},
			{Title: "IPv4", Width: 16, MinWidth: 12, Priority: PriLow},
			{Title: "TAGS", Width: 20, MinWidth: 8, Priority: PriLowest},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.NodeBalancer, error) {
			return c.Raw().ListNodeBalancers(ctx, nil)
		},
		Rower: func(nb linodego.NodeBalancer) table.Row {
			return table.Row{
				strconv.Itoa(nb.ID),
				deref(nb.Label),
				nb.Region,
				deref(nb.Hostname),
				deref(nb.IPv4),
				strings.Join(nb.Tags, ","),
			}
		},
		Matcher: func(nb linodego.NodeBalancer, needle string) bool {
			return containsAny(needle, deref(nb.Label), nb.Region, deref(nb.Hostname), deref(nb.IPv4)) ||
				tagMatch(nb.Tags, needle)
		},
		IDFn:         func(nb linodego.NodeBalancer) string { return strconv.Itoa(nb.ID) },
		BookmarkKind: "nodebalancers",
		TagsFn:       func(nb linodego.NodeBalancer) []string { return nb.Tags },
		OnEnter: func(nb linodego.NodeBalancer, _ Deps) tea.Cmd {
			label := deref(nb.Label)
			return func() tea.Msg {
				return NavigateMsg{
					Name: "nodebalancer_configs",
					Context: map[string]any{
						"nodebalancer_id":    nb.ID,
						"nodebalancer_label": label,
					},
				}
			}
		},
		Actions: []Action[linodego.NodeBalancer]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(nb linodego.NodeBalancer) string {
					return fmt.Sprintf("DELETE nodebalancer %s (id %d)?", deref(nb.Label), nb.ID)
				},
				Run: func(ctx context.Context, c *linode.Client, nb linodego.NodeBalancer) error {
					return c.Raw().DeleteNodeBalancer(ctx, nb.ID)
				},
			},
		},
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
