package tools

// Wire names and parameter keys match the frozen gemini-cli fork (base-declarations.ts).

const (
	ReadFileToolName      = "read_file"
	WriteFileToolName     = "write_file"
	ListDirectoryToolName = "list_directory"
	ShellToolName         = "run_shell_command"
	GrepToolName          = "grep_search"
)

const (
	ParamFilePath = "file_path"
	ParamDirPath  = "dir_path"
	ParamPattern  = "pattern"

	ReadFileParamStartLine = "start_line"
	ReadFileParamEndLine   = "end_line"
	WriteFileParamContent  = "content"

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

	ShellParamCommand      = "command"
	ShellParamIsBackground = "is_background"

	ListDirParamIgnore = "ignore"
)

// legacyAliases maps alternate tool names to canonical wire names.
var legacyAliases = map[string]string{
	"search_file_content": GrepToolName,
	"grep":                GrepToolName,
	"shell":               ShellToolName,
	"run_shell":           ShellToolName,
}
