package tools

import (
	"strings"
)

// ShellVerdict represents the classification of a shell command's safety for execution during a read-only inspection.
type ShellVerdict int

const (
	VerdictReadOnly ShellVerdict = iota
	VerdictUnknown
	VerdictMutating
)

func (v ShellVerdict) String() string {
	switch v {
	case VerdictReadOnly:
		return "ReadOnly"
	case VerdictMutating:
		return "Mutating"
	case VerdictUnknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

// ClassifyShellReadOnly evaluates a shell command to determine if it is safe to execute automatically
// during a read-only inspection turn. It evaluates each command segment independently; the weakest verdict wins.
// The fallback verdict is VerdictUnknown (deny-by-default allowlist polarity), which will escalate for user confirmation.
func ClassifyShellReadOnly(command string) (ShellVerdict, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return VerdictReadOnly, ""
	}
	tokens := strings.Fields(command)

	// Check for command substitution - $(...) or backticks
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return VerdictUnknown, "Command substitution may hide mutations"
	}

	// Output redirections are mutating.
	for i, tok := range tokens {
		if _, ok := redirectTarget(tok, tokens, i); ok {
			return VerdictMutating, "Output redirection is mutating"
		}
	}

	var overallVerdict = VerdictReadOnly
	var overallReason = "Command appears to be read-only"

	downgrade := func(v ShellVerdict, reason string) {
		if v > overallVerdict {
			overallVerdict = v
			overallReason = reason
		}
	}

	atSegmentStart := true
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if shellSeparators[tok] {
			atSegmentStart = true
			continue
		}
		if !atSegmentStart {
			continue
		}
		atSegmentStart = false

		segEnd := segmentEnd(tokens, i+1)
		segmentTokens := tokens[i:segEnd]

		v, reason := classifySegment(segmentTokens)
		downgrade(v, reason)
		if overallVerdict == VerdictMutating {
			break // Cannot get any worse
		}
	}

	return overallVerdict, overallReason
}

