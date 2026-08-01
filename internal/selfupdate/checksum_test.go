package selfupdate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()

	assetPath := filepath.Join(dir, "sagittarius_0.1.0_linux_amd64.tar.gz")
	if err := os.WriteFile(assetPath, []byte("fake archive content"), 0644); err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	h.Write([]byte("fake archive content"))
	expectedHash := fmt.Sprintf("%x", h.Sum(nil))

	checksumsPath := filepath.Join(dir, "checksums.txt")
	checksumsContent := fmt.Sprintf("%s  sagittarius_0.1.0_linux_amd64.tar.gz\n", expectedHash)
	if err := os.WriteFile(checksumsPath, []byte(checksumsContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := VerifySHA256(assetPath, checksumsPath, "sagittarius_0.1.0_linux_amd64.tar.gz"); err != nil {
		t.Errorf("expected verification to succeed, got: %v", err)
	}

	if err := VerifySHA256(assetPath, checksumsPath, "sagittarius_unknown.tar.gz"); err == nil {
		t.Error("expected error for unknown asset, got nil")
	}

	badChecksumsPath := filepath.Join(dir, "bad_checksums.txt")
	if err := os.WriteFile(badChecksumsPath, []byte("badhash  sagittarius_0.1.0_linux_amd64.tar.gz\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256(assetPath, badChecksumsPath, "sagittarius_0.1.0_linux_amd64.tar.gz"); err == nil {
		t.Error("expected checksum mismatch error, got nil")
	}
}
