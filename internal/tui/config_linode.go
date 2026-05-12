package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/linode/linodego"

	"github.com/linode/tui/internal/audit"
	"github.com/linode/tui/internal/linode"
	"github.com/linode/tui/internal/tui/views"
)

type configLinode struct {
	client *linode.Client
	id     int
	label  string
	action views.ConfigureLinodeAction

	phase createPhase
	err   error

	types  []linodego.LinodeType
	images []linodego.Image

	form *huh.Form

	// form fields
	newLabel    string
	newTags     string
	newType     string
	newImage    string
	newRootPass string
}

type configTypesMsg struct {
	items []linodego.LinodeType
	err   error
}
type configImagesMsg struct {
	items []linodego.Image
	err   error
}
type configSubmittedMsg struct{ err error }

func newConfigLinode(client *linode.Client, id int, label string, action views.ConfigureLinodeAction) *configLinode {
	return &configLinode{client: client, id: id, label: label, action: action, phase: createPhaseLoading}
}

// newConfigLinodeWithPrefill seeds the primary field of the form. Used by
// `:configure tags <csv>` to skip typing in the form body.
func newConfigLinodeWithPrefill(client *linode.Client, id int, label string, action views.ConfigureLinodeAction, prefill string) *configLinode {
	c := newConfigLinode(client, id, label, action)
	switch action {
	case views.ConfigureTags, views.ConfigureEdit:
		c.newTags = prefill
	}
	return c
}

func (m *configLinode) Init() tea.Cmd {
	switch m.action {
	case views.ConfigureEdit:
		// no preload needed
		m.newLabel = m.label
		return m.buildForm()
	case views.ConfigureTags:
		return m.buildForm()
	case views.ConfigureResize:
		return m.loadTypes()
	case views.ConfigureRebuild:
		return m.loadImages()
	}
	return nil
}

func (m *configLinode) loadTypes() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListTypes(ctx, nil)
		return configTypesMsg{items: items, err: err}
	}
}

func (m *configLinode) loadImages() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListImages(ctx, nil)
		return configImagesMsg{items: items, err: err}
	}
}

func (m *configLinode) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case configTypesMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load types: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.types = msg.items
		return m.buildForm()
	case configImagesMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load images: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.images = msg.items
		return m.buildForm()
	case configSubmittedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.phase = createPhaseAborted
			return nil
		}
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

func (m *configLinode) buildForm() tea.Cmd {
	switch m.action {
	case views.ConfigureEdit:
		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Label").
					Value(&m.newLabel),
				huh.NewInput().
					Title("Tags (comma-separated)").
					Value(&m.newTags),
			),
		)
	case views.ConfigureTags:
		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(fmt.Sprintf("Tags for %s (comma-separated)", m.label)).
					Value(&m.newTags),
			),
		)
	case views.ConfigureResize:
		opts := make([]huh.Option[string], 0, len(m.types))
		for _, t := range m.types {
			opts = append(opts, huh.NewOption(
				fmt.Sprintf("%s — %d vCPU · %d MB · %d GB disk", t.ID, t.VCPUs, t.Memory, t.Disk),
				t.ID,
			))
		}
		sort.Slice(opts, func(i, j int) bool { return opts[i].Key < opts[j].Key })
		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("New plan / type").
					Description("Linode will shut down to resize.").
					Options(opts...).
					Value(&m.newType),
			),
		)
	case views.ConfigureRebuild:
		opts := make([]huh.Option[string], 0, len(m.images))
		for _, img := range m.images {
			if !img.IsPublic || string(img.Status) != "available" {
				continue
			}
			opts = append(opts, huh.NewOption(
				fmt.Sprintf("%s — %s", img.ID, img.Label), img.ID,
			))
		}
		sort.Slice(opts, func(i, j int) bool { return opts[i].Key < opts[j].Key })
		m.form = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("New image").
					Options(opts...).
					Value(&m.newImage),
				huh.NewInput().
					Title("Root password").
					EchoMode(huh.EchoModePassword).
					Validate(minLen(11, "password")).
					Value(&m.newRootPass),
			),
		)
	default:
		m.err = fmt.Errorf("unknown configure action: %s", m.action)
		m.phase = createPhaseAborted
		return nil
	}
	m.phase = createPhaseForm
	return m.form.Init()
}

func (m *configLinode) submit() tea.Cmd {
	c := m.client
	id := m.id
	idStr := fmt.Sprintf("%d", id)
	label := m.label
	account := ""
	// configLinode doesn't carry a *config.Config — audit account stays empty
	// unless callers pass one in a future iteration. (The action name still
	// captures what happened.)
	switch m.action {
	case views.ConfigureEdit:
		tags := splitAndTrim(m.newTags)
		opts := linodego.InstanceUpdateOptions{
			Label: m.newLabel,
			Tags:  &tags,
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := c.Raw().UpdateInstance(ctx, id, opts)
			audit.Append(audit.Entry{Account: account, Action: "edit", Kind: "instances", ID: idStr, Label: label, Err: errMsg(err)})
			return configSubmittedMsg{err: err}
		}
	case views.ConfigureTags:
		tags := splitAndTrim(m.newTags)
		opts := linodego.InstanceUpdateOptions{Tags: &tags}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := c.Raw().UpdateInstance(ctx, id, opts)
			audit.Append(audit.Entry{Account: account, Action: "tags", Kind: "instances", ID: idStr, Label: label, Err: errMsg(err)})
			return configSubmittedMsg{err: err}
		}
	case views.ConfigureResize:
		opts := linodego.InstanceResizeOptions{Type: m.newType}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := c.Raw().ResizeInstance(ctx, id, opts)
			audit.Append(audit.Entry{Account: account, Action: "resize", Kind: "instances", ID: idStr, Label: label, Err: errMsg(err)})
			return configSubmittedMsg{err: err}
		}
	case views.ConfigureRebuild:
		booted := true
		opts := linodego.InstanceRebuildOptions{
			Image:    m.newImage,
			RootPass: m.newRootPass,
			Booted:   &booted,
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_, err := c.Raw().RebuildInstance(ctx, id, opts)
			audit.Append(audit.Entry{Account: account, Action: "rebuild", Kind: "instances", ID: idStr, Label: label, Err: errMsg(err)})
			return configSubmittedMsg{err: err}
		}
	}
	return nil
}

func (m *configLinode) View() string {
	switch m.phase {
	case createPhaseLoading:
		return fmt.Sprintf("loading data for %s…", m.action)
	case createPhaseForm:
		if m.form == nil {
			return "preparing form…"
		}
		return m.form.View()
	case createPhaseSubmitting:
		return fmt.Sprintf("submitting %s for %q…", m.action, m.label)
	case createPhaseDone:
		return fmt.Sprintf("applied %s on %s (id %d)", m.action, m.label, m.id)
	case createPhaseAborted:
		if m.err != nil {
			return fmt.Sprintf("aborted: %v", m.err)
		}
		return "aborted"
	}
	return ""
}

func (m *configLinode) Done() bool {
	return m.phase == createPhaseDone || m.phase == createPhaseAborted
}

func (m *configLinode) Result() string {
	if m.phase == createPhaseDone {
		return fmt.Sprintf("%s applied on %s (id %d)", m.action, m.label, m.id)
	}
	return ""
}

func (m *configLinode) Err() error { return m.err }

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func minLen(n int, what string) func(string) error {
	return func(s string) error {
		if len(s) < n {
			return fmt.Errorf("%s must be at least %d chars", what, n)
		}
		return nil
	}
}
