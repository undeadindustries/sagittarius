package tools

import (
	"fmt"
	"github.com/creack/pty"
	"os/exec"
	"testing"
)

func TestPTY(t *testing.T) {
	cmd := exec.Command("stty", "size")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := make([]byte, 100)
	n, _ := f.Read(b)
	fmt.Printf("%q\n", string(b[:n]))
}
