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
)

func init() {
	Register("instance_detail", []string{"linode_detail", "instance-detail"}, newInstanceDetail)
}

// instanceDetailTab is a section of the Cloud Manager-style detail view.
type instanceDetailTab int

const (
	tabSummary instanceDetailTab = iota
	tabMetrics
	tabSSH
	tabNetwork
	tabStorage
	tabConfigs
	tabBackups
	tabActivity
	tabAlerts
	tabSettings
)

var instanceDetailTabNames = []string{
	"summary", "metrics", "ssh", "network", "storage", "configs", "backups", "activity", "alerts", "settings",
}

// instanceDetailData buckets everything we've fetched. Each section pulls
// its own slice from this struct; nil pointers mean "not loaded yet".
type instanceDetailData struct {
	instance  *linodego.Instance
	ips       *linodego.InstanceIPAddressResponse
	configs   []linodego.InstanceConfig
	disks     []linodego.InstanceDisk
	volumes   []linodego.Volume
	firewalls []linodego.Firewall
	backups   *linodego.InstanceBackupsResponse
	events    []linodego.Event
	stats     *linodego.InstanceStats
	err       error
}

type instanceDetailLoadedMsg struct {
	id   uint64
	data instanceDetailData
}

type instanceDetailTickMsg struct{ id uint64 }

type instanceDetail struct {
	deps     Deps
	id       int
	tab      instanceDetailTab
	loading  bool
	data     instanceDetailData
	viewport viewport.Model
	w, h     int
	stamp    time.Time
	gen      uint64
}

var detailSeq atomic.Uint64

func newInstanceDetail(d Deps) View {
	gen := detailSeq.Add(1)
	id, _ := d.CtxInt("focus_id")
	if id == 0 {
		id, _ = d.CtxInt("instance_id")
	}
	vp := viewport.New(80, 20)
	return &instanceDetail{deps: d, id: id, viewport: vp, gen: gen}
}

func (m *instanceDetail) Title() string {
	if m.data.instance != nil {
		return fmt.Sprintf("Linode %s", m.data.instance.Label)
	}
	return fmt.Sprintf("Linode #%d", m.id)
}

func (m *instanceDetail) Init() tea.Cmd {
	m.loading = true
	return tea.Batch(m.fetchCmd(), m.tickCmd())
}

func (m *instanceDetail) fetchCmd() tea.Cmd {
	id, client, gen := m.id, m.deps.Linode, m.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var data instanceDetailData
		raw := client.Raw()
		inst, err := raw.GetInstance(ctx, id)
		if err != nil {
			return instanceDetailLoadedMsg{id: gen, data: instanceDetailData{err: err}}
		}
		data.instance = inst
		// The rest are best-effort; one failure doesn't blank the screen.
		if ips, err := raw.GetInstanceIPAddresses(ctx, id); err == nil {
			data.ips = ips
		}
		if cs, err := raw.ListInstanceConfigs(ctx, id, nil); err == nil {
			data.configs = cs
		}
		if ds, err := raw.ListInstanceDisks(ctx, id, nil); err == nil {
			data.disks = ds
		}
		if vs, err := raw.ListInstanceVolumes(ctx, id, nil); err == nil {
			data.volumes = vs
		}
		if fws, err := raw.ListInstanceFirewalls(ctx, id, nil); err == nil {
			data.firewalls = fws
		}
		if bks, err := raw.GetInstanceBackups(ctx, id); err == nil {
			data.backups = bks
		}
		// Stats can 400 if the Linode hasn't been running long enough; treat
		// any error as "no data" rather than propagating.
		if stats, err := raw.GetInstanceStats(ctx, id); err == nil {
			data.stats = stats
		}
		if evs, err := raw.ListEvents(ctx, nil); err == nil {
			// Filter to events targeting this Linode. ListEvents doesn't
			// take a server-side entity filter through linodego (it would
			// need a JSON-encoded X-Filter header), so we filter client-side.
			data.events = filterEventsForLinode(evs, id)
		}
		return instanceDetailLoadedMsg{id: gen, data: data}
	}
}

