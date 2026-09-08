//go:build windows

package project

import "io/fs"

// setMode is a no-op on Windows: the platform's read-only attribute
// would block the tree's own replacement on the next publication, and
// it enforces nothing against a writer with the directory's ACL, so
// the published tree is not write-locked there
// (spec/platform.md names this).
func setMode(string, fs.FileMode) error { return nil }
