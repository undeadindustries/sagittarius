package diagnostics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/tools/checks"
)

// Severity classifies how serious a diagnostic Finding is, driving whether it
// is surfaced to the model (error/warning) and/or the user (style included).
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityStyle   Severity = "style"
)

// Finding represents a single diagnostic issue from a tool or LSP server.
type Finding struct {
	Tool     string
	Severity Severity
	Message  string
}

// Report holds the aggregated results of a post-write check batch.
type Report struct {
	Findings     []Finding
	MissingTools []MissingTool
}

// MissingTool represents a tool that was required but not found or not approved.
type MissingTool struct {
	Name        string
	InstallHint string
}

// RepoLocalPolicy controls whether a toolResolver may run a binary resolved
// from inside the workspace (e.g. node_modules/.bin, .venv/bin) rather than a
// system-wide install. Repo-local binaries execute code checked into the
// repository being worked on, so this is a deliberate trust decision.
type RepoLocalPolicy string

const (
	// RepoLocalPrompt asks ApproveRepoLocal once per tool command per Collect
	// run. Without an ApproveRepoLocal callback (e.g. headless runs) it
	// denies automatically rather than prompting.
	RepoLocalPrompt RepoLocalPolicy = "prompt"
	// RepoLocalAllow always runs repo-local binaries without asking.
	RepoLocalAllow RepoLocalPolicy = "allow"
	// RepoLocalDeny never runs repo-local binaries; they are skipped silently
	// (not reported as a missing tool).
	RepoLocalDeny RepoLocalPolicy = "deny"
)

// Options configure the behavior of a diagnostics run.
type Options struct {
	Root       string
	Paths      []string // workspace-relative
	ModuleWide bool
	Timeout    time.Duration
	LSP        Diagnoser // nil when unavailable

	// RepoLocalPolicy governs whether a tool binary resolved from inside Root
	// may run. Defaults to RepoLocalPrompt.
	RepoLocalPolicy RepoLocalPolicy
	// ApproveRepoLocal is consulted once per tool command when RepoLocalPolicy
	// is RepoLocalPrompt; it receives the tool's command name and its
	// resolved repo-local path. Nil denies repo-local tools without
	// prompting.
	ApproveRepoLocal func(tool, path string) bool
}

// Diagnoser is the interface for querying diagnostics (implemented by LSP
// clients via lsp.PoolDiagnoser). root is the language's resolved module/
// workspace root (see findRoot), used to start or reuse the correct server
// instance — it is not necessarily Options.Root.
type Diagnoser interface {
	Diagnostics(ctx context.Context, root string, absPaths []string) ([]Finding, error)
}

// Tool represents an external binary check (linter or formatter).
type Tool struct {
	Name         string
	Command      string
	Args         []string
	FailOnOutput bool
	Severity     Severity
	Precondition func(root string) bool
	Fallback     *Tool
	InstallHint  string
}

// ServerSpec describes an LSP server for a language.
type ServerSpec struct {
	Command      string
	Args         []string
	Precondition func(root string) bool
	InstallHint  string
}

// Language defines the tooling registry entry for a file extension.
type Language struct {
	ID           string
	Extensions   []string
	RootMarkers  []string
	FileChecks   []Tool
	ModuleChecks []Tool
	Server       *ServerSpec
}

// lookPathCache avoids stat-ing the PATH on every write.
var lookPathCache sync.Map // string -> string (path) or error (stored as struct struct{err error})

type lookPathResult struct {
	path string
	err  error
}

func cachedLookPath(file string) (string, error) {
	if val, ok := lookPathCache.Load(file); ok {
		res := val.(lookPathResult)
		return res.path, res.err
	}
	path, err := exec.LookPath(file)
	lookPathCache.Store(file, lookPathResult{path: path, err: err})
	return path, err
}

// toolResolver resolves check binaries on PATH for the lifetime of one
// Collect run, applying the repo-local approval policy and memoizing prompt
// decisions per command so the user is asked at most once per run.
type toolResolver struct {
	wsRoot  string
	policy  RepoLocalPolicy
	approve func(tool, path string) bool
	decided map[string]bool
}

