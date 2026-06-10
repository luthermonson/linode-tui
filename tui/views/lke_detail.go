package views

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
	"github.com/linode/tui/tools"
)

func init() {
	Register("lke_detail", []string{"cluster_detail", "k8s_detail"}, newLKEDetail)
}

type lkeDetailTab int

const (
	lkeTabSummary lkeDetailTab = iota
	lkeTabNodePools
	lkeTabEndpoints
	lkeTabActivity
	lkeTabSettings
)

var lkeDetailTabNames = []string{
	"summary", "nodepools", "endpoints", "activity", "settings",
}

type lkeDetailData struct {
	cluster   *linodego.LKECluster
	pools     []linodego.LKENodePool
	endpoints []linodego.LKEClusterAPIEndpoint
	events    []linodego.Event
	err       error
}

type lkeDetailLoadedMsg struct {
	id   uint64
	data lkeDetailData
}

type lkeDetailTickMsg struct{ id uint64 }

type lkeDetail struct {
	deps     Deps
	id       int
	tab      lkeDetailTab
	loading  bool
	data     lkeDetailData
	viewport viewport.Model
	w, h     int
	stamp    time.Time
	gen      uint64
}

var lkeDetailSeq atomic.Uint64

func newLKEDetail(d Deps) View {
	gen := lkeDetailSeq.Add(1)
	id, _ := d.CtxInt("focus_id")
	if id == 0 {
		id, _ = d.CtxInt("cluster_id")
	}
	return &lkeDetail{deps: d, id: id, viewport: viewport.New(80, 20), gen: gen}
}

func (m *lkeDetail) Title() string {
	if m.data.cluster != nil {
		return "LKE " + m.data.cluster.Label
	}
	return fmt.Sprintf("LKE #%d", m.id)
}

func (m *lkeDetail) Init() tea.Cmd {
	m.loading = true
	return tea.Batch(m.fetchCmd(), m.tickCmd())
}

func (m *lkeDetail) fetchCmd() tea.Cmd {
	id, client, gen := m.id, m.deps.Linode, m.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var data lkeDetailData
		raw := client.Raw()
		c, err := raw.GetLKECluster(ctx, id)
		if err != nil {
			return lkeDetailLoadedMsg{id: gen, data: lkeDetailData{err: err}}
		}
		data.cluster = c
		if pools, err := raw.ListLKENodePools(ctx, id, nil); err == nil {
			data.pools = pools
		}
		if eps, err := raw.ListLKEClusterAPIEndpoints(ctx, id, nil); err == nil {
			data.endpoints = eps
		}
		if evs, err := raw.ListEvents(ctx, nil); err == nil {
			data.events = filterEventsForLKE(evs, id)
		}
		return lkeDetailLoadedMsg{id: gen, data: data}
	}
}

func (m *lkeDetail) tickCmd() tea.Cmd {
	d := 5 * time.Second
	if m.deps.Cfg != nil && m.deps.Cfg.Refresh > 0 {
		d = m.deps.Cfg.Refresh
	}
	gen := m.gen
	return tea.Tick(d, func(time.Time) tea.Msg { return lkeDetailTickMsg{id: gen} })
}

func filterEventsForLKE(evs []linodego.Event, id int) []linodego.Event {
	out := make([]linodego.Event, 0, len(evs))
	for _, e := range evs {
		if e.Entity == nil || e.Entity.Type != "lkecluster" {
			continue
		}
		var entID int
		switch v := e.Entity.ID.(type) {
		case int:
			entID = v
		case float64:
			entID = int(v)
		case int64:
			entID = int(v)
		}
		if entID == id {
			out = append(out, e)
		}
	}
	return out
}

