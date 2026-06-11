package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/luthermonson/linode-tui/tui/theme"
	"github.com/luthermonson/linode-tui/tui/views"
)

func renderHelp(th theme.Theme, current views.View, filter string, layouts map[string]string, folds string) string {
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	cellKey := lipgloss.NewStyle().Foreground(th.Secondary).PaddingRight(2)
	cellDesc := lipgloss.NewStyle().Foreground(th.Text)

	headerStyle := func(c lipgloss.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(c).Bold(true)
	}
	section := func(title string, color lipgloss.Color, entries []views.HelpEntry) string {
		entries = filterEntries(entries, filter)
		if len(entries) == 0 {
			return ""
		}
		var s strings.Builder
		s.WriteString(headerStyle(color).Render(title) + "\n")
		for _, e := range entries {
			s.WriteString("  " + cellKey.Render(e.Key) + cellDesc.Render(e.Desc) + "\n")
		}
		return s.String()
	}

	var b strings.Builder
	if filter != "" {
		b.WriteString(muted.Render("filter: " + filter + " (esc to clear)") + "\n\n")
	}
	if s := section("Global", th.Primary, globalHelp); s != "" {
		b.WriteString(s + "\n")
	}
	if s := section("Command bar", th.Secondary, cmdBarHelp); s != "" {
		b.WriteString(s + "\n")
	}
	if s := section("Views (`:<name>`)", th.Primary, viewHelp); s != "" {
		b.WriteString(s + "\n")
	}
	if s := section("Audit", th.Warn, auditHelp); s != "" {
		b.WriteString(s + "\n")
	}
	if h, ok := current.(views.Helper); ok {
		if s := section(current.Title(), th.Accent, h.Help()); s != "" {
			b.WriteString(s + "\n")
		}
	}
	if folds != "" {
		entries := []views.HelpEntry{
			{Key: folds, Desc: "current folds — widen / lengthen terminal to recover"},
			{Key: "config", Desc: "tune fold_width_secondary / fold_width_tertiary / fold_height_quaternary"},
		}
		if s := section("Folded panes", th.Warn, entries); s != "" {
			b.WriteString(s + "\n")
		}
	}
	if len(layouts) > 0 {
		entries := make([]views.HelpEntry, 0, len(layouts))
		names := make([]string, 0, len(layouts))
		for n := range layouts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			entries = append(entries, views.HelpEntry{Key: ":layout load " + n, Desc: layouts[n]})
		}
		if s := section("Saved layouts", th.Primary, entries); s != "" {
			b.WriteString(s + "\n")
		}
	}
	if filter == "" {
		b.WriteString(muted.Render("press any key to filter · ? or esc to close"))
	} else {
		b.WriteString(muted.Render("? or esc to close"))
	}
	return b.String()
}

func filterEntries(in []views.HelpEntry, filter string) []views.HelpEntry {
	if filter == "" {
		return in
	}
	f := strings.ToLower(filter)
	out := make([]views.HelpEntry, 0, len(in))
	for _, e := range in {
		if strings.Contains(strings.ToLower(e.Key), f) || strings.Contains(strings.ToLower(e.Desc), f) {
			out = append(out, e)
		}
	}
	return out
}

var globalHelp = []views.HelpEntry{
	{Key: ":", Desc: "open command bar"},
	{Key: "?", Desc: "toggle this help"},
	{Key: "ctrl+c", Desc: "quit"},
	{Key: "ctrl+y", Desc: "replay/undo last audit entry (when log non-empty)"},
	{Key: "↑/↓ j/k", Desc: "move cursor"},
}

var auditHelp = []views.HelpEntry{
	{Key: "ctrl+y", Desc: "replay/undo the most recent audit entry (dry-run by default)"},
	{Key: ":audit", Desc: "tail the last 200 (alias for :audit tail)"},
	{Key: ":audit recent [n]", Desc: "styled banner-style listing for the last n entries"},
	{Key: ":audit grep <s>", Desc: "filter entries by substring across action/kind/id/label/err"},
	{Key: ":audit purge <d>", Desc: "drop entries older than a duration (e.g. 720h)"},
	{Key: ":audit clear", Desc: "wipe the entire audit log (typed confirm)"},
	{Key: ":undo [step]", Desc: "inspect or execute the inverse of an audit entry"},
}

