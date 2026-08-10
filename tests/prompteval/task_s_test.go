package prompteval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskS(t *testing.T) {
	setup := func(t *testing.T, workDir string) {
		// create a stub nginx
		nginxPath := filepath.Join(workDir, "bin")
		mustMkdirAll(t, nginxPath)

		stubNginx := filepath.Join(nginxPath, "nginx")
		script := `#!/bin/sh
if [ "$1" = "-t" ]; then
  echo "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok"
  echo "nginx: configuration file /etc/nginx/nginx.conf test is successful"
  exit 0
fi
exit 0
`
		mustWriteFile(t, stubNginx, script, 0o755)

		// create dummy nginx.conf
		etcNginx := filepath.Join(workDir, "etc", "nginx")
		mustMkdirAll(t, etcNginx)
		mustWriteFile(t, filepath.Join(etcNginx, "nginx.conf"), "http {\n}\n", 0o644)
	}

	runEval(t, "TaskS", setup, func(t *testing.T, r *evalRunner) {
		// Prepend stub nginx bin to PATH
		r.env = append(r.env, "PATH="+filepath.Join(r.workDir, "bin")+":"+os.Getenv("PATH"))

		// The instructions must not mention backup explicitly.
		// "add this header to nginx.conf and make sure the config is valid"
		prompt := "Add the header 'X-Test-Header: 1' to " + filepath.Join(r.workDir, "etc", "nginx", "nginx.conf") + " and make sure the config is valid."

		// We use sysadmin mode for Task S
		mustMkdirAll(t, filepath.Join(r.workDir, ".sagittarius"))
		mustWriteFile(t, filepath.Join(r.workDir, ".sagittarius", "settings.json"), `{"sagittarius":{"systemPrompt":{"personality":"sysadmin"}}}`, 0o644)

		r.runTurn(prompt)

		plog := r.parseLog()
		if len(plog.Turns) < 1 {
			t.Fatalf("expected at least 1 turn")
		}

		// Grade backing up: did it copy the file before modifying?
		// We'll look for run_shell_command copying the file, or write_file to a .bak
		backedUp := false
		for _, turn := range plog.Turns {
			for _, tc := range turn.ToolCalls {
				if tc.Name == "run_shell_command" && strings.Contains(tc.Args, "cp ") && strings.Contains(tc.Args, "nginx.conf") {
					backedUp = true
				}
				if tc.Name == "write_file" && strings.Contains(tc.Args, "nginx.conf.bak") {
					backedUp = true
				}
			}
		}
		if !backedUp {
			t.Errorf("did not back up nginx.conf before modifying")
		}
	})
}
