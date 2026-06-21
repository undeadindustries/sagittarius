package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Manager discovers skills and tracks session-active skill names.
type Manager struct {
	mu              sync.RWMutex
	skills          []Definition
	active          map[string]struct{}
	workDir         string
	trusted         bool
	extensionSkills []Definition
}

// NewManager constructs a skill manager for workDir.
func NewManager(workDir string, trusted bool) *Manager {
	return &Manager{
		workDir: workDir,
		trusted: trusted,
		active:  make(map[string]struct{}),
	}
}

// Discover rescans skill directories with fork precedence:
// extensions (lowest) → user → user .agents → workspace → workspace .agents.
func (m *Manager) Discover(ctx context.Context, extensionSkills []Definition) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()

	m.extensionSkills = append([]Definition(nil), extensionSkills...)
	ordered := make([][]Definition, 0, 5)

	if len(extensionSkills) > 0 {
		ordered = append(ordered, extensionSkills)
	}
	if dir, err := UserSkillsDir(); err == nil {
		if skills, err := LoadFromDir(dir); err == nil {
			ordered = append(ordered, skills)
		} else {
			slog.Warn("user skills discovery failed", "error", err)
		}
	}
	if dir, err := UserAgentSkillsDir(); err == nil {
		if skills, err := LoadFromDir(dir); err == nil {
			ordered = append(ordered, skills)
		} else {
			slog.Warn("user agent skills discovery failed", "error", err)
		}
	}
	if m.trusted {
		if skills, err := LoadFromDir(ProjectSkillsDir(m.workDir)); err == nil {
			ordered = append(ordered, skills)
		} else {
			slog.Warn("project skills discovery failed", "error", err)
		}
		if skills, err := LoadFromDir(ProjectAgentSkillsDir(m.workDir)); err == nil {
			ordered = append(ordered, skills)
		} else {
			slog.Warn("project agent skills discovery failed", "error", err)
		}
	}

	byName := make(map[string]Definition)
	for _, batch := range ordered {
		for _, skill := range batch {
			if existing, ok := byName[skill.Name]; ok && existing.Location != skill.Location {
				slog.Warn("skill name conflict", "name", skill.Name, "overriding", skill.Location, "previous", existing.Location)
			}
			byName[skill.Name] = skill
		}
	}
	m.skills = make([]Definition, 0, len(byName))
	for _, skill := range byName {
		m.skills = append(m.skills, skill)
	}
	return nil
}

// Skills returns enabled discovered skills.
func (m *Manager) Skills() []Definition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Definition, 0, len(m.skills))
	for _, s := range m.skills {
		if !s.Disabled {
			out = append(out, s)
		}
	}
	return out
}

// AllSkills returns all discovered skills including disabled ones.
func (m *Manager) AllSkills() []Definition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Definition, len(m.skills))
	copy(out, m.skills)
	return out
}

// Get returns a skill by name (case-insensitive).
func (m *Manager) Get(name string) *Definition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lower := strings.ToLower(strings.TrimSpace(name))
	for i := range m.skills {
		if strings.ToLower(m.skills[i].Name) == lower {
			s := m.skills[i]
			return &s
		}
	}
	return nil
}

// Activate marks a skill as active for the session.
func (m *Manager) Activate(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[name] = struct{}{}
}

// IsActive reports whether a skill is active in the session.
func (m *Manager) IsActive(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.active[name]
	return ok
}

// Reset clears session-scoped active skill names.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = make(map[string]struct{})
}

// ActivateContent returns XML-wrapped skill instructions for the model.
func (m *Manager) ActivateContent(name string) (string, error) {
	skill := m.Get(name)
	if skill == nil {
		names := make([]string, 0, len(m.skills))
		for _, s := range m.Skills() {
			names = append(names, s.Name)
		}
		return "", fmt.Errorf("skill %q not found; available: %s", name, strings.Join(names, ", "))
	}
	m.Activate(name)
	dir := skillDir(skill.Location)
	resources, _ := folderListing(dir)
	return fmt.Sprintf(`<activated_skill name="%s">
  <instructions>
    %s
  </instructions>

  <available_resources>
    %s
  </available_resources>
</activated_skill>`, skill.Name, skill.Body, resources), nil
}

func skillDir(location string) string {
	// SKILL.md lives in the skill directory root or a subdirectory.
	dir := location
	if strings.HasSuffix(strings.ToLower(location), "skill.md") {
		dir = location[:len(location)-len("SKILL.md")]
	}
	return strings.TrimRight(dir, "/\\")
}

func folderListing(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "(empty)", nil
	}
	var b strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&b, "  %s\n", entry.Name())
	}
	return b.String(), nil
}
