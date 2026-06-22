package snapshot

import (
	"fmt"
	"strings"
)

// maxDiffCells bounds the LCS table size (rows*cols). Larger inputs fall back to
// a one-line summary so a huge generated file cannot allocate gigabytes.
const maxDiffCells = 4_000_000

// diffContext is the number of unchanged lines shown around each hunk.
const diffContext = 3

type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type diffOp struct {
	kind opKind
	a    int // index into the "before" lines (opEqual/opDelete)
	b    int // index into the "after" lines (opEqual/opInsert)
}

// UnifiedDiff renders a git-style unified diff of before -> after for path.
// It returns "" when the two inputs are identical. For very large inputs it
// returns a one-line summary instead of a full diff.
func UnifiedDiff(before, after, path string) string {
	if before == after {
		return ""
	}
	a := splitLines(before)
	b := splitLines(after)

	header := fmt.Sprintf("--- a/%s\n+++ b/%s\n", path, path)

	if (len(a)+1)*(len(b)+1) > maxDiffCells {
		return header + fmt.Sprintf("@@ diff too large to render (%d -> %d lines) @@\n", len(a), len(b))
	}

	ops := diffOps(a, b)
	hunks := buildHunks(ops, a, b)
	if hunks == "" {
		return ""
	}
	return header + hunks
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// A trailing newline yields a final empty element; drop it so it is not
	// rendered as a spurious blank line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// diffOps computes a line-level edit script via a standard LCS table.
func diffOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{opEqual, i, j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{opDelete, i, -1})
			i++
		default:
			ops = append(ops, diffOp{opInsert, -1, j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{opDelete, i, -1})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{opInsert, -1, j})
	}
	return ops
}

// buildHunks groups the edit script into unified-diff hunks with diffContext
// lines of surrounding context.
func buildHunks(ops []diffOp, a, b []string) string {
	// Indices of ops that are changes (insert/delete).
	changed := false
	for _, op := range ops {
		if op.kind != opEqual {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}

	var out strings.Builder
	i := 0
	for i < len(ops) {
		if ops[i].kind == opEqual {
			i++
			continue
		}
		// Find the start of this hunk including leading context.
		start := i
		ctx := 0
		for start > 0 && ops[start-1].kind == opEqual && ctx < diffContext {
			start--
			ctx++
		}
		// Extend to the end of the change run plus trailing context, merging
		// nearby changes separated by <= 2*diffContext equal lines.
		end := i
		for end < len(ops) {
			if ops[end].kind != opEqual {
				end++
				continue
			}
			// Count the equal run ahead.
			run := 0
			for end+run < len(ops) && ops[end+run].kind == opEqual {
				run++
			}
			hasMore := end+run < len(ops)
			if hasMore && run <= 2*diffContext {
				end += run
				continue
			}
			// Append up to diffContext trailing context lines and stop.
			trail := run
			if trail > diffContext {
				trail = diffContext
			}
			end += trail
			break
		}
		writeHunk(&out, ops[start:end], a, b)
		i = end
	}
	return out.String()
}

func writeHunk(out *strings.Builder, ops []diffOp, a, b []string) {
	aStart, bStart := -1, -1
	aCount, bCount := 0, 0
	for _, op := range ops {
		switch op.kind {
		case opEqual:
			if aStart < 0 {
				aStart, bStart = op.a, op.b
			}
			aCount++
			bCount++
		case opDelete:
			if aStart < 0 {
				aStart = op.a
				bStart = bIndexFor(ops, op)
			}
			aCount++
		case opInsert:
			if bStart < 0 {
				bStart = op.b
				aStart = aIndexFor(ops, op)
			}
			bCount++
		}
	}
	// Convert to 1-based line numbers; 0 count uses a 0 start per diff convention.
	a1 := aStart + 1
	b1 := bStart + 1
	if aCount == 0 {
		a1 = aStart
	}
	if bCount == 0 {
		b1 = bStart
	}
	fmt.Fprintf(out, "@@ -%d,%d +%d,%d @@\n", a1, aCount, b1, bCount)
	for _, op := range ops {
		switch op.kind {
		case opEqual:
			out.WriteString(" " + a[op.a] + "\n")
		case opDelete:
			out.WriteString("-" + a[op.a] + "\n")
		case opInsert:
			out.WriteString("+" + b[op.b] + "\n")
		}
	}
}

// bIndexFor finds the b-line anchor for a hunk that starts with a deletion.
func bIndexFor(ops []diffOp, target diffOp) int {
	for _, op := range ops {
		if op.kind != opDelete && op.b >= 0 {
			return op.b
		}
		if op.kind == opDelete && op == target {
			break
		}
	}
	return 0
}

// aIndexFor finds the a-line anchor for a hunk that starts with an insertion.
func aIndexFor(ops []diffOp, target diffOp) int {
	for _, op := range ops {
		if op.kind != opInsert && op.a >= 0 {
			return op.a
		}
		if op.kind == opInsert && op == target {
			break
		}
	}
	return 0
}
