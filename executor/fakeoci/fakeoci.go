// Package fakeoci is an in-process implementation of the small OCI
// contract the container adapter uses — start a container over a
// bind-mounted workspace, report the image digest, stop it, remove it —
// so the container adapter's drills run credential-free with no runtime
// on PATH (plans/os-083112ac.md D4). The run's environment names it
// `fake-oci:<digest>`, so a report never mistakes it for a real image.
package fakeoci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// Runtime is an in-process OCI runtime: it holds the containers it has
// started and answers the four operations the adapter needs.
type Runtime struct {
	mu         sync.Mutex
	next       int
	containers map[string]string // id -> image digest
	// Started, Stopped and Removed record the operations for a drill.
	Started, Stopped, Removed []string
}

// New constructs an empty runtime.
func New() *Runtime {
	return &Runtime{containers: map[string]string{}}
}

// Digest is the deterministic digest the fake reports for an image
// reference, so a drill can assert the resolved environment.
func Digest(image string) string {
	sum := sha256.Sum256([]byte(image))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// Start runs a container over the workspace and returns its id and the
// image's digest.
// ResolveImage names the digest Start will report for image, so the
// container adapter's static tuple can carry the environment ahead of
// the start.
func (r *Runtime) ResolveImage(image string) (string, error) {
	if image == "" {
		return "", fmt.Errorf("fakeoci: an image reference is required")
	}
	return Digest(image), nil
}

func (r *Runtime) Start(image, workspace string) (id, digest string, err error) {
	if image == "" {
		return "", "", fmt.Errorf("fakeoci: an image reference is required")
	}
	if workspace == "" {
		return "", "", fmt.Errorf("fakeoci: a workspace to bind-mount is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	id = fmt.Sprintf("fake-%d", r.next)
	digest = Digest(image)
	r.containers[id] = digest
	r.Started = append(r.Started, id)
	return id, digest, nil
}

// Inspect returns the running container's image digest.
func (r *Runtime) Inspect(id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.containers[id]
	if !ok {
		return "", fmt.Errorf("fakeoci: no container %q", id)
	}
	return d, nil
}

// Stop stops a running container.
func (r *Runtime) Stop(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.containers[id]; !ok {
		return fmt.Errorf("fakeoci: no container %q to stop", id)
	}
	r.Stopped = append(r.Stopped, id)
	return nil
}

// Remove removes a stopped container.
func (r *Runtime) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.containers[id]; !ok {
		return fmt.Errorf("fakeoci: no container %q to remove", id)
	}
	delete(r.containers, id)
	r.Removed = append(r.Removed, id)
	return nil
}
