// Command claudecontrol is a terminal control centre for Claude Code.
package main

import (
	"flag"
	"fmt"
	"os"

	"claudecontrol/internal/app"
	"claudecontrol/internal/config"

	// Register the built-in module types.
	_ "claudecontrol/modules/term"
)

func main() {
	cfgPath := flag.String("config", config.Path(), "path to the configuration file")
	flag.Parse()

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
