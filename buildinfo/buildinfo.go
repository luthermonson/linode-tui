// Package buildinfo exposes the version + commit set at build time so other
// packages can include them in user-visible output (e.g. telemetry, doctor).
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Set at build time by main via ldflags / by tests via the setters below.
//
// A plain `go install github.com/luthermonson/linode-tui/cmd/linode-tui@latest`
// doesn't run our ldflags, so main's version/commit vars stay at their "dev"/
// "none" zero values. Without help, that's what ends up here too. init()
// below fills in the module version + VCS revision that the Go toolchain
// embeds automatically (via runtime/debug.ReadBuildInfo) whenever ldflags
// didn't already stamp something more specific.
var (
	Version = "dev"
	Commit  = "none"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		Version = v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			Commit = s.Value
			break
		}
	}
}

// Set updates both fields, but only with values more specific than the
// package defaults ("dev" / "none"). This lets an ldflags-stamped build
// (main's version/commit vars, always passed here at startup) override the
// runtime/debug.ReadBuildInfo fallback from init(), while an unstamped build
// — where main only has "dev"/"none" to offer — leaves that fallback intact.
// Called from cmd/linode-tui/main once at startup.
func Set(version, commit string) {
	if version != "" && version != "dev" {
		Version = version
	}
	if commit != "" && commit != "none" {
		Commit = commit
	}
}

// Identity returns a map suitable for telemetry / doctor output. Contains
// version, commit, OS, and arch — no host, user, or token data.
func Identity() map[string]string {
	return map[string]string{
		"version": Version,
		"commit":  Commit,
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}
}