func (m *instanceDetail) tickCmd() tea.Cmd {
	d := 5 * time.Second
	if m.deps.Cfg != nil && m.deps.Cfg.Refresh > 0 {
		d = m.deps.Cfg.Refresh
	}
	gen := m.gen
	return tea.Tick(d, func(time.Time) tea.Msg { return instanceDetailTickMsg{id: gen} })
}

func filterEventsForLinode(evs []linodego.Event, id int) []linodego.Event {
	out := make([]linodego.Event, 0, len(evs))
	for _, e := range evs {
		if e.Entity == nil {
			continue
		}
		// e.Entity.ID is `any` for linodego.EventEntity; coerce to int via
		// json's float64 typical decode.
		var entID int
		switch v := e.Entity.ID.(type) {
		case int:
			entID = v
		case float64:
			entID = int(v)
		case int64:
			entID = int(v)
		}
		if entID == id && e.Entity.Type == "linode" {
			out = append(out, e)
		}
	}
	return out
}

func (m *instanceDetail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.viewport.Width = msg.Width
		// Tab bar takes one row; leave the rest for content.
		m.viewport.Height = msg.Height - 2
		m.renderBody()
		return m, nil
	case instanceDetailLoadedMsg:
		if msg.id != m.gen {
			return m, nil
		}
		m.loading = false
		m.data = msg.data
		m.stamp = time.Now()
		m.renderBody()
		return m, nil
	case instanceDetailTickMsg:
		if msg.id != m.gen {
			return m, nil
		}
		return m, tea.Batch(m.fetchCmd(), m.tickCmd())
	case tea.KeyMsg:
		switch msg.String() {
		case "[", "h", "left":
			m.tab = (m.tab + instanceDetailTab(len(instanceDetailTabNames)-1)) % instanceDetailTab(len(instanceDetailTabNames))
			m.viewport.GotoTop()
			m.renderBody()
			return m, nil
		case "]", "l", "right":
			m.tab = (m.tab + 1) % instanceDetailTab(len(instanceDetailTabNames))
			m.viewport.GotoTop()
			m.renderBody()
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			n := int(msg.String()[0] - '1')
			if n < len(instanceDetailTabNames) {
				m.tab = instanceDetailTab(n)
				m.viewport.GotoTop()
				m.renderBody()
			}
			return m, nil
		case "r", "ctrl+r":
			m.loading = true
			m.renderBody()
			return m, m.fetchCmd()
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *instanceDetail) renderBody() {
	if m.data.err != nil {
		m.viewport.SetContent("error: " + m.data.err.Error())
		return
	}
	if m.loading && m.data.instance == nil {
		m.viewport.SetContent("loading…")
		return
	}
	var body string
	switch m.tab {
	case tabSummary:
		body = m.renderSummary()
	case tabMetrics:
		body = m.renderMetrics()
	case tabSSH:
		body = m.renderSSH()
	case tabNetwork:
		body = m.renderNetwork()
	case tabStorage:
		body = m.renderStorage()
	case tabConfigs:
		body = m.renderConfigs()
	case tabBackups:
		body = m.renderBackups()
	case tabActivity:
		body = m.renderActivity()
	case tabAlerts:
		body = m.renderAlerts()
	case tabSettings:
		body = m.renderSettings()
	}
	m.viewport.SetContent(body)
}

func (m *instanceDetail) View() string {
	t := m.deps.Theme
	tabBar := m.renderTabBar()
	stamp := ""
	if !m.stamp.IsZero() {
		stamp = lipgloss.NewStyle().Foreground(t.Muted).Render(
			"  · refreshed " + humanAge(time.Since(m.stamp)) + " ago",
		)
	}
	header := tabBar + stamp
	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View())
}

func (m *instanceDetail) renderTabBar() string {
	t := m.deps.Theme
	active := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Underline(true)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	dim := lipgloss.NewStyle().Foreground(t.Text)
	pieces := make([]string, 0, len(instanceDetailTabNames))
	for i, name := range instanceDetailTabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if instanceDetailTab(i) == m.tab {
			pieces = append(pieces, active.Render(label))
		} else {
			pieces = append(pieces, dim.Render(label))
		}
	}
	return strings.Join(pieces, muted.Render("  "))
}

// --- Section renderers ----------------------------------------------------

