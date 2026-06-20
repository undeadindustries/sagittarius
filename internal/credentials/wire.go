package credentials

import (
	"encoding/json"
	"time"
)

// storedCredentials mirrors fork OAuthCredentials JSON written by providerCredentialStorage.
type storedCredentials struct {
	ServerName string      `json:"serverName"`
	Token      storedToken `json:"token"`
	UpdatedAt  int64       `json:"updatedAt"`
}

type storedToken struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
}

func encodeStoredAPIKey(providerID, apiKey string) string {
	creds := storedCredentials{
		ServerName: providerID,
		Token: storedToken{
			AccessToken: apiKey,
			TokenType:   "ApiKey",
		},
		UpdatedAt: time.Now().UnixMilli(),
	}
	b, err := json.Marshal(creds)
	if err != nil {
		return apiKey
	}
	return string(b)
}

func decodeStoredAPIKey(stored string) string {
	var creds storedCredentials
	if err := json.Unmarshal([]byte(stored), &creds); err != nil {
		return stored
	}
	if creds.Token.AccessToken != "" {
		return creds.Token.AccessToken
	}
	return stored
}
