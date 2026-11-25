package systempromptdialog

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/customprompt"
)

// fakeDeps is an in-memory prompt store that records calls.
type fakeDeps struct {
	mu               sync.Mutex
	current          string
	projectAvailable bool
	prompts          map[string]string
	applied          string
	appliedScope     config.SettingScope
	imported         string
}

func newFakeDeps(current string, names ...string) *fakeDeps {
	f := &fakeDeps{current: current, projectAvailable: true, prompts: map[string]string{}}
	for _, n := range names {
		f.prompts[n] = "Body of " + n
	}
	return f
}

func (f *fakeDeps) CurrentPresetID() string { return f.current }

func (f *fakeDeps) ProjectAvailable() bool { return f.projectAvailable }

func (f *fakeDeps) ApplyPreset(_ context.Context, id string, scope config.SettingScope) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = id
	f.appliedScope = scope
	if id == "" {
		return "System prompt → Programmer (cleared in " + scope.String() + ")", nil
	}
	if _, ok := f.prompts[id]; ok {
		return "System prompt → " + id + " (custom)", nil
	}
	return "applied " + id, nil
}

func (f *fakeDeps) ListCustomPrompts() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.prompts))
	for n := range f.prompts {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeDeps) LoadCustomPrompt(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.prompts[name]
	if !ok {
		return "", customprompt.ErrNotFound
	}
	return b, nil
}

func (f *fakeDeps) SaveCustomPrompt(_ context.Context, name, body string, create bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.prompts[name]; ok && create {
		return "", customprompt.ErrExists
	}
	f.prompts[name] = body
	return "Saved " + name + ".", nil
}

func (f *fakeDeps) ImportCustomPrompt(_ context.Context, name, path string, _ bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if path == "missing.md" {
		return "", errors.New("import missing.md: no such file")
	}
	f.imported = path
	f.prompts[name] = "imported from " + path
	return "Saved " + name + ".", nil
}

func (f *fakeDeps) RenameCustomPrompt(_ context.Context, oldName, newName string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts[newName] = f.prompts[oldName]
	delete(f.prompts, oldName)
	return "Renamed.", nil
}

func (f *fakeDeps) DeleteCustomPrompt(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.prompts, name)
	return "Deleted " + name + ".", nil
}

// drain runs cmd and feeds every resulting message back into the model,
// skipping spinner ticks and cursor blinks, until no command remains.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 50 {
			t.Fatal("command loop did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg, ok := runQuick(c)
		if !ok {
			continue
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
			continue
		case spinner.TickMsg, nil:
			continue
		}
		if _, isKey := msg.(tea.KeyMsg); !isKey && !isDialogMsg(msg) {
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		queue = append(queue, next)
	}
	return m
}

// quickCmdTimeout bounds how long drain waits for a command. Dialog I/O
// against fakeDeps returns at once; cursor blink and spinner ticks sleep, and
// their messages are irrelevant to these tests.
const quickCmdTimeout = 100 * time.Millisecond

func runQuick(c tea.Cmd) (tea.Msg, bool) {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- c() }()
	select {
	case msg := <-ch:
		return msg, true
	case <-time.After(quickCmdTimeout):
		return nil, false
	}
}

func isDialogMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case applyResultMsg, customListMsg, bodyLoadedMsg, opDoneMsg:
		return true
	}
	return false
}

func open(t *testing.T, deps Deps, w, h int) Model {
	t.Helper()
	m, cmd := New(context.Background(), deps)
	m = m.SetSize(w, h)
	return drain(t, m, cmd)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		m = drain(t, m, cmd)
	}
	return m
}

func paste(t *testing.T, m Model, text string) Model {
	t.Helper()
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	return drain(t, m, cmd)
}

func TestPickListsCustomPromptsAndManageRow(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps("orion", "atlas", "orion")
	m := open(t, deps, 100, 40)

	opts := m.pickOptions()
	if got := opts[len(opts)-1]; got.kind != optionManage {
		t.Fatalf("last row = %+v, want the Custom prompts row", got)
	}
	if got := opts[m.pickCursor]; got.id != "orion" || got.kind != optionCustom {
		t.Fatalf("cursor on %+v, want the selected custom prompt orion", got)
	}
	view := m.View()
	for _, want := range []string{"atlas (custom)", "> orion (custom)", manageRowLabel} {
		if !strings.Contains(view, want) {
			t.Errorf("pick view missing %q:\n%s", want, view)
		}
	}

	m = press(t, m, "enter")
	if deps.applied != "orion" || !m.Done() {
		t.Fatalf("applied = %q done = %v; want orion applied and closed", deps.applied, m.Done())
	}
	if !strings.Contains(m.Status(), "orion (custom)") {
		t.Fatalf("status = %q", m.Status())
	}
}

