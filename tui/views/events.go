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
		Columns: []Col{
			// Leading severity glyph; pinned because it's the fastest read on
			// the whole view and costs 2 cells.
			{Title: " ", Width: 2, MinWidth: 2, Priority: PriPinned},
			{Title: "ID", Width: 12, MinWidth: 6, Priority: PriPinned},
			{Title: "ACTION", Width: 24, MinWidth: 12, Priority: PriPinned, Flex: true},
			{Title: "STATUS", Width: 12, MinWidth: 8, Priority: PriHigh},
			{Title: "USER", Width: 16, MinWidth: 8, Priority: PriLow},
			{Title: "ENTITY", Width: 28, MinWidth: 12, Priority: PriMed},
			{Title: "%", Width: 4, MinWidth: 3, Priority: PriLow},
			{Title: "MESSAGE", Width: 40, MinWidth: 16, Priority: PriLowest},
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