var cmdBarHelp = []views.HelpEntry{
	{Key: ":account [name]", Desc: "list accounts or switch (re-resolves token)"},
	{Key: ":theme <name> | list", Desc: "switch theme (dark|light|dracula|solarized-light) or preview swatches"},
	{Key: ":refresh [view] <dur|off>", Desc: "set global / per-view interval, or :refresh defaults for the preset"},
	{Key: ":split <view>", Desc: "open a secondary pane next to current"},
	{Key: ":pane <slot> <view>", Desc: "swap one pane (primary | secondary | tertiary | quaternary)"},
	{Key: ":unsplit", Desc: "collapse all secondary panes"},
	{Key: ":layout save|load|list|delete|rename <name>", Desc: "manage saved pane layouts"},
	{Key: ":layout export|import|export-all|import-all", Desc: "round-trip layouts as YAML"},
	{Key: ":layout import-from <url> [name]", Desc: "fetch a layout (verifies optional ?sha256=…)"},
	{Key: ":layout pin|share <name> <url>", Desc: "render an import-from URL with the digest appended"},
	{Key: ":fold-char <ch|reset>", Desc: "prefix for folded pane labels (default '+')"},
	{Key: ":bookmark list|export|import|migrate|mv|clear|scope", Desc: "manage bookmarks (per-account or global)"},
	{Key: ":doctor [section] [group=name] [--json|fix]", Desc: "run health checks"},
	{Key: ":validate", Desc: "re-run the validate-config warning set"},
	{Key: ":config show | path", Desc: "view redacted YAML / resolved path"},
	{Key: ":cache size | prune <subdir|all>", Desc: "inspect or trim ~/.cache/linode-tui"},
	{Key: ":audit [tail|recent|grep|purge|clear|count]", Desc: "audit log gestures"},
	{Key: ":undo [step N] [execute]", Desc: "dry-run / execute the inverse of an audit entry"},
	{Key: ":diff snapshot <kind> <id> [@N]", Desc: "live JSON vs stored snapshot"},
	{Key: ":open <resource> [id]", Desc: "JSON of one resource in a detail modal"},
	{Key: ":stats [post|reset|reset all]", Desc: "view / send / clear local counters"},
	{Key: ":tools dir | upgrade [kind] | relocate <dir>", Desc: "external tool management"},
	{Key: ":new linode|nodebalancer|volume|vpc|lke", Desc: "open the create form"},
	{Key: ":clear-account [dry-run]", Desc: "DESTRUCTIVE: wipe every resource on the active account (typed confirm)"},
	{Key: ":read-only", Desc: "toggle the session-wide mutation block"},
	{Key: ":replay-last [step N]", Desc: "inspect the most recent audit entry (ctrl+y)"},
	{Key: "tab in cmdbar", Desc: "autocomplete the current verb to the longest match"},
}

// viewHelp lists the resource views reachable as `:<name>` — populated from
// the registry so the help is never stale.
var viewHelp = buildViewHelp()

func buildViewHelp() []views.HelpEntry {
	names := views.Names()
	sort.Strings(names)
	out := make([]views.HelpEntry, 0, len(names))
	for _, n := range names {
		out = append(out, views.HelpEntry{Key: ":" + n, Desc: viewSummary(n)})
	}
	return out
}

func viewSummary(name string) string {
	switch name {
	case "instances":
		return "Linodes (aliases: linodes, inst, li)"
	case "volumes":
		return "Block Storage volumes (vol, vols)"
	case "nodebalancers":
		return "NodeBalancers (nb, nbs)"
	case "nodebalancer_configs":
		return "NodeBalancer configs (drilled from a NodeBalancer)"
	case "firewalls":
		return "Cloud Firewalls (fw, firewall)"
	case "lke":
		return "LKE clusters (kubernetes, k8s)"
	case "domains":
		return "Domains (dom, dns)"
	case "domain_records":
		return "Domain records (drilled from a Domain)"
	case "vpcs":
		return "VPCs (vpc)"
	case "placementgroups":
		return "Placement Groups (pg, placement)"
	case "stackscripts":
		return "StackScripts (ss, stack)"
	case "images":
		return "Images (img, image)"
	case "objectstorage":
		return "Object Storage buckets (obj, buckets, s3)"
	case "databases":
		return "Managed Databases (db, dbs, dbaas)"
	case "events":
		return "Account events feed (ev, event)"
	case "watchlist":
		return "Synthetic view across all bookmarked rows (watch, starred)"
	case "fanout_instances":
		return "Linodes across all configured accounts"
	case "fanout_volumes":
		return "Volumes across all configured accounts"
	case "fanout_nodebalancers":
		return "NodeBalancers across all configured accounts"
	case "fanout_lke":
		return "LKE clusters across all configured accounts"
	case "fanout_firewalls":
		return "Firewalls across all configured accounts"
	case "fanout_domains":
		return "Domains across all configured accounts"
	default:
		return ""
	}
}
