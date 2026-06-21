package contextmgmt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Masking constants ported from toolOutputMaskingService.ts.
const (
	// MaskingIndicatorTag wraps masked tool output so it is recognized and not
	// re-masked.
	MaskingIndicatorTag = "tool_output_masked"
	// ToolOutputsDir is the subdirectory under the output base for offloaded files.
	ToolOutputsDir = "tool-outputs"

	// genericPreviewThreshold is the content length above which a head/tail
	// preview is shown for non-shell tools.
	genericPreviewThreshold = 500
	// genericPreviewSlice is the head/tail character count for the preview.
	genericPreviewSlice = 250
	// simplePreviewMaxLines keeps a preview verbatim at or below this line count.
	simplePreviewMaxLines = 20
	// simplePreviewEdgeLines is the head/tail line count when eliding.
	simplePreviewEdgeLines = 10
)

// Exempt tool names whose outputs are always high-signal and never masked.
// These mirror the fork's EXEMPT_TOOLS; the corresponding tools are not yet
// ported, but the names are kept stable for parity.
const (
	activateSkillToolName = "activate_skill"
	askUserToolName       = "ask_user"
	enterPlanModeToolName = "enter_plan_mode"
	exitPlanModeToolName  = "exit_plan_mode"
)

// DefaultExemptTools returns the set of tools never subject to masking/ejection.
func DefaultExemptTools() map[string]bool {
	return map[string]bool{
		activateSkillToolName: true,
		askUserToolName:       true,
		enterPlanModeToolName: true,
		exitPlanModeToolName:  true,
	}
}

var shellSectionRegex = regexp.MustCompile(`(?m)^(Output|Error|Exit Code|Signal|Background PIDs|Process Group PGID): `)

// MaskingResult reports the outcome of a masking pass.
type MaskingResult struct {
	// NewHistory has bulky tool outputs replaced by markers.
	NewHistory []Message
	// MaskedCount is the number of tool outputs masked.
	MaskedCount int
	// TokensSaved is the estimated tokens reclaimed.
	TokensSaved int
}

// Masker offloads bulky tool outputs to disk and replaces them with a compact
// marker. It implements the fork's "Hybrid Backward Scanned FIFO" algorithm.
type Masker struct {
	// Estimate overrides the default token estimator (test seam).
	Estimate EstimateFn
	// OutputDir is the base directory for offloaded tool-output files.
	OutputDir string
	// SessionID, when set, nests offloaded files under session-<id>.
	SessionID string
	// ExemptTools are never masked.
	ExemptTools map[string]bool
	// ShellToolName enables shell-aware previews for that tool.
	ShellToolName string
}

type prunablePart struct {
	contentIndex int
	partIndex    int
	tokens       int
	content      string
}

// Mask scans history backward, identifies prunable tool outputs beyond the
// protection window, and — once the prunable buffer exceeds the trigger —
// offloads them to disk and replaces each with a marker. The input history is
// not mutated. It returns an error only when the offload directory cannot be
// created or written.
func (m *Masker) Mask(history []Message, cfg ToolOutputMaskingConfig) (MaskingResult, error) {
	if len(history) == 0 {
		return MaskingResult{NewHistory: history}, nil
	}
	estimate := m.Estimate
	if estimate == nil {
		estimate = EstimateTokens
	}

	prunable, totalPrunable := m.scanPrunable(history, cfg, estimate)
	if totalPrunable < cfg.MinPrunableThresholdTokens {
		return MaskingResult{NewHistory: history}, nil
	}

	outputDir := m.resolveOutputDir()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return MaskingResult{}, fmt.Errorf("create tool-output dir: %w", err)
	}

	newHistory := cloneHistory(history)
	result := MaskingResult{NewHistory: newHistory}
	for _, item := range prunable {
		saved, err := m.maskPart(newHistory, item, estimate, outputDir)
		if err != nil {
			return MaskingResult{}, err
		}
		if saved > 0 {
			result.MaskedCount++
			result.TokensSaved += saved
		}
	}
	return result, nil
}

func (m *Masker) scanPrunable(history []Message, cfg ToolOutputMaskingConfig, estimate EstimateFn) ([]prunablePart, int) {
	scanStart := len(history) - 1
	if cfg.ProtectLatestTurn {
		scanStart = len(history) - 2
	}

	var prunable []prunablePart
	cumulative := 0
	boundaryReached := false
	total := 0

	for i := scanStart; i >= 0; i-- {
		parts := history[i].Parts
		for j := len(parts) - 1; j >= 0; j-- {
			fr := parts[j].FunctionResponse
			if fr == nil {
				continue
			}
			if m.ExemptTools[fr.Name] {
				continue
			}
			content := toolOutputContent(fr)
			if content == "" || isAlreadyMasked(content) {
				continue
			}
			tokens := estimate([]Part{parts[j]})
			if !boundaryReached {
				cumulative += tokens
				if cumulative > cfg.ProtectionThresholdTokens {
					boundaryReached = true
					total += tokens
					prunable = append(prunable, prunablePart{i, j, tokens, content})
				}
				continue
			}
			total += tokens
			prunable = append(prunable, prunablePart{i, j, tokens, content})
		}
	}
	return prunable, total
}