func (m *instanceDetail) renderSummary() string {
	inst := m.data.instance
	if inst == nil {
		return "(no instance loaded)"
	}
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "  %-14s %s\n", k+":", v) }
	row("Label", inst.Label)
	row("ID", fmt.Sprintf("%d", inst.ID))
	row("Status", string(inst.Status))
	row("Region", inst.Region)
	row("Type", inst.Type)
	row("Image", inst.Image)
	row("Hypervisor", inst.Hypervisor)
	if inst.HostUUID != "" {
		row("Host UUID", inst.HostUUID)
	}
	if inst.Created != nil && !inst.Created.IsZero() {
		row("Created", inst.Created.UTC().Format(time.RFC3339))
	}
	if inst.Updated != nil && !inst.Updated.IsZero() {
		row("Updated", inst.Updated.UTC().Format(time.RFC3339))
	}
	if len(inst.Tags) > 0 {
		row("Tags", strings.Join(inst.Tags, ", "))
	}
	if specs := inst.Specs; specs != nil {
		row("vCPUs", fmt.Sprintf("%d", specs.VCPUs))
		row("RAM", fmt.Sprintf("%d MB", specs.Memory))
		row("Disk", fmt.Sprintf("%d MB", specs.Disk))
		row("Transfer", fmt.Sprintf("%d GB", specs.Transfer))
		if specs.GPUs > 0 {
			row("GPUs", fmt.Sprintf("%d", specs.GPUs))
		}
	}
	if len(inst.IPv4) > 0 {
		ips := make([]string, 0, len(inst.IPv4))
		for _, ip := range inst.IPv4 {
			if ip != nil {
				ips = append(ips, ip.String())
			}
		}
		row("IPv4", strings.Join(ips, ", "))
	}
	if inst.IPv6 != "" {
		row("IPv6", inst.IPv6)
	}
	return b.String()
}

func (m *instanceDetail) renderSSH() string {
	inst := m.data.instance
	if inst == nil {
		return "(no instance loaded)"
	}
	var b strings.Builder
	primary := ""
	if len(inst.IPv4) > 0 && inst.IPv4[0] != nil {
		primary = inst.IPv4[0].String()
	}
	b.WriteString("  Public IPv4:\n")
	for _, ip := range inst.IPv4 {
		if ip == nil {
			continue
		}
		fmt.Fprintf(&b, "    %s\n", ip.String())
	}
	if inst.IPv6 != "" {
		fmt.Fprintf(&b, "\n  IPv6:\n    %s\n", inst.IPv6)
	}
	if primary != "" {
		fmt.Fprintf(&b, "\n  Direct SSH (assumes a user is provisioned):\n    ssh root@%s\n", primary)
	}
	fmt.Fprintf(&b, "\n  Lish console (Linode-routed, key-authenticated):\n    ssh -t %s@lish-%s.linode.com %s\n",
		m.lishUsername(), inst.Region, inst.Label)
	b.WriteString("\n  From the list view:\n")
	b.WriteString("    c  → direct SSH to the public IPv4 (uses tools.ssh)\n")
	b.WriteString("    C  → lish console (uses tools.lish)\n")
	return b.String()
}

func (m *instanceDetail) lishUsername() string {
	if m.deps.Cfg != nil {
		if acct, ok := m.deps.Cfg.Accounts[m.deps.Cfg.DefaultAccount]; ok && acct.LishUsername != "" {
			return acct.LishUsername
		}
	}
	return "<username>"
}

