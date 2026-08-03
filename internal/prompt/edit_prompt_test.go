package prompt

import (
	"strings"
	"testing"
)

func TestEditGuidanceGatedByEditEnabled(t *testing.T) {
	tests := []struct {
		name        string
		editEnabled bool
		wantPhrase  string
	}{
		{
			name:        "enabled",
			editEnabled: true,
			wantPhrase:  "prefer `edit` over `write_file`",
		},
		{
			name:        "disabled",
			editEnabled: false,
			wantPhrase:  "You cannot use patch/diff operations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{
				EditEnabled: tt.editEnabled,
			}
			got := liteToolUsage(opts.SymbolsEnabled, opts.EditEnabled)
			if !strings.Contains(got, tt.wantPhrase) {
				t.Errorf("liteToolUsage() missing phrase %q", tt.wantPhrase)
			}

			gotFull := fullPrimaryWorkflow(false, false, opts.EditEnabled)
			if tt.editEnabled {
				if !strings.Contains(gotFull, "Apply targeted, surgical changes with `edit` for partial changes") {
					t.Errorf("fullPrimaryWorkflow() missing edit phrase")
				}
			} else {
				if !strings.Contains(gotFull, "Apply targeted, surgical changes with `write_file` and `run_shell_command`") {
					t.Errorf("fullPrimaryWorkflow() missing write_file phrase")
				}
			}
		})
	}
}
