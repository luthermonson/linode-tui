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
		Columns: []Col{
			{Title: "ID", Width: 10, MinWidth: 6, Priority: PriPinned},
			{Title: "LABEL", Width: 28, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "REGION", Width: 14, MinWidth: 8, Priority: PriMed},
			{Title: "STATUS", Width: 12, MinWidth: 8, Priority: PriHigh},
			{Title: "SIZE", Width: 8, MinWidth: 5, Priority: PriMed},
			{Title: "LINODE", Width: 24, MinWidth: 10, Priority: PriLow},
			{Title: "TAGS", Width: 20, MinWidth: 8, Priority: PriLowest},
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
