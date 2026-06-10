package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
)

type createVPC struct {
	client *linode.Client

	phase createPhase
	err   error

	regions []linodego.Region

	form        *huh.Form
	label       string
	region      string
	description string

	result *linodego.VPC
}

type cvpcRegionsMsg struct {
	items []linodego.Region
	err   error
}
type cvpcSubmittedMsg struct {
	result *linodego.VPC
	err    error
}

func newCreateVPC(client *linode.Client) *createVPC {
	return &createVPC{client: client, phase: createPhaseLoading}
}

func (m *createVPC) Init() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListRegions(ctx, nil)
		return cvpcRegionsMsg{items: items, err: err}
	}
}

func (m *createVPC) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case cvpcRegionsMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load regions: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.regions = msg.items
		return m.buildForm()
	case cvpcSubmittedMsg:
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

func (m *createVPC) buildForm() tea.Cmd {
	opts := make([]huh.Option[string], 0, len(m.regions))
	for _, r := range m.regions {
		if r.Status != "ok" {
			continue
		}
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s — %s", r.ID, r.Label), r.ID))
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Key < opts[j].Key })

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Label").Validate(minLen(3, "label")).Value(&m.label),
			huh.NewSelect[string]().Title("Region").Options(opts...).Value(&m.region),
			huh.NewInput().Title("Description (optional)").Value(&m.description),
		),
	)
	m.phase = createPhaseForm
	return m.form.Init()
}

func (m *createVPC) submit() tea.Cmd {
	c := m.client
	opts := linodego.VPCCreateOptions{
		Label:       m.label,
		Region:      m.region,
		Description: m.description,
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		v, err := c.Raw().CreateVPC(ctx, opts)
		return cvpcSubmittedMsg{result: v, err: err}
	}
}

func (m *createVPC) View() string {
	switch m.phase {
	case createPhaseLoading:
		return "loading regions…"
	case createPhaseForm:
		if m.form != nil {
			return m.form.View()
		}
		return "preparing form…"
	case createPhaseSubmitting:
		return fmt.Sprintf("creating VPC %q in %s…", m.label, m.region)
	case createPhaseDone:
		return fmt.Sprintf("created VPC %s (id %d)", m.result.Label, m.result.ID)
	case createPhaseAborted:
		if m.err != nil {
			return fmt.Sprintf("aborted: %v", m.err)
		}
		return "aborted"
	}
	return ""
}

func (m *createVPC) Done() bool {
	return m.phase == createPhaseDone || m.phase == createPhaseAborted
}

func (m *createVPC) Result() string {
	if m.phase == createPhaseDone && m.result != nil {
		return fmt.Sprintf("created VPC %s (id %d)", m.result.Label, m.result.ID)
	}
	return ""
}

func (m *createVPC) Err() error { return m.err }
