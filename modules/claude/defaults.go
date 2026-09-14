package claude

import "sync"

// What a session starts with when nothing else says.
//
// A shell alias is the usual way to add a flag to every `claude` — the one in
// this author's zshrc adds `--effort max`. It is expanded by an interactive
// shell reading a command line, and this application does not go through one:
// it executes the program. The alias therefore never applied to a session
// opened here, which is how the same command came to run at two different
// efforts depending on where it was typed.
//
// So the same thing is said here instead, once, and every session gets it:
// the ones in the configuration file, the ones alt+a opens, the ones the +
// opens, and the ones the sessions panel brings back.

var (
	defaultsMu sync.RWMutex
	// baseBin is the program, and baseArgs what precedes a pane's own
	// arguments. Settled from the configuration before any session starts,
	// and read by whichever goroutine starts one — hence the lock.
	baseBin  string
	baseArgs []string
)

// SetDefaults settles the program every session runs and the arguments it runs
// with. Empty leaves the built-in answers: the program named "claude", and no
// arguments.
func SetDefaults(bin string, args []string) {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	baseBin = bin
	baseArgs = append([]string(nil), args...)
}

// defaults are those answers, copied: a caller that appends to them must not
// be able to change what the next session gets.
func defaults() (string, []string) {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	return baseBin, append([]string(nil), baseArgs...)
}
