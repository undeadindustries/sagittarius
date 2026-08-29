package slash

import "testing"

func TestIsBangInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		want bool
	}{
		{"!ls", true},
		{"  ! ls -la /tmp", true},
		{"!", true},
		{"/run ls -la", true},
		{"/run", true},
		{"/RUN echo hi", true},
		{"/run\tfoo", true},
		{"/runtime", false},
		{"/runfoo", false},
		{"ls", false},
		{"", false},
		{"/help", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			if got := IsBangInput(tt.line); got != tt.want {
				t.Errorf("IsBangInput(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseBangCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		cmd  string
		ok   bool
	}{
		{"!ls -la /tmp", "ls -la /tmp", true},
		{"  !  echo hi", "echo hi", true},
		{"!", "", true},
		{"/run ls -la", "ls -la", true},
		{"/RUN echo hi", "echo hi", true},
		{"/run", "", true},
		{"/help", "", false},
		{"ls", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseBangCommand(tt.line)
			if ok != tt.ok || got != tt.cmd {
				t.Errorf("ParseBangCommand(%q) = (%q, %v), want (%q, %v)",
					tt.line, got, ok, tt.cmd, tt.ok)
			}
		})
	}
}
