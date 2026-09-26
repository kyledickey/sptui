package awake

import (
	"os"
	"os/exec"
	"strconv"
)

// inhibit runs macOS's caffeinate, keeping the display (-d) and system (-i)
// awake until it's stopped or sptui exits (-w).
func inhibit(_, _ string) (func(), error) {
	cmd := exec.Command("caffeinate", "-d", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {
		cmd.Process.Kill()
		cmd.Wait()
	}, nil
}
