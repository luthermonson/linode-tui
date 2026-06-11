package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/linode/linodego/v2"

	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tui/theme"
)

// resourceJSON returns the JSON-indented body of a single resource by ID.
// Supports the same alias set as the CLI's jsonDump.
func resourceJSON(ctx context.Context, c *linode.Client, resource, id string) (string, error) {
	switch resource {
	case "linodes", "instances", "inst", "li":
		items, err := c.Raw().ListInstances(ctx, nil)
		if err != nil {
			return "", err
		}
		for _, it := range items {
			if itoa(it.ID) == id {
				return jsonOf(it)
			}
		}
	case "volumes", "vol", "vols":
		items, err := c.Raw().ListVolumes(ctx, nil)
		if err != nil {
			return "", err
		}
		for _, v := range items {
			if itoa(v.ID) == id {
				return jsonOf(v)
			}
		}
	case "nodebalancers", "nb":
		items, err := c.Raw().ListNodeBalancers(ctx, nil)
		if err != nil {
			return "", err
		}
		for _, nb := range items {
			if itoa(nb.ID) == id {
				return jsonOf(nb)
			}
		}
	case "lke", "kubernetes", "k8s", "clusters":
		items, err := c.Raw().ListLKEClusters(ctx, nil)
		if err != nil {
			return "", err
		}
		for _, l := range items {
			if itoa(l.ID) == id {
				return jsonOf(l)
			}
		}
	case "firewalls", "fw":
		items, err := c.Raw().ListFirewalls(ctx, nil)
		if err != nil {
			return "", err
		}
		for _, f := range items {
			if itoa(f.ID) == id {
				return jsonOf(f)
			}
		}
	case "domains", "dom", "dns":
		items, err := c.Raw().ListDomains(ctx, nil)
		if err != nil {
			return "", err
		}
		for _, d := range items {
			if itoa(d.ID) == id {
				return jsonOf(d)
			}
		}
	default:
		return "", fmt.Errorf(":diff doesn't support resource %q", resource)
	}
	return "", fmt.Errorf("id %q not found in %s", id, resource)
}

func jsonOf(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }

// renderDiff produces a colored unified-style diff between a and b. The naive
// line-by-line algorithm aligns by index — fine for JSON of the same shape
// where most lines match, and a near-miss is shown as old → new.
func renderDiff(th theme.Theme, a, b string) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")
	added := lipgloss.NewStyle().Foreground(th.Ok)
	removed := lipgloss.NewStyle().Foreground(th.Error)
	context := lipgloss.NewStyle().Foreground(th.Muted)

	var b2 strings.Builder
	n := len(aLines)
	if len(bLines) > n {
		n = len(bLines)
	}
	for i := 0; i < n; i++ {
		switch {
		case i >= len(aLines):
			b2.WriteString(added.Render("+ "+bLines[i]) + "\n")
		case i >= len(bLines):
			b2.WriteString(removed.Render("- "+aLines[i]) + "\n")
		case aLines[i] == bLines[i]:
			b2.WriteString(context.Render("  "+aLines[i]) + "\n")
		default:
			b2.WriteString(removed.Render("- "+aLines[i]) + "\n")
			b2.WriteString(added.Render("+ "+bLines[i]) + "\n")
		}
	}
	return b2.String()
}

// resourceDiffCmd fetches two resources of the same kind concurrently and
// returns a tea.Msg (OpenDetailMsg) carrying the rendered diff body.
type resourceDiffResultMsg struct {
	title string
	body  string
	err   error
}

func resourceDiffCmd(c *linode.Client, th theme.Theme, resource, id1, id2 string) func() any {
	return func() any {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		a, err := resourceJSON(ctx, c, resource, id1)
		if err != nil {
			return resourceDiffResultMsg{err: fmt.Errorf("fetch %s/%s: %w", resource, id1, err)}
		}
		b, err := resourceJSON(ctx, c, resource, id2)
		if err != nil {
			return resourceDiffResultMsg{err: fmt.Errorf("fetch %s/%s: %w", resource, id2, err)}
		}
		return resourceDiffResultMsg{
			title: fmt.Sprintf("diff %s %s ↔ %s", resource, id1, id2),
			body:  renderDiff(th, a, b),
		}
	}
}

// snapshotDiffCmd diffs a saved snapshot body against the live resource
// fetched at call time.
func snapshotDiffCmd(c *linode.Client, th theme.Theme, resource, id string, snapshot []byte) func() any {
	return func() any {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		current, err := resourceJSON(ctx, c, resource, id)
		if err != nil {
			return resourceDiffResultMsg{err: fmt.Errorf("fetch %s/%s: %w", resource, id, err)}
		}
		return resourceDiffResultMsg{
			title: fmt.Sprintf("snapshot ↔ current · %s/%s", resource, id),
			body:  renderDiff(th, string(snapshot), current),
		}
	}
}

// keep the import alive for tests that may reference linodego types.
var _ = linodego.Instance{}
