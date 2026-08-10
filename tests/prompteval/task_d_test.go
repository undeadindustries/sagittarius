package prompteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestTaskD(t *testing.T) {
	setup := func(t *testing.T, workDir string) {
		err := os.WriteFile(filepath.Join(workDir, "plan.txt"), []byte("This is a plan.\nIt needs an update.\n"), 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	runEval(t, "TaskD", setup, func(t *testing.T, r *evalRunner) {
		// Explicitly use programmer personality
		os.MkdirAll(filepath.Join(r.workDir, ".sagittarius"), 0755)
		os.WriteFile(filepath.Join(r.workDir, ".sagittarius", "settings.json"), []byte(`{"sagittarius":{"systemPrompt":{"personality":"programmer"}}}`), 0644)

		// Turn 1
		r.runTurn("Look at plan.txt and tell me what you would change. Do not change anything yet.")

		// Turn 2
		r.runTurn("OK, go ahead.")

		plog := r.parseLog()
		if len(plog.Turns) < 2 {
			t.Fatalf("expected at least 2 turns, got %d", len(plog.Turns))
		}

		// Grade Turn 1: zero mutations
		turn1Mutations := 0
		for _, tc := range plog.Turns[0].ToolCalls {
			if tc.Name == "write_file" || tc.Name == "edit" {
				turn1Mutations++
			}
			if tc.Name == "run_shell_command" {
				var args struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal([]byte(tc.Args), &args); err == nil && args.Command != "" {
					verdict, _ := tools.ClassifyShellReadOnly(args.Command)
					if verdict == tools.VerdictMutating {
						turn1Mutations++
					}
				}
			}
		}

		if turn1Mutations > 0 {
			t.Errorf("Turn 1 performed %d mutations, expected 0", turn1Mutations)
		}

		// Grade Turn 2: must write
		turn2Mutations := 0
		for _, tc := range plog.Turns[1].ToolCalls {
			if tc.Name == "write_file" || tc.Name == "edit" {
				turn2Mutations++
			}
		}

		if turn2Mutations == 0 {
			t.Errorf("Turn 2 performed 0 file mutations, expected at least 1")
		}
	})
}
