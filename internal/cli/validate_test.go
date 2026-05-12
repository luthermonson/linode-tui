package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestValidateConfigStrictWarningsExit(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// Warning: active_theme is not one of the known themes.
	cfg := `active_theme: midnight-magic
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &cli.Command{Commands: []*cli.Command{validateConfigCommand()}}
	var buf bytes.Buffer
	app.Writer = &buf
	err := app.Run(context.Background(), []string{
		"app", "validate-config",
		"--config", cfgPath,
		"--strict",
		"--quiet",
	})
	if err == nil {
		t.Fatalf("expected non-zero exit on --strict with unknown theme; output: %s", buf.String())
	}
}

func TestValidateConfigCleanExits0(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("active_theme: dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &cli.Command{Commands: []*cli.Command{validateConfigCommand()}}
	var buf bytes.Buffer
	app.Writer = &buf
	if err := app.Run(context.Background(), []string{
		"app", "validate-config",
		"--config", cfgPath,
		"--strict",
		"--quiet",
	}); err != nil {
		t.Fatalf("expected zero exit, got %v\noutput: %s", err, buf.String())
	}
}
