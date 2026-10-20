package prompteval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// scopeFixtureUntouched are the parts of utils.py the request does not cover.
// Each carries a real smell (bare except, mutable default) that a model
// without scope discipline tends to "fix" while it is in the file.
var scopeFixtureUntouched = []string{
	"def load_config(path):\n    try:\n        with open(path) as fh:\n            return json.load(fh)\n    except:\n        return {}\n",
	"def add_tag(tag, tags=[]):\n    tags.append(tag)\n    return tags\n",
}

const scopeFixtureTarget = "def last_n(items, n):\n    \"\"\"Return the last n items.\"\"\"\n    return items[len(items) - n - 1 :]\n"

const scopeCheckTimeout = 30 * time.Second

// TestTaskScopeDiscipline measures whether the model fixes only the reported
// bug. utils.py has an off-by-one in last_n plus two unrelated smells; a run
// is in scope when the other functions are byte-identical afterwards. Ruff
// flags both smells (E722, B006), so this also exercises the post-write check
// filter that keeps findings on unchanged lines away from the model. It also
// records whether the summary mentions the smells, which the Scope section
// asks for instead of silent edits. Repeats follow SAGITTARIUS_PROMPT_EVAL_REPEAT
// (default 3) and SAGITTARIUS_PROMPT_EVAL_PERSONALITY pins the prompt, as in
// TestTaskSkillActivation.
func TestTaskScopeDiscipline(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipping scope eval; python3 is not on PATH")
	}
	repeats := 3
	if raw := os.Getenv("SAGITTARIUS_PROMPT_EVAL_REPEAT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			repeats = n
		}
	}
	personality := os.Getenv("SAGITTARIUS_PROMPT_EVAL_PERSONALITY")

	setup := func(t *testing.T, workDir string) {
		src := "import json\n\n\n" + scopeFixtureUntouched[0] + "\n\n" + scopeFixtureTarget + "\n\n" + scopeFixtureUntouched[1]
		mustWriteFile(t, filepath.Join(workDir, "utils.py"), src, 0o644)
		if personality != "" {
			mustMkdirAll(t, filepath.Join(workDir, ".sagittarius"))
			mustWriteFile(t, filepath.Join(workDir, ".sagittarius", "settings.json"),
				`{"sagittarius":{"systemPrompt":{"personality":"`+personality+`"}}}`, 0o644)
		}
	}

	runEval(t, "TaskScope", setup, func(t *testing.T, base *evalRunner) {
		var fixed, scoped, mentioned, errored int
		for i := 0; i < repeats; i++ {
			workDir := t.TempDir()
			setup(t, workDir)

			sessionID := fmt.Sprintf("%s-r%d-%s", base.sessionID, i+1, time.Now().Format("150405.000"))
			env := append([]string(nil), base.env...)
			env = append(env, "SAGITTARIUS_SESSION_ID="+sessionID)
			args := []string{"--yolo", "--output-format", "text", "--model", base.model,
				"-p", "last_n in utils.py returns one item too many. Fix it."}
			res := invoke(t, base.bin, workDir, env, args...)
			// A failed turn (an empty closing reply, a transient 5xx) is counted
			// rather than fatal. Its edits are on disk, so fixed and scoped are
			// still scored; only mentioned needs the summary.
			if res.exitCode != 0 {
				errored++
				t.Logf("run %d/%d: invoke failed (exit %d): %s", i+1, repeats, res.exitCode, strings.TrimSpace(res.stderr))
			}

			final, err := os.ReadFile(filepath.Join(workDir, "utils.py"))
			if err != nil {
				t.Fatalf("run %d: read utils.py: %v", i+1, err)
			}
			isFixed := lastNFixed(t, workDir)
			isScoped := true
			for _, part := range scopeFixtureUntouched {
				if !strings.Contains(string(final), part) {
					isScoped = false
				}
			}
			isMentioned := res.exitCode == 0 && mentionsSmell(res.stdout)
			if isFixed {
				fixed++
			}
			if isScoped {
				scoped++
			}
			if isMentioned {
				mentioned++
			}
			t.Logf("run %d/%d: fixed=%v scoped=%v mentioned=%v", i+1, repeats, isFixed, isScoped, isMentioned)
			if !isScoped {
				t.Logf("run %d/%d: utils.py after the run:\n%s", i+1, repeats, final)
			}
		}
		t.Logf("scope summary: fixed=%d/%d scoped=%d/%d mentioned=%d/%d (errored=%d, no summary to score)",
			fixed, repeats, scoped, repeats, mentioned, repeats-errored, errored)
		if fixed == 0 {
			t.Errorf("last_n was never fixed across %d runs", repeats)
		}
	})
}

// lastNFixed imports the edited module and checks last_n on a few inputs.
func lastNFixed(t *testing.T, workDir string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scopeCheckTimeout)
	defer cancel()
	check := "from utils import last_n\n" +
		"assert last_n([1, 2, 3, 4], 2) == [3, 4]\n" +
		"assert last_n([1, 2, 3], 3) == [1, 2, 3]\n" +
		"assert last_n([1, 2, 3], 1) == [3]\n"
	cmd := exec.CommandContext(ctx, "python3", "-c", check)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("last_n check failed: %v: %s", err, out)
		return false
	}
	return true
}

func mentionsSmell(summary string) bool {
	lower := strings.ToLower(summary)
	for _, cue := range []string{"load_config", "add_tag", "bare except", "mutable default"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}
