package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// ExtractBinary extracts only the 'sagittarius' binary from the tar.gz archive
// into a temporary file and returns its path.
func ExtractBinary(archivePath string) (tmpPath string, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", fmt.Errorf("tar reader: %w", err)
		}
		// The binary is just named 'sagittarius' in the root of the archive.
		if hdr.Name == "sagittarius" && hdr.Typeflag == tar.TypeReg {
			tmpf, err := os.CreateTemp("", "sagittarius-update-*")
			if err != nil {
				return "", fmt.Errorf("create temp file: %w", err)
			}
			tmpPath = tmpf.Name()
			if _, err := io.Copy(tmpf, tr); err != nil {
				_ = tmpf.Close()
				_ = os.Remove(tmpPath)
				return "", fmt.Errorf("extract binary: %w", err)
			}
			if err := tmpf.Close(); err != nil {
				_ = os.Remove(tmpPath)
				return "", fmt.Errorf("close temp file: %w", err)
			}
			return tmpPath, nil
		}
	}
	return "", fmt.Errorf("binary 'sagittarius' not found in archive")
}
