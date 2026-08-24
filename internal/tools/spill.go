package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxModelOutputBytes = 64 * 1024
	spillDirPerm        = 0o700
	spillFilePerm       = 0o600
	spillHeadNumer      = 3
	spillTailNumer      = 3
	spillBudgetDenom    = 10
	spillFilePrefix     = "sagittarius-spill-"
	spillFileSuffix     = ".log"
)

type spillResult struct {
	output     string
	spillPath  string
	truncated  bool
	totalLines int
}

func maybeSpillOutput(output, spillDir string) spillResult {
	return maybeSpillOutputN(output, spillDir, maxModelOutputBytes)
}

func maybeSpillOutputN(output, spillDir string, budget int) spillResult {
	if budget <= 0 {
		budget = maxModelOutputBytes
	}
	if len(output) <= budget {
		return spillResult{output: output}
	}

	head, tail, omitted := splitHeadTail(output, budget)
	totalLines := countOutputLines(output)
	path := writeSpillFile(spillDir, output)
	marker := spillOmittedMarker(omitted, totalLines, path)
	return spillResult{
		output:     assemble(head, tail, marker),
		spillPath:  path,
		truncated:  true,
		totalLines: totalLines,
	}
}

func splitHeadTail(s string, maxBytes int) (head, tail string, omitted int) {
	if len(s) <= maxBytes {
		return s, "", 0
	}
	headBytes := maxBytes * spillHeadNumer / spillBudgetDenom
	tailBytes := maxBytes * spillTailNumer / spillBudgetDenom
	if headBytes < 1 {
		headBytes = 1
	}
	if tailBytes < 1 {
		tailBytes = 1
	}

	head = clipPrefix(s, headBytes)
	tail = clipSuffix(s, tailBytes)
	if idx := strings.LastIndex(head, "\n"); idx > 0 {
		head = head[:idx]
	}
	if idx := strings.Index(tail, "\n"); idx >= 0 {
		tail = tail[idx+1:]
	}

	omitted = len(s) - len(head) - len(tail)
	if omitted < 0 {
		omitted = 0
	}
	return head, tail, omitted
}

func assemble(head, tail, marker string) string {
	return head + "\n\n" + marker + "\n\n" + tail
}

func spillOmittedMarker(omitted, totalLines int, spillPath string) string {
	if spillPath != "" {
		return fmt.Sprintf(
			"[... %d bytes omitted (%d lines total). Full output: %s — read_file it with start_line/end_line ...]",
			omitted, totalLines, spillPath,
		)
	}
	return fmt.Sprintf(
		"[... %d bytes omitted (%d lines total); no spill file was written — re-run with narrower arguments to see the rest ...]",
		omitted, totalLines,
	)
}

func backgroundOmittedMarker(omitted, totalLines int, logPath string) string {
	return fmt.Sprintf(
		"[... %d bytes omitted (%d lines total). Full output is still being written to %s ...]",
		omitted, totalLines, logPath,
	)
}

func writeSpillFile(spillDir, output string) string {
	if strings.TrimSpace(spillDir) == "" {
		return ""
	}
	if err := os.MkdirAll(spillDir, spillDirPerm); err != nil {
		return ""
	}
	path := filepath.Join(spillDir, newSpillFileName())
	if err := writeExclusive(path, []byte(output)); err != nil {
		return ""
	}
	return path
}

func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, spillFilePerm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

// resolveSpillReadPath reports whether path is a spill artifact this process
// created: an absolute path whose parent is exactly spillDir, whose name
// matches the generated pattern, and which is a regular file (not a symlink).
func resolveSpillReadPath(path, spillDir string) (string, bool) {
	spillDir = strings.TrimSpace(spillDir)
	if spillDir == "" {
		return "", false
	}
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	parent := filepath.Clean(filepath.Dir(clean))
	if parent != filepath.Clean(spillDir) {
		return "", false
	}
	name := filepath.Base(clean)
	if !strings.HasPrefix(name, spillFilePrefix) || !strings.HasSuffix(name, spillFileSuffix) {
		return "", false
	}
	if len(name) <= len(spillFilePrefix)+len(spillFileSuffix) {
		return "", false
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", false
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false
	}
	return clean, true
}

func applySpillMeta(result map[string]any, spill spillResult) {
	if !spill.truncated {
		return
	}
	result["truncated"] = true
	result["total_lines"] = spill.totalLines
	if spill.spillPath != "" {
		result["spill_file"] = spill.spillPath
	}
}

func appendSpillHint(text string, result map[string]any) string {
	path, _ := result["spill_file"].(string)
	if path == "" {
		return text
	}
	return text + "\n\n[Full output: " + path + "]"
}

func countOutputLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func newSpillFileName() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s%d%s", spillFilePrefix, os.Getpid(), spillFileSuffix)
	}
	return spillFilePrefix + hex.EncodeToString(b[:]) + spillFileSuffix
}

func clipPrefix(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func clipSuffix(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
