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

type createNodeBalancer struct {
	client *linode.Client

	phase createPhase
	err   error

	regions []linodego.Region

	form     *huh.Form
	label    string
	region   string
	throttle string // string for huh.Input; parsed to int on submit

	result *linodego.NodeBalancer
}

type cnbRegionsMsg struct {
	items []linodego.Region
	err   error
}
type cnbSubmittedMsg struct {
	result *linodego.NodeBalancer
	err    error
}

func newCreateNodeBalancer(client *linode.Client) *createNodeBalancer {
	return &createNodeBalancer{client: client, phase: createPhaseLoading}
}

func (m *createNodeBalancer) Init() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListRegions(ctx, nil)
		return cnbRegionsMsg{items: items, err: err}
	}
}

func (m *createNodeBalancer) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case cnbRegionsMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load regions: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.regions = msg.items
		return m.buildForm()
	case cnbSubmittedMsg:
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

func (m *createNodeBalancer) buildForm() tea.Cmd {
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
			huh.NewInput().
				Title("Client conn throttle (0-20, blank = 0)").
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					n, err := strconv.Atoi(s)
					if err != nil || n < 0 || n > 20 {
						return fmt.Errorf("must be 0–20")
					}
					return nil
				}).
				Value(&m.throttle),
		),
	)
	m.phase = createPhaseForm
	return m.form.Init()
}

func (m *createNodeBalancer) submit() tea.Cmd {
	c := m.client
	label := m.label
	throttle, _ := strconv.Atoi(m.throttle)
	opts := linodego.NodeBalancerCreateOptions{
		Label:              &label,
		Region:             m.region,
		ClientConnThrottle: &throttle,
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		nb, err := c.Raw().CreateNodeBalancer(ctx, opts)
		return cnbSubmittedMsg{result: nb, err: err}
	}
}

func (m *createNodeBalancer) View() string {
	switch m.phase {
	case createPhaseLoading:
		return "loading regions…"
	case createPhaseForm:
		if m.form != nil {
			return m.form.View()
		}
		return "preparing form…"
	case createPhaseSubmitting:
		return fmt.Sprintf("creating nodebalancer %q in %s…", m.label, m.region)
	case createPhaseDone:
		return fmt.Sprintf("created nodebalancer (id %d)", m.result.ID)
	case createPhaseAborted:
		if m.err != nil {
			return fmt.Sprintf("aborted: %v", m.err)
		}
		return "aborted"
	}
	return ""
}

func (m *createNodeBalancer) Done() bool {
	return m.phase == createPhaseDone || m.phase == createPhaseAborted
}

func (m *createNodeBalancer) Result() string {
	if m.phase == createPhaseDone && m.result != nil {
		return fmt.Sprintf("created nodebalancer (id %d)", m.result.ID)
	}
	return ""
}

func (m *createNodeBalancer) Err() error { return m.err }
