package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

func init() {
	Register("fanout_volumes", []string{"fanout-volumes", "fan-volumes", "fan-vol", "all-volumes"}, newFanoutVolumes)
}

type FanoutVolume struct {
	Account string          `json:"account"`
	Volume  linodego.Volume `json:"volume"`
}

func newFanoutVolumes(d Deps) View {
	return newListView(listOpts[FanoutVolume]{
		Deps:  d,
		Title: "Volumes (all accounts)",
		Columns: []table.Column{
			{Title: "ACCOUNT", Width: 12},
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 28},
			{Title: "REGION", Width: 14},
			{Title: "STATUS", Width: 12},
			{Title: "SIZE", Width: 8},
			{Title: "LINODE", Width: 20},
		},
		Lister: func(ctx context.Context, _ *linode.Client) ([]FanoutVolume, error) {
			return fanoutVolumes(ctx, d)
		},
		Rower: func(fv FanoutVolume) table.Row {
			v := fv.Volume
			attached := "—"
			if v.LinodeID != nil {
				attached = strconv.Itoa(*v.LinodeID)
				if v.LinodeLabel != "" {
					attached = v.LinodeLabel
				}
			}
			return table.Row{
				fv.Account,
				strconv.Itoa(v.ID),
				v.Label,
				v.Region,
				string(v.Status),
				fmt.Sprintf("%dG", v.Size),
				attached,
			}
		},
		Matcher: func(fv FanoutVolume, needle string) bool {
			v := fv.Volume
			return containsAny(needle, fv.Account, v.Label, v.Region, string(v.Status), v.LinodeLabel) ||
				tagMatch(v.Tags, needle)
		},
		IDFn:         func(fv FanoutVolume) string { return fv.Account + ":" + strconv.Itoa(fv.Volume.ID) },
		BookmarkKind: "fanout_volumes",
	})
}

func fanoutVolumes(ctx context.Context, d Deps) ([]FanoutVolume, error) {
	names, err := fanoutAccountNames(d)
	if err != nil {
		return nil, err
	}
	var (
		mu   sync.Mutex
		out  []FanoutVolume
		errs []string
		wg   sync.WaitGroup
	)
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			c, err := fanoutClient(ctx, d, name)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			items, err := c.Raw().ListVolumes(ctx, nil)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				out = append(out, FanoutVolume{Account: name, Volume: it})
			}
			mu.Unlock()
		}(name)
	}
	wg.Wait()
	return joinFanout(out, errs)
}

// fanoutAccountNames returns the account names to query. Honors a comma list
// in Deps.Context["accounts"] (set by `:fanout <view> dev,e2e`). Falls back
// to every non-CLI account in cfg.
func fanoutAccountNames(d Deps) ([]string, error) {
	if filter := d.CtxString("accounts"); filter != "" {
		var names []string
		for _, n := range splitCSV(filter) {
			if _, ok := d.Cfg.Accounts[n]; ok {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no matching accounts in %q", filter)
		}
		return names, nil
	}
	names := make([]string, 0, len(d.Cfg.Accounts))
	for n := range d.Cfg.Accounts {
		if n == "__cli__" {
			continue
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no accounts configured")
	}
	return names, nil
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		if r == ' ' || r == '\t' {
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// fanoutClient resolves the token for an account and builds a linode.Client.
func fanoutClient(ctx context.Context, d Deps, name string) (*linode.Client, error) {
	tok, err := linode.ResolveTokenForAccount(ctx, d.Cfg, name)
	if err != nil {
		return nil, err
	}
	return linode.NewClient(tok)
}

func joinFanout[T any](out []T, errs []string) ([]T, error) {
	if len(errs) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("all accounts failed: %s", strings.Join(errs, "; "))
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("partial: %s", strings.Join(errs, "; "))
	}
	return out, nil
}
