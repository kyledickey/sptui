//go:build unix

package main

import "syscall"

// relaunch runs exe in place of this process, in the same terminal. It
// only returns if that fails.
func relaunch(exe string, args []string, version string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), relaunchEnv(version))
}
