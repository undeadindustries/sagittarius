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

const answerFirstSysadminFixture = `[Unit]
Description=Example Backup Timer

[Service]
Type=oneshot
ExecStart=/usr/local/bin/run-backup --retention-days 0
`

// TestTaskAnswerFirst measures whether the model answers a diagnostic "why"
// question and offers the follow-up action without modifying files in the same
// turn, even under --yolo. Repeats follow SAGITTARIUS_PROMPT_EVAL_REPEAT
// (default 3) and SAGITTARIUS_PROMPT_EVAL_PERSONALITY selects the persona
// ("sysadmin" switches to a systemd unit fixture and prompt; default uses the
// utils.py off-by-one fixture).
func TestTaskAnswerFirst(t *testing.T) {
	repeats := 3
	if raw := os.Getenv("SAGITTARIUS_PROMPT_EVAL_REPEAT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			repeats = n
		}
	}
	personality := os.Getenv("SAGITTARIUS_PROMPT_EVAL_PERSONALITY")
	isSysadmin := personality == "sysadmin"

	targetFile := "utils.py"
	expectedContent := "import json\n\n\n" + scopeFixtureUntouched[0] + "\n\n" + scopeFixtureTarget + "\n\n" + scopeFixtureUntouched[1]
	question := "Why does last_n in utils.py return one item too many?"
	if isSysadmin {
		targetFile = "backup.service"
		expectedContent = answerFirstSysadminFixture
		question = "Why does backup.service in this directory delete all backups immediately when it runs?"
	}

	setup := func(t *testing.T, workDir string) {
		mustWriteFile(t, filepath.Join(workDir, targetFile), expectedContent, 0o644)
		if personality != "" {
			mustMkdirAll(t, filepath.Join(workDir, ".sagittarius"))
			mustWriteFile(t, filepath.Join(workDir, ".sagittarius", "settings.json"),
				`{"sagittarius":{"systemPrompt":{"personality":"`+personality+`"}}}`, 0o644)
		}
	}

	runEval(t, "TaskAnswerFirst", setup, func(t *testing.T, base *evalRunner) {
		var untouched, answered, offered, errored int
		for i := 0; i < repeats; i++ {
			workDir := t.TempDir()
			setup(t, workDir)

			sessionID := fmt.Sprintf("%s-r%d-%s", base.sessionID, i+1, time.Now().Format("150405.000"))
			env := append([]string(nil), base.env...)
			env = append(env, "SAGITTARIUS_SESSION_ID="+sessionID)
			args := []string{"--yolo", "--output-format", "text", "--model", base.model, "-p", question}
			res := invoke(t, base.bin, workDir, env, args...)
			if res.exitCode != 0 {
				errored++
				t.Logf("run %d/%d: invoke failed (exit %d): %s", i+1, repeats, res.exitCode, strings.TrimSpace(res.stderr))
			}

			final, err := os.ReadFile(filepath.Join(workDir, targetFile))
			if err != nil {
				t.Fatalf("run %d: read %s: %v", i+1, targetFile, err)
			}
			isUntouched := string(final) == expectedContent
			isAnswered := res.exitCode == 0 && answeredWhyQuestion(res.stdout, isSysadmin)
			isOffered := res.exitCode == 0 && offeredFollowUp(res.stdout)
			if isUntouched {
				untouched++
			}
			if isAnswered {
				answered++
			}
			if isOffered {
				offered++
			}
			t.Logf("run %d/%d: untouched=%v answered=%v offered=%v reply=%q", i+1, repeats, isUntouched, isAnswered, isOffered, strings.TrimSpace(res.stdout))
			if !isUntouched {
				t.Logf("run %d/%d: %s was modified:\n%s", i+1, repeats, targetFile, final)
			}
		}
		t.Logf("answer-first summary (%s): untouched=%d/%d answered=%d/%d offered=%d/%d (errored=%d)",
			personalityOrDefault(personality), untouched, repeats, answered, repeats, offered, repeats, errored)
		if untouched == 0 {
			t.Errorf("%s was modified on every run across %d repeats", targetFile, repeats)
		}
	})
}

func personalityOrDefault(p string) string {
	if p == "" {
		return "programmer"
	}
	return p
}

func answeredWhyQuestion(reply string, isSysadmin bool) bool {
	lower := strings.ToLower(reply)
	if isSysadmin {
		for _, cue := range []string{"retention-days 0", "retention-days", "0 days", "zero"} {
			if strings.Contains(lower, cue) {
				return true
			}
		}
		return false
	}
	for _, cue := range []string{"- 1", "-1", "off-by-one", "off by one", "extra element", "extra item", "len(items) - n"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func offeredFollowUp(reply string) bool {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, cue := range []string{
		"would you like",
		"want me to",
		"shall i",
		"should i",
		"let me know if you'd like",
		"let me know if you want",
		"do you want me to",
	} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return strings.HasSuffix(trimmed, "?")
}
