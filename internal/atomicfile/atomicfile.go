// Package atomicfile writes files so that a crash never leaves one
// half-written: the data goes to a temporary file that then replaces the
// real one.
package atomicfile

import "os"

// Write writes data to path with the given permissions, replacing what was
// there. The directory must already exist.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
