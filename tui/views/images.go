package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("images", []string{"img", "image"}, newImages)
}

func newImages(d Deps) View {
	return newListView(listOpts[linodego.Image]{
		Deps:  d,
		Title: "Images",
		Columns: []Col{
			{Title: "ID", Width: 28, MinWidth: 12, Priority: PriPinned},
			{Title: "LABEL", Width: 30, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "TYPE", Width: 10, MinWidth: 6, Priority: PriMed},
			{Title: "STATUS", Width: 12, MinWidth: 8, Priority: PriHigh},
			{Title: "VENDOR", Width: 14, MinWidth: 8, Priority: PriLow},
			{Title: "SIZE", Width: 8, MinWidth: 5, Priority: PriMed},
			{Title: "PUBLIC", Width: 8, MinWidth: 6, Priority: PriLow},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Image, error) {
			return c.Raw().ListImages(ctx, nil)
		},
		Rower: func(i linodego.Image) table.Row {
			return table.Row{
				i.ID,
				i.Label,
				i.Type,
				string(i.Status),
				i.Vendor,
				fmt.Sprintf("%dM", i.Size),
				yesNo(i.IsPublic),
			}
		},
		Matcher: func(i linodego.Image, needle string) bool {
			return containsAny(needle, i.ID, i.Label, i.Type, i.Vendor, string(i.Status))
		},
		IDFn: func(i linodego.Image) string { return i.ID },
		Actions: []Action[linodego.Image]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(i linodego.Image) string {
					if strings.HasPrefix(i.ID, "linode/") || i.IsPublic {
						return fmt.Sprintf("Cannot delete public image %s.", i.ID)
					}
					return fmt.Sprintf("DELETE image %s (%s)?", i.ID, i.Label)
				},
				Run: func(ctx context.Context, c *linode.Client, i linodego.Image) error {
					if i.IsPublic {
						return fmt.Errorf("cannot delete public image %s", i.ID)
					}
					return c.Raw().DeleteImage(ctx, i.ID)
				},
			},
		},
	})
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
