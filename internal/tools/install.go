package tools

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/linode/tui/internal/config"
)

// ProgressFn is called periodically during asset download with the byte
// progress. total is 0 when the server didn't send Content-Length.
type ProgressFn func(done, total int64)

// Install is a convenience wrapper for InstallWithProgress without a callback.
func Install(ctx context.Context, kind Kind, cfg *config.Config) (string, error) {
	return InstallWithProgress(ctx, kind, cfg, nil)
}

// InstallWithProgress downloads the registered release for kind, verifies its
// SHA256 checksum, extracts the binary, drops it in dir, and returns the
// resulting path. If progress is non-nil it's called during the asset
// download. The chosen install dir is persisted to cfg.Tools.InstallDir on
// success when it was auto-picked. Retries the whole flow up to Tool.Retries
// times on transient errors (non-checksum failures) with linear backoff.
func InstallWithProgress(ctx context.Context, kind Kind, cfg *config.Config, progress ProgressFn) (string, error) {
	retries := configuredRetries(cfg, kind)
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		path, err := installOnce(ctx, kind, cfg, progress)
		if err == nil {
			return path, nil
		}
		lastErr = err
		// Checksum mismatches are not transient — bail immediately.
		if strings.Contains(err.Error(), "checksum mismatch") {
			return "", err
		}
	}
	return "", lastErr
}

func installOnce(ctx context.Context, kind Kind, cfg *config.Config, progress ProgressFn) (string, error) {
	rel, err := LookupReleaser(kind, configuredVersion(cfg, kind))
	if err != nil {
		return "", err
	}

	dir, autoPicked, err := pickInstallDir(cfg)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	asset, err := httpGetProgress(ctx, rel.DownloadURL(rel.AssetName), progress)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rel.AssetName, err)
	}
	sums, err := httpGet(ctx, rel.DownloadURL(rel.ChecksumName))
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rel.ChecksumName, err)
	}
	wantSum, err := lookupChecksum(sums, rel.AssetName)
	if err != nil {
		return "", err
	}
	gotSum := sha256.Sum256(asset)
	if hex.EncodeToString(gotSum[:]) != wantSum {
		return "", fmt.Errorf("%s checksum mismatch: got %x want %s", rel.AssetName, gotSum, wantSum)
	}

	bin, err := extractBinary(rel.AssetName, asset, rel.BinName)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, rel.BinName)
	if err := os.WriteFile(dest, bin, 0o755); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}

	if autoPicked && cfg != nil {
		cfg.Tools.InstallDir = dir
		_ = cfg.Save()
	}
	return dest, nil
}

// pickInstallDir returns (dir, autoPicked, error). autoPicked is true when we
// chose the dir ourselves; callers should persist it after a successful
// install.
func pickInstallDir(cfg *config.Config) (string, bool, error) {
	if cfg != nil && cfg.Tools.InstallDir != "" {
		return expandHome(cfg.Tools.InstallDir), false, nil
	}
	if dirs := pathDirsUnderHome(); len(dirs) > 0 {
		return dirs[0], true, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", false, fmt.Errorf("locate cache dir: %w", err)
	}
	return filepath.Join(cache, "linode-tui", "bin"), true, nil
}

// SuggestInstallDirs returns writable $PATH entries under $HOME, in PATH order,
// plus the always-available cache fallback. Callers (e.g. the TUI install
// prompt) pick from this list.
func SuggestInstallDirs() []string {
	dirs := pathDirsUnderHome()
	if cache, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, filepath.Join(cache, "linode-tui", "bin"))
	}
	return dirs
}

func pathDirsUnderHome() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	var out []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || !strings.HasPrefix(dir, home) {
			continue
		}
		if writable(dir) {
			out = append(out, dir)
		}
	}
	return out
}

func writable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	probe, err := os.CreateTemp(dir, ".linode-tui-write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	return httpGetProgress(ctx, url, nil)
}

func httpGetProgress(ctx context.Context, url string, progress ProgressFn) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "linode-tui/0.0.0 ("+runtime.GOOS+"; "+runtime.GOARCH+")")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if progress == nil {
		return io.ReadAll(resp.Body)
	}
	return io.ReadAll(&progressReader{r: resp.Body, total: resp.ContentLength, fn: progress})
}

type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	fn    ProgressFn
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.done += int64(n)
		if pr.fn != nil {
			pr.fn(pr.done, pr.total)
		}
	}
	return n, err
}

func lookupChecksum(file []byte, assetName string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(file)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := fields[0]
		// sha256sum-style lines start the filename with "*" or just whitespace
		name := strings.TrimPrefix(fields[1], "*")
		name = strings.TrimPrefix(name, "./")
		if name == assetName {
			return sum, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan checksums: %w", err)
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

// KnownKinds returns the tool kinds that can be auto-installed. Used by
// `:tools upgrade` to fan out.
func KnownKinds() []Kind {
	return []Kind{KindKubernetes, KindMySQL}
}

// configuredVersion returns the user-pinned version from config for kind, or
// empty string when none is set (so LookupReleaser falls back to the built-in
// default).
func configuredVersion(cfg *config.Config, kind Kind) string {
	if cfg == nil {
		return ""
	}
	switch kind {
	case KindKubernetes:
		return cfg.Tools.Kubernetes.Version
	case KindMySQL:
		return cfg.Tools.MySQL.Version
	case KindPostgreSQL:
		return cfg.Tools.PostgreSQL.Version
	}
	return ""
}

func configuredRetries(cfg *config.Config, kind Kind) int {
	if cfg == nil {
		return 0
	}
	switch kind {
	case KindKubernetes:
		return cfg.Tools.Kubernetes.Retries
	case KindMySQL:
		return cfg.Tools.MySQL.Retries
	case KindPostgreSQL:
		return cfg.Tools.PostgreSQL.Retries
	}
	return 0
}

// Relocate moves any binaries managed by us from the old install dir to a new
// one, and updates cfg.Tools.InstallDir on disk. If no current dir is set, the
// new dir is recorded and any later install will land there.
func Relocate(cfg *config.Config, newDir string) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	newDir = expandHome(newDir)
	if newDir == "" {
		return fmt.Errorf("empty dir")
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", newDir, err)
	}
	oldDir := expandHome(cfg.Tools.InstallDir)
	if oldDir != "" && oldDir != newDir {
		for _, kind := range KnownKinds() {
			rel, err := LookupReleaser(kind, configuredVersion(cfg, kind))
			if err != nil {
				continue
			}
			oldPath := filepath.Join(oldDir, rel.BinName)
			if _, err := os.Stat(oldPath); err != nil {
				continue
			}
			newPath := filepath.Join(newDir, rel.BinName)
			if err := moveFile(oldPath, newPath); err != nil {
				return fmt.Errorf("move %s → %s: %w", oldPath, newPath, err)
			}
		}
	}
	cfg.Tools.InstallDir = newDir
	return cfg.Save()
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// cross-filesystem fallback: copy + remove
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, in, 0o755); err != nil {
		return err
	}
	return os.Remove(src)
}

// ErrToolMissing means the configured exec for kind wasn't found anywhere and
// the caller should drive the install flow.
type ErrToolMissing struct {
	Kind Kind
	Tool config.Tool
}

func (e *ErrToolMissing) Error() string {
	return fmt.Sprintf("%s exec %q not found", e.Kind, e.Tool.Exec)
}

// IsToolMissing reports whether err wraps an *ErrToolMissing.
func IsToolMissing(err error) (*ErrToolMissing, bool) {
	var e *ErrToolMissing
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
