package tools

import (
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"
	"github.com/creack/pty"
)

func TestPTYFail(t *testing.T) {
	cmd := exec.Command("bash", "-c", "echo oops >&2; exit 7")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	// copy
	go func() {
		b, _ := io.ReadAll(f)
		fmt.Printf("Read %d bytes: %q\n", len(b), string(b))
	}()

	cmd.Wait()
	time.Sleep(10 * time.Millisecond) // Give Read a chance
	f.Close()
	time.Sleep(10 * time.Millisecond)
}
