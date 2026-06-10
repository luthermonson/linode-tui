package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/linode/tui/config"
)

type Kind string

const (
	KindKubernetes Kind = "kubernetes"
	KindMySQL      Kind = "mysql"
	KindPostgreSQL Kind = "postgresql"
	KindLish       Kind = "lish"
	KindSSH        Kind = "ssh"
)

type Runner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Runner {
	return &Runner{cfg: cfg}
}

// Run resolves the configured tool for kind, templates its args with vars, and
// either hands the terminal to it (mode: tui) or launches it detached (mode:
// gui). For tui mode the returned tea.Cmd should be returned from a tea.Model's
// Update so Bubble Tea can release the terminal. For gui mode the returned
// tea.Cmd is a no-op message after the launch — the caller just returns it.
func (r *Runner) Run(ctx context.Context, kind Kind, vars any) (tea.Cmd, error) {
	return r.RunWithEnv(ctx, kind, vars, nil)
}

// RunWithEnv is Run with extra env vars appended to the child's environment.
// Useful for KUBECONFIG, DATABASE_URL, etc. — caller-supplied env beats the
// parent shell's so a stale KUBECONFIG in the user's profile can't shadow
// the one we just wrote.
func (r *Runner) RunWithEnv(ctx context.Context, kind Kind, vars any, extraEnv []string) (tea.Cmd, error) {
	tool, err := r.tool(kind)
	if err != nil {
		return nil, err
	}

	bin, err := r.resolveExec(ctx, kind, tool)
	if err != nil {
		return nil, err
	}

	args, err := renderArgs(tool.Args, vars)
	if err != nil {
		return nil, fmt.Errorf("render args: %w", err)
	}

	prepare := func(c *exec.Cmd) {
		if len(extraEnv) > 0 {
			c.Env = append(os.Environ(), extraEnv...)
		}
	}

	switch tool.Mode {
	case config.ModeTUI, "":
		c := exec.CommandContext(ctx, bin, args...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		prepare(c)
		return tea.ExecProcess(c, func(err error) tea.Msg { return ExitMsg{Kind: kind, Err: err} }), nil
	case config.ModeGUI:
		c := exec.CommandContext(ctx, bin, args...)
		prepare(c)
		if err := c.Start(); err != nil {
			return nil, fmt.Errorf("launch %s: %w", bin, err)
		}
		go func() { _ = c.Wait() }()
		return func() tea.Msg { return LaunchedMsg{Kind: kind, PID: c.Process.Pid} }, nil
	default:
		return nil, fmt.Errorf("unknown tool mode %q for %s", tool.Mode, kind)
	}
}

type ExitMsg struct {
	Kind Kind
	Err  error
}

type LaunchedMsg struct {
	Kind Kind
	PID  int
}

func (r *Runner) tool(kind Kind) (config.Tool, error) {
	switch kind {
	case KindKubernetes:
		return r.cfg.Tools.Kubernetes, nil
	case KindMySQL:
		return r.cfg.Tools.MySQL, nil
	case KindPostgreSQL:
		return r.cfg.Tools.PostgreSQL, nil
	case KindLish:
		return r.cfg.Tools.Lish, nil
	case KindSSH:
		return r.cfg.Tools.SSH, nil
	default:
		return config.Tool{}, fmt.Errorf("unknown tool kind %q", kind)
	}
}

func (r *Runner) resolveExec(ctx context.Context, kind Kind, t config.Tool) (string, error) {
	if t.Exec == "" {
		return "", fmt.Errorf("%s: exec not configured", kind)
	}

	if strings.ContainsAny(t.Exec, `/\`) || filepath.IsAbs(t.Exec) {
		expanded := expandHome(t.Exec)
		if _, err := os.Stat(expanded); err == nil {
			return expanded, nil
		}
		return "", fmt.Errorf("%s: configured exec %q not found", kind, t.Exec)
	}

	if p, err := exec.LookPath(t.Exec); err == nil {
		return p, nil
	}

	cacheDir := expandHome(r.cfg.Tools.InstallDir)
	candidate := filepath.Join(cacheDir, withExeSuffix(t.Exec))
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	if !t.AutoInstall {
		return "", fmt.Errorf("%s: %q not found in PATH and auto_install is disabled", kind, t.Exec)
	}
	return "", &ErrToolMissing{Kind: kind, Tool: t}
}

func renderArgs(args []string, vars any) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if !strings.Contains(a, "{{") {
			out = append(out, a)
			continue
		}
		tpl, err := template.New("arg").Parse(a)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, vars); err != nil {
			return nil, err
		}
		out = append(out, buf.String())
	}
	return out, nil
}

func expandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

func withExeSuffix(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