func TestCreatePromptByPaste(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps("")
	m := open(t, deps, 100, 40)
	m.pickCursor = len(m.pickOptions()) - 1
	m = press(t, m, "enter")
	if m.screen != screenManage {
		t.Fatalf("screen = %v, want manage", m.screen)
	}

	m = press(t, m, "n")
	if m.screen != screenName {
		t.Fatalf("screen = %v, want name", m.screen)
	}
	m = press(t, m, "programmer", "enter")
	if m.screen != screenName || !strings.Contains(m.errMsg, "built-in") {
		t.Fatalf("built-in name should be rejected, errMsg = %q", m.errMsg)
	}
	m.nameInput.SetValue("")
	m = press(t, m, "Code Reviewer", "enter")
	if m.screen != screenContent || m.editName != "code-reviewer" || !m.creating {
		t.Fatalf("screen = %v name = %q creating = %v", m.screen, m.editName, m.creating)
	}

	m = press(t, m, "ctrl+s")
	if m.errMsg == "" {
		t.Fatal("saving an empty prompt should fail in the editor")
	}

	long := strings.Repeat("Review every diff line.\n", 150)
	m = paste(t, m, long)
	if got := m.body.Value(); got != long {
		t.Fatalf("150-line paste changed: got %d bytes / %d lines, want %d bytes / 150 lines",
			len(got), strings.Count(got, "\n"), len(long))
	}
	m = press(t, m, "ctrl+s")
	if m.screen != screenManage || m.errMsg != "" {
		t.Fatalf("after save screen = %v err = %q", m.screen, m.errMsg)
	}
	if !strings.HasPrefix(deps.prompts["code-reviewer"], "Review every diff line.") {
		t.Fatalf("saved body = %.40q", deps.prompts["code-reviewer"])
	}
	if len(m.customs) != 1 || m.customs[0] != "code-reviewer" {
		t.Fatalf("list not refreshed: %v", m.customs)
	}
}

func TestDuplicateNameRejected(t *testing.T) {
	t.Parallel()
	m := open(t, newFakeDeps("", "atlas"), 100, 40)
	m.screen = screenManage
	m = press(t, m, "n", "atlas", "enter")
	if m.screen != screenName || !strings.Contains(m.errMsg, "already exists") {
		t.Fatalf("screen = %v err = %q", m.screen, m.errMsg)
	}
}

func TestImportFromFile(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps("")
	m := open(t, deps, 100, 40)
	m.screen = screenManage
	m = press(t, m, "n", "ops", "enter", "tab")
	if m.src != sourceImport {
		t.Fatal("Tab should switch to import")
	}
	m = press(t, m, "missing.md", "enter")
	if m.screen != screenContent || !strings.Contains(m.errMsg, "no such file") {
		t.Fatalf("failed import should stay on the editor with the error, err = %q", m.errMsg)
	}
	m.pathInput.SetValue("")
	m = press(t, m, "~/ops.md", "enter")
	if deps.imported != "~/ops.md" || m.screen != screenManage {
		t.Fatalf("imported = %q screen = %v", deps.imported, m.screen)
	}
}

func TestEditRenameDelete(t *testing.T) {
	t.Parallel()
	deps := newFakeDeps("", "atlas", "orion")
	m := open(t, deps, 100, 40)
	m.screen = screenManage

	m = press(t, m, "enter")
	if m.screen != screenContent || m.creating || m.body.Value() != "Body of atlas" {
		t.Fatalf("edit should load the body: screen = %v body = %q", m.screen, m.body.Value())
	}
	m = press(t, m, "esc")

	m = press(t, m, "r")
	if m.screen != screenName || m.nameInput.Value() != "atlas" {
		t.Fatalf("rename should prefill the name, got %q", m.nameInput.Value())
	}
	m.nameInput.SetValue("")
	m = press(t, m, "atlas-v2", "enter")
	if _, ok := deps.prompts["atlas-v2"]; !ok || m.screen != screenManage {
		t.Fatalf("rename failed: prompts = %v", deps.prompts)
	}

	m.manageCursor = 1 // orion
	m = press(t, m, "d")
	if m.screen != screenConfirmDelete || !strings.Contains(m.View(), "Delete orion?") {
		t.Fatalf("delete should confirm first:\n%s", m.View())
	}
	m = press(t, m, "n")
	if _, ok := deps.prompts["orion"]; !ok || m.screen != screenManage {
		t.Fatal("any key other than y must cancel the delete")
	}
	m = press(t, m, "d", "y")
	if _, ok := deps.prompts["orion"]; ok {
		t.Fatal("y should delete")
	}
	if len(m.customs) != 1 || m.manageCursor > len(m.customs) {
		t.Fatalf("after delete customs = %v cursor = %d", m.customs, m.manageCursor)
	}
}