func classifySegment(tokens []string) (ShellVerdict, string) {
	if len(tokens) == 0 {
		return VerdictReadOnly, ""
	}

	cmd := commandBase(tokens[0])
	args := tokens[1:]

	hasSudo := false
	if cmd == "sudo" {
		hasSudo = true
		if len(args) == 0 {
			return VerdictReadOnly, ""
		}
		cmd = commandBase(args[0])
		args = args[1:]
	}

	// Default verdict for a recognized command is Unknown unless it matches a specific read-only pattern.
	// But let's build the logic properly.
	var v ShellVerdict
	var reason string

	switch cmd {
	case "sh", "bash", "zsh", "python", "python3", "node", "ruby", "perl", "awk", "tee", "xargs", "eval":
		v, reason = VerdictUnknown, "Execution of an interpreter, shell, or arbitrary script is unknown"

	case "kill", "pkill", "killall", "rm", "rmdir", "truncate", "chmod", "chown", "mkdir", "dd", "ln", "touch", "cp", "mv", "install":
		v, reason = VerdictMutating, "Command is explicitly mutating"

	case "systemctl":
		v, reason = classifySubcommand(args, map[string]ShellVerdict{
			"status": VerdictReadOnly, "show": VerdictReadOnly, "cat": VerdictReadOnly, "list-units": VerdictReadOnly, "is-enabled": VerdictReadOnly, "is-active": VerdictReadOnly,
			"start": VerdictMutating, "stop": VerdictMutating, "restart": VerdictMutating, "reload": VerdictMutating, "daemon-reload": VerdictMutating, "enable": VerdictMutating, "mask": VerdictMutating,
		})

	case "git":
		v, reason = classifySubcommand(args, map[string]ShellVerdict{
			"status": VerdictReadOnly, "log": VerdictReadOnly, "diff": VerdictReadOnly, "show": VerdictReadOnly, "blame": VerdictReadOnly,
			"checkout": VerdictMutating, "reset": VerdictMutating, "clean": VerdictMutating, "commit": VerdictMutating, "push": VerdictMutating, "pull": VerdictMutating, "fetch": VerdictMutating, "add": VerdictMutating, "rm": VerdictMutating,
		})

	case "docker":
		v, reason = classifySubcommand(args, map[string]ShellVerdict{
			"ps": VerdictReadOnly, "logs": VerdictReadOnly, "inspect": VerdictReadOnly,
			"run": VerdictMutating, "exec": VerdictMutating, "rm": VerdictMutating, "build": VerdictMutating, "push": VerdictMutating, "pull": VerdictMutating, "rmi": VerdictMutating,
		})
		// Special case for docker compose
		if len(args) > 0 && args[0] == "compose" {
			v, reason = classifySubcommand(args[1:], map[string]ShellVerdict{
				"config": VerdictReadOnly, "logs": VerdictReadOnly, "ps": VerdictReadOnly,
				"up": VerdictMutating, "down": VerdictMutating, "build": VerdictMutating, "rm": VerdictMutating,
			})
		}

	case "kubectl":
		v, reason = classifySubcommand(args, map[string]ShellVerdict{
			"get": VerdictReadOnly, "describe": VerdictReadOnly, "logs": VerdictReadOnly, "top": VerdictReadOnly,
			"apply": VerdictMutating, "delete": VerdictMutating, "patch": VerdictMutating, "exec": VerdictMutating, "create": VerdictMutating, "replace": VerdictMutating,
		})

	case "apt", "apt-get", "yum", "dpkg", "dnf", "pacman", "zypper", "apk", "brew", "npm", "yarn", "pnpm", "pip", "pip3", "pipx", "gem", "cargo", "go":
		v, reason = classifySubcommand(args, map[string]ShellVerdict{
			"list": VerdictReadOnly, "search": VerdictReadOnly, "show": VerdictReadOnly, "info": VerdictReadOnly,
			"install": VerdictMutating, "remove": VerdictMutating, "upgrade": VerdictMutating, "update": VerdictMutating, "add": VerdictMutating, "get": VerdictMutating,
		})

	case "sed":
		if hasInPlaceFlag(args) {
			v, reason = VerdictMutating, "sed with in-place flag is mutating"
		} else {
			v, reason = VerdictReadOnly, ""
		}

	case "find":
		if hasFindMutatingFlag(args) {
			v, reason = VerdictMutating, "find with mutating flag (-exec, -delete, etc) is mutating"
		} else {
			v, reason = VerdictReadOnly, ""
		}

	case "curl":
		if hasCurlMutatingFlag(args) {
			v, reason = VerdictUnknown, "curl with data/upload flags is unknown/mutating"
		} else {
			v, reason = VerdictReadOnly, ""
		}

	case "wget":
		if hasFlag(args, "-O") {
			v, reason = VerdictMutating, "wget saving to output file is mutating"
		} else {
			v, reason = VerdictReadOnly, ""
		}

	case "nginx":
		if hasFlag(args, "-t") {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "nginx command without -t validator flag"
		}
	case "sshd":
		if hasFlag(args, "-t") {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "sshd command without -t validator flag"
		}
	case "apachectl":
		if len(args) > 0 && args[0] == "configtest" {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "apachectl command without configtest"
		}
	case "named-checkconf", "unbound-checkconf", "postfix":
		if cmd == "postfix" && len(args) > 0 && args[0] == "check" {
			v, reason = VerdictReadOnly, ""
		} else if cmd != "postfix" {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "postfix without check"
		}
	case "haproxy":
		if hasFlag(args, "-c") {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "haproxy without -c validator flag"
		}
	case "visudo":
		if hasFlag(args, "-c") {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "visudo without -c validator flag"
		}
	case "systemd-analyze":
		if len(args) > 0 && args[0] == "verify" {
			v, reason = VerdictReadOnly, ""
		} else {
			v, reason = VerdictUnknown, "systemd-analyze without verify"
		}

	case "psql", "mysql", "sqlite3":
		v, reason = classifySQL(args)

	case "ls", "cat", "tail", "head", "less", "more", "grep", "rg", "ag", "ack", "stat", "file", "wc", "du", "df", "free", "uptime", "top", "htop", "ps", "pgrep", "netstat", "ss", "lsof", "ip", "ifconfig", "ping", "traceroute", "dig", "host", "nslookup", "whoami", "id", "groups", "pwd", "date", "cal", "echo", "printf", "dmesg", "journalctl":
		v, reason = VerdictReadOnly, ""

	default:
		v, reason = VerdictUnknown, "Command not explicitly recognized as read-only"
	}

	if hasSudo && v == VerdictReadOnly {
		return VerdictUnknown, "sudo prefix requires interactive confirmation for inspection"
	}

	return v, reason
}

