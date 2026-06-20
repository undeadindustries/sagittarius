package tools

import (
	"strings"
)

// IsDangerousCommand reports whether a shell command should be blocked or
// require strict confirmation. Ported subset of fork commandSafety.ts.
func IsDangerousCommand(command string) bool {
	args := splitShellCommand(command)
	if len(args) == 0 {
		return false
	}
	return isDangerousArgs(args)
}

func isDangerousArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	cmd := args[0]

	switch cmd {
	case "rm":
		if len(args) > 1 {
			switch args[1] {
			case "-f", "-rf", "-fr":
				return true
			}
		}
	case "sudo":
		return isDangerousArgs(args[1:])
	case "find":
		unsafe := map[string]bool{
			"-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
			"-delete": true, "-fls": true, "-fprint": true, "-fprint0": true, "-fprintf": true,
		}
		for _, arg := range args {
			if unsafe[arg] {
				return true
			}
		}
	case "curl", "wget":
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "--output") {
				return true
			}
		}
	}

	if isRipgrepCommand(cmd) {
		unsafeWithArgs := map[string]bool{"--pre": true, "--hostname-bin": true}
		unsafeWithoutArgs := map[string]bool{"--search-zip": true, "-z": true}
		for _, arg := range args {
			if unsafeWithoutArgs[arg] {
				return true
			}
			for opt := range unsafeWithArgs {
				if arg == opt || strings.HasPrefix(arg, opt+"=") {
					return true
				}
			}
		}
	}

	return false
}

func isRipgrepCommand(cmd string) bool {
	base := cmd
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		base = cmd[idx+1:]
	}
	return base == "rg" || base == "rg.exe"
}

// splitShellCommand performs a minimal whitespace split for safety checks.
// Full shell parsing is intentionally not implemented; dangerous patterns
// are blocked conservatively.
func splitShellCommand(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	return strings.Fields(command)
}
