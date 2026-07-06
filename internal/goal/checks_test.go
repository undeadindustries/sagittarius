package goal

import (
	"context"
	"testing"
)

func TestExtractCommands(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "no commands",
			text: "just do the thing",
			want: nil,
		},
		{
			name: "allowed command",
			text: "make sure `go test ./...` passes",
			want: []string{"go test ./..."},
		},
		{
			name: "disallowed command",
			text: "run `rm -rf /`",
			want: nil,
		},
		{
			name: "multiple commands",
			text: "`npm test` and `make build` and `git status`",
			want: []string{"npm test", "make build", "git status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCommands(tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d commands, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRunDeterministicChecks(t *testing.T) {
	ctx := context.Background()
	out, err := runDeterministicChecks(ctx, "run `echo hello`", ".")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("expected empty out for disallowed cmd, got %q", out)
	}

	// This is slightly tricky to test without actual commands in the workspace.
	// But `git status` should generally work if we are in a git repo.
	out, err = runDeterministicChecks(ctx, "run `git status`", ".")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Errorf("expected output from git status")
	}
}
