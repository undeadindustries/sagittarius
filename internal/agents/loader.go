package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Definition is a discovered local agent markdown definition (stub parity).
type Definition struct {
	Name        string
	DisplayName string
	Description string
	Kind        string
	Location    string
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---`)

// LoadFromDirectory reads .md agent files from dir (non-recursive).
func LoadFromDirectory(dir string) ([]Definition, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("list agents dir %q: %w", dir, err)}
	}
	var agents []Definition
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "_") || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		def, err := loadFromFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if def != nil {
			agents = append(agents, *def)
		}
	}
	return agents, errs
}

func loadFromFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent %q: %w", path, err)
	}
	match := frontmatterRegex.FindStringSubmatch(string(data))
	if match == nil {
		return nil, fmt.Errorf("parse agent %q: missing frontmatter", path)
	}
	meta := parseFrontmatter(match[1])
	if meta.Name == "" {
		return nil, fmt.Errorf("parse agent %q: missing name", path)
	}
	return &Definition{
		Name:        meta.Name,
		DisplayName: meta.DisplayName,
		Description: meta.Description,
		Kind:        meta.Kind,
		Location:    path,
	}, nil
}

type agentMeta struct {
	Name        string
	DisplayName string
	Description string
	Kind        string
}

func parseFrontmatter(content string) agentMeta {
	var meta agentMeta
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "name:"):
			meta.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		case strings.HasPrefix(line, "displayName:"):
			meta.DisplayName = strings.TrimSpace(strings.TrimPrefix(line, "displayName:"))
		case strings.HasPrefix(line, "description:"):
			meta.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		case strings.HasPrefix(line, "kind:"):
			meta.Kind = strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
		}
	}
	if meta.DisplayName == "" {
		meta.DisplayName = meta.Name
	}
	if meta.Kind == "" {
		meta.Kind = "local"
	}
	return meta
}