func classifySubcommand(args []string, actions map[string]ShellVerdict) (ShellVerdict, string) {
	if len(args) == 0 {
		return VerdictReadOnly, ""
	}
	subcmd := args[0]
	// If the first argument is a flag, ignore it for subcommand classification if possible,
	// but realistically many commands take flags before the subcommand.
	// For simplicity, we just scan args until we find a non-flag.
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			subcmd = arg
			break
		}
	}

	if verdict, ok := actions[subcmd]; ok {
		if verdict == VerdictMutating {
			return VerdictMutating, "Mutating subcommand: " + subcmd
		}
		return VerdictReadOnly, ""
	}
	return VerdictUnknown, "Unknown subcommand: " + subcmd
}

func hasFindMutatingFlag(args []string) bool {
	for _, t := range args {
		if t == "-exec" || t == "-execdir" || t == "-delete" || t == "-ok" || t == "-okdir" {
			return true
		}
	}
	return false
}

func hasCurlMutatingFlag(args []string) bool {
	for i, t := range args {
		if t == "-X" && i+1 < len(args) && (strings.ToUpper(args[i+1]) != "GET" && strings.ToUpper(args[i+1]) != "HEAD" && strings.ToUpper(args[i+1]) != "OPTIONS") {
			return true
		}
		if strings.HasPrefix(t, "-X") && len(t) > 2 {
			method := strings.ToUpper(t[2:])
			if method != "GET" && method != "HEAD" && method != "OPTIONS" {
				return true
			}
		}
		if t == "-d" || t == "--data" || strings.HasPrefix(t, "--data-") || t == "--upload-file" || t == "-o" || t == "--output" || t == "-O" || t == "--remote-name" || t == "-T" {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, t := range args {
		if t == flag {
			return true
		}
	}
	return false
}

func classifySQL(args []string) (ShellVerdict, string) {
	// Look for -c, -e, or just passing a query inline.
	// We also need to check for -f or < file.
	query := ""
	for i, arg := range args {
		if arg == "-f" || arg == "--file" {
			return VerdictUnknown, "SQL script file contents cannot be verified"
		}
		// If using -c (psql) or -e (mysql)
		if (arg == "-c" || arg == "-e" || arg == "--command") && i+1 < len(args) {
			query = strings.Join(args[i+1:], " ")
			break
		}
		// In sqlite3, usually it's `sqlite3 db.sqlite "SELECT * FROM table"`
		if !strings.HasPrefix(arg, "-") && i > 0 { // Assume the rest is query
			query = strings.Join(args[i:], " ")
			break
		}
	}

	if query == "" {
		// Just interactive shell or unknown usage
		return VerdictUnknown, "No inline SQL query found to verify"
	}
	
	// Strip quotes
	query = strings.TrimSpace(query)
	query = strings.Trim(query, `"'`)
	query = strings.TrimSpace(query)

	upperQuery := strings.ToUpper(query)
	// Must begin with SELECT, SHOW, or EXPLAIN
	if !(strings.HasPrefix(upperQuery, "SELECT") || strings.HasPrefix(upperQuery, "SHOW") || strings.HasPrefix(upperQuery, "EXPLAIN")) {
		return VerdictUnknown, "SQL query does not start with SELECT, SHOW, or EXPLAIN"
	}

	if strings.Contains(upperQuery, "EXPLAIN ANALYZE") {
		return VerdictMutating, "EXPLAIN ANALYZE executes the statement"
	}

	mutatingKeywords := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "GRANT", "REVOKE", "TRUNCATE", "REPLACE", "MERGE", "INTO"}
	for _, kw := range mutatingKeywords {
		// Basic boundary check to avoid matching inside words, e.g. "SELECT 'DROP'" is still a false positive but safe side.
		// "INTO" catches "SELECT ... INTO tbl".
		if strings.Contains(upperQuery, " "+kw+" ") || strings.Contains(upperQuery, "\t"+kw+" ") || strings.Contains(upperQuery, "\n"+kw+" ") || strings.HasPrefix(upperQuery, kw+" ") || strings.HasSuffix(upperQuery, " "+kw) {
			return VerdictMutating, "SQL query contains mutating keyword: " + kw
		}
	}

	return VerdictReadOnly, ""
}
