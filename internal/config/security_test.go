package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func TestProjectBoundaryEnforcedResolution(t *testing.T) {
	enforced := &Settings{Security: &SecuritySettings{
		ProjectBoundary: &ProjectBoundaryConfig{Enforce: boolPtr(true)},
	}}
	disabled := &Settings{Security: &SecuritySettings{
		ProjectBoundary: &ProjectBoundaryConfig{Enforce: boolPtr(false)},
	}}

	cases := []struct {
		name            string
		global, project *Settings
		want            bool
	}{
		{"both nil defaults false", nil, nil, false},
		{"global on", enforced, nil, true},
		{"project overrides global off", disabled, enforced, true},
		{"project overrides global on", enforced, disabled, false},
		{"global off only", disabled, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProjectBoundaryEnforced(tc.global, tc.project); got != tc.want {
				t.Fatalf("ProjectBoundaryEnforced = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSnapshotResolution(t *testing.T) {
	if !SnapshotsEnabled(nil, nil) {
		t.Fatal("snapshots should default enabled")
	}
	if got := SnapshotMaxFileBytes(nil, nil); got != DefaultSnapshotMaxFileBytes {
		t.Fatalf("default max = %d, want %d", got, DefaultSnapshotMaxFileBytes)
	}

	global := &Settings{Sagittarius: &SagittariusSettings{
		Snapshots: &SagittariusSnapshotConfig{Enabled: boolPtr(true), MaxFileBytes: intPtr(1000)},
	}}
	project := &Settings{Sagittarius: &SagittariusSettings{
		Snapshots: &SagittariusSnapshotConfig{Enabled: boolPtr(false)},
	}}
	if SnapshotsEnabled(global, project) {
		t.Fatal("project should disable snapshots over global")
	}
	if got := SnapshotMaxFileBytes(global, project); got != 1000 {
		t.Fatalf("max = %d, want 1000 (project unset falls back to global)", got)
	}
}

func TestSecuritySnapshotRoundTrip(t *testing.T) {
	in := &Settings{
		Security: &SecuritySettings{
			ProjectBoundary: &ProjectBoundaryConfig{Enforce: boolPtr(true)},
		},
		Sagittarius: &SagittariusSettings{
			Snapshots: &SagittariusSnapshotConfig{Enabled: boolPtr(true), MaxFileBytes: intPtr(4096)},
		},
		Raw: map[string]json.RawMessage{},
	}
	encoded, err := encodeSettingsDocument(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeSettingsDocument(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ProjectBoundaryEnforced(decoded, nil) {
		t.Fatalf("boundary lost in round-trip: %s", encoded)
	}
	if got := SnapshotMaxFileBytes(decoded, nil); got != 4096 {
		t.Fatalf("maxFileBytes = %d after round-trip", got)
	}
}

func TestEmptySnapshotConfigOmitted(t *testing.T) {
	in := &Settings{
		Sagittarius: &SagittariusSettings{DefaultModel: "x"},
		Raw:         map[string]json.RawMessage{},
	}
	encoded, err := encodeSettingsDocument(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := doc["security"]; ok {
		t.Errorf("empty security section should be omitted")
	}
}

func TestLoadProjectSettings(t *testing.T) {
	workDir := t.TempDir()
	dir := filepath.Join(workDir, SagittariusDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"security":{"projectBoundary":{"enforce":true}}}`
	if err := os.WriteFile(filepath.Join(dir, settingsFileName), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ps, err := LoadProjectSettings(workDir)
	if err != nil {
		t.Fatalf("LoadProjectSettings: %v", err)
	}
	if !ProjectBoundaryEnforced(nil, ps) {
		t.Fatal("project settings should enforce boundary")
	}

	// Missing project file returns (nil, nil).
	other, err := LoadProjectSettings(t.TempDir())
	if err != nil || other != nil {
		t.Fatalf("missing project settings = (%v, %v), want (nil, nil)", other, err)
	}
}
