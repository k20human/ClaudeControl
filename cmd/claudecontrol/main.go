// Command claudecontrol is a terminal control centre for Claude Code.
package main

import (
	"flag"
	"fmt"
	"os"

	"claudecontrol/internal/app"
	"claudecontrol/internal/config"
	"claudecontrol/internal/hooks"

	// Register the built-in module types.
	_ "claudecontrol/modules/claude"
	_ "claudecontrol/modules/hologram"
	_ "claudecontrol/modules/term"
)

func main() {
	hook := flag.String("hook", "", "internal: forward a Claude Code hook payload and exit")
	cfgPath := flag.String("config", config.Path(), "path to the configuration file")
	flag.Parse()

	// Hook mode is how a Claude Code hook reaches a running ClaudeControl. It
	// reads the payload on standard input, forwards it, and exits — never
	// printing anything, because its output would land in Claude Code's own
	// transcript.
	if *hook != "" {
		socket := os.Getenv(hooks.EnvSocket)
		if socket == "" {
			os.Exit(0)
		}
		_ = hooks.Send(socket, *hook, os.Stdin)
		os.Exit(0)
	}

	a, err := app.New(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "claudecontrol:", err)
		os.Exit(1)
	}
	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "claudecontrol:", err)
		os.Exit(1)
	}
}
