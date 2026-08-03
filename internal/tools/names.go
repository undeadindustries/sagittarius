package tools

// Wire names and parameter keys match the frozen gemini-cli fork (base-declarations.ts).

const (
	ReadFileToolName        = "read_file"
	WriteFileToolName       = "write_file"
	ListDirectoryToolName   = "list_directory"
	ShellToolName           = "run_shell_command"
	GrepToolName            = "grep_search"
	FindSymbolToolName      = "find_symbol"
	ProjectChecksToolName   = "run_project_checks"
	GoogleWebSearchToolName = "google_web_search"
	WebFetchToolName        = "web_fetch"
	EditToolName            = "edit"
	TaskToolName            = "task"
	// AskUserToolName is the grill-mode structured question tool (registered by
	// internal/agent, not NewBuiltinRegistry, but its name must be known here so
	// the scheduler's read-only gate can special-case it).
	AskUserToolName = "ask_user"
)

const (
	ParamFilePath = "file_path"
	ParamDirPath  = "dir_path"
	ParamPattern  = "pattern"
	ParamQuery    = "query"
	ParamPrompt   = "prompt"
	ParamURL      = "url"

	ReadFileParamStartLine = "start_line"
	ReadFileParamEndLine   = "end_line"
	WriteFileParamContent  = "content"

	EditParamOldString  = "old_string"
	EditParamNewString  = "new_string"
	EditParamReplaceAll = "replace_all"

	TaskParamDescription = "description"
	TaskParamPrompt      = "prompt"

	GrepParamIncludePattern    = "include_pattern"
	GrepParamExcludePattern    = "exclude_pattern"
	GrepParamNamesOnly         = "names_only"
	GrepParamMaxMatchesPerFile = "max_matches_per_file"
	GrepParamTotalMaxMatches   = "total_max_matches"
	GrepParamFixedStrings      = "fixed_strings"
	GrepParamContext           = "context"
	GrepParamAfter             = "after"
	GrepParamBefore            = "before"
	GrepParamNoIgnore          = "no_ignore"
	ParamCaseSensitive         = "case_sensitive"

	FindSymbolParamSymbol     = "symbol"
	FindSymbolParamKind       = "kind"
	FindSymbolParamMaxResults = "max_results"

	ShellParamCommand      = "command"
	ShellParamIsBackground = "is_background"

	ListDirParamIgnore = "ignore"

	ProjectChecksParamPaths = "paths"
	ProjectChecksParamFix   = "fix"
)

// legacyAliases maps alternate tool names to canonical wire names.
var legacyAliases = map[string]string{
	"search_file_content": GrepToolName,
	"grep":                GrepToolName,
	"shell":               ShellToolName,
	"run_shell":           ShellToolName,
}

// IsFileMutatingTool reports whether name mutates a single file whose path is
// in the file_path argument, so snapshotting, diffing, and post-write
// diagnostics apply.
func IsFileMutatingTool(name string) bool {
	c := canonicalToolName(name)
	return c == WriteFileToolName || c == EditToolName
}
