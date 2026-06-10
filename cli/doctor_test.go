package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

func TestDoctorStrictRefreshUnknown(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `refresh_overrides:
  not-a-real-view: 5s
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &cli.Command{Commands: []*cli.Command{doctorCommand()}}
	var buf bytes.Buffer
	app.Writer = &buf
	err := app.Run(context.Background(), []string{
		"app", "doctor",
		"--config", cfgPath,
		"--section", "refresh",
		"--strict",
		"--quiet",
	})
	if err == nil {
		t.Fatalf("expected non-zero exit; output=%s", buf.String())
	}
}

func TestDoctorSectionLayoutDigestsFlagsDrift(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// Active account "prod" has a different digest than the global map.
	cfg := `default_account: prod
accounts:
  prod:
    token: t
    layout_digests:
      dev: aaaaaaaaaaaaaaaa
layout_digests:
  dev: bbbbbbbbbbbbbbbb
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &cli.Command{Commands: []*cli.Command{doctorCommand()}}
	// --strict so the optional drift becomes a required failure
	err := app.Run(context.Background(), []string{
		"app", "doctor",
		"--config", cfgPath,
		"--section", "layout-digests",
		"--strict",
		"--quiet",
	})
	if err == nil {
		t.Fatal("expected non-zero exit when account/global digests disagree under --strict")
	}
}

func TestDoctorJSONNoColorBytes(t *testing.T) {
	// Capture os.Stdout because runDoctor writes there. Verify --json output
	// never contains ANSI escapes when --no-color is also set.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("active_theme: dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	app := &cli.Command{Commands: []*cli.Command{doctorCommand()}}
	go func() {
		_ = app.Run(context.Background(), []string{
			"app", "doctor",
			"--config", cfgPath,
			"--json",
			"--no-color",
			"--section", "config",
		})
		_ = w.Close()
	}()
	out, err := readAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte{0x1b}) {
		t.Fatalf("expected no ANSI escapes in --json --no-color output:\n%s", out)
	}
	if !bytes.Contains(out, []byte(`"ok"`)) {
		t.Fatalf("expected JSON containing \"ok\", got:\n%s", out)
	}
}

func TestDoctorWatchJSONLines(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("active_theme: dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	app := &cli.Command{Commands: []*cli.Command{doctorCommand()}}
	done := make(chan struct{})
	go func() {
		_ = app.Run(ctx, []string{
			"app", "doctor",
			"--config", cfgPath,
			"--section", "config",
			"--watch", "60ms",
			"--json",
			"--no-color",
		})
		_ = w.Close()
		close(done)
	}()
	out, err := readAll(r)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	lines := bytes.Count(out, []byte{'\n'})
	if lines < 2 {
		t.Fatalf("expected at least 2 newlines (multiple JSON objects), got %d\noutput:\n%s", lines, out)
	}
	if !bytes.Contains(out, []byte(`"ok"`)) {
		t.Fatalf("expected JSON containing \"ok\":\n%s", out)
	}
}

func readAll(r *os.File) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}

func TestDoctorJSONOK(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("refresh: 2s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &cli.Command{Commands: []*cli.Command{doctorCommand()}}
	var buf bytes.Buffer
	app.Writer = &buf
	// Token check is not gated by --section here, but it's optional/non-fatal
	// even when missing — pick the config section to keep this test hermetic.
	if err := app.Run(context.Background(), []string{
		"app", "doctor",
		"--config", cfgPath,
		"--section", "config",
		"--json",
	}); err != nil {
		t.Fatalf("expected zero exit, got %v\noutput: %s", err, buf.String())
	}
}
