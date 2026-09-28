package tools

import "strings"

// mutatorAllArgs are commands whose every non-flag path argument is a mutation
// target.
var mutatorAllArgs = map[string]bool{
	"rm": true, "rmdir": true, "truncate": true, "chmod": true,
	"chown": true, "mkdir": true, "dd": true, "ln": true,
	"tee": true, "touch": true,
}

// mutatorDestOnly are commands where only the final non-flag argument (the
// destination) is the mutation target; earlier path args are sources (reads).
var mutatorDestOnly = map[string]bool{
	"cp": true, "mv": true, "install": true,
}

// shellSeparators delimit command segments inside a single command string.
var shellSeparators = map[string]bool{
	";": true, "&&": true, "||": true, "|": true, "&": true,
}

// ShellMutatesOutsideRoot heuristically reports whether command writes to or
// deletes a path outside root, returning the offending target. It scans for
// output redirections and known mutator commands with out-of-root path
// arguments. It is intentionally conservative and cannot defeat obfuscation
// (eval, variable indirection, cd into another dir); those limits are
// documented in SECURITY.md.
func ShellMutatesOutsideRoot(command, root string) (bool, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return false, ""
	}
	tokens := strings.Fields(command)

	// 1. Output redirections (>, >>, 1>, 2>, &>, >|).
	for i, tok := range tokens {
		if target, ok := redirectTarget(tok, tokens, i); ok && resolvesOutsideRoot(target, root) {
			return true, target
		}
	}

	// 2. Mutator commands with out-of-root path arguments, per command segment.
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

		cmd := commandBase(tok)
		segEnd := segmentEnd(tokens, i+1)

		switch {
		case mutatorAllArgs[cmd]:
			for _, arg := range nonFlagArgs(tokens[i+1 : segEnd]) {
				if resolvesOutsideRoot(arg, root) {
					return true, arg
				}
			}
		case mutatorDestOnly[cmd]:
			args := nonFlagArgs(tokens[i+1 : segEnd])
			if len(args) > 0 {
				dest := args[len(args)-1]
				if resolvesOutsideRoot(dest, root) {
					return true, dest
				}
			}
		case cmd == "sed":
			if hasInPlaceFlag(tokens[i+1 : segEnd]) {
				for _, arg := range nonFlagArgs(tokens[i+1 : segEnd]) {
					if resolvesOutsideRoot(arg, root) {
						return true, arg
					}
				}
			}
		}
	}
	return false, ""
}

// redirectTarget extracts the file a redirection writes from a token (and
// possibly the following token), e.g. ">", ">>", "2>", "&>", ">|", or
// ">/abs/path". File-descriptor duplication ("2>&1", ">&2", "2>&-") and the
// standard device sinks write no file, so they are not targets: treating them
// as writes denied ordinary commands like "ls 2>&1" in read-only sessions.
func redirectTarget(tok string, tokens []string, i int) (string, bool) {
	idx := strings.Index(tok, ">")
	if idx < 0 {
		return "", false
	}
	if !isRedirectPrefix(tok[:idx]) {
		return "", false
	}
	rest := tok[idx:]
	rest = strings.TrimPrefix(rest, ">")
	rest = strings.TrimPrefix(rest, ">") // handle ">>"
	rest = strings.TrimPrefix(rest, "|") // handle ">|"
	dup := strings.HasPrefix(rest, "&")  // ">&word": a descriptor, or bash's file form
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "&"))

	target := rest
	if target == "" {
		if i+1 >= len(tokens) || shellSeparators[tokens[i+1]] {
			return "", false
		}
		target = tokens[i+1]
	}
	// Tokens are whitespace-split, so "2>/dev/null;" keeps its separator.
	target = strings.TrimRight(target, ";)")
	if target == "" {
		return "", false
	}
	if dup && isDescriptorWord(target) {
		return "", false
	}
	if harmlessRedirectSinks[target] {
		return "", false
	}
	return target, true
}

// harmlessRedirectSinks are device paths a redirection may name without
// writing any file.
var harmlessRedirectSinks = map[string]bool{
	"/dev/null":   true,
	"/dev/stdout": true,
	"/dev/stderr": true,
}

// isDescriptorWord reports whether s is the word after ">&" that names a file
// descriptor ("1", "2") or closes one ("-"), rather than a file.
func isDescriptorWord(s string) bool {
	if s == "-" {
		return true
	}
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isRedirectPrefix reports whether s is a valid prefix before a '>' redirection
// operator: empty, "&", or a file-descriptor number.
func isRedirectPrefix(s string) bool {
	if s == "" || s == "&" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func commandBase(tok string) string {
	if idx := strings.LastIndex(tok, "/"); idx >= 0 {
		return tok[idx+1:]
	}
	return tok
}

func segmentEnd(tokens []string, start int) int {
	for j := start; j < len(tokens); j++ {
		if shellSeparators[tokens[j]] {
			return j
		}
	}
	return len(tokens)
}

func nonFlagArgs(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if strings.HasPrefix(t, "-") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func hasInPlaceFlag(tokens []string) bool {
	for _, t := range tokens {
		if t == "-i" || strings.HasPrefix(t, "-i") || strings.HasPrefix(t, "--in-place") {
			return true
		}
	}
	return false
}
