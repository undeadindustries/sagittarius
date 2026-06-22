package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PlansDirRelative is the workspace-relative directory for plan files.
const PlansDirRelative = "docs/plans"

// PlansDir returns the absolute plans directory under the workspace root.
func PlansDir(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, PlansDirRelative)
}

// ResolvePlanPath resolves and validates that planPath stays within plansDir.
// Mirrors fork resolveAndValidatePlanPath (planUtils.ts).
func ResolvePlanPath(planPath, projectRoot, plansDir string) (string, error) {
	trimmed := strings.TrimSpace(planPath)
	if trimmed == "" {
		return "", fmt.Errorf("plan file path must be non-empty")
	}

	realPlans, err := realPath(plansDir)
	if err != nil {
		return "", fmt.Errorf("plans directory: %w", err)
	}

	if filepath.IsAbs(trimmed) {
		realAbs, err := realPath(trimmed)
		if err != nil {
			return "", fmt.Errorf("plan path: %w", err)
		}
		if isSubpath(realPlans, realAbs) {
			return trimmed, nil
		}
		return "", fmt.Errorf(
			"plan path %q must be within the plans directory (%s)",
			trimmed,
			realPlans,
		)
	}

	fromRoot := filepath.Clean(filepath.Join(projectRoot, trimmed))
	realFromRoot, err := realPath(fromRoot)
	if err == nil && isSubpath(realPlans, realFromRoot) {
		return fromRoot, nil
	}

	resolved := filepath.Clean(filepath.Join(plansDir, trimmed))
	realResolved, err := realPath(resolved)
	if err != nil {
		return "", fmt.Errorf("plan path: %w", err)
	}
	if !isSubpath(realPlans, realResolved) {
		return "", fmt.Errorf(
			"plan path %q must be within the plans directory (%s)",
			trimmed,
			realPlans,
		)
	}
	return resolved, nil
}

func validatePlanWritePath(pathStr string, ws *Workspace) error {
	if ws == nil {
		return fmt.Errorf("plan write requires a workspace")
	}
	_, err := ResolvePlanPath(pathStr, ws.Root(), PlansDir(ws.Root()))
	return err
}

func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	return real, nil
}

func isSubpath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
