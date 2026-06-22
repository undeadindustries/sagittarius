package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Snapshotter records file mutations so the user can review them (/diff) and
// revert them (/undo). The scheduler calls CaptureWrite immediately before a
// write_file execution and CommitWrite immediately after it succeeds. A nil
// Snapshotter disables snapshotting.
type Snapshotter interface {
	CaptureWrite(absPath string)
	CommitWrite(absPath, toolName string)
}

// IsProtectedWritePath reports whether absPath is Sagittarius-managed metadata
// the agent must never write to, regardless of boundary enforcement. Snapshot
// data lives under ~/.sagittarius (outside the workspace) but the in-repo
// <root>/.sagittarius/snapshots path is guarded defensively.
func IsProtectedWritePath(root, absPath string) bool {
	protected := filepath.Join(root, SagittariusDirName, "snapshots")
	return absPath == protected || isSubpath(protected, absPath)
}

// SagittariusDirName is the per-project Sagittarius directory name.
const SagittariusDirName = ".sagittarius"

// ProjectBoundaryAllow reports whether a tool call is permitted under the
// project-boundary policy. The protected-path guard for write_file is always
// applied; the out-of-root checks (file writes and the shell heuristic) apply
// only when enforce is true. ws may be nil (no workspace → allow).
func ProjectBoundaryAllow(enforce bool, toolName string, args map[string]any, ws *Workspace) (bool, string) {
	if ws == nil {
		return true, ""
	}
	name := canonicalToolName(toolName)

	if name == WriteFileToolName {
		if path, err := stringArg(args, ParamFilePath); err == nil {
			abs := resolveCandidate(path, ws.Root())
			if IsProtectedWritePath(ws.Root(), abs) {
				return false, "project boundary: writing to Sagittarius snapshot metadata is not allowed"
			}
		}
	}

	if !enforce {
		return true, ""
	}

	switch name {
	case WriteFileToolName:
		path, err := stringArg(args, ParamFilePath)
		if err != nil {
			return true, "" // let the tool surface its own argument error
		}
		if resolvesOutsideRoot(path, ws.Root()) {
			return false, fmt.Sprintf(
				"project boundary: writing outside the project root is blocked (%s)", path)
		}
		return true, ""
	case ShellToolName:
		cmd := optionalStringArg(args, ShellParamCommand)
		if outside, target := ShellMutatesOutsideRoot(cmd, ws.Root()); outside {
			return false, fmt.Sprintf(
				"project boundary: shell command appears to modify a path outside the project root (%s); "+
					"disable security.projectBoundary.enforce to allow", target)
		}
		return true, ""
	default:
		return true, ""
	}
}

// resolveCandidate builds the absolute path a target string refers to, relative
// to root for non-absolute inputs.
func resolveCandidate(target, root string) string {
	t := strings.Trim(strings.TrimSpace(target), "\"'")
	if t == "" {
		return root
	}
	if filepath.IsAbs(t) {
		return filepath.Clean(t)
	}
	return filepath.Clean(filepath.Join(root, t))
}

// resolvesOutsideRoot reports whether target points outside root. A leading "~"
// (home expansion) is treated as outside since it escapes the project.
func resolvesOutsideRoot(target, root string) bool {
	t := strings.Trim(strings.TrimSpace(target), "\"'")
	if t == "" {
		return false
	}
	if strings.HasPrefix(t, "~") {
		return true
	}
	abs := resolveCandidate(t, root)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
