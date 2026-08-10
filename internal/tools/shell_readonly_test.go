package tools

import (
	"testing"
)

func TestClassifyShellReadOnly(t *testing.T) {
	tests := []struct {
		command     string
		wantVerdict ShellVerdict
	}{
		// Read-only built-ins
		{"ls -l /tmp", VerdictReadOnly},
		{"cat /etc/passwd", VerdictReadOnly},
		{"journalctl -u nginx --since '1 hour ago'", VerdictReadOnly},
		{"systemctl status sshd", VerdictReadOnly},
		{"systemctl is-active docker", VerdictReadOnly},

		// Unknown (requires confirmation)
		{"some-unknown-command", VerdictUnknown},
		{"python script.py", VerdictUnknown},
		{"sudo ls /root", VerdictUnknown}, // sudo downgrades
		{"bash -c 'echo hi'", VerdictUnknown},

		// Mutating
		{"rm -rf /tmp/foo", VerdictMutating},
		{"systemctl restart nginx", VerdictMutating},
		{"systemctl stop sshd", VerdictMutating},
		{"systemctl daemon-reload", VerdictMutating},
		{"systemctl enable foo", VerdictMutating},
		{"docker run -it ubuntu bash", VerdictMutating},
		{"docker rm foo", VerdictMutating},
		{"git commit -m 'hi'", VerdictMutating},
		{"git push origin main", VerdictMutating},
		{"apt-get install nginx", VerdictMutating},
		{"npm install", VerdictMutating},

		// Redirections
		{"ls -l > out.txt", VerdictMutating},
		{"cat foo >> bar", VerdictMutating},
		{"echo hi 2> err.txt", VerdictMutating},
		{"echo hi &> out.txt", VerdictMutating},

		// Pipes and Subshells
		{"ls | grep foo", VerdictReadOnly},
		{"cat foo.sh | bash", VerdictUnknown},
		{"ls | tee out.txt", VerdictUnknown},
		{"echo $(whoami)", VerdictUnknown},
		{"echo `whoami`", VerdictUnknown},

		// Sed and Find
		{"sed 's/a/b/' foo.txt", VerdictReadOnly},
		{"sed -i 's/a/b/' foo.txt", VerdictMutating},
		{"find . -name '*.go'", VerdictReadOnly},
		{"find . -name '*.go' -delete", VerdictMutating},
		{"find . -name '*.go' -exec rm {} \\;", VerdictMutating},

		// Validators
		{"nginx -t", VerdictReadOnly},
		{"sshd -t", VerdictReadOnly},
		{"visudo -c", VerdictReadOnly},

		// SQL
		{"psql -c 'SELECT * FROM users'", VerdictReadOnly},
		{"mysql -e 'SHOW TABLES'", VerdictReadOnly},
		{"sqlite3 db.sqlite 'EXPLAIN QUERY PLAN SELECT 1'", VerdictReadOnly},
		{"psql -c 'DELETE FROM users'", VerdictUnknown},                        // Doesn't start with SELECT/SHOW/EXPLAIN
		{"psql -c 'SELECT * FROM users; DELETE FROM admins'", VerdictMutating}, // Contains DELETE
		{"psql -c 'EXPLAIN ANALYZE DELETE FROM users'", VerdictMutating},       // EXPLAIN ANALYZE is mutating
		{"mysql -e 'SELECT * INTO backup FROM users'", VerdictMutating},        // Contains INTO
		{"psql -f script.sql", VerdictUnknown},                                 // -f is unknown
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			gotVerdict, _ := ClassifyShellReadOnly(tt.command)
			if gotVerdict != tt.wantVerdict {
				t.Errorf("ClassifyShellReadOnly(%q) = %v, want %v", tt.command, gotVerdict, tt.wantVerdict)
			}
		})
	}
}
