package main

// III.A row 7 through the terminal (plans/os-db5cd353.md AC1, AC2,
// AC3): erasing a referenced artifact never breaks chain verification,
// and the erasure is itself an attributable event, read back from the
// chain by the audit and by a fresh reader.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: III.A row 7 — `artifact erase` records the erasure
// under the operator's key and then removes the bytes; the chain
// verifies afterward; `seal audit` names the erased subject with the
// record's position, signer and reason and stays clean, while a
// ciphertext deleted with no record stays seal_evidence_missing; a
// render on the erased subject refuses naming the erasure; a re-run
// finishes rather than re-records; a content artifact is erased on
// system; the grant, the reference and the shape refuse by name.
func TestArtifactEraseIsAttributableAndNeverBreaksTheChain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the sealed fixture's checks run a POSIX shell (spec/platform.md); the erasure rule's drills run everywhere in internal/admit")
	}
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head := verdictRepo(t)
	v1Key, v1Pub, v1FP := writeWorkerKey(t, 9)
	sealKey, sealPub, sealFP := writeWorkerKey(t, 10)
	implKey, implPub, implFP := writeWorkerKey(t, 11)
	for _, step := range [][]string{
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed1 + `"}`},
		{"actor.enrolled", v1FP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, v1Pub)},
		{"actor.enrolled", sealFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "sealer"}`, sealPub)},
		{"actor.enrolled", implFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "implementer"}`, implPub)},
		{"actor.granted", v1FP, `{"capability": "verdict"}`},
		{"actor.granted", sealFP, `{"capability": "sealer"}`},
		{"actor.granted", implFP, `{"capability": "claim"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)
	rng := base + ".." + head
	checks := writeChecks(t, "# sealed", "true")
	c1 := driveToReview(t, ld, src, sealKey, rootKey, "c-1", specCommit, rng, checks)
	c2 := driveToReview(t, ld, src, sealKey, rootKey, "c-2", specCommit, rng, checks)
	sealedPath := func(c string) string { return filepath.Join(src, "var", "artifacts", "sealed", c+".age") }
	for _, c := range []string{c1, c2} {
		if _, err := os.Stat(sealedPath(c)); err != nil {
			t.Fatalf("the fixture sealed %s: %v", c, err)
		}
	}
	rootFP, _ := signerAt(t, ld, "0")

	// The refusals, each by name, before anything is erased.
	erase := func(key, subject, digest, reason string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, "artifact", "erase", "--ledger", ld, "--key", key, "--subject", subject,
			"--artifact", digest, "--reason", reason, "--repo", src)
	}
	if e, code := erase(implKey, "c-1", c1, "x"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim-granted key may not erase: %d %+v", code, e)
	}
	if e, code := erase(priv, "c-1", c2, "x"); code != 3 || e.Error == nil || e.Error.Code != "erasure_refused" || !strings.Contains(e.Error.Message, "sealed commitment "+c1) {
		t.Fatalf("a digest the contract does not reference refuses naming what it references: %d %+v", code, e)
	}
	if e, code := erase(priv, "c-1", "sha256:"+c1, "x"); code != 3 || e.Error == nil || e.Error.Code != "erasure_refused" {
		t.Fatalf("the digest form is held before the session opens: %d %+v", code, e)
	}
	if _, code := runEnv(t, "artifact", "erase", "--ledger", ld, "--key", priv, "--subject", "c-1", "--artifact", c1, "--repo", src); code != 64 {
		t.Fatalf("an erasure names its reason: %d", code)
	}
	for _, c := range []string{c1, c2} {
		if _, err := os.Stat(sealedPath(c)); err != nil {
			t.Fatalf("a refusal erases nothing: %s %v", c, err)
		}
	}

	// The operator erases c-1's ciphertext: the record lands first,
	// then the bytes go.
	e, code := erase(priv, "c-1", c1, "a retention obligation")
	if code != 0 || !e.OK || e.Result["recorded"] != true || e.Position == nil {
		t.Fatalf("the erasure records: %d %+v", code, e)
	}
	erasedAt := fmt.Sprint(e.Result["erased_at"])
	if removed := fmt.Sprint(e.Result["removed"]); removed != "[sealed]" {
		t.Fatalf("the sealed bucket is emptied and named: %s", removed)
	}
	if _, err := os.Stat(sealedPath(c1)); !os.IsNotExist(err) {
		t.Fatalf("the ciphertext is gone: %v", err)
	}
	// Never breaks verification, and the record is attributable to
	// the operator from the chain alone.
	if v, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 || !v.OK {
		t.Fatalf("the chain verifies after the erasure: %d %+v", code, v)
	}
	if actor, verb := signerAt(t, ld, erasedAt); verb != "artifact.erased" || actor != rootFP {
		t.Fatalf("the erasure at %s is the operator's signed record, got %s %s", erasedAt, verb, actor)
	}
	// The audit names the honored erasure and stays clean.
	a, code := runEnv(t, "seal", "audit", "--ledger", ld, "--repo", src)
	if code != 0 || a.Result["clean"] != "true" {
		t.Fatalf("an honored erasure leaves the audit clean: %d %+v", code, a)
	}
	erased, _ := a.Result["erased"].([]any)
	if len(erased) != 1 {
		t.Fatalf("the audit lists the erased subject: %+v", a.Result)
	}
	row := erased[0].(map[string]any)
	if row["subject"] != "c-1" || row["commitment"] != c1 || fmt.Sprint(row["position"]) != erasedAt || row["by"] != rootFP || row["reason"] != "a retention obligation" {
		t.Fatalf("the erasure is attributed by position, signer and reason: %+v", row)
	}
	if by, _ := a.Result["by_class"].(map[string]any); by["seal_evidence_missing"] != nil {
		t.Fatalf("an attributed erasure is not missing evidence: %+v", by)
	}
	// A render on the erased subject refuses, naming the erasure.
	if r, code := runEnv(t, "verdict", "render", "--ledger", ld, "--subject", "c-1", "--repo", src, "--key", v1Key, "--verdict", "pass"); code != 22 || r.Error == nil || !strings.Contains(r.Error.Message, "was erased at position "+erasedAt+" by "+rootFP) {
		t.Fatalf("a render meets the attribution, not an absence: %d %+v", code, r)
	}
	// A re-run finishes rather than re-records.
	e, code = erase(priv, "c-1", c1, "again")
	if code != 0 || !e.OK || e.Result["recorded"] != false || fmt.Sprint(e.Result["erased_at"]) != erasedAt || fmt.Sprint(e.Result["removed"]) != "[]" {
		t.Fatalf("an erasure that stands is finished, never recorded twice: %d %+v", code, e)
	}
	if n, _ := runEnv(t, "ledger", "show", "--ledger", ld); fmt.Sprint(n.Result["count"]) != fmt.Sprint(chainCount(t, ld)) {
		t.Fatal("the chain did not grow on the re-run")
	}

	// A run that died between the append and the removal (the record
	// stands, the bytes remain): the next run finishes the removal
	// without a second record. Planted by landing the record raw on
	// c-2 and leaving its ciphertext in place.
	rawPos := rawAppend(t, ld, rootKey, "artifact.erased", "c-2", fmt.Sprintf(`{"artifact": %q, "reason": "the run died after the append"}`, c2))
	if _, err := os.Stat(sealedPath(c2)); err != nil {
		t.Fatalf("the bytes remain after the raw record: %v", err)
	}
	e, code = erase(priv, "c-2", c2, "finish")
	if code != 0 || !e.OK || e.Result["recorded"] != false || fmt.Sprint(e.Result["erased_at"]) != fmt.Sprint(rawPos) || fmt.Sprint(e.Result["removed"]) != "[sealed]" {
		t.Fatalf("a standing record's bytes are removed on the next run without a second record: %d %+v", code, e)
	}
	if _, err := os.Stat(sealedPath(c2)); !os.IsNotExist(err) {
		t.Fatal("the resume removed the ciphertext")
	}
	a, code = runEnv(t, "seal", "audit", "--ledger", ld, "--repo", src)
	if code != 0 || a.Result["clean"] != "true" {
		t.Fatalf("both erasures are honored: %d %+v", code, a)
	}
	if erased, _ := a.Result["erased"].([]any); len(erased) != 2 {
		t.Fatalf("both erased subjects are listed: %+v", a.Result)
	}

	// A digest two contracts reference is one object in the store, so
	// one erasure stands for both: c-3 sealed raw under c-1's
	// commitment reads as erased, attributed to the record on c-1, and
	// not as missing evidence.
	for _, step := range [][]string{
		{"intent.filed", `{"intent": "shares the seal", "tier": "trivial", "budget": "small", "routing": "core"}`},
		{"contract.specified", fmt.Sprintf(`{"acceptance": {"ref": "accept.md @ %s", "executable": false}}`, specCommit)},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv, "--verb", step[0], "--subject", "c-3", "--payload", step[1]); code != 0 {
			t.Fatalf("c-3 %s: %d %+v", step[0], code, e)
		}
	}
	rawAppend(t, ld, workerRawKey(10), "check.sealed", "c-3", fmt.Sprintf(`{"commitment": %q}`, c1))
	a, code = runEnv(t, "seal", "audit", "--ledger", ld, "--repo", src)
	if code != 0 || a.Result["clean"] != "true" {
		t.Fatalf("a shared digest's erasure is attributed to every contract that references it: %d %+v", code, a)
	}
	sharedAttributed := false
	for _, row := range a.Result["erased"].([]any) {
		r := row.(map[string]any)
		if r["subject"] == "c-3" && r["commitment"] == c1 && fmt.Sprint(r["position"]) == erasedAt {
			sharedAttributed = true
		}
	}
	if !sharedAttributed {
		t.Fatalf("c-3 reads as erased by the record on c-1: %+v", a.Result["erased"])
	}
	if e, code := erase(priv, "c-3", c1, "again"); code != 0 || !e.OK || e.Result["recorded"] != false || e.Result["subject"] != "c-1" {
		t.Fatalf("the tombstone is digest-wide, so c-3's erasure is finished under c-1's record: %d %+v", code, e)
	}

	// A ciphertext deleted with no record: the unattributed absence
	// stays a finding.
	c4 := driveToReview(t, ld, src, sealKey, rootKey, "c-4", specCommit, rng, checks)
	if err := os.Remove(sealedPath(c4)); err != nil {
		t.Fatal(err)
	}
	a, code = runEnv(t, "seal", "audit", "--ledger", ld, "--repo", src)
	if code != 0 || a.Result["clean"] != "false" {
		t.Fatalf("a deletion with no record is not clean: %d %+v", code, a)
	}
	if by, _ := a.Result["by_class"].(map[string]any); fmt.Sprint(by["seal_evidence_missing"]) != "1" {
		t.Fatalf("the unrecorded deletion is seal_evidence_missing: %+v", by)
	}
	if erased, _ := a.Result["erased"].([]any); len(erased) != 3 {
		t.Fatalf("the attributed erasures still stand apart: %+v", a.Result)
	}

	// A content artifact, referenced from a payload no fold indexes,
	// is erased on system under the operator's attestation.
	store := artifact.Open(filepath.Join(src, "var", "artifacts"))
	digest, err := store.Put([]byte("a packet body"))
	if err != nil {
		t.Fatal(err)
	}
	e, code = erase(priv, "system", digest, "the subject asked")
	if code != 0 || !e.OK || fmt.Sprint(e.Result["removed"]) != "[content]" {
		t.Fatalf("a content artifact is erased on system: %d %+v", code, e)
	}
	if _, err := store.Get(digest); err == nil {
		t.Fatal("the content is gone")
	}
	if v, code := runEnv(t, "ledger", "verify", "--ledger", ld); code != 0 || !v.OK {
		t.Fatalf("the chain verifies after both erasures: %d %+v", code, v)
	}
}

// signerAt reads a record's signer and verb back through ledger show:
// what a fresh reader attributes from the chain alone.
func signerAt(t *testing.T, ld, position string) (actor, verb string) {
	t.Helper()
	e, code := runEnv(t, "ledger", "show", "--ledger", ld, "--position", position)
	if code != 0 || !e.OK {
		t.Fatalf("show %s: %d %+v", position, code, e)
	}
	ev, _ := e.Result["event"].(map[string]any)
	return fmt.Sprint(ev["actor"]), fmt.Sprint(ev["verb"])
}

// erasureFixture is the sealed ground the review-round drills stand
// on: a ledger with an operator root, a sealer, a claim-granted
// implementer and a verifier, and a repository whose store holds each
// contract the fixture drives to review under seal.
type erasureFixture struct {
	ld, src, priv, implKey, v1Key string
	rootKey                       ed25519.PrivateKey
	rootFP                        string
	seal                          func(name string) string
}

func newErasureFixture(t *testing.T) *erasureFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the sealed fixture's checks run a POSIX shell (spec/platform.md); the erasure rule's drills run everywhere in internal/admit")
	}
	dir, priv, _ := writeKeys(t)
	ld := filepath.Join(dir, "ledger")
	if _, code := runEnv(t, "init", "--ledger", ld, "--key", priv); code != 0 {
		t.Fatal("init failed")
	}
	src, base, specCommit, head := verdictRepo(t)
	v1Key, v1Pub, v1FP := writeWorkerKey(t, 9)
	sealKey, sealPub, sealFP := writeWorkerKey(t, 10)
	implKey, implPub, implFP := writeWorkerKey(t, 11)
	for _, step := range [][]string{
		{"system.protocol.upgraded", "system", `{"to": "` + version.Seed1 + `"}`},
		{"actor.enrolled", v1FP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, v1Pub)},
		{"actor.enrolled", sealFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "sealer"}`, sealPub)},
		{"actor.enrolled", implFP, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "implementer"}`, implPub)},
		{"actor.granted", v1FP, `{"capability": "verdict"}`},
		{"actor.granted", sealFP, `{"capability": "sealer"}`},
		{"actor.granted", implFP, `{"capability": "claim"}`},
	} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", step[0], "--subject", step[1], "--payload", step[2]); code != 0 {
			t.Fatalf("%s: %d %+v", step[0], code, e)
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	rootKey := ed25519.NewKeyFromSeed(seed)
	rng := base + ".." + head
	checks := writeChecks(t, "# sealed", "true")
	rootFP, _ := signerAt(t, ld, "0")
	x := &erasureFixture{ld: ld, src: src, priv: priv, implKey: implKey, v1Key: v1Key, rootKey: rootKey, rootFP: rootFP}
	x.seal = func(name string) string {
		t.Helper()
		c := driveToReview(t, ld, src, sealKey, rootKey, name, specCommit, rng, checks)
		if _, err := os.Stat(x.sealedPath(c)); err != nil {
			t.Fatalf("the fixture sealed %s: %v", name, err)
		}
		return c
	}
	return x
}

func (x *erasureFixture) sealedPath(c string) string {
	return filepath.Join(x.src, "var", "artifacts", "sealed", c+".age")
}

func (x *erasureFixture) erase(t *testing.T, key, subject, digest, reason string) (ledgerEnv, int) {
	t.Helper()
	return runEnv(t, "artifact", "erase", "--ledger", x.ld, "--key", key, "--subject", subject,
		"--artifact", digest, "--reason", reason, "--repo", x.src)
}

func (x *erasureFixture) audit(t *testing.T) ledgerEnv {
	t.Helper()
	a, code := runEnv(t, "seal", "audit", "--ledger", x.ld, "--repo", x.src)
	if code != 0 {
		t.Fatalf("audit: %d %+v", code, a)
	}
	return a
}

func (x *erasureFixture) render(t *testing.T, subject string) ledgerEnv {
	t.Helper()
	r, code := runEnv(t, "verdict", "render", "--ledger", x.ld, "--subject", subject, "--repo", x.src, "--key", x.v1Key, "--verdict", "pass")
	if code != 22 || r.Error == nil || r.Error.Code != "seal_broken" {
		t.Fatalf("a render on an absent ciphertext refuses seal_broken: %d %+v", code, r)
	}
	return r
}

func erasedRaw(digest, reason string) string {
	return fmt.Sprintf(`{"artifact": %q, "reason": %q}`, digest, reason)
}

// conformance: III.A row 7; SEED-NEXT.md Part II "Capabilities": the
// resume path holds the grant the record took: with an operator's
// erasure standing and the bytes still present (a run that died between
// the append and the removal), a claim-granted key refuses out_of_grant
// before any removal, a key the chain has never seen refuses too,
// nothing is appended and nothing removed, and the operator's next run
// finishes under the standing record (review finding on the task PR).
func TestArtifactEraseResumeHoldsTheOperatorGrant(t *testing.T) {
	x := newErasureFixture(t)
	c1 := x.seal("c-1")
	rawPos := rawAppend(t, x.ld, x.rootKey, "artifact.erased", "c-1", erasedRaw(c1, "the run died after the append"))
	stranger, _, _ := writeWorkerKey(t, 12)
	if e, code := x.erase(t, x.implKey, "c-1", c1, "finish"); code != 14 || e.Error == nil || e.Error.Code != "out_of_grant" {
		t.Fatalf("a claim-granted key may not finish an operator's erasure: %d %+v", code, e)
	}
	if e, code := x.erase(t, stranger, "c-1", c1, "finish"); code == 0 || e.OK {
		t.Fatalf("a key the chain has never seen may not finish it either: %d %+v", code, e)
	}
	if _, err := os.Stat(x.sealedPath(c1)); err != nil {
		t.Fatalf("a refused resume removes nothing: %v", err)
	}
	if n := chainCount(t, x.ld); n != rawPos+1 {
		t.Fatalf("a refused resume appends nothing: %d records, the record stands at %d", n, rawPos)
	}
	e, code := x.erase(t, x.priv, "c-1", c1, "finish")
	if code != 0 || !e.OK || e.Result["recorded"] != false || fmt.Sprint(e.Result["erased_at"]) != fmt.Sprint(rawPos) || e.Result["by"] != x.rootFP || fmt.Sprint(e.Result["removed"]) != "[sealed]" {
		t.Fatalf("the operator finishes the removal under the standing record: %d %+v", code, e)
	}
	if _, err := os.Stat(x.sealedPath(c1)); !os.IsNotExist(err) {
		t.Fatalf("the ciphertext is gone: %v", err)
	}
}

// conformance: III.A row 7 (the erasure is itself an attributable
// event), fold presence is never proof of admission: a well-shaped
// artifact.erased the raw seam landed under a claim-granted key is kept
// by the fold and honored by nothing. With the ciphertext gone, the
// audit stays seal_evidence_missing and lists nothing erased, the render
// meets an absence rather than that record, the operator's own record
// admits over it, and that record is the one the audit, the render and
// the re-run attribute (review finding on the task PR).
func TestSealAuditHonorsOnlyAnAuthorizedErasure(t *testing.T) {
	x := newErasureFixture(t)
	c1 := x.seal("c-1")
	rawPos := rawAppend(t, x.ld, workerRawKey(11), "artifact.erased", "c-1", erasedRaw(c1, "not mine to honor"))
	if err := os.Remove(x.sealedPath(c1)); err != nil {
		t.Fatal(err)
	}
	a := x.audit(t)
	if a.Result["clean"] != "false" {
		t.Fatalf("an unauthorized tombstone does not clean the audit: %+v", a.Result)
	}
	if by, _ := a.Result["by_class"].(map[string]any); fmt.Sprint(by["seal_evidence_missing"]) != "1" {
		t.Fatalf("the absence stays unattributed: %+v", by)
	}
	if erased, _ := a.Result["erased"].([]any); len(erased) != 0 {
		t.Fatalf("nothing is listed as erased: %+v", erased)
	}
	if r := x.render(t, "c-1"); strings.Contains(r.Error.Message, "was erased at position") || !strings.Contains(r.Error.Message, "is not retrievable") {
		t.Fatalf("the render meets an absence, not the raw record: %s", r.Error.Message)
	}
	// The once rule counts only what passed the boundary: the operator's
	// record admits, and is the attribution from then on.
	e, code := x.erase(t, x.priv, "c-1", c1, "a retention obligation")
	if code != 0 || !e.OK || e.Result["recorded"] != true || fmt.Sprint(e.Result["erased_at"]) == fmt.Sprint(rawPos) || fmt.Sprint(e.Result["removed"]) != "[]" {
		t.Fatalf("the operator's erasure records over the unauthorized tombstone: %d %+v", code, e)
	}
	erasedAt := fmt.Sprint(e.Result["erased_at"])
	if actor, verb := signerAt(t, x.ld, erasedAt); verb != "artifact.erased" || actor != x.rootFP {
		t.Fatalf("the record at %s is the operator's: %s %s", erasedAt, verb, actor)
	}
	a = x.audit(t)
	erased, _ := a.Result["erased"].([]any)
	if a.Result["clean"] != "true" || len(erased) != 1 {
		t.Fatalf("the operator's record is honored: %+v", a.Result)
	}
	if row := erased[0].(map[string]any); fmt.Sprint(row["position"]) != erasedAt || row["by"] != x.rootFP {
		t.Fatalf("the audit attributes the operator's record, never the raw one: %+v", row)
	}
	if r := x.render(t, "c-1"); !strings.Contains(r.Error.Message, "was erased at position "+erasedAt+" by "+x.rootFP) {
		t.Fatalf("the render names the operator's record: %s", r.Error.Message)
	}
	if e, code := x.erase(t, x.priv, "c-1", c1, "again"); code != 0 || !e.OK || e.Result["recorded"] != false || fmt.Sprint(e.Result["erased_at"]) != erasedAt || e.Result["by"] != x.rootFP {
		t.Fatalf("a re-run finishes under the operator's record: %d %+v", code, e)
	}
	if v, code := runEnv(t, "ledger", "verify", "--ledger", x.ld); code != 0 || !v.OK {
		t.Fatalf("the chain verifies: %d %+v", code, v)
	}
}

// conformance: III.A row 7 (a record with the bytes present is a promise
// the next run keeps), a removal the store refuses after the record
// landed is a refusal: erasure_incomplete (exit 5) naming the position
// the record stands at, on the first run and again on the resume, no
// second record, and the next run once the store can remove finishes
// under the standing record (review finding on the task PR).
func TestArtifactEraseRefusesWhenTheRemovalFails(t *testing.T) {
	x := newErasureFixture(t)
	c1 := x.seal("c-1")
	p := x.sealedPath(c1)
	ct, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// A store that cannot remove the ciphertext: a non-empty directory
	// in its place, which os.Remove refuses for every user.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p, "held"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "held", "bytes"), ct, 0o644); err != nil {
		t.Fatal(err)
	}
	before := chainCount(t, x.ld)
	recordedAt := fmt.Sprint(before)
	e, code := x.erase(t, x.priv, "c-1", c1, "a retention obligation")
	if code != 5 || e.OK || e.Error == nil || e.Error.Code != "erasure_incomplete" || e.Position == nil || !strings.Contains(e.Error.Message, "recorded at position "+recordedAt) {
		t.Fatalf("a removal that fails after the record landed refuses naming the record: %d %+v", code, e)
	}
	if actor, verb := signerAt(t, x.ld, recordedAt); verb != "artifact.erased" || actor != x.rootFP {
		t.Fatalf("the record landed before the removal: %s %s", verb, actor)
	}
	if n := chainCount(t, x.ld); n != before+1 {
		t.Fatalf("exactly one record landed: %d", n)
	}
	// The resume meets the same store and refuses the same way, still
	// without a second record.
	if e, code := x.erase(t, x.priv, "c-1", c1, "finish"); code != 5 || e.Error == nil || e.Error.Code != "erasure_incomplete" || !strings.Contains(e.Error.Message, "recorded at position "+recordedAt) {
		t.Fatalf("the resume refuses the same way: %d %+v", code, e)
	}
	if n := chainCount(t, x.ld); n != before+1 {
		t.Fatalf("the refused resume appends nothing: %d", n)
	}
	// Once the store can remove, the next run finishes under the record.
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, ct, 0o644); err != nil {
		t.Fatal(err)
	}
	e, code = x.erase(t, x.priv, "c-1", c1, "finish")
	if code != 0 || !e.OK || e.Result["recorded"] != false || fmt.Sprint(e.Result["erased_at"]) != recordedAt || fmt.Sprint(e.Result["removed"]) != "[sealed]" {
		t.Fatalf("the next run finishes the removal under the standing record: %d %+v", code, e)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("the ciphertext is gone: %v", err)
	}
	if a := x.audit(t); a.Result["clean"] != "true" {
		t.Fatalf("the finished erasure is honored: %+v", a.Result)
	}
}
