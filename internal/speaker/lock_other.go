//go:build !unix

package speaker

import "errors"

var errLocked = errors.New("locked by another sptui")

// lock does nothing here: every sptui shares the first slot.
func lock(string) (func(), error) { return func() {}, nil }
