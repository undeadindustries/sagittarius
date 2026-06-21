package contextmgmt

import (
	"strings"
	"testing"
)

const shellTool = "run_shell_command"

func respMsg(name string, response map[string]any) Message {
	return Message{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: name, Response: response}}}}
}

func outMsg(name, output string) Message {
	return respMsg(name, map[string]any{"output": output})
}

func outResponse(m Message) string {
	if len(m.Parts) == 0 || m.Parts[0].FunctionResponse == nil {
		return ""
	}
	if v, ok := m.Parts[0].FunctionResponse.Response["output"].(string); ok {
		return v
	}
	if v, ok := m.Parts[0].FunctionResponse.Response["result"].(string); ok {
		return v
	}
	return ""
}

// maskedAwareEstimator returns maskedVal for already-masked parts, else the
// per-tool value (or fallback), mirroring the fork's mocked estimator.
func maskedAwareEstimator(maskedVal, fallback int, byName map[string]int) EstimateFn {
	return func(parts []Part) int {
		if len(parts) == 0 || parts[0].FunctionResponse == nil {
			return fallback
		}
		fr := parts[0].FunctionResponse
		content, _ := fr.Response["output"].(string)
		if content == "" {
			content, _ = fr.Response["result"].(string)
		}
		if strings.Contains(content, "<"+MaskingIndicatorTag) {
			return maskedVal
		}
		if v, ok := byName[fr.Name]; ok {
			return v
		}
		return fallback
	}
}

func newMasker(t *testing.T, estimate EstimateFn) *Masker {
	t.Helper()
	return &Masker{
		Estimate:      estimate,
		OutputDir:     t.TempDir(),
		ExemptTools:   DefaultExemptTools(),
		ShellToolName: shellTool,
	}
}

func TestMaskRespectsRemoteConfigOverrides(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(10, 0, map[string]int{"test_tool": 200}))
	history := []Message{outMsg("test_tool", strings.Repeat("A", 200))}
	res, err := m.Mask(history, ToolOutputMaskingConfig{
		ProtectionThresholdTokens:  100,
		MinPrunableThresholdTokens: 50,
		ProtectLatestTurn:          false,
	})
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 1 {
		t.Fatalf("MaskedCount = %d, want 1", res.MaskedCount)
	}
	if res.TokensSaved <= 0 {
		t.Errorf("TokensSaved = %d, want > 0", res.TokensSaved)
	}
}

func TestMaskSkipsBelowProtectionThreshold(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(10, 100, nil))
	history := []Message{outMsg("test_tool", "small output")}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 0 {
		t.Errorf("MaskedCount = %d, want 0", res.MaskedCount)
	}
}

func TestMaskProtectsLatestTurn(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(100, 0, map[string]int{"t1": 60_000, "t2": 20_000, "t3": 10_000}))
	history := []Message{
		outMsg("t1", strings.Repeat("A", 60_000)),
		outMsg("t2", strings.Repeat("B", 20_000)),
		outMsg("t3", strings.Repeat("C", 10_000)),
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 1 {
		t.Fatalf("MaskedCount = %d, want 1", res.MaskedCount)
	}
	if !strings.Contains(outResponse(res.NewHistory[0]), "<"+MaskingIndicatorTag) {
		t.Errorf("history[0] not masked")
	}
	if outResponse(res.NewHistory[1]) != strings.Repeat("B", 20_000) {
		t.Errorf("history[1] should be untouched")
	}
	if outResponse(res.NewHistory[2]) != strings.Repeat("C", 10_000) {
		t.Errorf("history[2] (latest) should be untouched")
	}
}

