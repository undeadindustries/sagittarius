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

// TestTaskSkillActivation measures how often the model calls activate_skill for
// python3 as its first tool call when asked to edit a Python project. It repeats
// the run SAGITTARIUS_PROMPT_EVAL_REPEAT times (default 3) so stochastic
// behavior at high temperature (e.g. 1.0) shows up as a rate rather than a
// single pass/fail coin flip. Sampling temperature comes from the user's
// configured sampling settings for SAGITTARIUS_PROMPT_EVAL_MODEL.
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

	setup := func(t *testing.T, workDir string) {
		mustWriteFile(t, filepath.Join(workDir, "pyproject.toml"), "[project]\nname = \"demo\"\nversion = \"0.1.0\"\nrequires-python = \">=3.11\"\n", 0o644)
		mustWriteFile(t, filepath.Join(workDir, "app.py"), "def greet(name: str) -> str:\n    return f\"hello {name}\"\n", 0o644)
	}

	runEval(t, "TaskSkill", setup, func(t *testing.T, base *evalRunner) {
		firstActivated := 0
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
			first := false
			any := false
			for idx, tc := range calls {
				if tc.Name == "activate_skill" && strings.Contains(tc.Args, "python3") {
					any = true
					if idx == 0 {
						first = true
					}
				}
			}
			if first {
				firstActivated++
			}
			if any {
				anyActivated++
			}
			firstName := "(none)"
			if len(calls) > 0 {
				firstName = calls[0].Name + "(" + calls[0].Args + ")"
			}
			t.Logf("run %d/%d: first_call=%s activated_first=%v activated_any=%v", i+1, repeats, firstName, first, any)
		}

		t.Logf("skill activation summary: first=%d/%d any=%d/%d", firstActivated, repeats, anyActivated, repeats)
		if firstActivated == 0 {
			t.Errorf("activate_skill(python3) was never the first tool call across %d runs (any=%d/%d)", repeats, anyActivated, repeats)
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