func (m *lkeDetail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 2
		m.renderBody()
		return m, nil
	case lkeDetailLoadedMsg:
		if msg.id != m.gen {
			return m, nil
		}
		m.loading = false
		m.data = msg.data
		m.stamp = time.Now()
		m.renderBody()
		return m, nil
	case lkeDetailTickMsg:
		if msg.id != m.gen {
			return m, nil
		}
		return m, tea.Batch(m.fetchCmd(), m.tickCmd())
	case DrillInMsg:
		// openLKECluster builds this with the kubeconfig path baked in;
		// hand it to the runner directly because the detail view is the
		// current pane (listView's DrillInMsg handler won't see it).
		return m, m.runDrillIn(msg)
	case lkeDetailDrillDoneMsg:
		// k9s exited; trigger a fresh fetch so cluster status reflects
		// anything that happened while we were detached.
		m.loading = true
		return m, m.fetchCmd()
	case tools.ExitMsg:
		if msg.Err != nil {
			m.data.err = fmt.Errorf("%s: %w", msg.Kind, msg.Err)
			m.renderBody()
		}
		return m, nil
	case ErrorMsg:
		m.data.err = msg.Err
		m.renderBody()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "[", "h", "left":
			m.tab = (m.tab + lkeDetailTab(len(lkeDetailTabNames)-1)) % lkeDetailTab(len(lkeDetailTabNames))
			m.viewport.GotoTop()
			m.renderBody()
			return m, nil
		case "]", "l", "right":
			m.tab = (m.tab + 1) % lkeDetailTab(len(lkeDetailTabNames))
			m.viewport.GotoTop()
			m.renderBody()
			return m, nil
		case "1", "2", "3", "4", "5":
			n := int(msg.String()[0] - '1')
			if n < len(lkeDetailTabNames) {
				m.tab = lkeDetailTab(n)
				m.viewport.GotoTop()
				m.renderBody()
			}
			return m, nil
		case "r", "ctrl+r":
			m.loading = true
			m.renderBody()
			return m, m.fetchCmd()
		case "c":
			// connect: fetch kubeconfig, write temp file, launch k9s.
			if m.data.cluster != nil {
				return m, openLKECluster(*m.data.cluster, m.deps)
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *lkeDetail) renderBody() {
	if m.data.err != nil {
		m.viewport.SetContent("error: " + m.data.err.Error())
		return
	}
	if m.loading && m.data.cluster == nil {
		m.viewport.SetContent("loading…")
		return
	}
	var body string
	switch m.tab {
	case lkeTabSummary:
		body = m.renderLKESummary()
	case lkeTabNodePools:
		body = m.renderLKENodePools()
	case lkeTabEndpoints:
		body = m.renderLKEEndpoints()
	case lkeTabActivity:
		body = m.renderLKEActivity()
	case lkeTabSettings:
		body = m.renderLKESettings()
	}
	m.viewport.SetContent(body)
}

func (m *lkeDetail) View() string {
	t := m.deps.Theme
	tabBar := m.renderLKETabBar()
	stamp := ""
	if !m.stamp.IsZero() {
		stamp = lipgloss.NewStyle().Foreground(t.Muted).Render(
			"  · refreshed " + humanAge(time.Since(m.stamp)) + " ago",
		)
	}
	header := tabBar + stamp
	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View())
}

func (m *lkeDetail) renderLKETabBar() string {
	t := m.deps.Theme
	active := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Underline(true)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	dim := lipgloss.NewStyle().Foreground(t.Text)
	pieces := make([]string, 0, len(lkeDetailTabNames))
	for i, name := range lkeDetailTabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if lkeDetailTab(i) == m.tab {
			pieces = append(pieces, active.Render(label))
		} else {
			pieces = append(pieces, dim.Render(label))
		}
	}
	return strings.Join(pieces, muted.Render("  "))
}

func (m *lkeDetail) renderLKESummary() string {
	c := m.data.cluster
	if c == nil {
		return "(no cluster loaded)"
	}
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "  %-18s %s\n", k+":", v) }
	row("Label", c.Label)
	row("ID", fmt.Sprintf("%d", c.ID))
	row("Status", string(c.Status))
	row("Region", c.Region)
	row("K8s Version", c.K8sVersion)
	if c.Tier != "" {
		row("Tier", c.Tier)
	}
	row("HA Control Plane", yesNo(c.ControlPlane.HighAvailability))
	if c.APLEnabled {
		row("APL Enabled", "yes")
	}
	if c.Created != nil && !c.Created.IsZero() {
		row("Created", c.Created.UTC().Format(time.RFC3339))
	}
	if c.Updated != nil && !c.Updated.IsZero() {
		row("Updated", c.Updated.UTC().Format(time.RFC3339))
	}
	if c.VpcID > 0 {
		row("VPC ID", fmt.Sprintf("%d", c.VpcID))
	}
	if c.SubnetID > 0 {
		row("Subnet ID", fmt.Sprintf("%d", c.SubnetID))
	}
	if len(c.Tags) > 0 {
		row("Tags", strings.Join(c.Tags, ", "))
	}

	totalNodes := 0
	for _, p := range m.data.pools {
		totalNodes += p.Count
	}
	if totalNodes > 0 {
		row("Total Nodes", fmt.Sprintf("%d across %d pool(s)", totalNodes, len(m.data.pools)))
	}
	b.WriteString("\n  Press  c  to launch k9s against this cluster (fetches kubeconfig).\n")
	return b.String()
}

