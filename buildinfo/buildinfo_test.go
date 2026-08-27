package buildinfo

import "testing"

// TestSetOverridesOnlyWithSpecificValues covers the precedence rule between
// an ldflags-stamped build and the runtime/debug.ReadBuildInfo fallback set
// by init(): Set must ignore the "dev"/"none" sentinels (what main always
// passes when ldflags didn't stamp anything) so it can't clobber whatever
// init() already resolved from the module's build info.
func TestSetOverridesOnlyWithSpecificValues(t *testing.T) {
	origV, origC := Version, Commit
	defer func() { Version, Commit = origV, origC }()

	Version, Commit = "dev", "none"

	Set("dev", "none")
	if Version != "dev" || Commit != "none" {
		t.Fatalf("Set(\"dev\", \"none\") should not override sentinel defaults, got version=%q commit=%q", Version, Commit)
	}

	Set("v1.2.3", "abc123")
	if Version != "v1.2.3" || Commit != "abc123" {
		t.Fatalf("Set with real values should override, got version=%q commit=%q", Version, Commit)
	}

	// A later sentinel call must not clobber an already-resolved value.
	Set("dev", "none")
	if Version != "v1.2.3" || Commit != "abc123" {
		t.Fatalf("Set(\"dev\", \"none\") clobbered a previously resolved value: version=%q commit=%q", Version, Commit)
	}

	Set("", "")
	if Version != "v1.2.3" || Commit != "abc123" {
		t.Fatalf("Set(\"\", \"\") clobbered a previously resolved value: version=%q commit=%q", Version, Commit)
	}
}

func TestIdentityIncludesOSArch(t *testing.T) {
	id := Identity()
	for _, k := range []string{"version", "commit", "os", "arch"} {
		if _, ok := id[k]; !ok {
			t.Errorf("Identity() missing key %q", k)
		}
	}
}
