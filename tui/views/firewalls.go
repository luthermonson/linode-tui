package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("firewalls", []string{"fw", "firewall"}, newFirewalls)
}

func newFirewalls(d Deps) View {
	return newListView(listOpts[linodego.Firewall]{
		Deps:  d,
		Title: "Firewalls",
		Columns: []Col{
			{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
			{Title: "LABEL", Width: 32, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "STATUS", Width: 12, MinWidth: 8, Priority: PriHigh},
			{Title: "ENTITIES", Width: 10, MinWidth: 6, Priority: PriMed},
			{Title: "TAGS", Width: 32, MinWidth: 8, Priority: PriLowest},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Firewall, error) {
			return c.Raw().ListFirewalls(ctx, nil)
		},
		Rower: func(f linodego.Firewall) table.Row {
			return table.Row{
				strconv.Itoa(f.ID),
				f.Label,
				string(f.Status),
				strconv.Itoa(len(f.Entities)),
				strings.Join(f.Tags, ","),
			}
		},
		Matcher: func(f linodego.Firewall, needle string) bool {
			return containsAny(needle, f.Label, string(f.Status)) || tagMatch(f.Tags, needle)
		},
		IDFn:         func(f linodego.Firewall) string { return strconv.Itoa(f.ID) },
		BookmarkKind: "firewalls",
		TagsFn:       func(f linodego.Firewall) []string { return f.Tags },
		Actions: []Action[linodego.Firewall]{
			{
				Key:    "d",
				Label:  "delete",
				Prompt: func(f linodego.Firewall) string { return fmt.Sprintf("DELETE firewall %s (id %d)?", f.Label, f.ID) },
				Run: func(ctx context.Context, c *linode.Client, f linodego.Firewall) error {
					return c.Raw().DeleteFirewall(ctx, f.ID)
				},
			},
		},
	})
}
