package hooks

import (
	"context"
	"fmt"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// HookInfo describes a loaded hook for reporting / CLI listing.
type HookInfo struct {
	Event       HookEventName `json:"event"`
	Matcher     string        `json:"matcher"`
	Sequential  bool          `json:"sequential"`
	Name        string        `json:"name"`
	Command     string        `json:"command"`
	Source      ConfigSource  `json:"source"`
	Enabled     bool          `json:"enabled"`
	Trusted     bool          `json:"trusted"`
	Timeout     int           `json:"timeout"`
	Description string        `json:"description,omitempty"`
}

// Registry manages loaded hook definitions, trust status, and execution.
type Registry struct {
	globalHome    string
	projectRoot   string
	plansDir      string
	runner        *Runner
	trustMgr      *TrustManager
	globalEnabled bool
	disabledNames map[string]bool
	events        map[HookEventName][]HookDefinition
	mu            sync.RWMutex
}

// NewRegistry initializes an empty Registry.
func NewRegistry(globalHome, projectRoot, plansDir string) *Registry {
	return &Registry{
		globalHome:    globalHome,
		projectRoot:   projectRoot,
		plansDir:      plansDir,
		runner:        NewRunner(plansDir),
		trustMgr:      NewTrustManager(globalHome),
		globalEnabled: true,
		disabledNames: make(map[string]bool),
		events:        make(map[HookEventName][]HookDefinition),
	}
}

// TrustManager returns the underlying trust manager.
func (r *Registry) TrustManager() *TrustManager {
	return r.trustMgr
}

// LoadConfig builds hook definitions from global and project config documents.
func (r *Registry) LoadConfig(globalCfg, projectCfg *config.HooksConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = make(map[HookEventName][]HookDefinition)
	r.disabledNames = make(map[string]bool)
	r.globalEnabled = true

	// Check global enabled
	if globalCfg != nil && globalCfg.Enabled != nil {
		r.globalEnabled = *globalCfg.Enabled
	}
	// Project enabled wins if defined
	if projectCfg != nil && projectCfg.Enabled != nil {
		r.globalEnabled = *projectCfg.Enabled
	}

	// Collect disabled names
	if globalCfg != nil {
		for _, name := range globalCfg.Disabled {
			r.disabledNames[name] = true
		}
	}
	if projectCfg != nil {
		for _, name := range projectCfg.Disabled {
			r.disabledNames[name] = true
		}
	}

	// Helper to load event definitions
	addDefs := func(cfg *config.HooksConfig, source ConfigSource) {
		if cfg == nil || len(cfg.Events) == 0 {
			return
		}
		for evtStr, defs := range cfg.Events {
			evt := HookEventName(evtStr)
			for _, d := range defs {
				hookConfigs := make([]HookConfig, 0, len(d.Hooks))
				for _, h := range d.Hooks {
					hc := HookConfig{
						Type:        HookType(h.Type),
						Command:     h.Command,
						Name:        h.Name,
						Description: h.Description,
						Timeout:     h.Timeout,
						Source:      source,
						Env:         h.Env,
					}
					if hc.Type == "" {
						hc.Type = TypeCommand
					}
					hookConfigs = append(hookConfigs, hc)
				}
				r.events[evt] = append(r.events[evt], HookDefinition{
					Matcher:    d.Matcher,
					Sequential: d.Sequential,
					Hooks:      hookConfigs,
				})
			}
		}
	}

	addDefs(globalCfg, SourceUser)
	addDefs(projectCfg, SourceProject)
}

// FireEvent runs all matching trusted enabled hooks for an event.
func (r *Registry) FireEvent(ctx context.Context, event HookEventName, target string, input HookInput) ([]ExecutionResult, error) {
	r.mu.RLock()
	if !r.globalEnabled {
		r.mu.RUnlock()
		return nil, nil
	}
	defs := r.events[event]
	projectRoot := r.projectRoot
	disabledMap := make(map[string]bool, len(r.disabledNames))
	for k, v := range r.disabledNames {
		disabledMap[k] = v
	}
	r.mu.RUnlock()

	if len(defs) == 0 {
		return nil, nil
	}

	var allResults []ExecutionResult

	for _, def := range defs {
		if !Match(event, def.Matcher, target) {
			continue
		}

		// Filter active hooks in this definition
		var activeHooks []HookConfig
		for _, h := range def.Hooks {
			if disabledMap[h.Key()] {
				continue
			}
			if !r.trustMgr.IsTrusted(projectRoot, h) {
				// Untrusted project hook skipped
				allResults = append(allResults, ExecutionResult{
					HookConfig: h,
					EventName:  event,
					Success:    false,
					Error:      fmt.Errorf("untrusted project hook %q skipped", h.Key()),
				})
				continue
			}
			activeHooks = append(activeHooks, h)
		}

		if len(activeHooks) == 0 {
			continue
		}

		var groupResults []ExecutionResult
		if def.Sequential {
			groupResults = r.runner.ExecuteHooksSequential(ctx, activeHooks, event, input)
		} else {
			groupResults = r.runner.ExecuteHooksParallel(ctx, activeHooks, event, input)
		}

		allResults = append(allResults, groupResults...)

		// Check if any hook stopped execution via continue=false or decision=deny/block
		for _, res := range groupResults {
			if res.Output != nil && (res.Output.IsBlocking() || res.Output.ShouldStop()) {
				return allResults, nil
			}
		}
	}

	return allResults, nil
}

// ListHooks returns a list of all configured hooks and their status.
func (r *Registry) ListHooks() []HookInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []HookInfo
	for evt, defs := range r.events {
		for _, d := range defs {
			for _, h := range d.Hooks {
				enabled := r.globalEnabled && !r.disabledNames[h.Key()]
				trusted := r.trustMgr.IsTrusted(r.projectRoot, h)
				list = append(list, HookInfo{
					Event:       evt,
					Matcher:     d.Matcher,
					Sequential:  d.Sequential,
					Name:        h.Name,
					Command:     h.Command,
					Source:      h.Source,
					Enabled:     enabled,
					Trusted:     trusted,
					Timeout:     h.Timeout,
					Description: h.Description,
				})
			}
		}
	}
	return list
}

// SetEnabled toggles global hooks execution.
func (r *Registry) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalEnabled = enabled
}

// IsEnabled returns whether hooks are globally enabled.
func (r *Registry) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.globalEnabled
}

// DisableHook disables a specific hook by name or command key.
func (r *Registry) DisableHook(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabledNames[key] = true
}

// EnableHook re-enables a specific hook by name or command key.
func (r *Registry) EnableHook(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.disabledNames, key)
}

// ExecuteTestHook executes a specific hook by name or command key for testing.
func (r *Registry) ExecuteTestHook(ctx context.Context, key string, input HookInput) (ExecutionResult, error) {
	r.mu.RLock()
	defs := r.events
	r.mu.RUnlock()

	for evt, defGroup := range defs {
		for _, d := range defGroup {
			for _, h := range d.Hooks {
				if h.Key() == key || h.Name == key || h.Command == key {
					res := r.runner.ExecuteHook(ctx, h, evt, input)
					return res, nil
				}
			}
		}
	}
	return ExecutionResult{}, fmt.Errorf("hook %q not found", key)
}
