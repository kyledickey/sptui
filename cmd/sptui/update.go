package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kyledickey/sptui/internal/update"
)

const installCommand = "curl -fsSL https://sptui.sh/install.sh | bash"

// newUpdater returns the updater for this build, or nil for a development
// build, which isn't a release to update from.
func newUpdater(cacheDir string, log *slog.Logger) *update.Updater {
	if !update.IsRelease(version) {
		return nil
	}
	exe, err := update.Executable()
	if err != nil {
		log.Warn("can't find sptui's binary; no updates", "err", err)
		return nil
	}
	return &update.Updater{
		Current:   version,
		Exe:       exe,
		CachePath: filepath.Join(cacheDir, "update.json"),
		Log:       log.With("pkg", "update"),
	}
}

// runUpdate is sptui update: install the latest release, or with -check
// only say whether there is one.
func runUpdate(ctx context.Context, args []string, cacheDir string, log *slog.Logger) error {
	fs := flag.NewFlagSet("sptui update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only say whether there's a newer release")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: sptui update [-check]\n\nInstalls the latest release of sptui in place of this one.\n\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	u := newUpdater(cacheDir, log)
	if u == nil {
		return fmt.Errorf("this is a development build (%s), which doesn't update itself; "+
			"rebuild it, or install a release with: %s", version, installCommand)
	}
	latest, err := u.Available(ctx, true)
	if err != nil {
		return err
	}
	switch {
	case latest == "":
		fmt.Printf("sptui %s is the latest.\n", version)
		return nil
	case *check:
		fmt.Printf("sptui %s is out (this is %s). Run sptui update to install it.\n", latest, version)
		return nil
	}
	fmt.Printf("Updating sptui %s → %s…\n", version, latest)
	if err := u.Install(ctx, latest); err != nil {
		if errors.Is(err, update.ErrNotWritable) {
			return fmt.Errorf("%w\nRun it as root (sudo sptui update), or reinstall with: %s", err, installCommand)
		}
		return err
	}
	fmt.Printf("Updated to %s.\n", latest)
	return nil
}

// sayNewRelease mentions a newer release on the way out, where the user
// can do something about it.
func (a *app) sayNewRelease() {
	switch {
	case a.newRelease == "":
	case a.installed:
		fmt.Printf("sptui %s is installed; it starts next time.\n", a.newRelease)
	default:
		fmt.Printf("sptui %s is out (this is %s). Run sptui update to install it.\n", a.newRelease, version)
	}
}

// relaunchError asks main to replace this process with the newly installed
// binary, once everything has been cleaned up.
type relaunchError struct {
	exe     string
	args    []string
	version string
}

func (*relaunchError) Error() string { return "relaunch" }

// relaunchEnv is the environment for the relaunched sptui: this one's, plus
// the version it was updated to.
func relaunchEnv(version string) []string {
	return append(os.Environ(), updatedEnv+"="+version)
}
