package config

// UpdateAutoCheckEnabled resolves whether background update checks are enabled.
// Returns true by default unless explicitly disabled in settings.
func UpdateAutoCheckEnabled(global, project *Settings) bool {
	if project != nil && project.Sagittarius != nil && project.Sagittarius.Update != nil && project.Sagittarius.Update.AutoCheck != nil {
		return *project.Sagittarius.Update.AutoCheck
	}
	if global != nil && global.Sagittarius != nil && global.Sagittarius.Update != nil && global.Sagittarius.Update.AutoCheck != nil {
		return *global.Sagittarius.Update.AutoCheck
	}
	return true
}
