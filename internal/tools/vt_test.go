package tools

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestVT(t *testing.T) {
	term := vt.NewEmulator(80, 24)
	for i := 0; i < 30; i++ {
		_, _ = fmt.Fprintf(term, "Line %d\r\n", i)
	}
	fmt.Printf("%q\n", term.String())
}
