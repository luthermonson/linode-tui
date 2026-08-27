package views

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
)

// fanoutDefaultRefresh is the default poll interval for every fanout_*.go
// view. Fanout views hit N accounts (each a full HTTP round trip, and
// potentially an `op read` shell-out on cache miss) on every tick; the
// listView package default of 2s was designed for a single-account view and
// is needlessly aggressive multiplied across accounts. 30s keeps the views
// reasonably fresh without hammering every configured account 30x/minute.
const fanoutDefaultRefresh = 30 * time.Second

func init() {
	Register("fanout_volumes", []string{"fanout-volumes", "fan-volumes", "fan-vol", "all-volumes"}, newFanoutVolumes)
}

type FanoutVolume struct {
	Account string          `json:"account"`
	Volume  linodego.Volume `json:"volume"`
}

func newFanoutVolumes(d Deps) View {
	return newListView(listOpts[FanoutVolume]{
		Deps:    d,
		Title:   "Volumes (all accounts)",
		Refresh: fanoutDefaultRefresh,
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

// fanoutClientCache holds one resolved *linode.Client per account for the
// life of the process, keyed by account name plus a short hash of that
// account's configured credential source (its literal token or op_ref).
//
// Before this cache existed, every fanout_*.go view resolved the token
// (which, for 1Password-backed accounts, means spawning `op read` — a
// subprocess and possibly a biometric prompt) and built a brand new
// linodego client on *every* refresh tick, for *every* fanout view, for
// *every* configured account. At the old 2s default tick that's N accounts
// worth of `op read` calls every two seconds per open fanout view.
//
// Keyed-by-hash means editing an account's token/op_ref in config naturally
// invalidates the old cache entry (new key, old one gets evicted below).
// Known limitations, accepted to keep this simple:
//   - Renaming an account to a different name with the same credentials
//     doesn't reuse the old cached client (harmless — just one extra resolve).
//   - Rotating the secret *behind* an unchanged op_ref (same ref, new value
//     in 1Password) is NOT detected; the cached client keeps the old token
//     until the process restarts. Given op refs are usually stable and
//     token rotation is rare/deliberate, a full TUI restart to pick up a
//     rotated secret is an acceptable tradeoff for not re-shelling-out to
//     `op` every 2-30 seconds.
var (
	fanoutClientCacheMu sync.Mutex
	fanoutClientCache   = map[string]*linode.Client{}
)

// fanoutClient resolves the token for an account and builds a linode.Client,
// reusing a cached client when the account's credential source hasn't
// changed. See fanoutClientCache for the caching contract and its limits.
func fanoutClient(ctx context.Context, d Deps, name string) (*linode.Client, error) {
	acct, ok := d.Cfg.Accounts[name]
	if !ok {
		return nil, fmt.Errorf("account %q not found in config", name)
	}
	key := fanoutClientCacheKey(name, acct.Token, acct.OPRef)

	fanoutClientCacheMu.Lock()
	if c, ok := fanoutClientCache[key]; ok {
		fanoutClientCacheMu.Unlock()
		return c, nil
	}
	fanoutClientCacheMu.Unlock()

	tok, err := linode.ResolveTokenForAccount(ctx, d.Cfg, name)
	if err != nil {
		return nil, err
	}
	c, err := linode.NewClient(tok)
	if err != nil {
		return nil, err
	}

	fanoutClientCacheMu.Lock()
	// Evict any stale entry for this account name whose credential source
	// has since changed, so the cache doesn't grow unboundedly across
	// config edits made during a long-running session.
	prefix := name + ":"
	for k := range fanoutClientCache {
		if strings.HasPrefix(k, prefix) && k != key {
			delete(fanoutClientCache, k)
		}
	}
	fanoutClientCache[key] = c
	fanoutClientCacheMu.Unlock()

	return c, nil
}

func fanoutClientCacheKey(name, token, opRef string) string {
	sum := sha256.Sum256([]byte(token + "\x00" + opRef))
	return name + ":" + hex.EncodeToString(sum[:])[:8]
}

// joinFanout merges per-account results from a fanout query. When every
// account failed, the error is returned so listView shows something rather
// than silently rendering an empty table.
//
// When only *some* accounts failed, we deliberately swallow the error and
// return just the successful rows. This is a workaround, not a design
// choice: listView (owned separately, not editable from here) treats any
// non-nil Lister error as fatal for that fetch and drops the items it
// already has — so surfacing a partial-failure error here would blank an
// otherwise-good table on every tick that one flaky/rate-limited account
// hiccups. Partial data beats a blank table. The failing account names are
// intentionally not otherwise surfaced (listOpts.Title is a static string,
// not something a Lister can update) — see the fix notes for this change.
func joinFanout[T any](out []T, errs []string) ([]T, error) {
	if len(errs) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("all accounts failed: %s", strings.Join(errs, "; "))
	}
	return out, nil
}
