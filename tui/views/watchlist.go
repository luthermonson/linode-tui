package views

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("watchlist", []string{"watch", "starred", "favorites"}, newWatchlist)
}

// WatchlistRow is a kind-tagged unified row for the watchlist view.
type WatchlistRow struct {
	Kind   string   `json:"kind"`
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Region string   `json:"region"`
	Status string   `json:"status"`
	Tags   []string `json:"tags,omitempty"`
	// Age is the formatted time since the bookmark was last captured. Filled
	// during list assembly from the snapshot file's mtime.
	Age string `json:"age,omitempty"`
	// Drift is true when the live JSON differs from the latest snapshot.
	Drift bool `json:"drift,omitempty"`
}

func newWatchlist(d Deps) View {
	return newListView(listOpts[WatchlistRow]{
		Deps:  d,
		Title: "Watchlist",
		Columns: []table.Column{
			{Title: "KIND", Width: 14},
			{Title: "ID", Width: 12},
			{Title: "LABEL", Width: 28},
			{Title: "REGION", Width: 12},
			{Title: "STATUS", Width: 12},
			{Title: "★ AGE", Width: 8},
			{Title: "Δ", Width: 2},
			{Title: "TAGS", Width: 20},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]WatchlistRow, error) {
			if d.Cfg == nil {
				return nil, fmt.Errorf("no config available")
			}
			bookmarks := d.Cfg.ActiveBookmarks()
			if len(bookmarks) == 0 {
				return nil, nil
			}
			return fetchWatchlist(ctx, d.Linode, bookmarks)
		},
		Rower: func(r WatchlistRow) table.Row {
			drift := " "
			if r.Drift {
				drift = "Δ"
			}
			return table.Row{r.Kind, r.ID, r.Label, r.Region, r.Status, r.Age, drift, strings.Join(r.Tags, ",")}
		},
		Matcher: func(r WatchlistRow, needle string) bool {
			return containsAny(needle, r.Kind, r.ID, r.Label, r.Region, r.Status) || tagMatch(r.Tags, needle)
		},
		IDFn:   func(r WatchlistRow) string { return r.Kind + ":" + r.ID },
		TagsFn: func(r WatchlistRow) []string { return r.Tags },
		OnEnter: func(r WatchlistRow, _ Deps) tea.Cmd {
			kind := r.Kind
			id := r.ID
			return func() tea.Msg {
				return NavigateMsg{
					Name:    kind,
					Context: map[string]any{"focus_id": id},
				}
			}
		},
	})
}

// fetchWatchlist fans out across the kinds that have bookmarks, fetches each
// resource type in parallel, and emits a unified WatchlistRow slice. Unknown
// kinds (e.g. drill-in sub-views) are skipped silently.
func fetchWatchlist(ctx context.Context, c *linode.Client, bookmarks map[string][]string) ([]WatchlistRow, error) {
	var (
		mu  sync.Mutex
		out []WatchlistRow
		wg  sync.WaitGroup
	)

	add := func(rows []WatchlistRow) {
		mu.Lock()
		out = append(out, rows...)
		mu.Unlock()
	}

	fetchers := map[string]func() ([]WatchlistRow, error){
		"instances": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListInstances(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, it := range items {
				id := strconv.Itoa(it.ID)
				if slices.Contains(bookmarks["instances"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "instances", ID: id, Label: it.Label,
						Region: it.Region, Status: string(it.Status), Tags: it.Tags,
						Drift: hasDrift("instances", id, it),
					})
				}
			}
			return rows, nil
		},
		"volumes": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListVolumes(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, v := range items {
				id := strconv.Itoa(v.ID)
				if slices.Contains(bookmarks["volumes"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "volumes", ID: id, Label: v.Label,
						Region: v.Region, Status: string(v.Status), Tags: v.Tags,
						Drift: hasDrift("volumes", id, v),
					})
				}
			}
			return rows, nil
		},
		"nodebalancers": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListNodeBalancers(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, nb := range items {
				id := strconv.Itoa(nb.ID)
				if slices.Contains(bookmarks["nodebalancers"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "nodebalancers", ID: id, Label: deref(nb.Label),
						Region: nb.Region, Status: "", Tags: nb.Tags,
					})
				}
			}
			return rows, nil
		},
		"lke": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListLKEClusters(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, l := range items {
				id := strconv.Itoa(l.ID)
				if slices.Contains(bookmarks["lke"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "lke", ID: id, Label: l.Label,
						Region: l.Region, Status: string(l.Status), Tags: l.Tags,
						Drift: hasDrift("lke", id, l),
					})
				}
			}
			return rows, nil
		},
		"firewalls": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListFirewalls(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, f := range items {
				id := strconv.Itoa(f.ID)
				if slices.Contains(bookmarks["firewalls"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "firewalls", ID: id, Label: f.Label,
						Region: "", Status: string(f.Status), Tags: f.Tags,
					})
				}
			}
			return rows, nil
		},
		"domains": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListDomains(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, d := range items {
				id := strconv.Itoa(d.ID)
				if slices.Contains(bookmarks["domains"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "domains", ID: id, Label: d.Domain,
						Region: "", Status: string(d.Status), Tags: d.Tags,
					})
				}
			}
			return rows, nil
		},
		"vpcs": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListVPCs(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, v := range items {
				id := strconv.Itoa(v.ID)
				if slices.Contains(bookmarks["vpcs"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "vpcs", ID: id, Label: v.Label,
						Region: v.Region, Status: "",
					})
				}
			}
			return rows, nil
		},
		"databases": func() ([]WatchlistRow, error) {
			items, err := c.Raw().ListDatabases(ctx, nil)
			if err != nil {
				return nil, err
			}
			rows := make([]WatchlistRow, 0)
			for _, db := range items {
				id := strconv.Itoa(db.ID)
				if slices.Contains(bookmarks["databases"], id) {
					rows = append(rows, WatchlistRow{
						Kind: "databases", ID: id, Label: db.Label,
						Region: db.Region, Status: string(db.Status),
					})
				}
			}
			return rows, nil
		},
	}

	for kind, ids := range bookmarks {
		if len(ids) == 0 {
			continue
		}
		fetcher, ok := fetchers[kind]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(f func() ([]WatchlistRow, error)) {
			defer wg.Done()
			rows, err := f()
			if err == nil {
				add(rows)
			}
		}(fetcher)
	}
	wg.Wait()

	// Fill snapshot ages.
	for i := range out {
		if age, ok := SnapshotAge(out[i].Kind, out[i].ID); ok {
			out[i].Age = formatAge(age)
		}
	}

	// Stable order: by kind then by ID.
	sortWatchlist(out)
	return out, nil
}

func sortWatchlist(rows []WatchlistRow) {
	// Bubble-sort substitute via stable sort would need import sort; cheaper:
	// only N kinds * a few items in practice. Use slices.SortStableFunc.
	slices.SortStableFunc(rows, func(a, b WatchlistRow) int {
		if a.Kind != b.Kind {
			if a.Kind < b.Kind {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}

// hasDrift returns true when the live item's JSON differs from the latest
// stored snapshot. Errors / missing snapshots count as no-drift.
func hasDrift(kind, id string, item any) bool {
	snap, err := LoadSnapshot(kind, id)
	if err != nil {
		return false
	}
	live, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return false
	}
	return !bytes.Equal(live, snap)
}

// formatAge renders a coarse human-readable duration like "5d" or "2h".
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Use this so the linodego import isn't accidentally pruned if all
// resource-specific helpers above get refactored out.
var _ = linodego.Instance{}
