package protections

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// conformance: III.L row 5 — least-privilege CI identities. The tree's
// own scheduled workflows are held to the scheduled-writer lint
// (plans/os-a00d3f34.md D4): the only scheduled workflow allowed a
// write is v1's maintenance lane, whose contents: write is the state
// ref's anchor and the operator identity's by design (.seed/config.toml
// roster); every other scheduled workflow, the scale benchmark first,
// is read-only, and a copy of it given contents: write is a finding.
func TestTreeWorkflowsHaveNoScheduledWriters(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	findings, err := LintWorkflows(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if filepath.Base(f.File) != "seed-maintenance.yml" {
			t.Errorf("a scheduled writer outside the v1 maintenance lane: %s: %s", f.File, f.Detail)
		}
	}
	scale, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "perf-scale.yml"))
	if err != nil {
		t.Fatalf("the scale workflow is committed: %v", err)
	}
	if !strings.Contains(string(scale), "schedule:") || !strings.Contains(string(scale), "contents: read") || strings.Contains(string(scale), "contents: write") {
		t.Fatal("perf-scale.yml is scheduled and read-only")
	}
	// Mutation: the same workflow given write permission is a finding.
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(scale), "contents: read", "contents: write", 1)
	if err := os.WriteFile(filepath.Join(wf, "perf-scale.yml"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LintWorkflows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Detail, "scheduled workflow") {
		t.Fatalf("the writable copy must be one finding, got %v", got)
	}
}

