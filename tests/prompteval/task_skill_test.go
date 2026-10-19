package prompteval

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTaskSkillActivation measures how often the model activates the python3
// skill before its first file edit when asked to change a Python project. It
// repeats the run SAGITTARIUS_PROMPT_EVAL_REPEAT times (default 3) so behavior
// at high temperature shows up as a rate rather than a single coin flip.
//
// SAGITTARIUS_PROMPT_EVAL_PERSONALITY (programmer, sysadmin, ...) pins the
// system prompt through the workspace settings; unset, the user's own setting
// applies. Sampling temperature comes from the user's settings for
// SAGITTARIUS_PROMPT_EVAL_MODEL.
func TestTaskSkillActivation(t *testing.T) {
	if !hasPython3Skill() {
		t.Skip("skipping skill activation eval; no python3 skill installed under ~/.agents/skills or ~/.sagittarius/skills")
	}

	repeats := 3
	if raw := os.Getenv("SAGITTARIUS_PROMPT_EVAL_REPEAT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			repeats = n
		}
	}
	personality := os.Getenv("SAGITTARIUS_PROMPT_EVAL_PERSONALITY")

	setup := func(t *testing.T, workDir string) {
		mustWriteFile(t, filepath.Join(workDir, "pyproject.toml"), "[project]\nname = \"demo\"\nversion = \"0.1.0\"\nrequires-python = \">=3.11\"\n", 0o644)
		mustWriteFile(t, filepath.Join(workDir, "app.py"), "def greet(name: str) -> str:\n    return f\"hello {name}\"\n", 0o644)
		if personality != "" {
			mustMkdirAll(t, filepath.Join(workDir, ".sagittarius"))
			mustWriteFile(t, filepath.Join(workDir, ".sagittarius", "settings.json"),
				`{"sagittarius":{"systemPrompt":{"personality":"`+personality+`"}}}`, 0o644)
		}
	}

	runEval(t, "TaskSkill", setup, func(t *testing.T, base *evalRunner) {
		beforeEdit := 0
		anyActivated := 0

		for i := 0; i < repeats; i++ {
			workDir := t.TempDir()
			setup(t, workDir)

			sessionID := fmt.Sprintf("%s-r%d-%s", base.sessionID, i+1, time.Now().Format("150405.000"))
			logFile := filepath.Join(os.Getenv("HOME"), ".sagittarius", "logs", fmt.Sprintf("chat-verbose-%s.log", sessionID))
			env := append([]string(nil), base.env...)
			env = append(env, "SAGITTARIUS_SESSION_ID="+sessionID)

			r := &evalRunner{
				t:         t,
				bin:       base.bin,
				workDir:   workDir,
				env:       env,
				sessionID: sessionID,
				logFile:   logFile,
				model:     base.model,
			}

			r.runTurn("In app.py, strip leading/trailing whitespace from name in greet() and raise ValueError on an empty name. Add a pytest test file covering both cases.")
			plog := r.parseLog()

			var calls []ToolCall
			for _, turn := range plog.Turns {
				calls = append(calls, turn.ToolCalls...)
			}
			activatedAt, firstEditAt := -1, -1
			for idx, tc := range calls {
				if activatedAt < 0 && tc.Name == "activate_skill" && strings.Contains(tc.Args, "python3") {
					activatedAt = idx
				}
				if firstEditAt < 0 && (tc.Name == "edit" || tc.Name == "write_file") {
					firstEditAt = idx
				}
			}
			before := activatedAt >= 0 && (firstEditAt < 0 || activatedAt < firstEditAt)
			if before {
				beforeEdit++
			}
			if activatedAt >= 0 {
				anyActivated++
			}
			t.Logf("run %d/%d: activate_skill at call %d, first edit at call %d, before_edit=%v",
				i+1, repeats, activatedAt, firstEditAt, before)
		}

		t.Logf("skill activation summary: before_edit=%d/%d any=%d/%d", beforeEdit, repeats, anyActivated, repeats)
		if beforeEdit == 0 {
			t.Errorf("activate_skill(python3) never ran before the first edit across %d runs (any=%d/%d)", repeats, anyActivated, repeats)
		}
	})
}

func hasPython3Skill() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	for _, rel := range []string{
		filepath.Join(".agents", "skills", "python3", "SKILL.md"),
		filepath.Join(".sagittarius", "skills", "python3", "SKILL.md"),
	} {
		if _, err := os.Stat(filepath.Join(home, rel)); err == nil {
			return true
		}
	}
	return false
}