func (m *Masker) maskPart(history []Message, item prunablePart, estimate EstimateFn, outputDir string) (int, error) {
	content := history[item.contentIndex]
	part := content.Parts[item.partIndex]
	fr := part.FunctionResponse
	if fr == nil {
		return 0, nil
	}

	toolName := fr.Name
	if toolName == "" {
		toolName = "unknown_tool"
	}
	filePath, err := m.writeOffload(outputDir, toolName, item.content)
	if err != nil {
		return 0, err
	}

	preview := m.buildPreview(toolName, fr.Response, item.content)
	snippet := formatMaskedSnippet(preview, filePath)
	maskedPart := Part{FunctionResponse: &FunctionResponse{
		Name:     fr.Name,
		Response: map[string]any{"output": snippet},
	}}

	saved := item.tokens - estimate([]Part{maskedPart})
	if saved <= 0 {
		return 0, nil
	}

	newParts := make([]Part, len(content.Parts))
	copy(newParts, content.Parts)
	newParts[item.partIndex] = maskedPart
	history[item.contentIndex] = Message{Role: content.Role, Parts: newParts}
	return saved, nil
}

func (m *Masker) buildPreview(toolName string, response map[string]any, content string) string {
	if m.ShellToolName != "" && toolName == m.ShellToolName {
		return formatShellPreview(response)
	}
	if len(content) > genericPreviewThreshold {
		return content[:genericPreviewSlice] + "\n... [TRUNCATED] ...\n" + content[len(content)-genericPreviewSlice:]
	}
	return content
}

func (m *Masker) resolveOutputDir() string {
	dir := m.OutputDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "sagittarius")
	}
	dir = filepath.Join(dir, ToolOutputsDir)
	if m.SessionID != "" {
		dir = filepath.Join(dir, "session-"+sanitizeFilenamePart(m.SessionID))
	}
	return dir
}

func (m *Masker) writeOffload(dir, toolName, content string) (string, error) {
	name := fmt.Sprintf("%s_%s.txt", strings.ToLower(sanitizeFilenamePart(toolName)), randomSuffix())
	filePath := filepath.Join(dir, name)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write tool-output offload: %w", err)
	}
	return filePath, nil
}

func toolOutputContent(fr *FunctionResponse) string {
	if fr.Response == nil {
		return ""
	}
	b, err := json.MarshalIndent(fr.Response, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func isAlreadyMasked(content string) bool {
	return strings.Contains(content, "<"+MaskingIndicatorTag)
}

func formatMaskedSnippet(preview, filePath string) string {
	return fmt.Sprintf("<%s>\n%s\n\nOutput too large. Full output available at: %s\n</%s>",
		MaskingIndicatorTag, preview, filePath, MaskingIndicatorTag)
}

func formatShellPreview(response map[string]any) string {
	content := stringField(response, "output")
	if content == "" {
		content = stringField(response, "stdout")
	}

	headers := shellSectionRegex.FindAllStringSubmatchIndex(content, -1)
	if len(headers) == 0 {
		return formatSimplePreview(content)
	}

	var previewParts []string
	if pre := strings.TrimSpace(content[:headers[0][0]]); pre != "" {
		previewParts = append(previewParts, formatSimplePreview(pre))
	}
	for idx, h := range headers {
		name := content[h[2]:h[3]]
		sectionStart := h[1]
		sectionEnd := len(content)
		if idx+1 < len(headers) {
			sectionEnd = headers[idx+1][0]
		}
		section := strings.TrimSpace(content[sectionStart:sectionEnd])
		if name == "Output" {
			previewParts = append(previewParts, "Output: "+formatSimplePreview(section))
		} else {
			previewParts = append(previewParts, name+": "+section)
		}
	}
	preview := strings.Join(previewParts, "\n")

	preview = appendRootShellFields(preview, response, content)
	return preview
}

func appendRootShellFields(preview string, response map[string]any, content string) string {
	exitCode, hasExit := response["exitCode"]
	if !hasExit {
		exitCode, hasExit = response["exit_code"]
	}
	if hasExit && !isZeroOrNil(exitCode) {
		marker := fmt.Sprintf("Exit Code: %v", exitCode)
		if !strings.Contains(content, marker) {
			preview += fmt.Sprintf("\n[Exit Code: %v]", exitCode)
		}
	}
	if errVal, ok := response["error"]; ok && errVal != nil {
		marker := fmt.Sprintf("Error: %v", errVal)
		if !strings.Contains(content, marker) {
			preview += fmt.Sprintf("\n[Error: %v]", errVal)
		}
	}
	return preview
}

func formatSimplePreview(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= simplePreviewMaxLines {
		return content
	}
	head := lines[:simplePreviewEdgeLines]
	tail := lines[len(lines)-simplePreviewEdgeLines:]
	omitted := len(lines) - len(head) - len(tail)
	return fmt.Sprintf("%s\n\n... [%d lines omitted] ...\n\n%s",
		strings.Join(head, "\n"), omitted, strings.Join(tail, "\n"))
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func isZeroOrNil(v any) bool {
	switch n := v.(type) {
	case nil:
		return true
	case int:
		return n == 0
	case int64:
		return n == 0
	case float64:
		return n == 0
	default:
		return false
	}
}

func sanitizeFilenamePart(part string) string {
	var b strings.Builder
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func randomSuffix() string {
	var buf [5]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "offload"
	}
	return hex.EncodeToString(buf[:])
}
