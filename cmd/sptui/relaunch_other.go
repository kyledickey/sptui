//go:build !unix

package main

import (
	"errors"
	"os"
	"os/exec"
)

// relaunch runs exe in this terminal and exits with it, since there's no
// replacing the process here.
func relaunch(exe string, args []string, version string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = relaunchEnv(version)
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		os.Exit(exit.ExitCode())
	}
	if err == nil {
		os.Exit(0)
	}
	return err
}
