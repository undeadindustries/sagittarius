package agent

import "github.com/undeadindustries/sagittarius/internal/config"

// baseDialogDeps carries the shared *App pointer behind the overlay Deps adapters
// (model picker, modes, MCP, settings) and supplies the two helpers they all need:
// ProjectAvailable (is a project-scoped settings file on disk?) and effective (the
// merged, read-only global+project settings view used for picker/list reads). The
// concrete deps types embed it so they no longer each copy these helpers.
type baseDialogDeps struct {
	app *App
}

// ProjectAvailable reports whether a project settings scope exists on disk, which
// gates whether the overlays offer a Global/Project scope selector.
func (d baseDialogDeps) ProjectAvailable() bool {
	docs := d.app.docs
	return docs != nil && docs.WorkDir() != ""
}

// effective returns the merged (global+project) settings for overlay READS so a
// project-scoped pick, activeModels list, or mode override is visible. It is
// read-only — never write through it; writes target Documents/TargetSettings(scope).
func (d baseDialogDeps) effective() *config.Settings { return d.app.effectiveSettings() }
