package config

import (
	"encoding/json"
)

// LegacyLocalMigrationResult mirrors fork migrateLegacyLocalSettings.ts output.
type LegacyLocalMigrationResult struct {
	Migrated     bool
	MigratedKeys []struct{ From, To string }
	DroppedKeys  []string
	Settings     *Settings
}

// MigrateLegacyLocalSettings rewrites local.* into providers.local-vllm.* when
// present. Idempotent stub — full parity with fork Phase 2.2 migration.
func MigrateLegacyLocalSettings(s *Settings) LegacyLocalMigrationResult {
	result := LegacyLocalMigrationResult{Settings: s}
	if s == nil {
		return result
	}
	localRaw, ok := s.Raw["local"]
	if !ok || len(localRaw) == 0 {
		return result
	}
	var local map[string]json.RawMessage
	if err := json.Unmarshal(localRaw, &local); err != nil {
		return result
	}
	nonEmpty := legacyNonEmptyKeys(local)
	if len(nonEmpty) == 0 {
		return result
	}

	mappings := []struct{ legacy, provider string }{
		{"url", "baseUrl"},
		{"model", "model"},
		{"contextLimit", "contextLimit"},
		{"timeout", "timeout"},
		{"enableTools", "enableTools"},
		{"promptMode", "promptMode"},
		{"compressionThreshold", "compressionThreshold"},
		{"preserveFraction", "preserveFraction"},
		{"temperature", "temperature"},
	}

	if s.Providers == nil {
		s.Providers = &ProvidersSettings{}
	}
	if s.Providers.Extra == nil {
		s.Providers.Extra = make(map[string]json.RawMessage)
	}

	localVllmRaw := s.Providers.Extra["local-vllm"]
	var localVllm map[string]json.RawMessage
	if len(localVllmRaw) > 0 {
		_ = json.Unmarshal(localVllmRaw, &localVllm)
	}
	if localVllm == nil {
		localVllm = make(map[string]json.RawMessage)
	}

	for _, legacyKey := range nonEmpty {
		mapped := false
		for _, m := range mappings {
			if m.legacy != legacyKey {
				continue
			}
			mapped = true
			if _, exists := localVllm[m.provider]; !exists {
				localVllm[m.provider] = local[legacyKey]
			}
			result.MigratedKeys = append(result.MigratedKeys, struct{ From, To string }{legacyKey, m.provider})
			break
		}
		if !mapped {
			result.DroppedKeys = append(result.DroppedKeys, legacyKey)
		}
	}

	b, _ := json.Marshal(localVllm)
	s.Providers.Extra["local-vllm"] = b

	if s.Providers.Active == "" {
		s.Providers.Active = "local-vllm"
	}

	delete(s.Raw, "local")
	result.Migrated = true
	result.Settings = s
	return result
}

func legacyNonEmptyKeys(local map[string]json.RawMessage) []string {
	var keys []string
	for k, v := range local {
		if len(v) == 0 {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil && s == "" {
			continue
		}
		if string(v) == "null" {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}
