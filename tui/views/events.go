package views

import (
	"context"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("events", []string{"ev", "event"}, newEvents)
}

func newEvents(d Deps) View {
	return newListView(listOpts[linodego.Event]{
		Deps:    d,
		Title:   "Events",
		Refresh: 5 * time.Second,
		Columns: []table.Column{
			{Title: " ", Width: 2},
			{Title: "ID", Width: 12},
			{Title: "ACTION", Width: 24},
			{Title: "STATUS", Width: 12},
			{Title: "USER", Width: 16},
			{Title: "ENTITY", Width: 28},
			{Title: "%", Width: 4},
			{Title: "MESSAGE", Width: 40},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Event, error) {
			return c.Raw().ListEvents(ctx, nil)
		},
		Rower: func(e linodego.Event) table.Row {
			entity := ""
			if e.Entity != nil {
				entity = e.Entity.Label
				if entity == "" {
					entity = string(e.Entity.Type)
				}
			}
			return table.Row{
				eventGlyph(e),
				strconv.Itoa(e.ID),
				string(e.Action),
				string(e.Status),
				e.Username,
				entity,
				strconv.Itoa(e.PercentComplete),
				truncate(e.Message, 40),
			}
		},
		Matcher: func(e linodego.Event, needle string) bool {
			entity := ""
			if e.Entity != nil {
				entity = e.Entity.Label
			}
			return containsAny(needle, string(e.Action), string(e.Status), e.Username, entity, e.Message)
		},
		Sort: func(a, b linodego.Event) int {
			// Newest first by ID (events are append-only with monotonic IDs).
			if a.ID < b.ID {
				return 1
			}
			if a.ID > b.ID {
				return -1
			}
			return 0
		},
	})
}

// eventGlyph returns a one-rune lead-in marking event state. Pure-text since
// bubbles/table doesn't support per-row colors.
func eventGlyph(e linodego.Event) string {
	switch e.Status {
	case "started", "scheduled":
		return "●"
	case "failed":
		return "✗"
	case "finished":
		return "✓"
	default:
		return " "
	}
}