func (m *lkeDetail) renderLKENodePools() string {
	if len(m.data.pools) == 0 {
		return "  (no node pools)\n"
	}
	var b strings.Builder
	for _, p := range m.data.pools {
		label := ""
		if p.Label != nil {
			label = *p.Label
		}
		title := fmt.Sprintf("Pool [%d]", p.ID)
		if label != "" {
			title += " " + label
		}
		fmt.Fprintf(&b, "  %s\n", title)
		fmt.Fprintf(&b, "    Type:        %s\n", p.Type)
		fmt.Fprintf(&b, "    Count:       %d\n", p.Count)
		if len(p.Tags) > 0 {
			fmt.Fprintf(&b, "    Tags:        %s\n", strings.Join(p.Tags, ", "))
		}
		if p.K8sVersion != nil && *p.K8sVersion != "" {
			fmt.Fprintf(&b, "    K8s:         %s\n", *p.K8sVersion)
		}
		// Autoscaler may be zero-valued (default disabled). Show only when enabled.
		if p.Autoscaler.Enabled {
			fmt.Fprintf(&b, "    Autoscaler:  enabled, min=%d max=%d\n", p.Autoscaler.Min, p.Autoscaler.Max)
		}
		if p.FirewallID != nil && *p.FirewallID > 0 {
			fmt.Fprintf(&b, "    Firewall:    %d\n", *p.FirewallID)
		}
		if len(p.Taints) > 0 {
			b.WriteString("    Taints:\n")
			for _, t := range p.Taints {
				fmt.Fprintf(&b, "      %s=%s:%s\n", t.Key, t.Value, t.Effect)
			}
		}
		if labels := lkeLabelsMap(p.Labels); len(labels) > 0 {
			b.WriteString("    Labels:\n")
			for k, v := range labels {
				fmt.Fprintf(&b, "      %s=%s\n", k, v)
			}
		}
		if len(p.Linodes) > 0 {
			b.WriteString("    Nodes:\n")
			for _, n := range p.Linodes {
				fmt.Fprintf(&b, "      %-22s instance=%d  %s\n", n.ID, n.InstanceID, n.Status)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// lkeLabelsMap turns the LKENodePoolLabels type (which is a string→string
// map under the hood) into a regular map for iteration. Works whether the
// underlying type is map[string]string or a named alias.
func lkeLabelsMap(l linodego.LKENodePoolLabels) map[string]string {
	out := map[string]string{}
	for k, v := range l {
		out[k] = v
	}
	return out
}

func (m *lkeDetail) renderLKEEndpoints() string {
	if len(m.data.endpoints) == 0 {
		return "  (no API endpoints reported — cluster may still be provisioning)\n"
	}
	var b strings.Builder
	for _, e := range m.data.endpoints {
		fmt.Fprintf(&b, "  %s\n", e.Endpoint)
	}
	return b.String()
}

func (m *lkeDetail) renderLKEActivity() string {
	if len(m.data.events) == 0 {
		return "  (no recent events touching this cluster)\n"
	}
	var b strings.Builder
	for _, e := range m.data.events {
		stamp := ""
		if e.Created != nil && !e.Created.IsZero() {
			stamp = e.Created.UTC().Format("01-02 15:04:05")
		}
		fmt.Fprintf(&b, "  %s  %-30s  %s  %s\n", stamp, e.Action, e.Status, e.Username)
		if e.Message != "" {
			fmt.Fprintf(&b, "      %s\n", e.Message)
		}
	}
	return b.String()
}

func (m *lkeDetail) renderLKESettings() string {
	c := m.data.cluster
	if c == nil {
		return "(no cluster loaded)"
	}
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "  %-22s %s\n", k+":", v) }
	row("Label", c.Label)
	row("HA Control Plane", yesNo(c.ControlPlane.HighAvailability))
	row("K8s Version", c.K8sVersion)
	if c.Tier != "" {
		row("Tier", c.Tier)
	}
	row("Tags", strings.Join(c.Tags, ", "))
	b.WriteString("\n  Edit via Cloud Manager or `linode-cli lke cluster-update <id> --label=… --tags=…`.\n")
	return b.String()
}

func (m *lkeDetail) SelectedID() string {
	if m.data.cluster != nil {
		return fmt.Sprintf("%d", m.data.cluster.ID)
	}
	return fmt.Sprintf("%d", m.id)
}

func (m *lkeDetail) Help() []HelpEntry {
	return []HelpEntry{
		{Key: "1–5", Desc: "jump to tab by number"},
		{Key: "← / → · [ / ] · h / l", Desc: "previous / next tab"},
		{Key: "c", Desc: "connect: launch k9s against this cluster"},
		{Key: "r · ctrl+r", Desc: "refresh data"},
		{Key: "esc · ctrl+b", Desc: "back to the list"},
		{Key: "↑/↓ · pgup/pgdn", Desc: "scroll within a tab"},
	}
}

// runDrillIn invokes the tools runner for a DrillInMsg arriving while the
// detail view is the active pane. Mirrors listView.drillIn but stays
// self-contained so we don't depend on listView semantics. Uses
// context.Background() so the wrapped exec.Cmd doesn't die before tea
// invokes it.
func (m *lkeDetail) runDrillIn(msg DrillInMsg) tea.Cmd {
	runner := tools.New(m.deps.Cfg)
	exec, err := runner.RunWithEnv(context.Background(), msg.Tool, msg.Vars, msg.Env)
	if err != nil {
		if msg.Cleanup != nil {
			msg.Cleanup()
		}
		m.data.err = err
		m.renderBody()
		return nil
	}
	cleanup := msg.Cleanup
	return tea.Sequence(exec, func() tea.Msg {
		if cleanup != nil {
			cleanup()
		}
		return lkeDetailDrillDoneMsg{}
	})
}

type lkeDetailDrillDoneMsg struct{}

// Ensure linode pkg is referenced so future helpers (if added) compile.
var _ = linode.NewClient
