package executor

// OCICommand is the real OCI runtime: it drives docker or podman on PATH
// with fixed argument vectors — nothing is interpolated into a shell
// (plans/os-083112ac.md D4). A declared docker or podman that is not on
// PATH refuses by name; the credential-free arm declares the `fake`
// runtime (executor/fakeoci) explicitly, so a runtime absent in CI is a
// named reason, never a silent fallback.

import (
	"fmt"
	"os/exec"
	"strings"
)

// OCICommand shells to Bin (docker or podman).
type OCICommand struct{ Bin string }

// Available reports whether the runtime binary is on PATH.
func (r OCICommand) Available() bool {
	_, err := exec.LookPath(r.Bin)
	return err == nil
}

// ResolveImage names the digest of an image already present locally,
// the same id Start reports for a container over it. An image the
// runtime has not pulled resolves to an error, and the static tuple
// leaves the environment empty rather than guessing.
func (r OCICommand) ResolveImage(image string) (string, error) {
	if image == "" {
		return "", fmt.Errorf("%s: an image reference is required", r.Bin)
	}
	out, err := exec.Command(r.Bin, "image", "inspect", "--format", "{{.Id}}", image).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s image inspect: %v: %s", r.Bin, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Start runs a container over the workspace, bind-mounted at a fixed
// path, and returns its id and the image digest.
func (r OCICommand) Start(image, workspace string) (id, digest string, err error) {
	if image == "" || workspace == "" {
		return "", "", fmt.Errorf("%s: an image and a workspace are required", r.Bin)
	}
	run := exec.Command(r.Bin, "run", "-d", "-v", workspace+":/workspace", image, "sleep", "infinity")
	out, err := run.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("%s run: %v: %s", r.Bin, err, strings.TrimSpace(string(out)))
	}
	id = strings.TrimSpace(string(out))
	inspect := exec.Command(r.Bin, "inspect", "--format", "{{.Image}}", id)
	dout, ierr := inspect.CombinedOutput()
	if ierr != nil {
		_ = r.Stop(id)
		_ = r.Remove(id)
		return "", "", fmt.Errorf("%s inspect: %v: %s", r.Bin, ierr, strings.TrimSpace(string(dout)))
	}
	return id, strings.TrimSpace(string(dout)), nil
}

// Stop stops a running container.
func (r OCICommand) Stop(id string) error {
	if out, err := exec.Command(r.Bin, "stop", id).CombinedOutput(); err != nil {
		return fmt.Errorf("%s stop: %v: %s", r.Bin, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Remove removes a container.
func (r OCICommand) Remove(id string) error {
	if out, err := exec.Command(r.Bin, "rm", "-f", id).CombinedOutput(); err != nil {
		return fmt.Errorf("%s rm: %v: %s", r.Bin, err, strings.TrimSpace(string(out)))
	}
	return nil
}
