package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/trajectory"
)

func TestE2E_MockATIFSmoke(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "smoke.txt", "hello smoke")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()
	env := mockEnv(home, "smoke-session")

	atifOut := filepath.Join(work, "smoke.atif.json")
	res := invoke(t, bin, work, env,
		"--yolo", "--output-format", "stream-json", "-p", "create smoke.txt with content hello smoke", "--atif-out", atifOut)
	if res.exitCode != 0 {
		t.Fatalf("run failed exit=%d stderr=%s stdout=%s", res.exitCode, res.stderr, res.stdout)
	}

	data, err := os.ReadFile(atifOut)
	if err != nil {
		t.Fatalf("read atif output: %v", err)
	}

	var traj trajectory.Trajectory
	if err := json.Unmarshal(data, &traj); err != nil {
		t.Fatalf("unmarshal trajectory: %v", err)
	}

	valErrs := trajectory.Validate(&traj)
	if len(valErrs) > 0 {
		t.Fatalf("trajectory validation errors: %v", valErrs)
	}

	// Now run analyze-trajectory CLI flag on the generated file
	resAnalyze := invoke(t, bin, work, env, "--analyze-trajectory", atifOut)
	if resAnalyze.exitCode != 0 {
		t.Fatalf("analyze-trajectory CLI failed exit=%d stderr=%s", resAnalyze.exitCode, resAnalyze.stderr)
	}
}
