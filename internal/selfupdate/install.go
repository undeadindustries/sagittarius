package selfupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

type CheckResult struct {
	Available bool
	Current   string
	Latest    string
	URL       string
}

type InstallResult struct {
	Version string
}

// CurrentExecutablePath returns the resolved path to the running executable.
func CurrentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}
	return resolved, nil
}

// ReplaceBinary atomically replaces targetPath with the file at newBinaryPath.
// It writes a temporary file in the same directory as targetPath to ensure
// os.Rename can be an atomic cross-filesystem operation.
func ReplaceBinary(newBinaryPath, targetPath string) error {
	dir := filepath.Dir(targetPath)
	tmpf, err := os.CreateTemp(dir, ".sagittarius-new-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission for %s; re-run: sudo sagittarius --self-update", dir)
		}
		return fmt.Errorf("create temp replacement: %w", err)
	}
	tmpName := tmpf.Name()

	src, err := os.Open(newBinaryPath)
	if err != nil {
		_ = tmpf.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("open new binary: %w", err)
	}
	defer func() { _ = src.Close() }()

	if _, err := io.Copy(tmpf, src); err != nil {
		_ = tmpf.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("copy new binary: %w", err)
	}

	if err := tmpf.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp replacement: %w", err)
	}

	if err := os.Chmod(tmpName, 0755); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod new binary: %w", err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		_ = os.Remove(tmpName)
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission for %s; re-run: sudo sagittarius --self-update", targetPath)
		}
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}

type InstallOptions struct {
	Repo           string
	CurrentVersion string
	TargetPath     string
}

// Install orchestrates the full download, verify, extract, and replace sequence.
func Install(ctx context.Context, opts InstallOptions) (*InstallResult, error) {
	rel, err := CheckLatest(ctx, opts.Repo)
	if err != nil {
		return nil, fmt.Errorf("check latest: %w", err)
	}
	if !IsNewer(opts.CurrentVersion, rel.TagName) {
		return nil, fmt.Errorf("already at latest version")
	}

	assetName := AssetName(rel.TagName, runtime.GOOS, runtime.GOARCH)
	var assetURL string
	var checksumsURL string

	for _, a := range rel.Assets {
		if a.Name == assetName {
			assetURL = a.BrowserDownloadURL
		}
		if a.Name == "checksums.txt" {
			checksumsURL = a.BrowserDownloadURL
		}
	}

	if assetURL == "" {
		return nil, fmt.Errorf("asset %s not found in release", assetName)
	}
	if checksumsURL == "" {
		return nil, fmt.Errorf("checksums.txt not found in release")
	}

	// Download files
	dir, err := os.MkdirTemp("", "sagittarius-update-dl-*")
	if err != nil {
		return nil, fmt.Errorf("create download dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	tarPath := filepath.Join(dir, assetName)
	if err := downloadFile(ctx, assetURL, tarPath); err != nil {
		return nil, fmt.Errorf("download asset: %w", err)
	}

	sumsPath := filepath.Join(dir, "checksums.txt")
	if err := downloadFile(ctx, checksumsURL, sumsPath); err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}

	// Verify checksum
	if err := VerifySHA256(tarPath, sumsPath, assetName); err != nil {
		return nil, fmt.Errorf("verify checksum: %w", err)
	}

	// Extract
	newBin, err := ExtractBinary(tarPath)
	if err != nil {
		return nil, fmt.Errorf("extract binary: %w", err)
	}
	defer func() { _ = os.Remove(newBin) }()

	// Replace
	if err := ReplaceBinary(newBin, opts.TargetPath); err != nil {
		return nil, err
	}

	return &InstallResult{Version: rel.TagName}, nil
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "SagittariusBot/1.0 (+https://github.com/undeadindustries/sagittarius)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}
