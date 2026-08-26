package tools

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// filePathAliases are the alternate keys open-weight models commonly emit for
// the single-file tools' file_path parameter. Order is precedence order.
var filePathAliases = []string{"path", "filePath", "filepath", "filename", "file_name", "file", "target", "dest"}

// builtinArgAliases maps a canonical built-in tool name to its canonical
// parameter keys and the aliases accepted for each. Tables are deliberately
// explicit and tool-scoped: "path" means a file for write_file but a directory
// for list_directory, and a fuzzy matcher would happily bind "data" to the
// wrong field. MCP tools are absent on purpose — their schemas are the
// server's to define, so nothing is remapped for them.
//
// Within one tool, alias lists must not overlap (enforced by a test), so the
// map iteration order in NormalizeToolArgs cannot change the outcome.
var builtinArgAliases = map[string]map[string][]string{
	ReadFileToolName: {
		ParamFilePath: filePathAliases,
	},
	WriteFileToolName: {
		ParamFilePath:         filePathAliases,
		WriteFileParamContent: {"contents", "text", "code", "file_text", "file_content", "body", "data", "source", "value"},
	},
	EditToolName: {
		ParamFilePath:      filePathAliases,
		EditParamOldString: {"old", "old_text", "oldString", "old_str"},
		EditParamNewString: {"new", "new_text", "newString", "new_str"},
	},
	ShellToolName: {
		ShellParamCommand: {"cmd", "shell_command", "shell", "cmdline"},
	},
	ListDirectoryToolName: {
		ParamDirPath: {"path", "directory", "dir", "folder"},
	},
}

// NormalizeToolArgs renames known alternate argument keys onto the canonical
// schema keys a built-in tool declares. Qwen-family models served through
// vLLM/SGLang routinely emit a complete call under the wrong key names
// (`{path, text}` for write_file), which strict extraction rejects as a
// missing parameter and the model then retries verbatim.
//
// A canonical key already present always wins, including when its value is an
// empty string; aliases for a satisfied key are dropped rather than left to
// confuse the confirm card or a hook. The returned map is a copy whenever a
// rename happens, and args itself otherwise, so the common case allocates
// nothing. Normalization is idempotent, so applying it at more than one seam
// is safe.
func NormalizeToolArgs(name string, args map[string]any) map[string]any {
	table, ok := builtinArgAliases[canonicalToolName(name)]
	if !ok || len(args) == 0 {
		return args
	}

	out := args
	copied := false
	for canonical, aliases := range table {
		_, satisfied := args[canonical]
		for _, alias := range aliases {
			value, present := args[alias]
			if !present {
				continue
			}
			if !copied {
				out = maps.Clone(args)
				copied = true
			}
			if !satisfied {
				out[canonical] = value
				satisfied = true
			}
			delete(out, alias)
		}
	}
	return out
}

// missingParamError names the keys the model actually sent alongside the one
// the schema wanted, so a reasoning model can correct its next call instead of
// repeating the same JSON. Values are never echoed: a file body is huge and
// may hold secrets.
// toolArgParseError reports a provider-side JSON decode failure that was
// tagged onto the args map so the scheduler can tell the model the payload
// was unreadable instead of claiming it sent no parameters.
func toolArgParseError(args map[string]any) (string, bool) {
	if args == nil {
		return "", false
	}
	rawErr, ok := args[provider.ToolArgParseErrorKey]
	if !ok {
		return "", false
	}
	errMsg, _ := rawErr.(string)
	if errMsg == "" {
		errMsg = "invalid JSON"
	}
	preview, _ := args[provider.ToolArgRawArgumentsKey].(string)
	if preview != "" {
		return fmt.Sprintf("failed to parse tool arguments as JSON: %s. raw arguments: %s", errMsg, preview), true
	}
	return fmt.Sprintf("failed to parse tool arguments as JSON: %s", errMsg), true
}

func missingParamError(args map[string]any, key string) error {
	received := make([]string, 0, len(args))
	for k := range args {
		received = append(received, k)
	}
	sort.Strings(received)
	return fmt.Errorf(
		"missing required parameter %q. received: [%s]. schema expects: %q",
		key, strings.Join(received, " "), key,
	)
}
