package views

import (
	"context"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego"

	"github.com/linode/tui/internal/linode"
)

func init() {
	Register("placementgroups", []string{"pg", "placement", "placementgroup"}, newPlacementGroups)
}

func newPlacementGroups(d Deps) View {
	return newListView(listOpts[linodego.PlacementGroup]{
		Deps:  d,
		Title: "Placement Groups",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 30},
			{Title: "REGION", Width: 14},
			{Title: "TYPE", Width: 14},
			{Title: "POLICY", Width: 14},
			{Title: "MEMBERS", Width: 10},
			{Title: "COMPLIANT", Width: 10},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.PlacementGroup, error) {
			return c.Raw().ListPlacementGroups(ctx, nil)
		},
		Rower: func(pg linodego.PlacementGroup) table.Row {
			return table.Row{
				strconv.Itoa(pg.ID),
				pg.Label,
				pg.Region,
				string(pg.PlacementGroupType),
				string(pg.PlacementGroupPolicy),
				strconv.Itoa(len(pg.Members)),
				yesNo(pg.IsCompliant),
			}
		},
		Matcher: func(pg linodego.PlacementGroup, needle string) bool {
			return containsAny(needle, pg.Label, pg.Region, string(pg.PlacementGroupType), string(pg.PlacementGroupPolicy))
		},
		IDFn: func(pg linodego.PlacementGroup) string { return strconv.Itoa(pg.ID) },
		Actions: []Action[linodego.PlacementGroup]{
			{
				Key:    "d",
				Label:  "delete",
				Prompt: func(pg linodego.PlacementGroup) string { return fmt.Sprintf("DELETE placement group %s (id %d)?", pg.Label, pg.ID) },
				Run: func(ctx context.Context, c *linode.Client, pg linodego.PlacementGroup) error {
					return c.Raw().DeletePlacementGroup(ctx, pg.ID)
				},
			},
		},
	})
}