func TestMaskGlobalAggregation(t *testing.T) {
	t.Parallel()
	estimate := func(parts []Part) int {
		fr := parts[0].FunctionResponse
		content, _ := fr.Response["output"].(string)
		if strings.Contains(content, "<"+MaskingIndicatorTag) {
			return 100
		}
		return len(content)
	}
	m := newMasker(t, estimate)
	history := make([]Message, 12)
	for i := range history {
		history[i] = outMsg("tool", strings.Repeat("A", 10_000))
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 6 {
		t.Errorf("MaskedCount = %d, want 6", res.MaskedCount)
	}
	if res.TokensSaved <= 0 {
		t.Errorf("TokensSaved = %d, want > 0", res.TokensSaved)
	}
}

func TestMaskShellAwarePreview(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(100, 100, map[string]int{shellTool: 100_000, "p": 60_000}))
	history := []Message{
		outMsg(shellTool, "Output: line1\nline2\nline3\nline4\nline5\nError: failed\nExit Code: 1"),
		outMsg("p", strings.Repeat("p", 60_000)),
		outMsg("l", "l"),
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	masked := outResponse(res.NewHistory[0])
	for _, want := range []string{"Output: line1\nline2\nline3\nline4\nline5", "Exit Code: 1", "Error: failed"} {
		if !strings.Contains(masked, want) {
			t.Errorf("masked shell preview missing %q; got:\n%s", want, masked)
		}
	}
}

func TestMaskSkipsAlreadyMasked(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(100, 60_000, nil))
	history := []Message{
		outMsg("tool1", "<"+MaskingIndicatorTag+">...</"+MaskingIndicatorTag+">"),
		outMsg("tool2", strings.Repeat("A", 60_000)),
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 0 {
		t.Errorf("MaskedCount = %d, want 0 (tool1 masked, tool2 protected latest)", res.MaskedCount)
	}
}

func TestMaskDifferentResponseKeys(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(100, 60_000, nil))
	history := []Message{
		respMsg("t1", map[string]any{"result": strings.Repeat("A", 60_000)}),
		respMsg("p", map[string]any{"output": strings.Repeat("P", 60_000)}),
		{Role: RoleUser, Parts: []Part{{Text: "latest"}}},
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 2 {
		t.Fatalf("MaskedCount = %d, want 2", res.MaskedCount)
	}
	keys := res.NewHistory[0].Parts[0].FunctionResponse.Response
	if len(keys) != 1 {
		t.Errorf("masked response keys = %v, want only output", keys)
	}
	if _, ok := keys["output"]; !ok {
		t.Errorf("masked response missing output key")
	}
}

func TestMaskPreservesSiblingParts(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(100, 100, map[string]int{"t1": 60_000, "p": 60_000}))
	history := []Message{
		{Role: RoleUser, Parts: []Part{
			{FunctionResponse: &FunctionResponse{Name: "t1", Response: map[string]any{"output": strings.Repeat("A", 60_000)}}},
			{Text: "sibling"},
		}},
		outMsg("p", strings.Repeat("p", 60_000)),
		{Role: RoleUser, Parts: []Part{{Text: "latest"}}},
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 2 {
		t.Fatalf("MaskedCount = %d, want 2", res.MaskedCount)
	}
	if len(res.NewHistory[0].Parts) != 2 {
		t.Fatalf("history[0] parts = %d, want 2", len(res.NewHistory[0].Parts))
	}
	if res.NewHistory[0].Parts[1].Text != "sibling" {
		t.Errorf("sibling part not preserved")
	}
	if !strings.Contains(outResponse(res.NewHistory[0]), "<"+MaskingIndicatorTag) {
		t.Errorf("history[0] functionResponse not masked")
	}
}

// nameOnlyEstimator keys purely on tool name (no masked-tag detection), so a
// masked part re-estimates to the same value as the original — used to prove
// masking is skipped when it would not reduce tokens.
func nameOnlyEstimator(fallback int, byName map[string]int) EstimateFn {
	return func(parts []Part) int {
		if len(parts) == 0 || parts[0].FunctionResponse == nil {
			return fallback
		}
		if v, ok := byName[parts[0].FunctionResponse.Name]; ok {
			return v
		}
		return fallback
	}
}

func TestMaskSkipsWhenMaskingInflates(t *testing.T) {
	t.Parallel()
	m := newMasker(t, nameOnlyEstimator(1_000, map[string]int{"tiny_tool": 5, "padding": 60_000}))
	history := []Message{
		outMsg("tiny_tool", "tiny"),
		outMsg("padding", strings.Repeat("B", 60_000)),
		{Role: RoleUser, Parts: []Part{{Text: "latest"}}},
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 0 {
		t.Errorf("MaskedCount = %d, want 0", res.MaskedCount)
	}
}

func TestMaskNeverMasksExemptTools(t *testing.T) {
	t.Parallel()
	m := newMasker(t, maskedAwareEstimator(100, 10, map[string]int{
		activateSkillToolName: 1_000, "bulky_tool": 60_000, "padding": 60_000,
	}))
	history := []Message{
		outMsg(activateSkillToolName, "High value instructions for skill"),
		outMsg("bulky_tool", strings.Repeat("A", 60_000)),
		outMsg("padding", strings.Repeat("B", 60_000)),
		{Role: RoleUser, Parts: []Part{{Text: "latest"}}},
	}
	res, err := m.Mask(history, defaultMaskingConfig())
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if res.MaskedCount != 2 {
		t.Fatalf("MaskedCount = %d, want 2", res.MaskedCount)
	}
	if outResponse(res.NewHistory[0]) != "High value instructions for skill" {
		t.Errorf("exempt tool was masked")
	}
	if !strings.Contains(outResponse(res.NewHistory[1]), MaskingIndicatorTag) {
		t.Errorf("bulky_tool should be masked")
	}
}

func defaultMaskingConfig() ToolOutputMaskingConfig {
	return ToolOutputMaskingConfig{
		ProtectionThresholdTokens:  DefaultToolProtectionThreshold,
		MinPrunableThresholdTokens: DefaultMinPrunableTokensThreshold,
		ProtectLatestTurn:          true,
	}
}
