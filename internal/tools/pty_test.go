package tools

import (
	"fmt"
	"os/exec"
	"testing"
	"github.com/creack/pty"
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
