package extensions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/skills"
)

// Extension mirrors fork GeminiCLIExtension at stub depth.
type Extension struct {
	Name      string
	Version   string
	Path      string
	IsActive  bool
	MCPServer map[string]config.MCPServerConfig
	Skills    []skills.Definition
	Agents    []agents.Definition
}

// Loader discovers extensions from settings and well-known directories.
type Loader struct {
	extensions []Extension
}

// NewLoader constructs an extension loader.
func NewLoader() *Loader {
	return &Loader{}
}

// Reload rescans extension declarations.
func (l *Loader) Reload(settings *config.Settings) error {
	var out []Extension
	fromSettings, err := extensionsFromSettings(settings)
	if err != nil {
		return err
	}
	out = append(out, fromSettings...)
	fromDisk, err := discoverInstalledExtensions()
	if err != nil {
		return err
	}
	out = append(out, fromDisk...)
	l.extensions = mergeExtensions(out)
	return nil
}

// Extensions returns loaded extensions.
func (l *Loader) Extensions() []Extension {
	out := make([]Extension, len(l.extensions))
	copy(out, l.extensions)
	return out
}

// ActiveMCPServers merges MCP servers declared by active extensions.
func (l *Loader) ActiveMCPServers() map[string]config.MCPServerConfig {
	out := make(map[string]config.MCPServerConfig)
	for _, ext := range l.extensions {
		if !ext.IsActive || len(ext.MCPServer) == 0 {
			continue
		}
		for name, cfg := range ext.MCPServer {
			out[ext.Name+"-"+name] = cfg
		}
	}
	return out
}

// ActiveSkills returns skills contributed by active extensions.
func (l *Loader) ActiveSkills() []skills.Definition {
	var out []skills.Definition
	for _, ext := range l.extensions {
		if !ext.IsActive {
			continue
		}
		for _, skill := range ext.Skills {
			skill.ExtensionName = ext.Name
			out = append(out, skill)
		}
	}
	return out
}

// ActiveAgents returns agents contributed by active extensions.
func (l *Loader) ActiveAgents() []agents.Definition {
	var out []agents.Definition
	for _, ext := range l.extensions {
		if !ext.IsActive {
			continue
		}
		out = append(out, ext.Agents...)
	}
	return out
}

func extensionsFromSettings(settings *config.Settings) ([]Extension, error) {
	if settings == nil || settings.Raw == nil {
		return nil, nil
	}
	raw, ok := settings.Raw["extensions"]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var entries []struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Enabled  *bool  `json:"enabled"`
		Disabled *bool  `json:"disabled"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		// Also accept map form used by some settings files.
		var asMap map[string]json.RawMessage
		if err2 := json.Unmarshal(raw, &asMap); err2 != nil {
			return nil, fmt.Errorf("decode extensions: %w", err)
		}
		for name, body := range asMap {
			ext, err := loadExtensionManifest(name, string(body))
			if err == nil && ext != nil {
				entries = append(entries, struct {
					Name     string `json:"name"`
					Path     string `json:"path"`
					Enabled  *bool  `json:"enabled"`
					Disabled *bool  `json:"disabled"`
				}{Name: ext.Name, Path: ext.Path})
			}
		}
	}
	var out []Extension
	for _, entry := range entries {
		path := entry.Path
		if path == "" {
			continue
		}
		ext, err := loadExtensionDir(path)
		if err != nil {
			continue
		}
		if entry.Name != "" {
			ext.Name = entry.Name
		}
		out = append(out, *ext)
	}
	return out, nil
}

func discoverInstalledExtensions() ([]Extension, error) {
	dir, err := config.ResolveGeminiDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "extensions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list extensions dir: %w", err)
	}
	var out []Extension
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ext, err := loadExtensionDir(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, *ext)
	}
	return out, nil
}

func loadExtensionDir(path string) (*Extension, error) {
	manifest := filepath.Join(path, "gemini-extension.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	ext, err := loadExtensionManifest(name, string(data))
	if err != nil {
		return nil, err
	}
	ext.Path = path
	ext.IsActive = true
	if skillDir := filepath.Join(path, "skills"); dirExists(skillDir) {
		skillsFound, err := skills.LoadFromDir(skillDir)
		if err == nil {
			ext.Skills = skillsFound
		}
	}
	if agentDir := filepath.Join(path, "agents"); dirExists(agentDir) {
		agentsFound, _ := agents.LoadFromDirectory(agentDir)
		ext.Agents = agentsFound
	}
	if rawMCP, ok := parseRawMCPServers(data); ok {
		ext.MCPServer = rawMCP
	}
	return ext, nil
}

func loadExtensionManifest(name, data string) (*Extension, error) {
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(data), &manifest); err != nil {
		return nil, err
	}
	if manifest.Name == "" {
		manifest.Name = name
	}
	return &Extension{Name: manifest.Name, Version: manifest.Version, IsActive: true}, nil
}

func parseRawMCPServers(data []byte) (map[string]config.MCPServerConfig, bool) {
	var raw struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.MCPServers) == 0 {
		return nil, false
	}
	return raw.MCPServers, true
}

func mergeExtensions(in []Extension) []Extension {
	byName := make(map[string]Extension)
	for _, ext := range in {
		byName[ext.Name] = ext
	}
	out := make([]Extension, 0, len(byName))
	for _, ext := range byName {
		out = append(out, ext)
	}
	return out
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
