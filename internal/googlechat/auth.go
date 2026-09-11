package googlechat

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ChatBotScope is the OAuth2 scope needed for Google Chat bot operations.
const ChatBotScope = "https://www.googleapis.com/auth/chat.bot"

// ExpandPath expands a leading "~/" or bare "~" to the user's home directory.
// Other paths are returned unchanged.
func ExpandPath(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// NewOAuthHTTPClient returns an *http.Client authenticated as a Google Chat service account.
// If credentialsFile is provided and non-empty, it reads service-account JSON from that path.
// Otherwise, it falls back to Application Default Credentials (ADC) or GOOGLE_APPLICATION_CREDENTIALS.
func NewOAuthHTTPClient(ctx context.Context, credentialsFile string) (*http.Client, error) {
	if credentialsFile != "" {
		filePath := ExpandPath(credentialsFile)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read google credentials file: %w", err)
		}
		// CredentialsFromJSONWithType validates the file is actually a service
		// account key before loading it; plain CredentialsFromJSON is
		// deprecated because it accepts any credential type unvalidated.
		creds, err := google.CredentialsFromJSONWithType(ctx, data, google.ServiceAccount, ChatBotScope)
		if err != nil {
			return nil, fmt.Errorf("parse google credentials from file: %w", err)
		}
		return oauth2.NewClient(ctx, creds.TokenSource), nil
	}

	creds, err := google.FindDefaultCredentials(ctx, ChatBotScope)
	if err != nil {
		return nil, fmt.Errorf("find google default credentials: %w", err)
	}
	return oauth2.NewClient(ctx, creds.TokenSource), nil
}
