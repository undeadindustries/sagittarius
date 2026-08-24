package slash

import "testing"

func TestExtractFencedCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    string
		want  string
		count int
	}{
		{name: "none", in: "just prose", want: "", count: 0},
		{
			name:  "one block",
			in:    "run this:\n```\neval \"$(starship init bash)\"\n```\n",
			want:  "eval \"$(starship init bash)\"",
			count: 1,
		},
		{
			name:  "language tag",
			in:    "```bash\nsudo mkfs.ext4 -L drive /dev/sda1\n```",
			want:  "sudo mkfs.ext4 -L drive /dev/sda1",
			count: 1,
		},
		{
			name:  "multiple blocks",
			in:    "```\nfirst\n```\nprose\n```\nsecond\n```",
			want:  "first\n\nsecond",
			count: 2,
		},
		{
			name:  "indented fence",
			in:    "  ```\n  sudo mount -a\n  ```",
			want:  "  sudo mount -a",
			count: 1,
		},
		{
			name:  "empty fence skipped",
			in:    "```\n```\n```\nreal\n```",
			want:  "real",
			count: 1,
		},
		{
			name:  "unclosed at eof",
			in:    "```\nstill useful",
			want:  "still useful",
			count: 1,
		},
		{
			name:  "crlf body",
			in:    "```\nline\r\n```",
			want:  "line",
			count: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, n := extractFencedCode(tc.in)
			if n != tc.count {
				t.Fatalf("count = %d, want %d (body %q)", n, tc.count, got)
			}
			if got != tc.want {
				t.Fatalf("body = %q, want %q", got, tc.want)
			}
		})
	}
}
