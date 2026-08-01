package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

var githubBaseURL = "https://api.github.com"

// CheckLatest hits the GitHub releases API to get the latest release info.
func CheckLatest(ctx context.Context, repo string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubBaseURL, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "SagittariusBot/1.0 (+https://github.com/undeadindustries/sagittarius)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}

	return &rel, nil
}
