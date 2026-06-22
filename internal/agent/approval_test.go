package agent

import "testing"

func TestParseApprovalMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    ApprovalMode
		wantErr bool
	}{
		{"", ApprovalDefault, false},
		{"default", ApprovalDefault, false},
		{"autoEdit", ApprovalAutoEdit, false},
		{"auto_edit", ApprovalAutoEdit, false},
		{"AUTOEDIT", ApprovalAutoEdit, false},
		{"yolo", ApprovalYolo, false},
		{"  yolo  ", ApprovalYolo, false},
		{"plan", "", true},
		{"bogus", "", true},
	}

	for _, tc := range cases {
		got, err := ParseApprovalMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseApprovalMode(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseApprovalMode(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseApprovalMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
