// Package buildinfo exposes the version + commit set at build time so other
// packages can include them in user-visible output (e.g. telemetry, doctor).
package buildinfo

import "runtime"

// Set at build time by main via ldflags / by tests via the setters below.
var (
	Version = "dev"
	Commit  = "none"
)

// Set updates both fields. Called from cmd/linode-tui/main once at startup.
func Set(version, commit string) {
	if version != "" {
		Version = version
	}
	if commit != "" {
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
