package tools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestExtractBinaryTarGz(t *testing.T) {
	want := []byte("#!/bin/sh\necho hi\n")
	archive := makeTarGz(t, "subdir/k9s", want)

	got, err := extractBinary("foo.tar.gz", archive, "k9s")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("bytes mismatch: got %q want %q", got, want)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	want := []byte("MZ\x90\x00fake-pe")
	archive := makeZip(t, "k9s.exe", want)

	got, err := extractBinary("foo.zip", archive, "k9s.exe")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("bytes mismatch: got %q want %q", got, want)
	}
}

func TestExtractBinaryNotFound(t *testing.T) {
	archive := makeTarGz(t, "other-binary", []byte("nope"))
	_, err := extractBinary("foo.tar.gz", archive, "k9s")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error doesn't mention missing binary: %v", err)
	}
}

func TestExtractBinaryUnsupportedFormat(t *testing.T) {
	_, err := extractBinary("foo.7z", []byte("nope"), "k9s")
	if err == nil {
		t.Fatal("expected unsupported format error")
	}
}

// Real checksum entries are 64 hex chars (sha256); lookupChecksum now
// enforces that shape (see TestLookupChecksumMalformed*), so fixtures below
// use full-length placeholder sums instead of the old truncated "abc123"
// style.
var (
	sumA = strings.Repeat("0123456789abcdef", 4) // 64 hex chars
	sumB = strings.Repeat("fedcba9876543210", 4) // 64 hex chars
)

func TestLookupChecksumGitHubStyle(t *testing.T) {
	// goreleaser-style: "<sha256>  <filename>"
	file := []byte(sumA + "  k9s_Linux_amd64.tar.gz\n" + sumB + "  k9s_Darwin_amd64.tar.gz\n")
	got, err := lookupChecksum(file, "k9s_Linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != sumA {
		t.Fatalf("got %q want %q", got, sumA)
	}
}

func TestLookupChecksumSha256sumStyle(t *testing.T) {
	// sha256sum-style with "*" binary marker
	file := []byte(sumA + " *lazysql_Linux_x86_64.tar.gz\n")
	got, err := lookupChecksum(file, "lazysql_Linux_x86_64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != sumA {
		t.Fatalf("got %q want %q", got, sumA)
	}
}

func TestLookupChecksumDotSlashStyle(t *testing.T) {
	file := []byte(sumA + "  ./k9s_Linux_arm64.tar.gz\n")
	got, err := lookupChecksum(file, "k9s_Linux_arm64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != sumA {
		t.Fatalf("got %q want %q", got, sumA)
	}
}

func TestLookupChecksumNotFound(t *testing.T) {
	file := []byte(sumA + "  some-other.tar.gz\n")
	_, err := lookupChecksum(file, "k9s_Linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected not found")
	}
}

func TestLookupChecksumIgnoresBlanksAndComments(t *testing.T) {
	file := []byte("\n# header\n  \n" + sumA + "  k9s_Linux_amd64.tar.gz\n")
	got, err := lookupChecksum(file, "k9s_Linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != sumA {
		t.Fatalf("got %q want %q", got, sumA)
	}
}

func TestLookupChecksumMalformedTooShort(t *testing.T) {
	file := []byte("deadbeef  k9s_Linux_amd64.tar.gz\n")
	_, err := lookupChecksum(file, "k9s_Linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected malformed checksum file error")
	}
	if !strings.Contains(err.Error(), "malformed checksum file") {
		t.Fatalf("expected malformed checksum file error, got: %v", err)
	}
}

func TestLookupChecksumMalformedNonHex(t *testing.T) {
	file := []byte(strings.Repeat("z", 64) + "  k9s_Linux_amd64.tar.gz\n")
	_, err := lookupChecksum(file, "k9s_Linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected malformed checksum file error")
	}
	if !strings.Contains(err.Error(), "malformed checksum file") {
		t.Fatalf("expected malformed checksum file error, got: %v", err)
	}
}

func TestK9sReleaser(t *testing.T) {
	r, err := k9sReleaser("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if r.AssetName != "k9s_Linux_amd64.tar.gz" {
		t.Errorf("asset = %q", r.AssetName)
	}
	if r.BinName != "k9s" {
		t.Errorf("bin = %q", r.BinName)
	}
	r2, _ := k9sReleaser("windows", "arm64")
	if r2.AssetName != "k9s_Windows_arm64.zip" || r2.BinName != "k9s.exe" {
		t.Errorf("windows asset = %q bin = %q", r2.AssetName, r2.BinName)
	}
	if _, err := k9sReleaser("plan9", "amd64"); err == nil {
		t.Errorf("expected error for plan9")
	}
}

func TestLazysqlReleaser(t *testing.T) {
	r, err := lazysqlReleaser("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if r.AssetName != "lazysql_Linux_x86_64.tar.gz" {
		t.Errorf("asset = %q", r.AssetName)
	}
	if !strings.Contains(r.ChecksumName, r.Version[1:]) {
		t.Errorf("checksum filename %q should embed version %q", r.ChecksumName, r.Version)
	}
}

func TestApplyVersionOverride(t *testing.T) {
	base, _ := lazysqlReleaser("linux", "amd64")
	r := applyVersionOverride(base, KindMySQL, "v9.9.9")
	if r.Version != "v9.9.9" {
		t.Errorf("version = %q", r.Version)
	}
	if r.ChecksumName != "lazysql_9.9.9_checksums.txt" {
		t.Errorf("checksum = %q", r.ChecksumName)
	}
}

func TestRoundTripChecksumPlusExtract(t *testing.T) {
	// Sanity: SHA256 a fake archive, look up against a sums file, extract its binary.
	binBytes := []byte("hello world\n")
	archive := makeTarGz(t, "k9s", binBytes)
	sum := sha256.Sum256(archive)
	sumsFile := []byte(hex.EncodeToString(sum[:]) + "  test_k9s.tar.gz\n")

	got, err := lookupChecksum(sumsFile, "test_k9s.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum mismatch")
	}
	extracted, err := extractBinary("test_k9s.tar.gz", archive, "k9s")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted, binBytes) {
		t.Fatalf("extracted bytes mismatch")
	}
}

// helpers

func makeTarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
