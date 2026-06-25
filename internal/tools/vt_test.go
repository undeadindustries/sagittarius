package tools

import (
	"fmt"
	"github.com/charmbracelet/x/vt"
	"testing"
)

func TestVT(t *testing.T) {
	term := vt.NewEmulator(80, 24)
	for i := 0; i < 30; i++ {
		term.Write([]byte(fmt.Sprintf("Line %d\r\n", i)))
	}
	fmt.Printf("%q\n", term.String())
}
