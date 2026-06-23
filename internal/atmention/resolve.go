package atmention

import (
	"fmt"
	"os"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

// resolved is a successfully validated file reference.
type resolved struct {
	display string // the path as written by the user (workspace-relative)
	abs     string // canonical absolute path within the workspace
}

// resolveMention validates a single "@path" against the workspace. It rejects
// paths outside the trusted root (via Workspace.ResolvePath), missing paths, and
// directories (directory references are not supported in v1).
func resolveMention(ws *tools.Workspace, path string) (resolved, error) {
	abs, err := ws.ResolvePath(path)
	if err != nil {
		return resolved{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return resolved{}, fmt.Errorf("no such file")
		}
		return resolved{}, err
	}
	if info.IsDir() {
		return resolved{}, fmt.Errorf("is a directory; directory references are not supported yet")
	}
	return resolved{display: path, abs: abs}, nil
}