func TestViewFitsShortTerminal(t *testing.T) {
	t.Parallel()
	const height = 10
	base := open(t, newFakeDeps("", "atlas", "orion"), 80, height)
	opts := base.pickOptions()
	for _, cursor := range []int{0, len(opts) - 1} {
		m := base
		m.pickCursor = cursor
		view := m.View()
		if got := strings.Count(view, "\n") + 1; got > height {
			t.Errorf("cursor %d: view is %d lines, want <= %d:\n%s", cursor, got, height, view)
		}
		for _, want := range []string{"System Prompt", "Esc close", "> " + opts[cursor].label} {
			if !strings.Contains(view, want) {
				t.Errorf("cursor %d: view lost %q:\n%s", cursor, want, view)
			}
		}
	}

	m := base
	m.screen = screenManage
	m = press(t, m, "n", "small", "enter")
	m = paste(t, m, strings.Repeat("line\n", 40))
	view := m.View()
	if got := strings.Count(view, "\n") + 1; got > height {
		t.Errorf("content screen is %d lines, want <= %d:\n%s", got, height, view)
	}
	if !strings.Contains(view, "New Custom Prompt: small") {
		t.Errorf("content screen lost its title:\n%s", view)
	}
}

func TestResizeOnContentScreen(t *testing.T) {
	t.Parallel()
	m := open(t, newFakeDeps(""), 0, 0)
	m = m.SetSize(120, 50) // must not panic before the editor exists
	m.screen = screenManage
	m = press(t, m, "n", "wide", "enter")
	m = m.SetSize(60, 20)
	if w := m.body.Width(); w <= 0 || w > 60 {
		t.Fatalf("editor width = %d after resize to 60 columns", w)
	}
}

func TestPickScopeToggleClearAndHomeDirFallback(t *testing.T) {
	t.Parallel()

	t.Run("Tab switches scope and Enter saves globally", func(t *testing.T) {
		t.Parallel()
		deps := newFakeDeps("programmer")
		m := open(t, deps, 100, 30)
		m = press(t, m, "tab", "enter")
		if deps.applied != "programmer" || deps.appliedScope != config.ScopeGlobal || !m.Done() {
			t.Fatalf("applied=(%q, %v) done=%v, want (programmer, global) done=true", deps.applied, deps.appliedScope, m.Done())
		}
	})

	t.Run("r clears selected scope", func(t *testing.T) {
		t.Parallel()
		deps := newFakeDeps("programmer")
		m := open(t, deps, 100, 30)
		m = press(t, m, "r")
		if deps.applied != "" || deps.appliedScope != config.ScopeProject || !m.Done() {
			t.Fatalf("applied=(%q, %v) done=%v, want (empty, project) done=true", deps.applied, deps.appliedScope, m.Done())
		}
		if !strings.Contains(m.Status(), "cleared in project") {
			t.Fatalf("status = %q, want clear confirmation", m.Status())
		}
	})

	t.Run("home directory hides scope selector and names global file", func(t *testing.T) {
		t.Parallel()
		deps := newFakeDeps("programmer")
		deps.projectAvailable = false
		m := open(t, deps, 100, 30)
		view := m.View()
		if !strings.Contains(view, "~/.sagittarius/settings.json") {
			t.Fatalf("home-dir view should name global settings file:\n%s", view)
		}
		m = press(t, m, "tab", "enter")
		if deps.appliedScope != config.ScopeGlobal || !m.Done() {
			t.Fatalf("home-dir scope = %v done = %v, want ScopeGlobal / true", deps.appliedScope, m.Done())
		}
	})
}
