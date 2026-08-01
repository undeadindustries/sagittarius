package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func createTestArchive(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	content := []byte("fake binary content")
	hdr := &tar.Header{
		Name: "sagittarius",
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	archivePath := createTestArchive(t, dir)

	tmpPath, err := ExtractBinary(archivePath)
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(content, []byte("fake binary content")) {
		t.Errorf("content mismatch: got %q", content)
	}
}
