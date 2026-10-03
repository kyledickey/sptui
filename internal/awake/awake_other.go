//go:build !linux && !darwin && !windows

package awake

import "errors"

func inhibit(_, _ string) (func(), error) {
	return nil, errors.New("keeping the screen awake isn't supported on this system yet")
}
