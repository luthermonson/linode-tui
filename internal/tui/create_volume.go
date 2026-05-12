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

type createVolume struct {
	client *linode.Client

	phase createPhase
	err   error

	regions []linodego.Region

	form   *huh.Form
	label  string
	region string
	size   string

	result *linodego.Volume
}

type cvolRegionsMsg struct {
	items []linodego.Region
	err   error
}
type cvolSubmittedMsg struct {
	result *linodego.Volume
	err    error
}

func newCreateVolume(client *linode.Client) *createVolume {
	return &createVolume{client: client, phase: createPhaseLoading, size: "10"}
}

func (m *createVolume) Init() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListRegions(ctx, nil)
		return cvolRegionsMsg{items: items, err: err}
	}
}

func (m *createVolume) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case cvolRegionsMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load regions: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.regions = msg.items
		return m.buildForm()
	case cvolSubmittedMsg:
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

func (m *createVolume) buildForm() tea.Cmd {
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
				Title("Size (GB, 10–10240)").
				Validate(func(s string) error {
					n, err := strconv.Atoi(s)
					if err != nil || n < 10 || n > 10240 {
						return fmt.Errorf("size must be 10–10240")
					}
					return nil
				}).
				Value(&m.size),
		),
	)
	m.phase = createPhaseForm
	return m.form.Init()
}

func (m *createVolume) submit() tea.Cmd {
	c := m.client
	size, _ := strconv.Atoi(m.size)
	opts := linodego.VolumeCreateOptions{
		Label:  m.label,
		Region: m.region,
		Size:   size,
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		v, err := c.Raw().CreateVolume(ctx, opts)
		return cvolSubmittedMsg{result: v, err: err}
	}
}

func (m *createVolume) View() string {
	switch m.phase {
	case createPhaseLoading:
		return "loading regions…"
	case createPhaseForm:
		if m.form != nil {
			return m.form.View()
		}
		return "preparing form…"
	case createPhaseSubmitting:
		return fmt.Sprintf("creating volume %q in %s…", m.label, m.region)
	case createPhaseDone:
		return fmt.Sprintf("created volume %s (id %d)", m.result.Label, m.result.ID)
	case createPhaseAborted:
		if m.err != nil {
			return fmt.Sprintf("aborted: %v", m.err)
		}
		return "aborted"
	}
	return ""
}

func (m *createVolume) Done() bool {
	return m.phase == createPhaseDone || m.phase == createPhaseAborted
}

func (m *createVolume) Result() string {
	if m.phase == createPhaseDone && m.result != nil {
		return fmt.Sprintf("created volume %s (id %d)", m.result.Label, m.result.ID)
	}
	return ""
}

func (m *createVolume) Err() error { return m.err }
