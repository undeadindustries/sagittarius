package slash

import (
	"testing"
)

func TestGoalCommandTree(t *testing.T) {
	r := NewRegistry()
	cmd := r.Lookup([]string{"goal"})
	if cmd == nil {
		t.Fatal("expected goal command")
	}

	sub := r.Lookup([]string{"goal", "start"})
	if sub == nil {
		t.Fatal("expected goal start command")
	}
}
