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

	"github.com/luthermonson/linode-tui/config"
)

// ProgressFn is called periodically during asset download with the byte
// progress. total is 0 when the server didn't send Content-Length.
type ProgressFn func(done, total int64)

// ErrChecksumMismatch is returned (wrapped, with asset context) when a
// downloaded asset's SHA256 doesn't match the published checksum. It's the
// only signal InstallWithProgress uses to decide a failure is NOT safe to
// retry — a tampered or corrupted download must never be silently retried
// and installed anyway. Detect it with errors.Is, not string matching.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// maxRetries caps Tool.Retries regardless of what's configured, so a typo'd
// or malicious config can't turn a transient network hiccup into an
// effectively infinite retry loop.
const maxRetries = 10

// maxBackoff caps the linear per-attempt backoff delay.
const maxBackoff = 30 * time.Second

// maxAssetBytes and maxChecksumBytes bound how much we'll read from a
// release asset / checksum file response, regardless of what the server
// (or a Content-Length lie) claims. Without a cap a malicious or
// misbehaving server could stream an unbounded body and OOM the process.
const (
	maxAssetBytes    = 200 * 1024 * 1024
	maxChecksumBytes = 1 * 1024 * 1024
)

// Install is a convenience wrapper for InstallWithProgress without a callback.
func Install(ctx context.Context, kind Kind, cfg *config.Config) (string, error) {
	return InstallWithProgress(ctx, kind, cfg, nil)
}

// InstallWithProgress downloads the registered release for kind, verifies its
// SHA256 checksum, extracts the binary, drops it in dir, and returns the
// resulting path. If progress is non-nil it's called during the asset
// download. The chosen install dir is persisted to cfg.Tools.InstallDir on
// success when it was auto-picked. Retries the whole flow up to Tool.Retries
// times (clamped to maxRetries) on transient errors (non-checksum failures)
// with linear backoff (clamped to maxBackoff).
func InstallWithProgress(ctx context.Context, kind Kind, cfg *config.Config, progress ProgressFn) (string, error) {
	retries := configuredRetries(cfg, kind)
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}
		path, err := installOnce(ctx, kind, cfg, progress)
		if err == nil {
			return path, nil
		}
		lastErr = err
		// Checksum mismatches are not transient — bail immediately rather
		// than retrying (and potentially installing) a tampered download.
		if errors.Is(err, ErrChecksumMismatch) {
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

	dest, err := installAsset(ctx, dir, rel.DownloadURL(rel.AssetName), rel.AssetName, rel.DownloadURL(rel.ChecksumName), rel.BinName, progress)
	if err != nil {
		return "", err
	}

	if autoPicked && cfg != nil {
		cfg.Tools.InstallDir = dir
		_ = cfg.Save()
	}
	return dest, nil
}

// installAsset downloads assetURL and checksumURL, verifies the SHA256 sum
// of the asset against the checksum file's entry for assetName, extracts
// binName from the archive, and atomically installs it into dir. Returns the
// installed path. Factored out of installOnce so tests can point it at an
// httptest.Server instead of GitHub.
func installAsset(ctx context.Context, dir, assetURL, assetName, checksumURL, binName string, progress ProgressFn) (string, error) {
	asset, err := httpGetProgress(ctx, assetURL, maxAssetBytes, progress)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", assetName, err)
	}
	sums, err := httpGet(ctx, checksumURL, maxChecksumBytes)
	if err != nil {
		return "", fmt.Errorf("download checksums for %s: %w", assetName, err)
	}
	wantSum, err := lookupChecksum(sums, assetName)
	if err != nil {
		return "", err
	}
	gotSum := sha256.Sum256(asset)
	if !strings.EqualFold(hex.EncodeToString(gotSum[:]), wantSum) {
		return "", fmt.Errorf("%s: %w: got %x want %s", assetName, ErrChecksumMismatch, gotSum, wantSum)
	}

	bin, err := extractBinary(assetName, asset, binName)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, binName)
	if err := atomicWriteExecutable(dest, bin); err != nil {
		return "", err
	}
	return dest, nil
}

// atomicWriteExecutable writes data to a temp file next to dest, marks it
// executable, and renames it into place. This avoids two failure modes of a
// plain os.WriteFile(dest, data, 0o755): a partial write (crash/kill
// mid-write) leaving a truncated, broken binary on PATH, and WriteFile's
// mode argument only applying when the file is created — so upgrading an
// existing 0644 file would silently leave it non-executable.
func atomicWriteExecutable(dest string, data []byte) error {
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	// Belt-and-suspenders: WriteFile only chmods on create, so if tmp
	// somehow already existed with different permissions, force it.
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := renameOverwrite(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install %s: %w", dest, err)
	}
	return nil
}

// renameOverwrite renames src to dst, replacing dst if it exists. Go's
// os.Rename already replaces an existing regular file on both Windows and
// POSIX, but Windows can still refuse (e.g. the destination is open/locked
// by a running instance of the tool, or a differing volume) with an error
// os.Rename alone won't recover from. Fall back to an explicit remove+rename
// so upgrades work cross-platform.
func renameOverwrite(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
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

func httpGet(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	return httpGetProgress(ctx, url, maxBytes, nil)
}

func httpGetProgress(ctx context.Context, url string, maxBytes int64, progress ProgressFn) ([]byte, error) {
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
	// Read one byte past the limit so we can tell "exactly at the limit"
	// apart from "truncated because it was too big" and error on the latter.
	r := io.LimitReader(resp.Body, maxBytes+1)
	if progress != nil {
		r = &progressReader{r: r, total: resp.ContentLength, fn: progress}
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("GET %s: response exceeds %d byte limit", url, maxBytes)
	}
	return body, nil
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
			if !isHexSHA256(sum) {
				return "", fmt.Errorf("malformed checksum file: checksum for %s is not a 64-character hex sha256 sum: %q", assetName, sum)
			}
			return sum, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan checksums: %w", err)
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

// isHexSHA256 reports whether s is exactly 64 hex characters, i.e. shaped
// like a valid SHA256 sum. Uppercase upstream checksum files are common
// (some tools' goreleaser configs emit them), so callers should compare with
// strings.EqualFold rather than requiring lowercase here.
func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
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
	r := 0
	if cfg != nil {
		switch kind {
		case KindKubernetes:
			r = cfg.Tools.Kubernetes.Retries
		case KindMySQL:
			r = cfg.Tools.MySQL.Retries
		case KindPostgreSQL:
			r = cfg.Tools.PostgreSQL.Retries
		}
	}
	if r < 0 {
		return 0
	}
	if r > maxRetries {
		return maxRetries
	}
	return r
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
