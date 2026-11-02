package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/customprompt"
)

const customPromptLabelSuffix = " (custom)"

// customPromptLabel is how a custom prompt appears in pickers, the status bar,
// and confirmation messages, so users can tell it apart from a built-in.
func customPromptLabel(name string) string {
	return name + customPromptLabelSuffix
}

// ListCustomPrompts returns the names of the stored custom prompts, sorted.
func (a *App) ListCustomPrompts() ([]string, error) {
	store, err := customprompt.DefaultStore()
	if err != nil {
		return nil, err
	}
	entries, err := store.List()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names, nil
}

// LoadCustomPrompt returns the stored text of a custom prompt for editing.
func (a *App) LoadCustomPrompt(name string) (string, error) {
	store, err := customprompt.DefaultStore()
	if err != nil {
		return "", err
	}
	return store.Load(name)
}

// SaveCustomPrompt stores body under name. With create set it refuses to
// replace an existing prompt, so a new prompt never silently overwrites one
// the user already has. When the saved prompt is the one in use, the system
// instruction is reloaded so the change applies to the next request.
func (a *App) SaveCustomPrompt(ctx context.Context, name, body string, create bool) (string, error) {
	store, err := customprompt.DefaultStore()
	if err != nil {
		return "", err
	}
	var saved string
	if create {
		saved, err = store.Create(name, body)
	} else {
		saved, err = store.Save(name, body)
	}
	if err != nil {
		return "", err
	}
	return a.afterCustomPromptWrite(saved)
}

// ImportCustomPrompt copies the text of the file at path into the store under
// name. Relative paths resolve against the session's working directory. The
// source file is not referenced after the copy.
func (a *App) ImportCustomPrompt(ctx context.Context, name, path string, create bool) (string, error) {
	workDir := ""
	if a.runner != nil {
		workDir = a.runner.workDir
	}
	body, err := customprompt.ReadImportFile(path, workDir)
	if err != nil {
		return "", err
	}
	return a.SaveCustomPrompt(ctx, name, body, create)
}

// RenameCustomPrompt renames a stored prompt and updates every settings file
// that selects it, so a rename never leaves a dangling selection.
func (a *App) RenameCustomPrompt(ctx context.Context, oldName, newName string) (string, error) {
	store, err := customprompt.DefaultStore()
	if err != nil {
		return "", err
	}
	from, err := customprompt.NormalizeName(oldName)
	if err != nil {
		return "", err
	}
	to, err := store.Rename(from, newName)
	if err != nil {
		return "", err
	}
	if to == from {
		return fmt.Sprintf("%s is unchanged.", to), nil
	}
	changed, err := a.retargetCustomPromptSelection(from, to)
	if err != nil {
		return "", fmt.Errorf("renamed %s to %s, but updating settings failed: %w", from, to, err)
	}
	if changed {
		if err := a.reloadAfterSelectionChange(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("Renamed %s to %s and updated the selection.", from, to), nil
	}
	return fmt.Sprintf("Renamed %s to %s.", from, to), nil
}

// DeleteCustomPrompt removes a stored prompt and clears it from every
// settings file that selects it, so the built-in default applies instead of a
// fallback warning on every launch.
func (a *App) DeleteCustomPrompt(ctx context.Context, name string) (string, error) {
	store, err := customprompt.DefaultStore()
	if err != nil {
		return "", err
	}
	n, err := customprompt.NormalizeName(name)
	if err != nil {
		return "", err
	}
	if err := store.Delete(n); err != nil {
		return "", err
	}
	changed, err := a.retargetCustomPromptSelection(n, "")
	if err != nil {
		return "", fmt.Errorf("deleted %s, but clearing the selection failed: %w", n, err)
	}
	if changed {
		if err := a.reloadAfterSelectionChange(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("Deleted %s. It was selected, so the built-in default is in use now.", n), nil
	}
	return fmt.Sprintf("Deleted %s.", n), nil
}

// afterCustomPromptWrite reloads the system instruction when the written
// prompt is the one the runner resolves, and returns the confirmation text.
func (a *App) afterCustomPromptWrite(name string) (string, error) {
	if a.runner == nil || a.selectedCustomPrompt() != name {
		return fmt.Sprintf("Saved %s.", name), nil
	}
	if err := a.runner.ReloadSystemInstruction(); err != nil {
		return "", fmt.Errorf("saved %s, but reloading the system prompt failed: %w", name, err)
	}
	return fmt.Sprintf("Saved %s. It is in use, so the change applies to the next request.", name), nil
}

// selectedCustomPrompt is the custom prompt name the merged settings select
// for the live provider and model, or "".
func (a *App) selectedCustomPrompt() string {
	s := a.effectiveSettings()
	if s == nil || a.runner == nil {
		return ""
	}
	return config.ResolveCustomPrompt(s, s.ActiveProvider(), a.runner.Model())
}

// retargetCustomPromptSelection rewrites sagittarius.systemPrompt.custom from
// `from` to `to` in each settings file that selects `from`; an empty `to`
// clears the selection. It never creates a project file that does not exist.
func (a *App) retargetCustomPromptSelection(from, to string) (bool, error) {
	if a.docs == nil {
		return false, nil
	}
	changed := false
	scopes := []struct {
		scope    config.SettingScope
		settings *config.Settings
	}{
		{config.ScopeGlobal, a.docs.Global},
		{config.ScopeProject, a.docs.Project},
	}
	for _, sc := range scopes {
		s := sc.settings
		if s == nil || s.Sagittarius == nil || s.Sagittarius.SystemPrompt == nil {
			continue
		}
		if strings.TrimSpace(s.Sagittarius.SystemPrompt.Custom) != from {
			continue
		}
		next := *s.Sagittarius.SystemPrompt
		next.Custom = to
		if next.IsEmpty() && len(next.Extra) == 0 {
			s.Sagittarius.SystemPrompt = nil
		} else {
			s.Sagittarius.SystemPrompt = &next
		}
		if err := a.docs.Save(sc.scope); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// reloadAfterSelectionChange pushes a settings change that affects the system
// prompt selection into the live runner and status bar.
func (a *App) reloadAfterSelectionChange(ctx context.Context) error {
	h := &appHooks{app: a}
	if err := h.ReloadSystemInstruction(ctx); err != nil {
		return err
	}
	if _, _, err := h.RebuildRunner(ctx); err != nil {
		return err
	}
	return nil
}

// errNoSettingsDocuments is returned when a selection write is attempted
// without loaded settings documents (tests and misconfigured callers).
var errNoSettingsDocuments = errors.New("settings documents not loaded")
