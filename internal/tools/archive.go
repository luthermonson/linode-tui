package tools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
)

// extractBinary pulls a single named binary out of an in-memory release
// archive. Supports .tar.gz and .zip. The binary may live at the archive root
// or in a subdirectory; we match on basename. Returns its bytes or an error.
func extractBinary(assetName string, archive []byte, binName string) ([]byte, error) {
	switch {
	case strings.HasSuffix(assetName, ".tar.gz") || strings.HasSuffix(assetName, ".tgz"):
		return extractFromTarGz(archive, binName)
	case strings.HasSuffix(assetName, ".zip"):
		return extractFromZip(archive, binName)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", assetName)
	}
}

func extractFromTarGz(archive []byte, binName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in archive", binName)
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if path.Base(h.Name) != binName {
			continue
		}
		return io.ReadAll(tr)
	}
}

func extractFromZip(archive []byte, binName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != binName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s not found in archive", binName)
}
