// Package symbols extracts code symbol definitions and references from a single
// source buffer using a pure-Go tree-sitter runtime (gotreesitter). It has no
// dependency on the filesystem, no exec, and no UI toolkit, so it is a
// construction-only leaf that callers (e.g. the find_symbol tool) drive.
//
// # Stateless / ephemeral contract
//
// TagSource parses only the bytes it is handed, in memory, and discards the
// parse tree before it returns. The package holds no persistent index, no
// on-disk cache, no file watcher, and starts no background goroutine. There is
// nothing to keep in sync with edits: every call reparses from scratch. This is
// deliberate — it is what lets the find_symbol tool feel like an on-demand
// navigation command rather than a continuously reindexing service.
package symbols
