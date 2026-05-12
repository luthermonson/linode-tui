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
	Register("objectstorage", []string{"obj", "buckets", "bucket", "s3"}, newObjectStorage)
}

func newObjectStorage(d Deps) View {
	return newListView(listOpts[linodego.ObjectStorageBucket]{
		Deps:  d,
		Title: "Object Storage",
		Columns: []table.Column{
			{Title: "LABEL", Width: 30},
			{Title: "REGION", Width: 14},
			{Title: "ENDPOINT", Width: 12},
			{Title: "HOSTNAME", Width: 40},
			{Title: "OBJECTS", Width: 10},
			{Title: "SIZE", Width: 10},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.ObjectStorageBucket, error) {
			return c.Raw().ListObjectStorageBuckets(ctx, nil)
		},
		Rower: func(b linodego.ObjectStorageBucket) table.Row {
			return table.Row{
				b.Label,
				b.Region,
				string(b.EndpointType),
				b.Hostname,
				strconv.Itoa(b.Objects),
				humanBytes(b.Size),
			}
		},
		Matcher: func(b linodego.ObjectStorageBucket, needle string) bool {
			return containsAny(needle, b.Label, b.Region, b.Hostname, string(b.EndpointType))
		},
		IDFn: func(b linodego.ObjectStorageBucket) string { return b.Region + "/" + b.Label },
		Actions: []Action[linodego.ObjectStorageBucket]{
			{
				Key:   "d",
				Label: "delete",
				Prompt: func(b linodego.ObjectStorageBucket) string {
					if b.Objects > 0 {
						return fmt.Sprintf("Bucket %s has %d objects — empty it first.", b.Label, b.Objects)
					}
					return fmt.Sprintf("DELETE bucket %s (%s)?", b.Label, b.Region)
				},
				Run: func(ctx context.Context, c *linode.Client, b linodego.ObjectStorageBucket) error {
					if b.Objects > 0 {
						return fmt.Errorf("bucket %s is non-empty (%d objects)", b.Label, b.Objects)
					}
					return c.Raw().DeleteObjectStorageBucket(ctx, b.Region, b.Label)
				},
			},
		},
	})
}

func humanBytes(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := int64(n) / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
