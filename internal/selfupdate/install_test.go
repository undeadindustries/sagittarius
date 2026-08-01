package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallEndToEnd(t *testing.T) {
	dir := t.TempDir()

	archivePath := createTestArchive(t, dir)
	content, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	h.Write(content)
	hash := fmt.Sprintf("%x", h.Sum(nil))

	assetName := AssetName("v1.0.0", runtime.GOOS, runtime.GOARCH)
	sumsContent := fmt.Sprintf("%s  %s\n", hash, assetName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/fake/repo/releases/latest" || r.URL.Path == "/repos/undeadindustries/sagittarius/releases/latest" {
			w.Header().Set("Content-Type", "application/json")
			rel := Release{
				TagName: "v1.0.0",
				HTMLURL: "http://example.com",
				Assets: []Asset{
					{Name: assetName, BrowserDownloadURL: "http://" + r.Host + "/" + assetName},
					{Name: "checksums.txt", BrowserDownloadURL: "http://" + r.Host + "/checksums.txt"},
				},
			}
			_ = json.NewEncoder(w).Encode(rel)
			return
		}
		if r.URL.Path == "/"+assetName {
			_, _ = w.Write(content)
			return
		}
		if r.URL.Path == "/checksums.txt" {
			_, _ = w.Write([]byte(sumsContent))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	originalBaseURL := githubBaseURL
	githubBaseURL = srv.URL
	defer func() { githubBaseURL = originalBaseURL }()

	targetBin := filepath.Join(dir, "target_bin")
	if err := os.WriteFile(targetBin, []byte("old content"), 0755); err != nil {
		t.Fatal(err)
	}

	opts := InstallOptions{
		Repo:           "fake/repo",
		CurrentVersion: "v0.9.0",
		TargetPath:     targetBin,
	}

	res, err := Install(context.Background(), opts)
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	if res.Version != "v1.0.0" {
		t.Errorf("expected version v1.0.0, got %s", res.Version)
	}

	newContent, err := os.ReadFile(targetBin)
	if err != nil {
		t.Fatal(err)
	}
	if string(newContent) != "fake binary content" {
		t.Errorf("target binary not replaced correctly, got %q", string(newContent))
	}
}

func TestReplaceBinaryPermissionError(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "readonly_dir")
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(targetDir, "bin")
	if err := os.WriteFile(targetPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	// Make dir read-only so rename/create fails
	if err := os.Chmod(targetDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(targetDir, 0755) }() // cleanup

	newBin := filepath.Join(dir, "newbin")
	if err := os.WriteFile(newBin, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}

	err := ReplaceBinary(newBin, targetPath)
	if err == nil {
		t.Fatal("expected error replacing binary in readonly dir")
	}
	// We might not get a permission error on CreateTemp if it's running as root,
	// but normally it should fail. Let's just ensure it returns an error.
}
