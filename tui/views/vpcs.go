package views

import (
	"context"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("vpcs", []string{"vpc"}, newVPCs)
}

func newVPCs(d Deps) View {
	return newListView(listOpts[linodego.VPC]{
		Deps:  d,
		Title: "VPCs",
		Columns: []Col{
			{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
			{Title: "LABEL", Width: 30, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "REGION", Width: 14, MinWidth: 8, Priority: PriMed},
			{Title: "SUBNETS", Width: 10, MinWidth: 7, Priority: PriHigh},
			{Title: "DESCRIPTION", Width: 40, MinWidth: 14, Priority: PriLowest},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.VPC, error) {
			return c.Raw().ListVPCs(ctx, nil)
		},
		Rower: func(v linodego.VPC) table.Row {
			return table.Row{
				strconv.Itoa(v.ID),
				v.Label,
				v.Region,
				strconv.Itoa(len(v.Subnets)),
				v.Description,
			}
		},
		Matcher: func(v linodego.VPC, needle string) bool {
			return containsAny(needle, v.Label, v.Region, v.Description)
		},
		IDFn:         func(v linodego.VPC) string { return strconv.Itoa(v.ID) },
		BookmarkKind: "vpcs",
		Actions: []Action[linodego.VPC]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(v linodego.VPC) string {
					return fmt.Sprintf("DELETE VPC %s (id %d)? Subnets must be empty.", v.Label, v.ID)
				},
				Run: func(ctx context.Context, c *linode.Client, v linodego.VPC) error {
					return c.Raw().DeleteVPC(ctx, v.ID)
				},
			},
		},
	})
}
