package views

import (
	"context"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
)

func init() {
	Register("vpcs", []string{"vpc"}, newVPCs)
}

func newVPCs(d Deps) View {
	return newListView(listOpts[linodego.VPC]{
		Deps:  d,
		Title: "VPCs",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 30},
			{Title: "REGION", Width: 14},
			{Title: "SUBNETS", Width: 10},
			{Title: "DESCRIPTION", Width: 40},
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
