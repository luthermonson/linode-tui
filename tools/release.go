package tools

import (
	"fmt"
	"runtime"
)

// Releaser describes how to fetch a tool's binary from a GitHub release.
type Releaser struct {
	Tool         string // "k9s", "lazysql" — matches Kind values
	Repo         string // "owner/name"
	Version      string // tag, e.g. "v0.50.18"
	AssetName    string // resolved asset filename for current GOOS/GOARCH
	ChecksumName string // resolved checksum file name
	BinName      string // name of the binary inside the archive
}

// DownloadURL returns the GitHub release download URL for a given asset name.
func (r Releaser) DownloadURL(name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", r.Repo, r.Version, name)
}

// LookupReleaser resolves a Releaser for the given tool kind and the current
// runtime platform. versionOverride, when non-empty, replaces the hardcoded
// pinned version (e.g. "v0.50.18"). Returns an error if the platform isn't
// supported.
func LookupReleaser(kind Kind, versionOverride string) (Releaser, error) {
	var (
		r   Releaser
		err error
	)
	switch kind {
	case KindKubernetes:
		r, err = k9sReleaser(runtime.GOOS, runtime.GOARCH)
	case KindMySQL, KindPostgreSQL:
		r, err = lazysqlReleaser(runtime.GOOS, runtime.GOARCH)
	default:
		return Releaser{}, fmt.Errorf("no releaser registered for %s", kind)
	}
	if err != nil {
		return Releaser{}, err
	}
	if versionOverride != "" {
		r = applyVersionOverride(r, kind, versionOverride)
	}
	return r, nil
}

// applyVersionOverride swaps version-dependent fields for tools whose asset or
// checksum filename includes the version string.
func applyVersionOverride(r Releaser, kind Kind, version string) Releaser {
	r.Version = version
	if kind == KindMySQL || kind == KindPostgreSQL {
		// lazysql checksum filename embeds the version sans leading "v"
		v := version
		if len(v) > 0 && v[0] == 'v' {
			v = v[1:]
		}
		r.ChecksumName = fmt.Sprintf("lazysql_%s_checksums.txt", v)
	}
	return r
}

func k9sReleaser(goos, goarch string) (Releaser, error) {
	osName, ok := map[string]string{"darwin": "Darwin", "linux": "Linux", "windows": "Windows"}[goos]
	if !ok {
		return Releaser{}, fmt.Errorf("k9s: unsupported OS %s", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return Releaser{}, fmt.Errorf("k9s: unsupported arch %s", goarch)
	}
	ext := "tar.gz"
	bin := "k9s"
	if goos == "windows" {
		ext = "zip"
		bin = "k9s.exe"
	}
	return Releaser{
		Tool:         "k9s",
		Repo:         "derailed/k9s",
		Version:      "v0.50.18",
		AssetName:    fmt.Sprintf("k9s_%s_%s.%s", osName, goarch, ext),
		ChecksumName: "checksums.sha256",
		BinName:      bin,
	}, nil
}

func lazysqlReleaser(goos, goarch string) (Releaser, error) {
	osName, ok := map[string]string{"darwin": "Darwin", "linux": "Linux", "windows": "Windows"}[goos]
	if !ok {
		return Releaser{}, fmt.Errorf("lazysql: unsupported OS %s", goos)
	}
	arch, ok := map[string]string{"amd64": "x86_64", "arm64": "arm64", "386": "i386"}[goarch]
	if !ok {
		return Releaser{}, fmt.Errorf("lazysql: unsupported arch %s", goarch)
	}
	ext := "tar.gz"
	bin := "lazysql"
	if goos == "windows" {
		ext = "zip"
		bin = "lazysql.exe"
	}
	version := "v0.5.0"
	return Releaser{
		Tool:         "lazysql",
		Repo:         "jorgerojas26/lazysql",
		Version:      version,
		AssetName:    fmt.Sprintf("lazysql_%s_%s.%s", osName, arch, ext),
		ChecksumName: fmt.Sprintf("lazysql_%s_checksums.txt", version[1:]), // strip "v"
		BinName:      bin,
	}, nil
}
