package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Definition describes a discovered agent skill from SKILL.md.
type Definition struct {
	Name          string
	Description   string
	Location      string
	Body          string
	Disabled      bool
	IsBuiltin     bool
	ExtensionName string
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---(?:\r?\n(.*))?$`)

// LoadFromDir discovers SKILL.md files under dir (root and one level deep).
func LoadFromDir(dir string) ([]Definition, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat skills dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil
	}

	var files []string
	rootSkill := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(rootSkill); err == nil {
		files = append(files, rootSkill)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir %q: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(candidate); err == nil {
			files = append(files, candidate)
		}
	}

	var out []Definition
	for _, file := range files {
		def, err := LoadFromFile(file)
		if err != nil {
			continue
		}
		if def != nil {
			out = append(out, *def)
		}
	}
	return out, nil
}

// LoadFromFile parses a single SKILL.md file.
func LoadFromFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file %q: %w", path, err)
	}
	match := frontmatterRegex.FindStringSubmatch(string(data))
	if match == nil {
		return nil, nil
	}
	meta := parseFrontmatter(match[1])
	if meta == nil {
		return nil, nil
	}
	body := strings.TrimSpace(match[2])
	name := sanitizeName(meta.Name)
	return &Definition{
		Name:        name,
		Description: meta.Description,
		Location:    path,
		Body:        body,
	}, nil
}

type frontmatterMeta struct {
	Name        string
	Description string
}

func parseFrontmatter(content string) *frontmatterMeta {
	name, desc := "", ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			continue
		}
		if strings.HasPrefix(trimmed, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		}
	}
	if name == "" || desc == "" {
		return nil
	}
	return &frontmatterMeta{Name: name, Description: desc}
}

func sanitizeName(name string) string {
	replacer := strings.NewReplacer(":", "-", "\\", "-", "/", "-", "<", "-", ">", "-", "*", "-", "?", "-", "\"", "-", "|", "-")
	return replacer.Replace(name)
}
