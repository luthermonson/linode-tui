package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/linode/linodego"

	"github.com/linode/tui/internal/audit"
	"github.com/linode/tui/internal/config"
	"github.com/linode/tui/internal/linode"
)

type createPhase int

const (
	createPhaseLoading createPhase = iota
	createPhaseForm
	createPhaseSubmitting
	createPhaseDone
	createPhaseAborted
)

type createLinode struct {
	client *linode.Client
	cfg    *config.Config

	phase createPhase
	err   error

	regions []linodego.Region
	types   []linodego.LinodeType
	images  []linodego.Image
	sshKeys []linodego.SSHKey
	gotReg  bool
	gotTyp  bool
	gotImg  bool
	gotSSH  bool

	form         *huh.Form
	region       string
	instanceType string
	image        string
	label        string
	rootPass     string
	authKeys     []string // SSH key bodies selected from profile

	result *linodego.Instance
}

type createRegionsMsg struct {
	items []linodego.Region
	err   error
}
type createTypesMsg struct {
	items []linodego.LinodeType
	err   error
}
type createImagesMsg struct {
	items []linodego.Image
	err   error
}
type createSSHKeysMsg struct {
	items []linodego.SSHKey
	err   error
}
type createSubmittedMsg struct {
	result *linodego.Instance
	err    error
}

func newCreateLinode(client *linode.Client) *createLinode {
	return &createLinode{client: client, phase: createPhaseLoading}
}

// newCreateLinodeForCfg returns a createLinode that knows about the active
// account so it can read/write DefaultSSHKeys.
func newCreateLinodeForCfg(client *linode.Client, cfg *config.Config) *createLinode {
	c := newCreateLinode(client)
	c.cfg = cfg
	return c
}

func (m *createLinode) Init() tea.Cmd {
	return tea.Batch(m.loadRegions(), m.loadTypes(), m.loadImages(), m.loadSSHKeys())
}

func (m *createLinode) loadSSHKeys() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListSSHKeys(ctx, nil)
		// Treat "permission denied" / 401 as soft failure — proceed without keys.
		return createSSHKeysMsg{items: items, err: err}
	}
}

func (m *createLinode) loadRegions() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListRegions(ctx, nil)
		return createRegionsMsg{items: items, err: err}
	}
}

func (m *createLinode) loadTypes() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListTypes(ctx, nil)
		return createTypesMsg{items: items, err: err}
	}
}

func (m *createLinode) loadImages() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := c.Raw().ListImages(ctx, nil)
		return createImagesMsg{items: items, err: err}
	}
}

func (m *createLinode) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case createRegionsMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load regions: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.regions = msg.items
		m.gotReg = true
		return m.maybeBuildForm()
	case createTypesMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load types: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.types = msg.items
		m.gotTyp = true
		return m.maybeBuildForm()
	case createImagesMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("load images: %w", msg.err)
			m.phase = createPhaseAborted
			return nil
		}
		m.images = msg.items
		m.gotImg = true
		return m.maybeBuildForm()
	case createSSHKeysMsg:
		// non-fatal: degrade gracefully if listing keys errors
		if msg.err == nil {
			m.sshKeys = msg.items
		}
		m.gotSSH = true
		return m.maybeBuildForm()
	case createSubmittedMsg:
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

func (m *createLinode) maybeBuildForm() tea.Cmd {
	if !(m.gotReg && m.gotTyp && m.gotImg && m.gotSSH) {
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
			fmt.Sprintf("%s — %d vCPU · %d MB · %d GB disk", t.ID, t.VCPUs, t.Memory, t.Disk),
			t.ID,
		))
	}
	sort.Slice(typeOpts, func(i, j int) bool { return typeOpts[i].Key < typeOpts[j].Key })

	imageOpts := make([]huh.Option[string], 0, len(m.images))
	for _, img := range m.images {
		if !img.IsPublic || string(img.Status) != "available" {
			continue
		}
		imageOpts = append(imageOpts, huh.NewOption(
			fmt.Sprintf("%s — %s", img.ID, img.Label),
			img.ID,
		))
	}
	sort.Slice(imageOpts, func(i, j int) bool { return imageOpts[i].Key < imageOpts[j].Key })

	// Pre-populate region/type/image from the active account's LastCreate.
	if m.cfg != nil {
		if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok {
			if regionExists(m.regions, acct.LastCreate.Region) {
				m.region = acct.LastCreate.Region
			}
			if typeExists(m.types, acct.LastCreate.Type) {
				m.instanceType = acct.LastCreate.Type
			}
			if imageExists(m.images, acct.LastCreate.Image) {
				m.image = acct.LastCreate.Image
			}
		}
	}

	sshOpts := make([]huh.Option[string], 0, len(m.sshKeys))
	for _, k := range m.sshKeys {
		// Option value is the label so pre-selection from config persists
		// across key rotations. submit() maps labels back to key bodies.
		sshOpts = append(sshOpts, huh.NewOption(k.Label, k.Label))
	}
	// Pre-select labels from the active account's DefaultSSHKeys.
	if m.cfg != nil {
		if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok {
			valid := map[string]bool{}
			for _, k := range m.sshKeys {
				valid[k.Label] = true
			}
			for _, label := range acct.DefaultSSHKeys {
				if valid[label] {
					m.authKeys = append(m.authKeys, label)
				}
			}
		}
	}

	fields := []huh.Field{
		huh.NewSelect[string]().
			Title("Region").
			Options(regionOpts...).
			Value(&m.region),
		huh.NewSelect[string]().
			Title("Plan / Type").
			Options(typeOpts...).
			Value(&m.instanceType),
		huh.NewSelect[string]().
			Title("Image").
			Options(imageOpts...).
			Value(&m.image),
		huh.NewInput().
			Title("Label").
			Placeholder("my-linode").
			Validate(func(s string) error {
				if len(s) < 3 {
					return fmt.Errorf("label must be at least 3 chars")
				}
				return nil
			}).
			Value(&m.label),
	}
	if len(sshOpts) > 0 {
		fields = append(fields, huh.NewMultiSelect[string]().
			Title("Authorized SSH keys (optional)").
			Description("Selected keys let you skip the root password.").
			Options(sshOpts...).
			Value(&m.authKeys))
	}
	fields = append(fields, huh.NewInput().
		Title("Root password").
		Description("Required if no SSH keys are selected.").
		EchoMode(huh.EchoModePassword).
		Validate(func(s string) error {
			if len(m.authKeys) > 0 && s == "" {
				return nil
			}
			if len(s) < 11 {
				return fmt.Errorf("password must be at least 11 chars (or pick at least one SSH key)")
			}
			return nil
		}).
		Value(&m.rootPass))

	m.form = huh.NewForm(huh.NewGroup(fields...))
	m.phase = createPhaseForm
	return m.form.Init()
}

