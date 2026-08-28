// Command claudecontrol is a terminal control centre for Claude Code.
package main

import (
	"flag"
	"fmt"
	"os"

	"claudecontrol/internal/app"
	"claudecontrol/internal/config"
	"claudecontrol/internal/hooks"
	"claudecontrol/internal/relay"

	// Register the built-in module types.
	_ "claudecontrol/modules/claude"
	_ "claudecontrol/modules/hologram"
	_ "claudecontrol/modules/services"
	_ "claudecontrol/modules/stats"
	_ "claudecontrol/modules/supervisor"
	_ "claudecontrol/modules/tabs"
	_ "claudecontrol/modules/tasks"
	_ "claudecontrol/modules/term"
)

func main() {
	relayDir := flag.String("relay", "", "internal: drain a service's terminal into this directory and exit")
	hook := flag.String("hook", "", "internal: forward a Claude Code hook payload and exit")
	pane := flag.String("pane", "", "internal: which pane's session the hook belongs to")
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
		_ = hooks.Send(socket, *hook, *pane, os.Stdin)
		os.Exit(0)
	}

	// Relay mode is the process that stays behind a service kept across a
	// restart. It owns the terminal the service writes to and empties it into
	// a file, which is what keeps the service off a full buffer once the
	// application it was started from has gone.
	if *relayDir != "" {
		if err := relay.Run(*relayDir, flag.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "claudecontrol:", err)
			os.Exit(1)
		}
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
