package bgproc

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestManager(t *testing.T) {
	mgr := NewManager()
	
	cmd := exec.Command("sleep", "2")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	
	mgr.Register(cmd.Process.Pid, cmd.Process.Pid, "sleep 2", "")
	
	// kill it
	if err := mgr.Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	
	time.Sleep(2 * time.Second) // wait for reaper
	
	p, ok := mgr.Get(cmd.Process.Pid)
	if !ok {
		t.Fatal("expected proc to still be in registry")
	}
	if p.Status != StatusExited {
		t.Fatalf("expected exited, got %s", p.Status)
	}
}
