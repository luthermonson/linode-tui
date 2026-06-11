package views

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/luthermonson/linode-tui/linode"
	"github.com/luthermonson/linode-tui/tools"
)

// (tea import used by OnEdit field below)

// Action describes a per-row mutation triggered by a hotkey. Run is called
// after the user confirms via the global modal; Prompt returns the
// confirmation text shown to the user for a specific item.
type Action[T any] struct {
	Key    string
	Label  string
	Prompt func(T) string
	Run    func(context.Context, *linode.Client, T) error
}

// ConfirmMsg requests that the root model show a confirmation modal. OnYes is
// executed only if the user picks "Yes".
type ConfirmMsg struct {
	Prompt string
	OnYes  tea.Cmd
}

// TypedConfirmMsg asks the user to type a specific string before running
// OnYes. Used for high-count bulk operations where a single Yes/No is too
// permissive.
type TypedConfirmMsg struct {
	Prompt string
	Match  string
	OnYes  tea.Cmd
}

// ActionDoneMsg signals a successful action. listView shows Label in status
// and forces a refresh.
type ActionDoneMsg struct {
	Label string
}

// ActionErrorMsg signals a failed action.
type ActionErrorMsg struct {
	Label string
	Err   error
}

// ConfigureLinodeAction discriminates configure flows on a Linode.
type ConfigureLinodeAction string

const (
	ConfigureEdit    ConfigureLinodeAction = "edit"
	ConfigureResize  ConfigureLinodeAction = "resize"
	ConfigureRebuild ConfigureLinodeAction = "rebuild"
	ConfigureTags    ConfigureLinodeAction = "tags"
)

// ConfigureLinodeMsg requests a configure form for an existing Linode. If
// Prefill is set, the form's primary field is pre-populated (useful for the
// :configure tags <csv> shorthand).
type ConfigureLinodeMsg struct {
	Action  ConfigureLinodeAction
	ID      int
	Label   string
	Prefill string
}

// Identifiable lets the root model ask the current view for the highlighted
// row's IDFn output. Used by command-bar shortcuts that act on the focused
// row without going through key dispatch.
type Identifiable interface {
	SelectedID() string
}

// HelpEntry describes one row of the help overlay.
type HelpEntry struct {
	Key  string
	Desc string
}

// OpenDetailMsg asks the root model to show a scrollable read-only viewport
// containing Body, with Title shown in the header. If OnEdit is non-nil,
// pressing 'e' in the viewport closes the detail and emits this cmd.
type OpenDetailMsg struct {
	Title  string
	Body   string
	OnEdit tea.Cmd
}

// NavigateMsg asks the root model to push the current view onto the back
// stack and switch to Name, passing Context to the new view's factory.
type NavigateMsg struct {
	Name    string
	Context map[string]any
}

// Filterable lets the root model ask a view whether it's currently in filter
// mode; used to decide whether esc clears the filter or pops the view stack.
type Filterable interface {
	Filtering() bool
}

// Counter exposes row counts for the header bar.
type Counter interface {
	Counts() (total, bookmarked, visible int)
}

// ExportScope narrows what `:export` includes.
type ExportScope string

const (
	ExportVisible    ExportScope = "visible"
	ExportSelected   ExportScope = "selected"
	ExportBookmarked ExportScope = "bookmarked"
)

// Exportable lets a view emit its current data as CSV or JSON. Implemented by
// listView; consumed by the `:export` command.
type Exportable interface {
	ExportCSV(scope ExportScope) (string, error)
	ExportJSON(scope ExportScope) ([]byte, error)
}

// Helper is implemented by views that contribute per-view help entries.
type Helper interface {
	Help() []HelpEntry
}

// DrillInMsg requests that the surrounding listView invoke an external tool
// via tools.Runner. An OnEnter callback returns one of these from a tea.Cmd
// once any async setup (e.g. fetching a kubeconfig and writing a temp file)
// is complete. Cleanup, if set, runs after the tool exits.
type DrillInMsg struct {
	Tool    tools.Kind
	Vars    any
	Cleanup func()
	// Env appends key=value strings to the child process's environment.
	// Used to set KUBECONFIG when launching k9s so the tool picks up the
	// kubeconfig even if its --kubeconfig flag is ignored or misread.
	Env []string
}

// ErrorMsg surfaces a non-fatal error from an OnEnter cmd into the listView's
// error pane.
type ErrorMsg struct{ Err error }

// InstallNeededMsg bubbles up to the root model when a DrillInMsg can't find
// its tool. The root drives the install flow and, on success, re-emits Drill.
type InstallNeededMsg struct {
	Kind  tools.Kind
	Drill DrillInMsg
}

// InstallProgressMsg is emitted periodically while an install download is in
// flight. Percent is 0 when the server didn't send Content-Length.
type InstallProgressMsg struct {
	Kind    tools.Kind
	Percent int
}

// InstallDoneMsg signals a successful install. Root replays the saved Drill.
type InstallDoneMsg struct {
	Kind tools.Kind
	Path string
}

// InstallErrorMsg signals a failed install. Root runs any pending Cleanup and
// surfaces Err in the status bar.
type InstallErrorMsg struct {
	Kind tools.Kind
	Err  error
}

// drillinDoneMsg is internal: emitted after a tool exits so the view can
// resume its refresh loop and run cleanup.
type drillinDoneMsg struct {
	id      uint64
	cleanup func()
}
