package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/linode/linodego"

	"github.com/linode/tui/internal/linode"
)

type createLKE struct {
	client *linode.Client

	phase createPhase
	err   error

	regions  []linodego.Region
	types    []linodego.LinodeType
	versions []linodego.LKEVersion
	gotReg   bool
	gotTyp   bool
	gotVer   bool

	form       *huh.Form
	label      string
	region     string
	k8sVersion string
	poolType   string
	poolCount  string

	result *linodego.LKECluster
}

type clkeRegionsMsg struct {
	items []linodego.Region
	err   error
}
type clkeTypesMsg struct {
	items []linodego.LinodeType
	err   error
}
type clkeVersionsMsg struct {
	items []linodego.LKEVersion
	err   error
}
type clkeSubmittedMsg struct {
	result *linodego.LKECluster
	err    error
}

func newCreateLKE(client *linode.Client) *createLKE {
	return &createLKE{client: client, phase: createPhaseLoading, poolCount: "3"}
}

func (m *createLKE) Init() tea.Cmd {
	c := m.client
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			items, err := c.Raw().ListRegions(ctx, nil)
			return clkeRegionsMsg{items: items, err: err}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			items, err := c.Raw().ListTypes(ctx, nil)
			return clkeTypesMsg{items: items, err: err}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			items, err := c.Raw().ListLKEVersions(ctx, nil)
			return clkeVersionsMsg{items: items, err: err}
		},
	)
}

func (m *createLKE) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case clkeRegionsMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load regions: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.regions = msg.items
		m.gotReg = true
		return m.maybeBuildForm()
	case clkeTypesMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load types: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.types = msg.items
		m.gotTyp = true
		return m.maybeBuildForm()
	case clkeVersionsMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load k8s versions: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.versions = msg.items
		m.gotVer = true
		return m.maybeBuildForm()
	case clkeSubmittedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.phase = createPhaseAborted
			return nil
		}
		m.result = msg.result
		m.phase = createPhaseDone
		return nil
	}
	if m.form != nil && m.phase == createPhaseForm {
		next, cmd := m.form.Update(msg)
		if f, ok := next.(*huh.Form); ok {
			m.form = f
		}
		switch m.form.State {
		case huh.StateCompleted:
			m.phase = createPhaseSubmitting
			return m.submit()
		case huh.StateAborted:
			m.phase = createPhaseAborted
		}
		return cmd
	}
	return nil
}

func (m *createLKE) maybeBuildForm() tea.Cmd {
	if !(m.gotReg && m.gotTyp && m.gotVer) {
		return nil
	}
	regionOpts := make([]huh.Option[string], 0, len(m.regions))
	for _, r := range m.regions {
		if r.Status != "ok" {
			continue
		}
		regionOpts = append(regionOpts, huh.NewOption(fmt.Sprintf("%s — %s", r.ID, r.Label), r.ID))
	}
	sort.Slice(regionOpts, func(i, j int) bool { return regionOpts[i].Key < regionOpts[j].Key })

	typeOpts := make([]huh.Option[string], 0, len(m.types))
	for _, t := range m.types {
		typeOpts = append(typeOpts, huh.NewOption(
			fmt.Sprintf("%s — %d vCPU · %d MB", t.ID, t.VCPUs, t.Memory), t.ID,
		))
	}
	sort.Slice(typeOpts, func(i, j int) bool { return typeOpts[i].Key < typeOpts[j].Key })

	verOpts := make([]huh.Option[string], 0, len(m.versions))
	for _, v := range m.versions {
		verOpts = append(verOpts, huh.NewOption(v.ID, v.ID))
	}
	if len(verOpts) > 0 {
		m.k8sVersion = m.versions[len(m.versions)-1].ID // default to newest
	}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Label").Validate(minLen(3, "label")).Value(&m.label),
			huh.NewSelect[string]().Title("Region").Options(regionOpts...).Value(&m.region),
			huh.NewSelect[string]().Title("Kubernetes version").Options(verOpts...).Value(&m.k8sVersion),
			huh.NewSelect[string]().Title("Default pool plan").Options(typeOpts...).Value(&m.poolType),
			huh.NewInput().
				Title("Default pool node count (1–100)").
				Validate(func(s string) error {
					n, err := strconv.Atoi(s)
					if err != nil || n < 1 || n > 100 {
						return fmt.Errorf("must be 1–100")
					}
					return nil
				}).
				Value(&m.poolCount),
		),
	)
	m.phase = createPhaseForm
	return m.form.Init()
}

func (m *createLKE) submit() tea.Cmd {
	c := m.client
	count, _ := strconv.Atoi(m.poolCount)
	opts := linodego.LKEClusterCreateOptions{
		Label:      m.label,
		Region:     m.region,
		K8sVersion: m.k8sVersion,
		NodePools: []linodego.LKENodePoolCreateOptions{
			{Count: count, Type: m.poolType},
		},
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cl, err := c.Raw().CreateLKECluster(ctx, opts)
		return clkeSubmittedMsg{result: cl, err: err}
	}
}

func (m *createLKE) View() string {
	switch m.phase {
	case createPhaseLoading:
		var pending []string
		if !m.gotReg {
			pending = append(pending, "regions")
		}
		if !m.gotTyp {
			pending = append(pending, "types")
		}
		if !m.gotVer {
			pending = append(pending, "k8s versions")
		}
		if len(pending) == 0 {
			return "preparing form…"
		}
		return "loading: " + joinComma(pending) + "…"
	case createPhaseForm:
		if m.form != nil {
			return m.form.View()
		}
		return "preparing form…"
	case createPhaseSubmitting:
		return fmt.Sprintf("creating LKE cluster %q in %s…", m.label, m.region)
	case createPhaseDone:
		return fmt.Sprintf("created LKE cluster %s (id %d)", m.result.Label, m.result.ID)
	case createPhaseAborted:
		if m.err != nil {
			return fmt.Sprintf("aborted: %v", m.err)
		}
		return "aborted"
	}
	return ""
}

func (m *createLKE) Done() bool {
	return m.phase == createPhaseDone || m.phase == createPhaseAborted
}

func (m *createLKE) Result() string {
	if m.phase == createPhaseDone && m.result != nil {
		return fmt.Sprintf("created LKE cluster %s (id %d)", m.result.Label, m.result.ID)
	}
	return ""
}

func (m *createLKE) Err() error { return m.err }

func joinComma(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += ", "
		}
		s += p
	}
	return s
}
