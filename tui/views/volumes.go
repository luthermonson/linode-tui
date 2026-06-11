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
	Register("volumes", []string{"vol", "vols"}, newVolumes)
}

func newVolumes(d Deps) View {
	return newListView(listOpts[linodego.Volume]{
		Deps:  d,
		Title: "Volumes",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 28},
			{Title: "REGION", Width: 14},
			{Title: "STATUS", Width: 12},
			{Title: "SIZE", Width: 8},
			{Title: "LINODE", Width: 24},
			{Title: "TAGS", Width: 20},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Volume, error) {
			return c.Raw().ListVolumes(ctx, nil)
		},
		Rower: func(v linodego.Volume) table.Row {
			attached := "—"
			if v.LinodeID != nil {
				attached = strconv.Itoa(*v.LinodeID)
				if v.LinodeLabel != "" {
					attached = v.LinodeLabel
				}
			}
			return table.Row{
				strconv.Itoa(v.ID),
				v.Label,
				v.Region,
				string(v.Status),
				fmt.Sprintf("%dG", v.Size),
				attached,
				strings.Join(v.Tags, ","),
			}
		},
		Matcher: func(v linodego.Volume, needle string) bool {
			return containsAny(needle, v.Label, v.Region, string(v.Status), v.LinodeLabel) ||
				tagMatch(v.Tags, needle)
		},
		IDFn:         func(v linodego.Volume) string { return strconv.Itoa(v.ID) },
		BookmarkKind: "volumes",
		TagsFn:       func(v linodego.Volume) []string { return v.Tags },
		FieldFn: map[string]func(linodego.Volume) string{
			"region": func(v linodego.Volume) string { return v.Region },
			"status": func(v linodego.Volume) string { return string(v.Status) },
			"label":  func(v linodego.Volume) string { return v.Label },
			"linode": func(v linodego.Volume) string { return v.LinodeLabel },
		},
		Actions: []Action[linodego.Volume]{
			{
				Key:    "d",
				Label:  "delete",
				Prompt: func(v linodego.Volume) string { return fmt.Sprintf("DELETE volume %s (id %d)?", v.Label, v.ID) },
				Run: func(ctx context.Context, c *linode.Client, v linodego.Volume) error {
					return c.Raw().DeleteVolume(ctx, v.ID)
				},
			},
		},
	})
}
