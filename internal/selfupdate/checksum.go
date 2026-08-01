package selfupdate

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifySHA256 reads the expected checksum for assetName from the checksumsPath file
// and verifies that the file at path matches it.
func VerifySHA256(path, checksumsPath, assetName string) error {
	content, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}

	expected := ""
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			expected = parts[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum found for %s", assetName)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file for checksum: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}

	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}