func newToolResolver(opts Options) *toolResolver {
	policy := opts.RepoLocalPolicy
	if policy == "" {
		policy = RepoLocalPrompt
	}
	return &toolResolver{
		wsRoot:  opts.Root,
		policy:  policy,
		approve: opts.ApproveRepoLocal,
		decided: make(map[string]bool),
	}
}

// resolve checks preconditions and resolves the command on the PATH, gating
// repo-local binaries behind the configured RepoLocalPolicy.
func (tr *toolResolver) resolve(root string, t Tool) (*Tool, error) {
	if t.Precondition != nil && !t.Precondition(root) {
		return nil, nil // Not an error, just skip silently
	}

	path, err := cachedLookPath(t.Command)
	if err == nil {
		if tr.isRepoLocal(path) && !tr.approved(t.Command, path) {
			return nil, nil // policy-denied, not a missing tool
		}
		return &t, nil
	}

	if t.Fallback != nil {
		return tr.resolve(root, *t.Fallback)
	}

	return nil, err
}

// isRepoLocal reports whether resolvedPath lives inside the workspace root.
func (tr *toolResolver) isRepoLocal(resolvedPath string) bool {
	if tr.wsRoot == "" {
		return false
	}
	rel, err := filepath.Rel(tr.wsRoot, resolvedPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// approved applies the repo-local policy for command, prompting (and
// memoizing the answer) at most once per Collect run under RepoLocalPrompt.
func (tr *toolResolver) approved(command, path string) bool {
	switch tr.policy {
	case RepoLocalAllow:
		return true
	case RepoLocalDeny:
		return false
	default: // RepoLocalPrompt
		if v, ok := tr.decided[command]; ok {
			return v
		}
		if tr.approve == nil {
			return false // headless / no prompt available: deny by default
		}
		v := tr.approve(command, path)
		tr.decided[command] = v
		return v
	}
}

// exists is a helper for preconditions.
func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

// Registry exposes the internal language registry for documentation or tests.
func Registry() []Language {
	return registry
}

// The registry definitions.
var registry = []Language{
	{
		ID:          "go",
		Extensions:  []string{".go"},
		RootMarkers: []string{"go.mod"},
		FileChecks: []Tool{
			{Name: "gofmt", Command: "gofmt", Args: []string{"-l"}, FailOnOutput: true, Severity: SeverityStyle, InstallHint: "included with go"},
		},
		ModuleChecks: []Tool{
			{Name: "vet", Command: "go", Args: []string{"vet", "./..."}, Severity: SeverityError},
			{Name: "build", Command: "go", Args: []string{"build", "./..."}, Severity: SeverityError},
		},
		Server: &ServerSpec{
			Command:      "gopls",
			Precondition: func(root string) bool { return exists(root, "go.mod") },
			InstallHint:  "go install golang.org/x/tools/gopls@latest",
		},
	},
	{
		ID:          "python",
		Extensions:  []string{".py"},
		RootMarkers: []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg"},
		FileChecks: []Tool{
			{Name: "ruff", Command: "ruff", Args: []string{"check"}, Severity: SeverityWarning, InstallHint: "pip install ruff", Fallback: &Tool{Name: "py_compile", Command: "python3", Args: []string{"-m", "py_compile"}, Severity: SeverityError}},
			{Name: "ruff format", Command: "ruff", Args: []string{"format", "--check"}, Severity: SeverityStyle},
			{Name: "mypy", Command: "mypy", Args: []string{}, Severity: SeverityError, Precondition: func(root string) bool { return exists(root, "mypy.ini") || exists(root, "pyproject.toml") }},
		},
		Server: &ServerSpec{
			Command:     "pyright",
			InstallHint: "npm install -g pyright",
		},
	},
	{
		ID:          "typescript",
		Extensions:  []string{".ts", ".tsx"},
		RootMarkers: []string{"package.json", "tsconfig.json"},
		FileChecks: []Tool{
			{Name: "eslint", Command: "eslint", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool {
				return exists(root, "eslint.config.js") || exists(root, ".eslintrc.js") || exists(root, ".eslintrc.json")
			}, InstallHint: "npm install eslint", Fallback: &Tool{Name: "node check", Command: "node", Args: []string{"--check"}, Severity: SeverityError}},
			{Name: "prettier", Command: "prettier", Args: []string{"--check"}, Severity: SeverityStyle, Precondition: func(root string) bool { return exists(root, ".prettierrc") || exists(root, ".prettierrc.json") }},
		},
		ModuleChecks: []Tool{
			{Name: "tsc", Command: "tsc", Args: []string{"--noEmit"}, Severity: SeverityError, Precondition: func(root string) bool { return exists(root, "tsconfig.json") }},
		},
		Server: &ServerSpec{
			Command:      "typescript-language-server",
			Args:         []string{"--stdio"},
			Precondition: func(root string) bool { return exists(root, "package.json") || exists(root, "tsconfig.json") },
			InstallHint:  "npm install -g typescript-language-server typescript",
		},
	},
	{
		ID:          "javascript",
		Extensions:  []string{".js", ".jsx", ".mjs", ".cjs"},
		RootMarkers: []string{"package.json"},
		FileChecks: []Tool{
			{Name: "eslint", Command: "eslint", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool {
				return exists(root, "eslint.config.js") || exists(root, ".eslintrc.js") || exists(root, ".eslintrc.json")
			}, InstallHint: "npm install eslint", Fallback: &Tool{Name: "node check", Command: "node", Args: []string{"--check"}, Severity: SeverityError}},
			{Name: "prettier", Command: "prettier", Args: []string{"--check"}, Severity: SeverityStyle, Precondition: func(root string) bool { return exists(root, ".prettierrc") || exists(root, ".prettierrc.json") }},
		},
		Server: &ServerSpec{
			Command:      "typescript-language-server",
			Args:         []string{"--stdio"},
			Precondition: func(root string) bool { return exists(root, "package.json") },
			InstallHint:  "npm install -g typescript-language-server typescript",
		},
	},
	{
		ID:          "rust",
		Extensions:  []string{".rs"},
		RootMarkers: []string{"Cargo.toml"},
		FileChecks: []Tool{
			{Name: "rustfmt", Command: "rustfmt", Args: []string{"--check"}, Severity: SeverityStyle, InstallHint: "included with rustup"},
		},
		ModuleChecks: []Tool{
			{Name: "clippy", Command: "cargo", Args: []string{"clippy"}, Severity: SeverityWarning},
		},
		Server: &ServerSpec{
			Command:      "rust-analyzer",
			Precondition: func(root string) bool { return exists(root, "Cargo.toml") },
			InstallHint:  "install rust-analyzer via rustup or package manager",
		},
	},
	{
		ID:          "c/c++",
		Extensions:  []string{".c", ".cc", ".cpp", ".cxx", ".h", ".hpp"},
		RootMarkers: []string{"compile_commands.json", "compile_flags.txt", "CMakeLists.txt", "Makefile"},
		FileChecks: []Tool{
			{Name: "clang-format", Command: "clang-format", Args: []string{"--dry-run", "-Werror"}, Severity: SeverityStyle, InstallHint: "install clang-format"},
			{Name: "clang-tidy", Command: "clang-tidy", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool { return exists(root, "compile_commands.json") }, InstallHint: "install clang-tidy"},
		},
		Server: &ServerSpec{
			Command: "clangd",
			Precondition: func(root string) bool {
				return exists(root, "compile_commands.json") || exists(root, "compile_flags.txt")
			},
			InstallHint: "install clangd",
		},
	},
	{
		ID:          "java",
		Extensions:  []string{".java"},
		RootMarkers: []string{"pom.xml", "build.gradle", "build.gradle.kts"},
		FileChecks: []Tool{
			{Name: "google-java-format", Command: "google-java-format", Args: []string{"--dry-run"}, Severity: SeverityStyle, InstallHint: "install google-java-format"},
		},
		Server: &ServerSpec{
			Command: "jdtls",
			Precondition: func(root string) bool {
				return exists(root, "pom.xml") || exists(root, "build.gradle") || exists(root, "build.gradle.kts")
			},
			InstallHint: "install eclipse.jdt.ls",
		},
	},
	{
		ID:          "kotlin",
		Extensions:  []string{".kt", ".kts"},
		RootMarkers: []string{"build.gradle.kts", "build.gradle", "pom.xml"},
		FileChecks: []Tool{
			{Name: "ktlint", Command: "ktlint", Args: []string{}, Severity: SeverityStyle, InstallHint: "install ktlint"},
		},
		Server: &ServerSpec{
			Command: "kotlin-language-server",
			Precondition: func(root string) bool {
				return exists(root, "build.gradle.kts") || exists(root, "build.gradle") || exists(root, "pom.xml")
			},
			InstallHint: "install kotlin-language-server",
		},
	},
	{
		ID:          "php",
		Extensions:  []string{".php"},
		RootMarkers: []string{"composer.json"},
		FileChecks: []Tool{
			{Name: "php syntax", Command: "php", Args: []string{"-l"}, Severity: SeverityError, InstallHint: "install php"},
			{Name: "php-cs-fixer", Command: "php-cs-fixer", Args: []string{"fix", "--dry-run", "--diff"}, Severity: SeverityStyle, Precondition: func(root string) bool {
				return exists(root, ".php-cs-fixer.dist.php") || exists(root, ".php-cs-fixer.php")
			}, InstallHint: "composer require --dev friendsofphp/php-cs-fixer"},
			{Name: "phpcs", Command: "phpcs", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool { return exists(root, "phpcs.xml") || exists(root, "phpcs.xml.dist") }, InstallHint: "composer require --dev squizlabs/php_codesniffer"},
		},
		Server: &ServerSpec{
			Command:      "intelephense",
			Args:         []string{"--stdio"},
			Precondition: func(root string) bool { return exists(root, "composer.json") },
			InstallHint:  "npm install -g intelephense",
		},
	},
	{
		ID:          "csharp",
		Extensions:  []string{".cs"},
		RootMarkers: []string{".sln", ".csproj"},
		ModuleChecks: []Tool{
			{Name: "dotnet format", Command: "dotnet", Args: []string{"format", "--verify-no-changes"}, Severity: SeverityStyle, InstallHint: "install dotnet sdk"},
		},
	},
	{
		ID:          "ruby",
		Extensions:  []string{".rb"},
		RootMarkers: []string{"Gemfile"},
		FileChecks: []Tool{
			{Name: "rubocop", Command: "rubocop", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool { return exists(root, ".rubocop.yml") }, InstallHint: "gem install rubocop", Fallback: &Tool{Name: "ruby syntax", Command: "ruby", Args: []string{"-c"}, Severity: SeverityError}},
		},
		Server: &ServerSpec{
			Command:      "solargraph",
			Args:         []string{"stdio"},
			Precondition: func(root string) bool { return exists(root, "Gemfile") },
			InstallHint:  "gem install solargraph",
		},
	},
	{
		ID:          "bash",
		Extensions:  []string{".sh", ".bash"},
		RootMarkers: []string{".git"}, // Fallback to repo root, mostly for standalone scripts
		FileChecks: []Tool{
			{Name: "shellcheck", Command: "shellcheck", Args: []string{}, Severity: SeverityWarning, InstallHint: "install shellcheck", Fallback: &Tool{Name: "bash syntax", Command: "bash", Args: []string{"-n"}, Severity: SeverityError}},
			{Name: "shfmt", Command: "shfmt", Args: []string{"-d"}, Severity: SeverityStyle, InstallHint: "install shfmt"},
		},
		Server: &ServerSpec{
			Command:     "bash-language-server",
			Args:        []string{"start"},
			InstallHint: "npm install -g bash-language-server",
		},
	},
	{
		ID:          "crontab",
		Extensions:  []string{"crontab"},
		RootMarkers: []string{".git"},
		FileChecks: []Tool{
			{Name: "crontab syntax", Command: "crontab", Args: []string{"-c"}, Severity: SeverityError, Fallback: &Tool{Name: "crontab", Command: "crontab", Args: []string{"-l"}, Severity: SeverityError}},
		},
	},
	{
		ID:          "yaml",
		Extensions:  []string{".yml", ".yaml"},
		RootMarkers: []string{".git"},
		FileChecks: []Tool{
			{Name: "yamllint", Command: "yamllint", Args: []string{}, Severity: SeverityWarning, InstallHint: "pip install yamllint"},
			{Name: "ansible-lint", Command: "ansible-lint", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool { return exists(root, "ansible.cfg") }, InstallHint: "pip install ansible-lint"},
		},
	},
	{
		ID:          "dockerfile",
		Extensions:  []string{".dockerfile", "Dockerfile"},
		RootMarkers: []string{".git"},
		FileChecks: []Tool{
			{Name: "hadolint", Command: "hadolint", Args: []string{}, Severity: SeverityWarning, InstallHint: "install hadolint"},
		},
	},
	{
		ID:          "systemd",
		Extensions:  []string{".service", ".timer", ".socket", ".mount", ".target"},
		RootMarkers: []string{".git"},
		FileChecks: []Tool{
			{Name: "systemd-analyze", Command: "systemd-analyze", Args: []string{"verify"}, Severity: SeverityError, InstallHint: "linux only"},
		},
	},
	{
		ID:          "json",
		Extensions:  []string{".json"},
		RootMarkers: []string{".git"},
		FileChecks: []Tool{
			{Name: "jq", Command: "jq", Args: []string{"empty"}, Severity: SeverityError, InstallHint: "install jq", Fallback: &Tool{Name: "python json.tool", Command: "python3", Args: []string{"-m", "json.tool"}, Severity: SeverityError}},
		},
	},
	{
		ID:          "toml",
		Extensions:  []string{".toml"},
		RootMarkers: []string{".git"},
		FileChecks: []Tool{
			{Name: "taplo", Command: "taplo", Args: []string{"check"}, Severity: SeverityWarning, InstallHint: "install taplo", Fallback: &Tool{Name: "python json.tool", Command: "python3", Args: []string{"-m", "json.tool"}, Severity: SeverityError}},
		},
	},
	{
		ID:          "terraform",
		Extensions:  []string{".tf"},
		RootMarkers: []string{".terraform"},
		FileChecks: []Tool{
			{Name: "terraform validate", Command: "terraform", Args: []string{"validate"}, Severity: SeverityError, Precondition: func(root string) bool { return exists(root, ".terraform") }, InstallHint: "install terraform"},
			{Name: "tflint", Command: "tflint", Args: []string{}, Severity: SeverityWarning, Precondition: func(root string) bool { return exists(root, ".tflint.hcl") }, InstallHint: "install tflint"},
		},
	},
}

// FindLanguage returns the registry entry matching the file extension.
func FindLanguage(path string) *Language {
	ext := filepath.Ext(path)
	if ext == "" {
		// e.g. "Dockerfile"
		ext = filepath.Base(path)
	}

	for i := range registry {
		for _, e := range registry[i].Extensions {
			if strings.EqualFold(e, ext) {
				return &registry[i]
			}
		}
	}
	return nil
}

// findRoot walks up from startDir looking for root markers.
// Stops at wsRoot if provided, or the filesystem root.
func findRoot(startDir, wsRoot string, markers []string) string {
	dir := filepath.Clean(startDir)
	if wsRoot != "" {
		wsRoot = filepath.Clean(wsRoot)
	}

	for {
		for _, m := range markers {
			if exists(dir, m) {
				return dir
			}
		}

		if dir == wsRoot {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Collect runs post-write diagnostics on the given files.
func Collect(ctx context.Context, opts Options) (Report, error) {
	if len(opts.Paths) == 0 {
		return Report{}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var r Report
	seenMissing := make(map[string]bool)
	tr := newToolResolver(opts)

	// Group files by directory and language
	type runTarget struct {
		Lang  *Language
		Root  string
		Dir   string // Where to run if no root found
		Files []string
	}
	targets := make(map[string]*runTarget)

	for _, p := range opts.Paths {
		absPath := filepath.Join(opts.Root, p)
		lang := FindLanguage(absPath)
		if lang == nil {
			continue
		}

		dir := filepath.Dir(absPath)
		root := findRoot(dir, opts.Root, lang.RootMarkers)

		key := fmt.Sprintf("%s:%s", lang.ID, root)
		if root == "" {
			// No root found (e.g. out-of-workspace sysadmin write).
			// Group by the file's own directory instead, and never run module-wide checks.
			key = fmt.Sprintf("%s:dir:%s", lang.ID, dir)
		}

		if t, ok := targets[key]; ok {
			t.Files = append(t.Files, absPath)
		} else {
			targets[key] = &runTarget{
				Lang:  lang,
				Root:  root,
				Dir:   dir,
				Files: []string{absPath},
			}
		}
	}

	for _, t := range targets {
		runDir := t.Root
		if runDir == "" {
			runDir = t.Dir
		}

		// LSP diagnostics (if available) replace module checks.
		lspHandled := false
		if opts.LSP != nil && t.Lang.Server != nil {
			findings, err := opts.LSP.Diagnostics(runCtx, runDir, t.Files)
			if err == nil {
				r.Findings = append(r.Findings, findings...)
				lspHandled = true
			} else {
				slog.Debug("LSP diagnostics failed", "language", t.Lang.ID, "root", runDir, "error", err)
			}
		}

		// File checks (lint, format) always run
		for _, check := range t.Lang.FileChecks {
			tool, err := tr.resolve(runDir, check)
			if err != nil {
				if !seenMissing[check.Command] {
					seenMissing[check.Command] = true
					r.MissingTools = append(r.MissingTools, MissingTool{Name: check.Command, InstallHint: check.InstallHint})
				}
				continue
			}
			if tool == nil {
				continue // Precondition failed, silent skip
			}

			// We need to pass the file paths relative to the runDir
			relPaths := make([]string, 0, len(t.Files))
			for _, f := range t.Files {
				if rel, err := filepath.Rel(runDir, f); err == nil {
					relPaths = append(relPaths, rel)
				} else {
					relPaths = append(relPaths, f)
				}
			}

			// The registry stores each check's Args as pure flags with no
			// trailing default target (unlike run_project_checks's
			// checks.Check, whose Args end in a replaceable "." / "./...").
			// checks.Argv narrows a trailing target; here there is none to
			// narrow, so the file paths are simply appended.
			cc := checks.Check{
				Name:         tool.Name,
				Command:      tool.Command,
				Args:         tool.Args,
				FailOnOutput: tool.FailOnOutput,
			}
			argv := append(slices.Clone(tool.Args), relPaths...)

			ok, _, output := checks.Run(runCtx, runDir, cc, argv)
			if !ok {
				r.Findings = append(r.Findings, Finding{
					Tool:     tool.Name,
					Severity: tool.Severity,
					Message:  output,
				})
			}
		}

		// Module checks (vet, build) run if enabled and not subsumed by LSP
		if opts.ModuleWide && !lspHandled && t.Root != "" {
			for _, check := range t.Lang.ModuleChecks {
				tool, err := tr.resolve(runDir, check)
				if err != nil {
					if !seenMissing[check.Command] {
						seenMissing[check.Command] = true
						r.MissingTools = append(r.MissingTools, MissingTool{Name: check.Command, InstallHint: check.InstallHint})
					}
					continue
				}
				if tool == nil {
					continue
				}

				cc := checks.Check{
					Name:         tool.Name,
					Command:      tool.Command,
					Args:         tool.Args,
					FailOnOutput: tool.FailOnOutput,
				}

				ok, _, output := checks.Run(runCtx, runDir, cc, tool.Args) // Module checks don't get relPaths
				if !ok {
					r.Findings = append(r.Findings, Finding{
						Tool:     tool.Name,
						Severity: tool.Severity,
						Message:  output,
					})
				}
			}
		}
	}

	return r, nil
}