func (m *instanceDetail) renderNetwork() string {
	var b strings.Builder
	b.WriteString("  IP Addresses\n  " + strings.Repeat("─", 40) + "\n")
	if m.data.ips != nil {
		if v4 := m.data.ips.IPv4; v4 != nil {
			if len(v4.Public) > 0 {
				b.WriteString("  Public IPv4:\n")
				for _, ip := range v4.Public {
					fmt.Fprintf(&b, "    %-18s rDNS=%s gateway=%s\n", ip.Address, ip.RDNS, ip.Gateway)
				}
			}
			if len(v4.Private) > 0 {
				b.WriteString("  Private IPv4:\n")
				for _, ip := range v4.Private {
					fmt.Fprintf(&b, "    %-18s\n", ip.Address)
				}
			}
			if len(v4.Shared) > 0 {
				b.WriteString("  Shared IPv4:\n")
				for _, ip := range v4.Shared {
					fmt.Fprintf(&b, "    %-18s\n", ip.Address)
				}
			}
			if len(v4.Reserved) > 0 {
				b.WriteString("  Reserved IPv4:\n")
				for _, ip := range v4.Reserved {
					fmt.Fprintf(&b, "    %-18s\n", ip.Address)
				}
			}
		}
		if v6 := m.data.ips.IPv6; v6 != nil {
			b.WriteString("\n  IPv6:\n")
			if v6.SLAAC != nil {
				fmt.Fprintf(&b, "    SLAAC     %s\n", v6.SLAAC.Address)
			}
			if v6.LinkLocal != nil {
				fmt.Fprintf(&b, "    LinkLocal %s\n", v6.LinkLocal.Address)
			}
			for _, g := range v6.Global {
				fmt.Fprintf(&b, "    Global    %s/%d region=%s\n", g.Range, g.Prefix, g.Region)
			}
		}
	} else {
		b.WriteString("  (no IP data — set LINODE_TOKEN with ips:read_only scope)\n")
	}

	b.WriteString("\n  Firewalls\n  " + strings.Repeat("─", 40) + "\n")
	if len(m.data.firewalls) == 0 {
		b.WriteString("  (none attached)\n")
	} else {
		for _, fw := range m.data.firewalls {
			fmt.Fprintf(&b, "    [%d] %s · %s\n", fw.ID, fw.Label, fw.Status)
		}
	}
	return b.String()
}

func (m *instanceDetail) renderStorage() string {
	var b strings.Builder
	b.WriteString("  Disks\n  " + strings.Repeat("─", 40) + "\n")
	if len(m.data.disks) == 0 {
		b.WriteString("  (no disks)\n")
	} else {
		for _, d := range m.data.disks {
			fmt.Fprintf(&b, "    [%d] %-24s %-8s %6d MB %s\n",
				d.ID, d.Label, d.Filesystem, d.Size, d.Status)
		}
	}
	b.WriteString("\n  Block Storage Volumes\n  " + strings.Repeat("─", 40) + "\n")
	if len(m.data.volumes) == 0 {
		b.WriteString("  (no volumes attached)\n")
	} else {
		for _, v := range m.data.volumes {
			fmt.Fprintf(&b, "    [%d] %-24s %6d GB %s\n", v.ID, v.Label, v.Size, v.Status)
		}
	}
	return b.String()
}

