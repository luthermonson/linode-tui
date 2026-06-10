package views

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
)

func init() {
	Register("instances", []string{"linodes", "inst", "li"}, newInstances)
}

func newInstances(d Deps) View {
	return newListView(listOpts[linodego.Instance]{
		Deps:  d,
		Title: "Linodes",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 28},
			{Title: "REGION", Width: 14},
			{Title: "TYPE", Width: 18},
			{Title: "STATUS", Width: 12},
			{Title: "IPv4", Width: 16},
			{Title: "TAGS", Width: 24},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Instance, error) {
			return c.Raw().ListInstances(ctx, nil)
		},
		Rower: func(it linodego.Instance) table.Row {
			ip := ""
			if len(it.IPv4) > 0 && it.IPv4[0] != nil {
				ip = it.IPv4[0].String()
			}
			return table.Row{
				strconv.Itoa(it.ID),
				it.Label,
				it.Region,
				it.Type,
				string(it.Status),
				ip,
				strings.Join(it.Tags, ","),
			}
		},
		Matcher: func(it linodego.Instance, needle string) bool {
			return containsAny(needle, it.Label, it.Region, it.Type, string(it.Status)) ||
				tagMatch(it.Tags, needle)
		},
		IDFn:         func(it linodego.Instance) string { return strconv.Itoa(it.ID) },
		BookmarkKind: "instances",
		OnEnter: func(it linodego.Instance, _ Deps) tea.Cmd {
			id := it.ID
			return func() tea.Msg {
				return NavigateMsg{
					Name:    "instance_detail",
					Context: map[string]any{"focus_id": id, "instance_id": id},
				}
			}
		},
		TagsFn:       func(it linodego.Instance) []string { return it.Tags },
		FieldFn: map[string]func(linodego.Instance) string{
			"region": func(it linodego.Instance) string { return it.Region },
			"type":   func(it linodego.Instance) string { return it.Type },
			"status": func(it linodego.Instance) string { return string(it.Status) },
			"label":  func(it linodego.Instance) string { return it.Label },
		},
		Sort: func(a, b linodego.Instance) int {
			return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
		},
		EditCmd: func(it linodego.Instance, _ Deps) tea.Cmd {
			return func() tea.Msg {
				return ConfigureLinodeMsg{Action: ConfigureEdit, ID: it.ID, Label: it.Label}
			}
		},
		Actions: []Action[linodego.Instance]{
			{
				Key:    "R",
				Label:  "reboot",
				Prompt: func(it linodego.Instance) string { return fmt.Sprintf("Reboot %s (id %d)?", it.Label, it.ID) },
				Run: func(ctx context.Context, c *linode.Client, it linodego.Instance) error {
					return c.Raw().RebootInstance(ctx, it.ID, linodego.InstanceRebootOptions{})
				},
			},
			{
				Key:    "b",
				Label:  "boot",
				Prompt: func(it linodego.Instance) string { return fmt.Sprintf("Boot %s (id %d)?", it.Label, it.ID) },
				Run: func(ctx context.Context, c *linode.Client, it linodego.Instance) error {
					return c.Raw().BootInstance(ctx, it.ID, linodego.InstanceBootOptions{})
				},
			},
			{
				Key:    "s",
				Label:  "shutdown",
				Prompt: func(it linodego.Instance) string { return fmt.Sprintf("Shut down %s (id %d)?", it.Label, it.ID) },
				Run: func(ctx context.Context, c *linode.Client, it linodego.Instance) error {
					return c.Raw().ShutdownInstance(ctx, it.ID)
				},
			},
			{
				Key:    "d",
				Label:  "delete",
				Prompt: func(it linodego.Instance) string { return fmt.Sprintf("DELETE %s (id %d)? This cannot be undone.", it.Label, it.ID) },
				Run: func(ctx context.Context, c *linode.Client, it linodego.Instance) error {
					return c.Raw().DeleteInstance(ctx, it.ID)
				},
			},
		},
		KeyHandlers: map[string]func(linodego.Instance, Deps) tea.Cmd{
			"e": configureKey(ConfigureEdit),
			"z": configureKey(ConfigureResize),
			"B": configureKey(ConfigureRebuild),
			"T": configureKey(ConfigureTags),
			// c = direct SSH to root@<public-ip> (configurable via tools.ssh)
			// C = lish console via Linode's bastion
			"c": openSSH,
			"C": openLish,
		},
		KeyHelp: map[string]string{
			"e": "edit label / tags (form)",
			"z": "resize to a new plan (form)",
			"B": "rebuild from an image (form)",
			"T": "edit tags (form)",
			"c": "direct ssh to public IPv4 (tools.ssh, default `ssh root@<ip>`)",
			"C": "lish console via Linode bastion (tools.lish)",
		},
	})
}

// openSSH drills into a direct ssh root@<public-ip> session using the
// `tools.ssh` exec template. Surfaces an inline error if the Linode has no
// public IPv4 (e.g., private-only fleets).
func openSSH(it linodego.Instance, _ Deps) tea.Cmd {
	return func() tea.Msg {
		ip := ""
		for _, v := range it.IPv4 {
			if v != nil {
				ip = v.String()
				break
			}
		}
		if ip == "" {
			return ErrorMsg{Err: fmt.Errorf("no public IPv4 on %s — try C for lish", it.Label)}
		}
		return DrillInMsg{
			Tool: "ssh",
			Vars: struct{ IP, Label string }{IP: ip, Label: it.Label},
		}
	}
}

func openLish(it linodego.Instance, d Deps) tea.Cmd {
	return func() tea.Msg {
		username := ""
		if d.Cfg != nil {
			if acct, ok := d.Cfg.Accounts[d.Cfg.DefaultAccount]; ok {
				username = acct.LishUsername
			}
		}
		if username == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			prof, err := d.Linode.Raw().GetProfile(ctx)
			if err != nil {
				return ErrorMsg{Err: fmt.Errorf("fetch profile (set accounts[%s].lish_username to skip): %w", d.Cfg.DefaultAccount, err)}
			}
			username = prof.Username
		}
		return DrillInMsg{
			Tool: "lish",
			Vars: struct{ Username, Region, Label string }{
				Username: username,
				Region:   it.Region,
				Label:    it.Label,
			},
		}
	}
}

func configureKey(action ConfigureLinodeAction) func(linodego.Instance, Deps) tea.Cmd {
	return func(it linodego.Instance, _ Deps) tea.Cmd {
		return func() tea.Msg {
			return ConfigureLinodeMsg{Action: action, ID: it.ID, Label: it.Label}
		}
	}
}

func containsAny(needle string, fields ...string) bool {
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), needle) {
			return true
		}
	}
	return false
}

func tagMatch(tags []string, needle string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), needle) {
			return true
		}
	}
	return false
}
