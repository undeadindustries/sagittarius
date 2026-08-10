package prompteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskP(t *testing.T) {
	setup := func(t *testing.T, workDir string) {
		err := os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n\nfunc main() {\n\tprintln(\"Hello\")\n}\n"), 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	runEval(t, "TaskP", setup, func(t *testing.T, r *evalRunner) {
		// Explicitly use programmer personality
		os.MkdirAll(filepath.Join(r.workDir, ".sagittarius"), 0755)
		os.WriteFile(filepath.Join(r.workDir, ".sagittarius", "settings.json"), []byte(`{"sagittarius":{"systemPrompt":{"personality":"programmer"}}}`), 0644)

		// Turn 1
		r.runTurn("Read main.go and add a new function sayHi() that prints 'Hi'. Also run `echo done &`.")

		// Turn 2
		r.runTurn("Now call sayHi() from main()")

		// Turn 3
		r.runTurn("Add a comment explaining main()")

		plog := r.parseLog()
		if len(plog.Turns) != 3 {
			t.Fatalf("expected 3 turns, got %d", len(plog.Turns))
		}

		readOrWritten := make(map[string]bool)
		readBeforeWriteViolations := 0
		isBackgroundUsed := false

		for _, turn := range plog.Turns {
			for _, tc := range turn.ToolCalls {
				if tc.Name == "read_file" {
					var args struct{ Path string }
					if err := json.Unmarshal([]byte(tc.Args), &args); err == nil {
						readOrWritten[args.Path] = true
					}
				}
				if tc.Name == "write_file" || tc.Name == "edit" {
					var args struct{ Path string }
					if err := json.Unmarshal([]byte(tc.Args), &args); err == nil {
						if !readOrWritten[args.Path] {
							readBeforeWriteViolations++
						}
						readOrWritten[args.Path] = true
					}
				}
				if tc.Name == "run_shell_command" {
					var args struct {
						Command      string
						IsBackground bool `json:"is_background"`
					}
					if err := json.Unmarshal([]byte(tc.Args), &args); err == nil {
						if args.IsBackground {
							isBackgroundUsed = true
						}
					}
				}
			}
		}

		if readBeforeWriteViolations > 0 {
			t.Errorf("read_before_write violated %d times (wrote without reading first)", readBeforeWriteViolations)
		}
		
		if !isBackgroundUsed {
			t.Log("is_background not used (recorded finding, not a failure)")
		}
	})
}
