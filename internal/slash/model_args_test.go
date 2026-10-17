package slash

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestParseModelArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      string
		wantPick  string
		wantScope config.SettingScope
		wantErr   bool
	}{
		{name: "empty opens picker", args: "", wantScope: config.ScopeGlobal},
		{name: "plain pick is global", args: "gemini/gemini-2.5-pro", wantPick: "gemini/gemini-2.5-pro", wantScope: config.ScopeGlobal},
		{name: "flag before pick", args: "--project gemini/gemini-2.5-pro", wantPick: "gemini/gemini-2.5-pro", wantScope: config.ScopeProject},
		{name: "flag after pick", args: "  gemini/gemini-2.5-pro   --project ", wantPick: "gemini/gemini-2.5-pro", wantScope: config.ScopeProject},
		{name: "bare flag opens picker", args: "--project", wantScope: config.ScopeProject},
		{name: "two picks rejected", args: "a/b c/d", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pick, scope, err := parseModelArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if pick != tt.wantPick || scope != tt.wantScope {
				t.Errorf("parseModelArgs(%q) = %q, %v; want %q, %v", tt.args, pick, scope, tt.wantPick, tt.wantScope)
			}
		})
	}
}