// conformance: III.P row 1 (clone-and-init adoption from tagged
// releases; everything executable hash-pinned) and build plan section
// 5's distribution precondition (plans/os-2e46aa2f.md D5) — the Seed
// release workflow is dispatch-only, holds exactly the three
// permissions a release needs, mints its tag in the seed/v namespace
// at HEAD, publishes sha256 checksums and attests build provenance;
// the scheduled-writer lint reports nothing for it, and the same file
// given a schedule is a finding. The shape the review on #329 fixed is
// held too: the job runs in the seed-release environment from the
// default branch alone, the version is semver by semver.org's grammar,
// the tag is pushed only after the checksums exist and a same-commit
// re-run resumes, and the release is a draft until the attestation
// exists.
func TestSeedReleaseWorkflowIsDispatchOnly(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "seed-release.yml"))
	if err != nil {
		t.Fatalf("the release workflow is committed: %v", err)
	}
	wf := string(b)
	// The trigger block is workflow_dispatch and nothing else.
	on := wf[strings.Index(wf, "\non:"):strings.Index(wf, "\npermissions:")]
	for _, forbidden := range []string{"schedule:", "push:", "pull_request", "tags:", "release:"} {
		if strings.Contains(on, forbidden) {
			t.Errorf("a release is the operator's act: the trigger block carries %q", forbidden)
		}
	}
	if !strings.Contains(on, "workflow_dispatch:") || !strings.Contains(on, "version:") {
		t.Fatal("the workflow is dispatched with a version input")
	}
	perms := wf[strings.Index(wf, "\npermissions:"):strings.Index(wf, "\nconcurrency:")]
	for _, want := range []string{"contents: write", "id-token: write", "attestations: write"} {
		if !strings.Contains(perms, want) {
			t.Errorf("the release needs %s", want)
		}
	}
	if n := strings.Count(perms, ": write") + strings.Count(perms, ": read"); n != 3 {
		t.Errorf("exactly three permissions, got %d in %q", n, perms)
	}
	for _, want := range []string{
		`tag="seed/v$VERSION"`,
		`git tag "$TAG"`,
		`-X github.com/shaunlmason/open-seed-v2/internal/version.Version=$VERSION`,
		"sha256sum",
		"gh release create",
		"actions/attest-build-provenance@",
		"subject-checksums: dist/checksums.txt",
		// The privileges stay on the default branch: the environment
		// carries the deployment branch policy the operator sets, and
		// the ref guard restates it in the file.
		"environment: seed-release",
		"if: github.ref == format('refs/heads/{0}', github.event.repository.default_branch)",
		// A tag at another commit refuses; the same commit resumes.
		`[ "$at" != "$GITHUB_SHA" ]`,
		"a release is cut once",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the workflow lacks %q", want)
		}
	}
	// The tag follows the artifacts and the publish follows the
	// attestation: a failed build leaves no tag, a failed attestation
	// leaves a draft. The same reading refuses a tag pushed before the
	// checksums and a release created public.
	if v := releaseShape(wf); len(v) > 0 {
		t.Errorf("the release shape: %s", strings.Join(v, "; "))
	}
	early := strings.Replace(wf, "run: sha256sum", "run: git push origin \"refs/tags/$TAG\" && sha256sum", 1)
	if early == wf || len(releaseShape(early)) == 0 {
		t.Error("a tag pushed before the checksums must be refused")
	}
	public := strings.Replace(wf, "--draft --title", "--title", 1)
	if public == wf || len(releaseShape(public)) == 0 {
		t.Error("a release created public must be refused")
	}
	// The version is validated against semver.org's grammar, not a
	// shell glob: the expression the workflow holds is read out and
	// driven with the values the glob let through.
	m := regexp.MustCompile(`semver='([^']*)'`).FindStringSubmatch(wf)
	if m == nil {
		t.Fatal("the workflow holds its semver expression as semver='...'")
	}
	semver, err := regexp.Compile(m[1])
	if err != nil {
		t.Fatalf("the semver expression compiles: %v", err)
	}
	for _, ok := range []string{"0.1.0", "1.2.3", "1.0.0-rc.1", "1.0.0-alpha+001", "1.2.3+build.5", "1.0.0-0.3.7"} {
		if !semver.MatchString(ok) {
			t.Errorf("semver %q must be accepted", ok)
		}
	}
	for _, bad := range []string{"1.2.3foo", "1.2.3.4", "01.2.3", "1.2.3/foo", "v1.2.3", "1.2.3-01", "1.2", "1.2.3-", "1.2.3+", ""} {
		if semver.MatchString(bad) {
			t.Errorf("%q is not semver and must be refused", bad)
		}
	}
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"} {
		if !strings.Contains(wf, target) {
			t.Errorf("the matrix lacks %s", target)
		}
	}
	// Every third-party action is pinned to a commit SHA (D4): the job
	// holds OIDC, attestation and write privileges, so nothing in it
	// resolves through a mutable tag. A local composite action is a
	// path in this tree and is pinned by the commit that runs it.
	uses := 0
	for _, line := range strings.Split(wf, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if !strings.HasPrefix(line, "uses:") {
			continue
		}
		ref := strings.TrimSpace(strings.TrimPrefix(line, "uses:"))
		if i := strings.Index(ref, "#"); i >= 0 {
			ref = strings.TrimSpace(ref[:i])
		}
		if strings.HasPrefix(ref, "./") {
			continue
		}
		uses++
		at := strings.LastIndex(ref, "@")
		if at < 0 || !isHexSHA(ref[at+1:]) {
			t.Errorf("action %q is not pinned to a commit SHA", ref)
		}
	}
	if uses < 2 {
		t.Fatalf("the workflow uses checkout and the attestation action, found %d third-party actions", uses)
	}
	// The namespace it tags is one the reconciled desired state
	// protects (D8): a released tag cannot be retargeted on the forge.
	desired, err := Desired(&posture.Config{Posture: posture.EnforcedForgeHosted, Admission: &posture.Admission{Endpoint: "https://admit.example", Identity: "app:1", LedgerRef: "refs/heads/seed-ledger"}}, "main")
	if err != nil {
		t.Fatal(err)
	}
	protected := false
	for _, ref := range desired.Rulesets[RulesetTags].Refs {
		if ref == "refs/tags/seed/v*" {
			protected = true
		}
	}
	if !protected {
		t.Fatalf("the seed/v namespace is not among the immutable release tags: %v", desired.Rulesets[RulesetTags].Refs)
	}
	findings, err := LintWorkflows(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if filepath.Base(f.File) == "seed-release.yml" {
			t.Errorf("a dispatch-only writer is not a scheduled writer: %s", f.Detail)
		}
	}
	// Mutation: the same workflow on a schedule is a finding.
	dir := t.TempDir()
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mutation: a tag reference where a SHA belongs is caught by the
	// same reading.
	unpinned := strings.Replace(wf, "actions/attest-build-provenance@e8998f949152b193b063cb0ec769d69d929409be", "actions/attest-build-provenance@v2", 1)
	if unpinned == wf {
		t.Fatal("the pin mutation did not apply")
	}
	if !hasUnpinnedAction(unpinned) {
		t.Fatal("a tag reference must read as unpinned")
	}
	mutated := strings.Replace(wf, "on:\n  workflow_dispatch:", "on:\n  schedule:\n    - cron: \"0 0 * * *\"\n  workflow_dispatch:", 1)
	if mutated == wf {
		t.Fatal("the mutation did not apply")
	}
	if err := os.WriteFile(filepath.Join(wfDir, "seed-release.yml"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err = LintWorkflows(dir)
	if err != nil || len(findings) != 1 {
		t.Fatalf("a scheduled release writer is one finding: %v %v", findings, err)
	}
}

// releaseShape reads the order the release workflow's steps must hold:
// the tag is pushed only after the checksums exist, the release is
// created after the tag and as a draft, and the attestation runs before
// the draft is published. Each violation is one line.
func releaseShape(wf string) []string {
	push := strings.Index(wf, `git push origin "refs/tags/$TAG"`)
	sums := strings.Index(wf, "sha256sum")
	create := strings.Index(wf, "gh release create")
	attest := strings.Index(wf, "actions/attest-build-provenance@")
	publish := strings.Index(wf, "--draft=false")
	if push < 0 || sums < 0 || create < 0 || attest < 0 || publish < 0 {
		return []string{"the push, checksums, draft create, attestation and publish steps are all present"}
	}
	var v []string
	if push < sums {
		v = append(v, "the tag is pushed before the checksums exist")
	}
	if create < push {
		v = append(v, "the release is created before the tag is pushed")
	}
	if attest < create {
		v = append(v, "the attestation runs before the release exists")
	} else if !strings.Contains(wf[create:attest], "--draft ") {
		v = append(v, "the release is created public rather than as a draft")
	}
	if publish < attest {
		v = append(v, "the draft is published before the attestation")
	}
	return v
}

func isHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// hasUnpinnedAction reports whether any third-party action in a
// workflow resolves through something other than a commit SHA.
func hasUnpinnedAction(wf string) bool {
	for _, line := range strings.Split(wf, "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "- ")
		if !strings.HasPrefix(line, "uses:") {
			continue
		}
		ref := strings.TrimSpace(strings.TrimPrefix(line, "uses:"))
		if i := strings.Index(ref, "#"); i >= 0 {
			ref = strings.TrimSpace(ref[:i])
		}
		if strings.HasPrefix(ref, "./") {
			continue
		}
		at := strings.LastIndex(ref, "@")
		if at < 0 || !isHexSHA(ref[at+1:]) {
			return true
		}
	}
	return false
}
