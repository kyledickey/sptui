// Command sptui-intros is a workbench for sptui's startup animations: it
// plays them on a loop and lets you pause, step through frames, scrub,
// slow down and flip the theme, to inspect them closely.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/intro"
)

const usage = `sptui-intros plays sptui's startup animations on a loop, to inspect them.

Usage: sptui-intros [flags] [intro]

intro is the one to start on: %s.

Flags:
`

func main() {
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "sptui-intros:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	configDir, err := config.Dir()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("sptui-intros", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join(configDir, "config.toml"), "config file, for the theme")
	name := fs.String("name", defaultName(), `who to greet; "" for no one`)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), usage, joinNames())
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	start := 0
	if fs.NArg() > 0 {
		start = -1
		for i, a := range intro.All {
			if a.Name == fs.Arg(0) {
				start = i
			}
		}
		if start < 0 {
			return fmt.Errorf("no intro called %q; there's %s", fs.Arg(0), joinNames())
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(newLab(cfg, *name, start)).Run()
	return err
}

// defaultName is the name on this computer's account, as sptui would know
// the Spotify one.
func defaultName() string {
	if u, err := user.Current(); err == nil && u.Name != "" {
		return u.Name
	}
	return "Ada Lovelace"
}

func joinNames() string { return strings.Join(intro.Names(), ", ") }
