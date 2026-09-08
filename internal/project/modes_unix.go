//go:build !windows

package project

import (
	"io/fs"
	"os"
)

// setMode locks or unlocks a published path by mode: the projection
// tree is read-only once published (spec/projections.md).
func setMode(path string, mode fs.FileMode) error { return os.Chmod(path, mode) }
