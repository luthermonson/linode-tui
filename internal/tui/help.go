package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/linode/tui/internal/tui/theme"
	"github.com/linode/tui/internal/tui/views"
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
	{Key: ":<resource>", Desc: "switch view (e.g. :linodes, :databases, :lke)"},
	{Key: ":theme <name>", Desc: "switch theme: dark | light | dracula | solarized-light"},
	{Key: ":account [name]", Desc: "list accounts or switch"},
	{Key: ":tools upgrade", Desc: "re-install pinned k9s/lazysql"},
	{Key: ":tools relocate <dir>", Desc: "move binaries + persist install dir"},
	{Key: ":tools dir", Desc: "show current install dir"},
	{Key: ":new linode", Desc: "create a new Linode"},
}
