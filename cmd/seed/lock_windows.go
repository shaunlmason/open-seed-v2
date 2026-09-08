//go:build windows

package main

import (
	"errors"
	"os"
	"time"
)

// lockFile holds the state dir by creating path exclusively (Windows
// has no flock): a rival waits, retrying, until the holder removes it;
// a lock older than the wait bound is taken over as abandoned, which
// spec/platform.md states as the platform's honest limit.
func lockFile(path string) (func(), error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > 2*time.Minute {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the client state dir is held by another seed process (" + path + ")")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
