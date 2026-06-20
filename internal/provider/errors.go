package provider

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// ErrInvalidAPIKey indicates the provider rejected the configured key.
var ErrInvalidAPIKey = errors.New("invalid api key")

// ErrQuotaExceeded indicates a rate or quota limit was hit.
var ErrQuotaExceeded = errors.New("quota exceeded")

// ErrModelNotFound indicates the requested model id is unavailable.
var ErrModelNotFound = errors.New("model not found")

// MapAPIError converts genai transport errors into user-facing provider errors.
func MapAPIError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return mapAPIError(apiErr)
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "api key not valid"),
		strings.Contains(msg, "invalid api key"),
		strings.Contains(msg, "api_key_invalid"):
		return fmt.Errorf("%w: check GEMINI_API_KEY or GOOGLE_API_KEY, or run /provider set gemini-apikey key",
			ErrInvalidAPIKey)
	case strings.Contains(msg, "quota"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "resource_exhausted"):
		return fmt.Errorf("%w: try again later or check your Gemini API quota", ErrQuotaExceeded)
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "model") && strings.Contains(msg, "404"):
		return fmt.Errorf("%w: verify the model name in settings or /model", ErrModelNotFound)
	}

	return fmt.Errorf("gemini request failed: %w", err)
}

func mapAPIError(apiErr genai.APIError) error {
	status := strings.ToUpper(strings.TrimSpace(apiErr.Status))
	msg := strings.TrimSpace(apiErr.Message)

	switch apiErr.Code {
	case 401, 403:
		if msg == "" {
			msg = "the API key was rejected"
		}
		return fmt.Errorf("%w: %s. Set GEMINI_API_KEY or GOOGLE_API_KEY, or run /provider set gemini-apikey key",
			ErrInvalidAPIKey, msg)
	case 404:
		if msg == "" {
			msg = "the requested model was not found"
		}
		return fmt.Errorf("%w: %s", ErrModelNotFound, msg)
	case 429:
		if msg == "" {
			msg = "quota or rate limit exceeded"
		}
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, msg)
	}

	switch status {
	case "INVALID_ARGUMENT":
		if strings.Contains(strings.ToLower(msg), "api key") {
			return fmt.Errorf("%w: %s", ErrInvalidAPIKey, msg)
		}
	case "NOT_FOUND":
		return fmt.Errorf("%w: %s", ErrModelNotFound, msg)
	case "RESOURCE_EXHAUSTED":
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, msg)
	}

	if msg != "" {
		return fmt.Errorf("gemini api error (%d %s): %s", apiErr.Code, status, msg)
	}
	return fmt.Errorf("gemini api error (%d): %w", apiErr.Code, apiErr)
}

func mapOpenAIHTTPError(status int, body string) error {
	msg := strings.TrimSpace(body)
	lower := strings.ToLower(msg)
	switch status {
	case 401, 403:
		if msg == "" {
			msg = "the API key was rejected"
		}
		return fmt.Errorf("%w: %s", ErrInvalidAPIKey, msg)
	case 404:
		if msg == "" {
			msg = "the requested model was not found"
		}
		return fmt.Errorf("%w: %s", ErrModelNotFound, msg)
	case 429:
		if msg == "" {
			msg = "quota or rate limit exceeded"
		}
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, msg)
	}
	if strings.Contains(lower, "invalid api key") || strings.Contains(lower, "incorrect api key") {
		return fmt.Errorf("%w: %s", ErrInvalidAPIKey, msg)
	}
	if msg != "" {
		return fmt.Errorf("openai request failed (HTTP %d): %s", status, msg)
	}
	return fmt.Errorf("openai request failed: HTTP %d", status)
}

func mapOpenAITransportError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("cannot reach provider endpoint: %w", err)
	default:
		return fmt.Errorf("openai request failed: %w", err)
	}
}
