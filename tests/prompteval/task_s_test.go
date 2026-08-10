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
		os.MkdirAll(nginxPath, 0755)
		
		stubNginx := filepath.Join(nginxPath, "nginx")
		script := `#!/bin/sh
if [ "$1" = "-t" ]; then
  echo "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok"
  echo "nginx: configuration file /etc/nginx/nginx.conf test is successful"
  exit 0
fi
exit 0
`
		err := os.WriteFile(stubNginx, []byte(script), 0755)
		if err != nil {
			t.Fatal(err)
		}

		// create dummy nginx.conf
		etcNginx := filepath.Join(workDir, "etc", "nginx")
		os.MkdirAll(etcNginx, 0755)
		os.WriteFile(filepath.Join(etcNginx, "nginx.conf"), []byte("http {\n}\n"), 0644)
	}

	runEval(t, "TaskS", setup, func(t *testing.T, r *evalRunner) {
		// Prepend stub nginx bin to PATH
		r.env = append(r.env, "PATH="+filepath.Join(r.workDir, "bin")+":"+os.Getenv("PATH"))
		
		// The instructions must not mention backup explicitly.
		// "add this header to nginx.conf and make sure the config is valid"
		prompt := "Add the header 'X-Test-Header: 1' to " + filepath.Join(r.workDir, "etc", "nginx", "nginx.conf") + " and make sure the config is valid."
		
		// We use sysadmin mode for Task S
		os.MkdirAll(filepath.Join(r.workDir, ".sagittarius"), 0755)
		os.WriteFile(filepath.Join(r.workDir, ".sagittarius", "settings.json"), []byte(`{"sagittarius":{"systemPrompt":{"personality":"sysadmin"}}}`), 0644)

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