func (m *instanceDetail) renderConfigs() string {
	var b strings.Builder
	if len(m.data.configs) == 0 {
		return "  (no boot configs)\n"
	}
	for _, c := range m.data.configs {
		fmt.Fprintf(&b, "  [%d] %s\n", c.ID, c.Label)
		fmt.Fprintf(&b, "    Kernel:        %s\n", c.Kernel)
		fmt.Fprintf(&b, "    Root Device:   %s\n", c.RootDevice)
		fmt.Fprintf(&b, "    Run Level:     %s\n", c.RunLevel)
		fmt.Fprintf(&b, "    Virt Mode:     %s\n", c.VirtMode)
		fmt.Fprintf(&b, "    Memory Limit:  %d MB\n", c.MemoryLimit)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *instanceDetail) renderBackups() string {
	bks := m.data.backups
	if bks == nil {
		return "  (no backup data — check `backups:read_only` scope)\n"
	}
	var b strings.Builder
	if bks.Automatic != nil && len(bks.Automatic) > 0 {
		b.WriteString("  Automatic Backups\n  " + strings.Repeat("─", 40) + "\n")
		for _, bk := range bks.Automatic {
			created := ""
			if bk.Created != nil {
				created = bk.Created.UTC().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(&b, "    [%d] %s  %s  type=%s  %s\n", bk.ID, created, bk.Status, bk.Type, bk.Label)
		}
	}
	if bks.Snapshot != nil {
		b.WriteString("\n  Snapshots\n  " + strings.Repeat("─", 40) + "\n")
		if bks.Snapshot.Current != nil {
			cur := bks.Snapshot.Current
			created := ""
			if cur.Created != nil {
				created = cur.Created.UTC().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(&b, "    Current:    [%d] %s  %s  %s\n", cur.ID, created, cur.Status, cur.Label)
		}
		if bks.Snapshot.InProgress != nil {
			ip := bks.Snapshot.InProgress
			created := ""
			if ip.Created != nil {
				created = ip.Created.UTC().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(&b, "    In Progress: [%d] %s  %s  %s\n", ip.ID, created, ip.Status, ip.Label)
		}
	}
	if b.Len() == 0 {
		return "  (no backups taken; enable with `linode-cli linodes backups-enable <id>`)\n"
	}
	return b.String()
}

func (m *instanceDetail) renderActivity() string {
	if len(m.data.events) == 0 {
		return "  (no recent events touching this Linode)\n"
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

func (m *instanceDetail) renderAlerts() string {
	inst := m.data.instance
	if inst == nil || inst.Alerts == nil {
		return "  (no alert thresholds set)\n"
	}
	a := inst.Alerts
	var b strings.Builder
	b.WriteString("  Alert thresholds (set to 0 to disable)\n  " + strings.Repeat("─", 40) + "\n")
	fmt.Fprintf(&b, "    CPU:                 %d%%\n", a.CPU)
	fmt.Fprintf(&b, "    Network In:          %d Mbit/s\n", a.NetworkIn)
	fmt.Fprintf(&b, "    Network Out:         %d Mbit/s\n", a.NetworkOut)
	fmt.Fprintf(&b, "    Transfer Quota:      %d%%\n", a.TransferQuota)
	fmt.Fprintf(&b, "    IO Rate:             %d IOPS\n", a.IO)
	b.WriteString("\n  Edit thresholds in Cloud Manager → Settings, or via `linode-cli linodes update <id> --alerts.*`.\n")
	return b.String()
}

func (m *instanceDetail) renderSettings() string {
	inst := m.data.instance
	if inst == nil {
		return "(no instance loaded)"
	}
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "  %-22s %s\n", k+":", v) }
	row("Label", inst.Label)
	row("Group (deprecated)", inst.Group)
	row("WatchdogEnabled", fmt.Sprintf("%v", inst.WatchdogEnabled))
	switch {
	case inst.Backups != nil && inst.Backups.Enabled:
		row("Backups", "enabled (automatic)")
	default:
		row("Backups", "disabled")
	}
	if inst.PlacementGroup != nil {
		row("PlacementGroup", fmt.Sprintf("[%d] %s", inst.PlacementGroup.ID, inst.PlacementGroup.Label))
	}
	row("Tags", strings.Join(inst.Tags, ", "))
	b.WriteString("\n  Use `e` on the list view to edit label/tags · `z` resize · `B` rebuild · `T` tags.\n")
	return b.String()
}

// Identifiable so split-preview etc. work.
func (m *instanceDetail) SelectedID() string {
	if m.data.instance != nil {
		return fmt.Sprintf("%d", m.data.instance.ID)
	}
	return fmt.Sprintf("%d", m.id)
}

// Helper guarantees inline use of the deps even if disabled at startup.
var _ = linode.NewClient

// Help implements Helper.
func (m *instanceDetail) Help() []HelpEntry {
	return []HelpEntry{
		{Key: "1–9", Desc: "jump to tab by number"},
		{Key: "← / → · [ / ] · h / l", Desc: "previous / next tab"},
		{Key: "r · ctrl+r", Desc: "refresh data"},
		{Key: "esc · ctrl+b", Desc: "back to the list (uses the view stack)"},
		{Key: "↑/↓ · pgup/pgdn", Desc: "scroll within a tab"},
	}
}

// renderMetrics shows API-derived time series for CPU, network, and IO.
// Charts are 4-row unicode block bar charts arranged two per row. Each
// metric also shows current / min / max / avg values. The Linode stats
// endpoint returns 24 hours of data at 5-minute resolution (~288 points).
func (m *instanceDetail) renderMetrics() string {
	if m.data.stats == nil {
		return "  (no metrics — endpoint returns data only after the Linode has been running for a few minutes)\n"
	}
	d := m.data.stats.Data
	t := m.deps.Theme
	accent := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	chartStyle := lipgloss.NewStyle().Foreground(t.Accent)

	// Two charts per row; each chart occupies ~half the viewport with a
	// 2-column gutter between.
	const chartRows = 4
	colWidth := (m.viewport.Width - 4 - 2) / 2 // 4 for indent, 2 for gap
	if colWidth < 20 {
		colWidth = 20
	}
	if colWidth > 90 {
		colWidth = 90
	}
	chartWidth := colWidth - 2 // 2-cell indent inside each column

	type metric struct {
		label  string
		points [][]float64
		unit   string
		scale  float64
	}
	metrics := []metric{
		{"CPU", d.CPU, "%", 1.0},
		{"Network IPv4 In", d.NetV4.In, "Mbit/s", 1.0 / 1_000_000},
		{"Network IPv4 Out", d.NetV4.Out, "Mbit/s", 1.0 / 1_000_000},
		{"Block IO", d.IO.IO, "blocks/s", 1.0},
	}
	if hasData(d.NetV4.PrivateIn) || hasData(d.NetV4.PrivateOut) {
		metrics = append(metrics,
			metric{"Private IPv4 In", d.NetV4.PrivateIn, "Mbit/s", 1.0 / 1_000_000},
			metric{"Private IPv4 Out", d.NetV4.PrivateOut, "Mbit/s", 1.0 / 1_000_000},
		)
	}
	if hasData(d.IO.Swap) {
		metrics = append(metrics, metric{"Swap IO", d.IO.Swap, "blocks/s", 1.0})
	}

	renderOne := func(mm metric) string {
		values := make([]float64, 0, len(mm.points))
		for _, pt := range mm.points {
			if len(pt) < 2 {
				continue
			}
			values = append(values, pt[1]*mm.scale)
		}
		var chart, footer string
		if len(values) == 0 {
			chart = muted.Render(strings.Repeat(" ", chartWidth))
			for i := 1; i < chartRows; i++ {
				chart += "\n" + muted.Render(strings.Repeat(" ", chartWidth))
			}
			footer = muted.Render("(no data)")
		} else {
			chart = chartStyle.Render(renderBarChart(values, chartWidth, chartRows))
			minV, maxV, cur, avg := stats(values)
			footer = muted.Render(fmt.Sprintf(
				"now %.2f %s · min %.2f · max %.2f · avg %.2f",
				cur, mm.unit, minV, maxV, avg,
			))
		}
		header := accent.Render(mm.label)
		body := header + "\n" + chart + "\n" + footer
		return lipgloss.NewStyle().Width(colWidth).Render(body)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", muted.Render(m.data.stats.Title))
	gap := lipgloss.NewStyle().Width(2).Render(" ")
	for i := 0; i < len(metrics); i += 2 {
		left := renderOne(metrics[i])
		var pair string
		if i+1 < len(metrics) {
			right := renderOne(metrics[i+1])
			pair = lipgloss.JoinHorizontal(lipgloss.Top, left, gap, right)
		} else {
			pair = left
		}
		// Indent the whole row two columns.
		indented := lipgloss.NewStyle().PaddingLeft(2).Render(pair)
		b.WriteString("\n" + indented + "\n")
	}
	return b.String()
}

func hasData(points [][]float64) bool {
	for _, p := range points {
		if len(p) >= 2 && p[1] > 0 {
			return true
		}
	}
	return false
}

// renderBarChart returns a `rows`-tall × `width`-wide unicode block bar
// chart. Each column uses 8 sub-row levels (8 partial-block characters)
// for a total of rows*8 vertical resolution. Baseline is 0 so bar height
// reflects absolute value; the top of the tallest bar equals `rows`.
func renderBarChart(values []float64, width, rows int) string {
	if len(values) == 0 || width <= 0 || rows <= 0 {
		return ""
	}
	buckets := bucketValues(values, width)
	var maxV float64
	for _, v := range buckets {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		// Flat baseline at the bottom row.
		base := strings.Repeat("▁", width)
		gap := strings.Repeat(" ", width)
		out := make([]string, rows)
		for i := 0; i < rows-1; i++ {
			out[i] = gap
		}
		out[rows-1] = base
		return strings.Join(out, "\n")
	}
	const subPerRow = 8
	totalSteps := rows * subPerRow
	partial := []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇'}
	out := make([]string, rows)
	cols := make([]int, width)
	for i, v := range buckets {
		h := int((v / maxV) * float64(totalSteps))
		if h < 0 {
			h = 0
		}
		if h > totalSteps {
			h = totalSteps
		}
		cols[i] = h
	}
	for r := 0; r < rows; r++ {
		// Distance from this row's bottom up: bottom row covers steps 0..8,
		// next row covers 8..16, etc.
		rowFromBottom := rows - 1 - r
		base := rowFromBottom * subPerRow
		var line strings.Builder
		line.Grow(width * 3)
		for _, h := range cols {
			cell := h - base
			switch {
			case cell <= 0:
				line.WriteRune(' ')
			case cell >= subPerRow:
				line.WriteRune('█')
			default:
				line.WriteRune(partial[cell])
			}
		}
		out[r] = line.String()
	}
	return strings.Join(out, "\n")
}

// bucketValues stretches/compresses a series of values into exactly `width`
// cells. If there are fewer samples than cells, the values pad the right
// edge (most recent samples land at the right). Otherwise each cell is the
// mean of its assigned input bucket.
func bucketValues(values []float64, width int) []float64 {
	out := make([]float64, width)
	if len(values) == 0 || width == 0 {
		return out
	}
	if len(values) <= width {
		offset := width - len(values)
		for i, v := range values {
			out[offset+i] = v
		}
		return out
	}
	samplesPerCell := float64(len(values)) / float64(width)
	for cell := 0; cell < width; cell++ {
		lo := int(float64(cell) * samplesPerCell)
		hi := int(float64(cell+1) * samplesPerCell)
		if hi > len(values) {
			hi = len(values)
		}
		if lo >= hi {
			out[cell] = values[lo]
			continue
		}
		var sum float64
		for _, v := range values[lo:hi] {
			sum += v
		}
		out[cell] = sum / float64(hi-lo)
	}
	return out
}

// renderSparkline turns a series of values into a single line of unicode
// block characters. Width controls how many cells to render — values are
// bucketed if there are more samples than cells. Baseline is 0 so the
// height of each bar corresponds to the absolute value (not min-max scale).
func renderSparkline(values []float64, width int) string {
	if len(values) == 0 || width <= 0 {
		return ""
	}
	// Bucket to width cells. Each cell is the mean of its bucket.
	buckets := make([]float64, width)
	if len(values) <= width {
		// Stretch: pad the left with zeros so the recent samples land on
		// the right edge (most readable).
		offset := width - len(values)
		for i, v := range values {
			buckets[offset+i] = v
		}
	} else {
		samplesPerCell := float64(len(values)) / float64(width)
		for cell := 0; cell < width; cell++ {
			lo := int(float64(cell) * samplesPerCell)
			hi := int(float64(cell+1) * samplesPerCell)
			if hi > len(values) {
				hi = len(values)
			}
			if lo >= hi {
				buckets[cell] = values[lo]
				continue
			}
			var sum float64
			for _, v := range values[lo:hi] {
				sum += v
			}
			buckets[cell] = sum / float64(hi-lo)
		}
	}
	var maxV float64
	for _, v := range buckets {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		// All zeros — render a flat baseline.
		return strings.Repeat("▁", width)
	}
	// 8-level unicode block ramp.
	levels := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var b strings.Builder
	for _, v := range buckets {
		idx := int((v / maxV) * float64(len(levels)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(levels) {
			idx = len(levels) - 1
		}
		b.WriteRune(levels[idx])
	}
	return b.String()
}

func stats(values []float64) (minV, maxV, cur, avg float64) {
	if len(values) == 0 {
		return 0, 0, 0, 0
	}
	minV = values[0]
	maxV = values[0]
	var sum float64
	for _, v := range values {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
		sum += v
	}
	avg = sum / float64(len(values))
	cur = values[len(values)-1]
	return
}

// humanAge formats a short relative age string.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
