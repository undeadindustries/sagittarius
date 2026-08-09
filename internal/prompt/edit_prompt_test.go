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

			gotLiteEdit := liteEditRules(opts.EditEnabled)
			if !strings.Contains(gotLiteEdit, tt.wantPhrase) {
				t.Errorf("liteEditRules() missing phrase %q", tt.wantPhrase)
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

			gotFullOps := fullOperationalGuidelines(opts.EditEnabled, false)
			if tt.editEnabled {
				if !strings.Contains(gotFullOps, "then `write_file` or `edit`.") {
					t.Errorf("fullOperationalGuidelines() missing edit phrase")
				}
			} else {
				if strings.Contains(gotFullOps, "`edit`") {
					t.Errorf("fullOperationalGuidelines() should not contain edit phrase")
				}
			}
		})
	}
}

func TestProgrammerBackgroundShell(t *testing.T) {
	out := fullOperationalGuidelines(false, false)
	if !strings.Contains(out, "is_background") {
		t.Error("programmer operational guidelines should teach is_background")
	}
	if strings.Contains(out, " &)") || strings.Contains(out, "server.js &") {
		t.Error("programmer operational guidelines should not teach bare & backgrounding")
	}

	liteOut := liteShellSafety(false)
	if !strings.Contains(liteOut, "is_background") {
		t.Error("liteShellSafety should teach is_background")
	}
	if strings.Contains(liteOut, " &)") || strings.Contains(liteOut, "server.js &") {
		t.Error("liteShellSafety should not teach bare & backgrounding")
	}
}
