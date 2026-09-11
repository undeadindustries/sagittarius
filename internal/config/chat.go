package config

const (
	// DefaultGoogleChatMaxResultRunes is the character cap for tool execution
	// output before it is posted to Google Chat.
	DefaultGoogleChatMaxResultRunes = 2000

	// DefaultGoogleChatConfirmTimeout is the time in seconds before an interactive
	// tool confirmation card in Google Chat expires and fails closed with a deny.
	DefaultGoogleChatConfirmTimeout = 300
)

// GoogleChatResolved holds fully resolved Google Chat configuration settings.
type GoogleChatResolved struct {
	Enabled         bool
	SpaceID         string
	AuthorizedUsers []string
	ProjectID       string
	SubscriptionID  string
	CredentialsFile string
	MaxResultRunes  int
	ConfirmTimeout  int
}

// ResolveGoogleChat resolves the Google Chat configuration with project settings overriding global.
func ResolveGoogleChat(global, project *Settings) GoogleChatResolved {
	res := GoogleChatResolved{
		MaxResultRunes: DefaultGoogleChatMaxResultRunes,
		ConfirmTimeout: DefaultGoogleChatConfirmTimeout,
	}

	apply := func(cfg *SagittariusGoogleChatConfig) {
		if cfg == nil {
			return
		}
		if cfg.Enabled != nil {
			res.Enabled = *cfg.Enabled
		}
		if cfg.SpaceID != "" {
			res.SpaceID = cfg.SpaceID
		}
		if len(cfg.AuthorizedUsers) > 0 {
			res.AuthorizedUsers = append([]string(nil), cfg.AuthorizedUsers...)
		}
		if cfg.ProjectID != "" {
			res.ProjectID = cfg.ProjectID
		}
		if cfg.SubscriptionID != "" {
			res.SubscriptionID = cfg.SubscriptionID
		}
		if cfg.CredentialsFile != "" {
			res.CredentialsFile = cfg.CredentialsFile
		}
		if cfg.MaxResultRunes != nil && *cfg.MaxResultRunes > 0 {
			res.MaxResultRunes = *cfg.MaxResultRunes
		}
		if cfg.ConfirmTimeout != nil && *cfg.ConfirmTimeout > 0 {
			res.ConfirmTimeout = *cfg.ConfirmTimeout
		}
	}

	if global != nil && global.Sagittarius != nil && global.Sagittarius.Chat != nil {
		apply(global.Sagittarius.Chat.GoogleChat)
	}
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Chat != nil {
		apply(project.Sagittarius.Chat.GoogleChat)
	}

	return res
}
