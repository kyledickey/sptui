//go:build !unix && !windows

package speaker

import "errors"

var errLocked = errors.New("locked by another sptui")

// lock does nothing here: every sptui runs its own speaker.
func lock(string) (func(), error) { return func() {}, nil }

func lockWait(string) (func(), error) { return func() {}, nil }