func (m *createLinode) submit() tea.Cmd {
	c := m.client
	booted := true

	// authKeys holds labels; map back to key bodies for the API call.
	keyBodies := make([]string, 0, len(m.authKeys))
	for _, label := range m.authKeys {
		for _, k := range m.sshKeys {
			if k.Label == label {
				keyBodies = append(keyBodies, k.SSHKey)
				break
			}
		}
	}

	opts := linodego.InstanceCreateOptions{
		Region:         m.region,
		Type:           m.instanceType,
		Image:          m.image,
		Label:          m.label,
		RootPass:       m.rootPass,
		AuthorizedKeys: keyBodies,
		Booted:         &booted,
	}

	// Persist chosen SSH key labels + region/type/image on the active account.
	if m.cfg != nil && m.cfg.DefaultAccount != "" {
		if acct, ok := m.cfg.Accounts[m.cfg.DefaultAccount]; ok {
			acct.DefaultSSHKeys = append([]string(nil), m.authKeys...)
			acct.LastCreate = config.CreateDefaults{
				Region: m.region,
				Type:   m.instanceType,
				Image:  m.image,
			}
			m.cfg.Accounts[m.cfg.DefaultAccount] = acct
			_ = m.cfg.Save()
		}
	}

	account := ""
	if m.cfg != nil {
		account = m.cfg.DefaultAccount
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		inst, err := c.Raw().CreateInstance(ctx, opts)
		id := ""
		label := opts.Label
		if inst != nil {
			id = fmt.Sprintf("%d", inst.ID)
		}
		audit.Append(audit.Entry{Account: account, Action: "create", Kind: "instances", ID: id, Label: label, Err: errMsg(err)})
		return createSubmittedMsg{result: inst, err: err}
	}
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *createLinode) View() string {
	switch m.phase {
	case createPhaseLoading:
		state := []string{}
		if !m.gotReg {
			state = append(state, "regions")
		}
		if !m.gotTyp {
			state = append(state, "types")
		}
		if !m.gotImg {
			state = append(state, "images")
		}
		if len(state) == 0 {
			return "preparing form…"
		}
		s := "loading: "
		for i, n := range state {
			if i > 0 {
				s += ", "
			}
			s += n
		}
		s += "…"
		return s
	case createPhaseForm:
		if m.form == nil {
			return "preparing form…"
		}
		return m.form.View()
	case createPhaseSubmitting:
		return fmt.Sprintf("creating linode %q in %s…", m.label, m.region)
	case createPhaseDone:
		return fmt.Sprintf("created linode %s (id %d)", m.result.Label, m.result.ID)
	case createPhaseAborted:
		if m.err != nil {
			return fmt.Sprintf("aborted: %v", m.err)
		}
		return "aborted"
	}
	return ""
}

func (m *createLinode) Done() bool {
	return m.phase == createPhaseDone || m.phase == createPhaseAborted
}

func (m *createLinode) Result() string {
	if m.phase == createPhaseDone && m.result != nil {
		return fmt.Sprintf("created linode %s (id %d)", m.result.Label, m.result.ID)
	}
	return ""
}

func (m *createLinode) Err() error { return m.err }

func regionExists(items []linodego.Region, id string) bool {
	if id == "" {
		return false
	}
	for _, r := range items {
		if r.ID == id && r.Status == "ok" {
			return true
		}
	}
	return false
}

func typeExists(items []linodego.LinodeType, id string) bool {
	if id == "" {
		return false
	}
	for _, t := range items {
		if t.ID == id {
			return true
		}
	}
	return false
}

func imageExists(items []linodego.Image, id string) bool {
	if id == "" {
		return false
	}
	for _, i := range items {
		if i.ID == id && i.IsPublic && string(i.Status) == "available" {
			return true
		}
	}
	return false
}
