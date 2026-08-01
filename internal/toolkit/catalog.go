package toolkit

// Group defines a category of tools in the checklist.
type Group struct {
	Name   string
	Items  []Item
	SkipIf func(cfg ScanConfig) bool
}

// Item defines a single tool to check.
type Item struct {
	Name        string
	Commands    []string // If any of these are found, it's installed
	InstallHint string
	DetectFunc  func(cfg ScanConfig) bool // Optional custom detector (e.g. MCPs, Web)
}

// Catalog returns the curated toolkit checklist definitions.
func Catalog() []Group {
	return []Group{
		{
			Name: "Core",
			Items: []Item{
				{Name: "ripgrep", Commands: []string{"rg"}, InstallHint: "install via package manager (e.g. apt install ripgrep, brew install ripgrep)"},
				{Name: "git", Commands: []string{"git"}, InstallHint: "install via package manager"},
			},
		},
		{
			Name: "Python",
			Items: []Item{
				{Name: "python", Commands: []string{"python3", "python"}, InstallHint: "install from python.org"},
				{Name: "ruff", Commands: []string{"ruff"}, InstallHint: "pip install ruff"},
			},
		},
		{
			Name: "JavaScript/TS",
			Items: []Item{
				{Name: "node", Commands: []string{"node"}, InstallHint: "install from nodejs.org"},
				{Name: "npm", Commands: []string{"npm"}, InstallHint: "install from nodejs.org"},
			},
		},
		{
			Name: "Go",
			Items: []Item{
				{Name: "go", Commands: []string{"go"}, InstallHint: "install from go.dev"},
				{Name: "gofmt", Commands: []string{"gofmt"}, InstallHint: "included with go"},
				{Name: "golangci-lint", Commands: []string{"golangci-lint"}, InstallHint: "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"},
				{
					Name: "gopls (MCP)",
					DetectFunc: func(cfg ScanConfig) bool {
						if cfg.Settings == nil {
							return false
						}
						// Check if any MCP server is gopls
						servers, err := cfg.Settings.MCPServers()
						if err == nil {
							for _, srv := range servers {
								if srv.Command == "gopls" {
									return true
								}
							}
						}
						return false
					},
					InstallHint: "add 'gopls mcp' via /mcp",
				},
			},
		},
		{
			Name: "Rust",
			Items: []Item{
				{Name: "rustc", Commands: []string{"rustc"}, InstallHint: "install via rustup.rs"},
				{Name: "cargo", Commands: []string{"cargo"}, InstallHint: "included with rustup"},
			},
		},
		{
			Name: "C/C++",
			Items: []Item{
				{Name: "compiler", Commands: []string{"gcc", "clang"}, InstallHint: "install build-essential or xcode-select"},
				{Name: "make", Commands: []string{"make"}, InstallHint: "install via package manager"},
			},
		},
		{
			Name: "Java",
			Items: []Item{
				{Name: "java", Commands: []string{"javac", "java"}, InstallHint: "install Temurin/OpenJDK"},
			},
		},
		{
			Name: "C#",
			Items: []Item{
				{Name: "dotnet", Commands: []string{"dotnet"}, InstallHint: "install from dotnet.microsoft.com"},
			},
		},
		{
			Name: "PHP",
			Items: []Item{
				{Name: "php", Commands: []string{"php"}, InstallHint: "install from php.net"},
				{Name: "composer", Commands: []string{"composer"}, InstallHint: "install from getcomposer.org"},
			},
		},
		{
			Name: "SQL Clients",
			Items: []Item{
				{Name: "client", Commands: []string{"sqlite3", "psql", "mysql"}, InstallHint: "install sqlite3, postgresql-client, or mysql-client"},
			},
		},
		{
			Name: "Sysadmin",
			Items: []Item{
				{Name: "curl", Commands: []string{"curl"}, InstallHint: "install via package manager"},
				{Name: "jq", Commands: []string{"jq"}, InstallHint: "install via package manager"},
				{Name: "systemctl", Commands: []string{"systemctl"}, InstallHint: "linux only"},
			},
			SkipIf: func(cfg ScanConfig) bool {
				return cfg.GOOS == "windows"
			},
		},
		{
			Name: "Web",
			Items: []Item{
				{
					Name: "google_web_search / web_fetch",
					DetectFunc: func(cfg ScanConfig) bool {
						return cfg.WebReady
					},
					InstallHint: "export GEMINI_API_KEY and set sagittarius.web.searchEnabled: true",
				},
			},
		},
	}
}
