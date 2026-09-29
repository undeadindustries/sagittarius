package atmention

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

// resolved is a successfully validated file reference.
type resolved struct {
	display string // the path as written by the user (workspace-relative)
	abs     string // canonical absolute path within the workspace
}

// resolveMention validates a single "@path" against the workspace.
// It returns (resolved, true, nil) when path resolves to an existing regular file
// within the workspace.
// If the path does not exist on disk, it checks whether the token actually looks
// like an intended file path (contains a path separator '/' or '\', or has a
// recognized file extension like '.txt', '.go', '.py'). If not, it returns
// (resolved{}, false, nil) so that non-file tokens (such as programming
// decorators @schedule_open_buffer, @property, annotations @Override,
// JSDoc tags @param, handles @user, etc.) are preserved verbatim in the user's
// prompt text rather than failing the turn with an error.
func resolveMention(ws *tools.Workspace, path string) (resolved, bool, error) {
	abs, err := ws.ResolvePath(path)
	if err != nil {
		if !looksLikeFilePath(path) {
			return resolved{}, false, nil
		}
		return resolved{}, false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			if !looksLikeFilePath(path) {
				return resolved{}, false, nil
			}
			return resolved{}, false, fmt.Errorf("no such file")
		}
		return resolved{}, false, err
	}
	if info.IsDir() {
		return resolved{}, false, fmt.Errorf("is a directory; directory references are not supported yet")
	}
	return resolved{display: path, abs: abs}, true, nil
}

// looksLikeFilePath reports whether path has structural characteristics of an
// intended file path (directory separators '/' or '\', or a typical file extension
// like '.txt', '.go', '.py', '.json', etc.). Bare programming identifiers like
// "schedule_open_buffer", "Override", "property", "param" return false.
func looksLikeFilePath(s string) bool {
	if strings.ContainsAny(s, `/\`) {
		return true
	}
	ext := filepath.Ext(s)
	if ext == "" {
		return false
	}
	ext = ext[1:] // strip '.'
	if len(ext) < 1 || len(ext) > 5 {
		return false
	}
	for _, r := range ext {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return isKnownFileExtension(ext)
}

func isKnownFileExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case "go", "py", "ts", "js", "tsx", "jsx", "txt", "md", "json", "yaml", "yml",
		"toml", "c", "h", "cpp", "hpp", "rs", "sh", "bash", "zsh", "html", "css",
		"scss", "sql", "java", "kt", "rb", "php", "dart", "swift", "lua", "xml",
		"env", "log", "diff", "patch", "ini", "cfg", "conf":
		return true
	default:
		return false
	}
}
