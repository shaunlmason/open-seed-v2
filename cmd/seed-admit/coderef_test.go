package main

// The code-ref half's drills (plans/os-465e356e.md D1, D2; acceptance
// criterion 1): the default branch, contract branches, tags, other
// refs, the protected surface, the identity, and the no-ledger case,
// each judged by the installed hook over a real push.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const anchor40 = "0123456789abcdef0123456789abcdef01234567"

// codeStand is a guarded remote with a ledger far enough along to
// authorize code pushes: seed/1 active, a root (operator), two workers
// holding claim, three contracts (c-1 held by worker A, c-3 ready and
// unheld, c-4 filed and unspecified).
type codeStand struct {
	remote                 string
	root, workerA, b, op   ed25519.PrivateKey
	rootFP, aFP, bFP, opFP string
	resolve                ledger.Resolver
	// fence is the position of worker A's admitted claim on c-1.
	fence int
}

func newCodeStand(t *testing.T) *codeStand {
	t.Helper()
	remote := guardedRemote(t)
	// Pin the default branch: the code-ref half reads HEAD's symref,
	// and git's init.defaultBranch varies by host.
	if out, err := exec.Command("git", "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("symbolic-ref: %v %s", err, out)
	}
	resolve := seedGenesis(t, remote)
	s := &codeStand{remote: remote, root: fixtureKey(t), workerA: altKey(t, 21), b: altKey(t, 22), op: altKey(t, 23)}
	s.rootFP, s.aFP, s.bFP, s.opFP = fpFor(t, s.root), fpFor(t, s.workerA), fpFor(t, s.b), fpFor(t, s.op)
	s.resolve = anyResolver(t, s.root, s.workerA, s.b, s.op)

	pos := 0
	stage := func(priv ed25519.PrivateKey, v, verb, subject, payload string) {
		t.Helper()
		err := craftPush(t, remote, resolve, func(dir string, store *ledger.Store) {
			appendRaw(t, store, s.resolve, signedBy(t, priv, v, verb, subject, payload, tipOf(t, store)))
		})
		if err != nil {
			t.Fatalf("staging %s %s: %v", verb, subject, err)
		}
		pos++
	}
	stage(s.root, "seed/0", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, w := range []struct {
		key  ed25519.PrivateKey
		name string
		cap  string
	}{{s.workerA, "a", "claim"}, {s.b, "b", "claim"}, {s.op, "op", "operator"}} {
		stage(s.root, version.Seed1, "actor.enrolled", fpFor(t, w.key), enrollFor(t, w.key, "agent", w.name))
		stage(s.root, version.Seed1, "actor.granted", fpFor(t, w.key), `{"capability": "`+w.cap+`"}`)
	}
	for _, c := range []string{"c-1", "c-3", "c-4"} {
		stage(s.root, version.Seed1, "intent.filed", c, `{"intent": "work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	}
	for _, c := range []string{"c-1", "c-3"} {
		stage(s.root, version.Seed1, "contract.specified", c, `{"acceptance": {"ref": "ACCEPT.md @ `+anchor40+`", "executable": false}}`)
	}
	stage(s.workerA, version.Seed1, "claim.taken", "c-1", `{}`)
	s.fence = pos
	return s
}

// pushCode commits files in a fresh repository (on top of base when
// given) and pushes HEAD to ref on the remote as pusher, returning
// git's combined output and error. An empty pusher asserts no identity.
func pushCode(t *testing.T, remote, pusher, ref string, force bool, base string, files map[string]string) (string, error) {
	t.Helper()
	work := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	hardenGitRepo(t, work)
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if base != "" {
		run("fetch", "-q", remote, base)
		run("checkout", "-q", "FETCH_HEAD")
	}
	for name, body := range files {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "--allow-empty", "-m", "push")
	args := []string{"-C", work, "push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, remote, "HEAD:"+ref)
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), pusherEnv+"="+pusher)
	if pusher == "" {
		cmd.Env = append(os.Environ(), pusherEnv+"=")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// deleteRef pushes a deletion of ref as pusher.
func deleteRef(t *testing.T, remote, pusher, ref string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", remote, "push", remote, ":"+ref)
	cmd.Env = append(os.Environ(), pusherEnv+"="+pusher)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func refTip(t *testing.T, remote, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "--quiet", "--verify", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func mustRefuse(t *testing.T, what, out string, err error, reason string) {
	t.Helper()
	if err == nil || !strings.Contains(out, reason) {
		t.Fatalf("%s must refuse with %q, got %v\n%s", what, reason, err, out)
	}
}

func mustAdmit(t *testing.T, what, out string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s must be admitted, got %v\n%s", what, err, out)
	}
}

// conformance: III.O compromised-actor drill, the code side; III.L the
// protected surface — the default branch is the operator's, and
// append-only for everyone.
func TestCodeRefDefaultBranch(t *testing.T) {
	s := newCodeStand(t)
	out, err := pushCode(t, s.remote, s.aFP, "refs/heads/main", false, "", map[string]string{"README": "a"})
	mustRefuse(t, "agent creating the default branch", out, err, "operator standing only")
	if refTip(t, s.remote, "refs/heads/main") != "" {
		t.Fatal("refused push moved the ref")
	}
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "", map[string]string{"README": "a"})
	mustAdmit(t, "operator creating the default branch", out, err)
	first := refTip(t, s.remote, "refs/heads/main")

	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "refs/heads/main", map[string]string{"README": "b"})
	mustAdmit(t, "operator fast-forwarding the default branch", out, err)
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/main", false, "refs/heads/main", map[string]string{"README": "c"})
	mustRefuse(t, "agent fast-forwarding the default branch", out, err, "operator standing only")

	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", true, "", map[string]string{"README": "rewritten"})
	mustRefuse(t, "operator force-updating the default branch", out, err, "non-fast-forward update of the default branch is refused for everyone")
	out, err = deleteRef(t, s.remote, s.rootFP, "refs/heads/main")
	mustRefuse(t, "operator deleting the default branch", out, err, "deletion of the default branch is refused for everyone")
	out, err = deleteRef(t, s.remote, s.aFP, "refs/heads/main")
	mustRefuse(t, "agent deleting the default branch", out, err, "deletion of the default branch is refused for everyone")
	if tip := refTip(t, s.remote, "refs/heads/main"); tip == first || tip == "" {
		t.Fatal("the admitted fast-forward must stand and nothing after it may move the ref")
	}
}

// conformance: III.O compromised-actor drill, the code side — a contract
// branch is authorized by the active claim and nothing else.
func TestCodeRefContractBranchIsTheClaimHolders(t *testing.T) {
	s := newCodeStand(t)
	out, err := pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", false, "", map[string]string{"work.txt": "1"})
	mustAdmit(t, "holder creating its contract branch", out, err)
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", true, "", map[string]string{"work.txt": "rewritten"})
	mustAdmit(t, "holder force-updating its own branch", out, err)
	out, err = pushCode(t, s.remote, s.bFP, "refs/heads/seed/c-1", false, "refs/heads/seed/c-1", map[string]string{"work.txt": "b"})
	mustRefuse(t, "another claim-holding worker pushing the branch", out, err, "does not hold the active claim on c-1")
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/seed/c-1", false, "refs/heads/seed/c-1", map[string]string{"work.txt": "root"})
	mustRefuse(t, "operator standing pushing a held contract branch", out, err, "does not hold the active claim on c-1")
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-3", false, "", map[string]string{"work.txt": "3"})
	mustRefuse(t, "a ready, unheld contract", out, err, "no claim is active on c-3")
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-9", false, "", map[string]string{"work.txt": "9"})
	mustRefuse(t, "a contract that does not exist", out, err, "no contract c-9 exists")
	out, err = deleteRef(t, s.remote, s.bFP, "refs/heads/seed/c-1")
	mustRefuse(t, "a non-holder deleting the branch", out, err, "does not hold the active claim on c-1")

	// The window closes: the holder releases, and its branch closes
	// with it (the "lease" clause on the code side, D4).
	packet := `{"acceptance": ["done"], "decisions": [], "base": "` + anchor40 + `..` + anchor40 + `", "refs": [], "findings": []}`
	err = craftPush(t, s.remote, s.resolve, func(dir string, store *ledger.Store) {
		appendRaw(t, store, s.resolve, signedBy(t, s.workerA, version.Seed1, "claim.released", "c-1", fmt.Sprintf(`{"fence": "%d", "packet": %s}`, s.fence, packet), tipOf(t, store)))
	})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", false, "refs/heads/seed/c-1", map[string]string{"work.txt": "after"})
	mustRefuse(t, "the former holder after its window closed", out, err, "no claim is active on c-1")
	out, err = deleteRef(t, s.remote, s.aFP, "refs/heads/seed/c-1")
	mustAdmitOrRefuseDeletion(t, out, err)
}

// A deletion by a former holder is refused too: the branch closed with
// the window, and deleting it is an update like any other.
func mustAdmitOrRefuseDeletion(t *testing.T, out string, err error) {
	t.Helper()
	mustRefuse(t, "the former holder deleting the branch", out, err, "no claim is active on c-1")
}

// conformance: III.O compromised-actor drill, the code side; §II.14
// immutable tags — created by operator standing, immutable after.
func TestCodeRefTagsAreImmutable(t *testing.T) {
	s := newCodeStand(t)
	out, err := pushCode(t, s.remote, s.aFP, "refs/tags/v1", false, "", map[string]string{"f": "1"})
	mustRefuse(t, "agent creating a tag", out, err, "tags are created by operator standing only")
	out, err = pushCode(t, s.remote, s.rootFP, "refs/tags/v1", false, "", map[string]string{"f": "1"})
	mustAdmit(t, "operator creating a tag", out, err)
	tip := refTip(t, s.remote, "refs/tags/v1")
	out, err = pushCode(t, s.remote, s.rootFP, "refs/tags/v1", true, "", map[string]string{"f": "2"})
	mustRefuse(t, "operator moving a tag", out, err, "tags are immutable")
	out, err = deleteRef(t, s.remote, s.rootFP, "refs/tags/v1")
	mustRefuse(t, "operator deleting a tag", out, err, "tags are immutable")
	out, err = deleteRef(t, s.remote, s.aFP, "refs/tags/v1")
	mustRefuse(t, "agent deleting a tag", out, err, "tags are immutable")
	if refTip(t, s.remote, "refs/tags/v1") != tip {
		t.Fatal("the tag moved")
	}
}

// conformance: III.O compromised-actor drill, the code side — an agent
// credential's surface is its contract branch and nothing else;
// operator standing creates or fast-forwards other refs and never
// rewrites or deletes them.
func TestCodeRefOtherRefsAndIdentity(t *testing.T) {
	s := newCodeStand(t)
	out, err := pushCode(t, s.remote, s.aFP, "refs/heads/feature", false, "", map[string]string{"f": "1"})
	mustRefuse(t, "agent pushing an arbitrary branch", out, err, "outside "+s.aFP+"'s authorized code surface")
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/feature", false, "", map[string]string{"f": "1"})
	mustAdmit(t, "operator creating a branch", out, err)
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/feature", false, "refs/heads/feature", map[string]string{"f": "2"})
	mustAdmit(t, "operator fast-forwarding a branch", out, err)
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/feature", true, "", map[string]string{"f": "3"})
	mustRefuse(t, "operator force-updating a branch", out, err, "non-fast-forward update is refused")
	out, err = deleteRef(t, s.remote, s.rootFP, "refs/heads/feature")
	mustRefuse(t, "operator deleting a branch", out, err, "deletion is refused")

	// No asserted identity refuses on every code ref, including one the
	// pusher would otherwise be authorized for.
	out, err = pushCode(t, s.remote, "", "refs/heads/seed/c-1", false, "", map[string]string{"f": "1"})
	mustRefuse(t, "a push asserting no identity", out, err, pusherEnv)
	// An identity the ledger does not know holds nothing.
	stranger, err := event.Fingerprint(altKey(t, 99).Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	out, err = pushCode(t, s.remote, stranger, "refs/heads/feature2", false, "", map[string]string{"f": "1"})
	mustRefuse(t, "an unenrolled identity", out, err, "outside "+stranger+"'s authorized code surface")
	// The ledger ref itself is judged exactly as before, whoever pushes:
	// a valid raw append lands with no identity at all.
	err = craftPush(t, s.remote, s.resolve, func(dir string, store *ledger.Store) {
		appendRaw(t, store, s.resolve, signedBy(t, s.workerA, version.Seed1, "message.sent", "c-1", fmt.Sprintf(`{"fence": "%d", "n": 1}`, s.fence), tipOf(t, store)))
	})
	if err != nil {
		t.Fatalf("the ledger half is unchanged by the code-ref half: %v", err)
	}
}

// conformance: III.L — the protected surface is write-denied to every
// agent key whose work it gates, read from the declaration at the
// default branch's CURRENT tip, its own path protected by construction.
func TestCodeRefProtectedSurface(t *testing.T) {
	s := newCodeStand(t)
	decl := `{"posture": "enforced-self-hosted", "protected": ["Makefile", "ci/"]}`
	out, err := pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "", map[string]string{posture.DeclarationPath: decl, "Makefile": "check:\n", "README": "r"})
	mustAdmit(t, "operator committing the declaration", out, err)

	for name, files := range map[string]map[string]string{
		"Makefile":           {"Makefile": "check: evil\n"},
		"ci/x.yml":           {"ci/x.yml": "on: push\n"},
		"the declaration":    {posture.DeclarationPath: `{"posture": "enforced-self-hosted"}`},
		"unprotect-and-edit": {posture.DeclarationPath: `{"posture": "enforced-self-hosted", "protected": []}`, "Makefile": "check: evil\n"},
	} {
		out, err := pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", true, "refs/heads/main", files)
		mustRefuse(t, "holder touching "+name, out, err, "on the protected surface")
		if refTip(t, s.remote, "refs/heads/seed/c-1") != "" {
			t.Fatalf("%s: refused push moved the ref", name)
		}
	}
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", false, "refs/heads/main", map[string]string{"Makefile.notes": "x", "src/a.go": "package a\n", "a_test.go": "package a\n"})
	mustAdmit(t, "holder touching only unprotected paths (test content included: the charter's named residual)", out, err)

	// A commit on the default branch touching the surface is the
	// operator's; merging it into the contract branch introduces
	// nothing, so the merge is admitted.
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "refs/heads/main", map[string]string{"Makefile": "check: operator\n"})
	mustAdmit(t, "operator changing the surface on the default branch", out, err)
	work := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		cmd.Env = append(os.Environ(), pusherEnv+"="+s.aFP)
		o, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, o)
		}
		return string(o)
	}
	git("init", "-q")
	hardenGitRepo(t, work)
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("fetch", "-q", s.remote, "refs/heads/seed/c-1:refs/heads/work", "refs/heads/main:refs/heads/main")
	git("checkout", "-q", "work")
	git("merge", "-q", "--no-edit", "main")
	git("push", s.remote, "HEAD:refs/heads/seed/c-1")

	// The operator is exempt: root claims c-3 (operator holds claim's
	// row) and changes the surface on its branch.
	err = craftPush(t, s.remote, s.resolve, func(dir string, store *ledger.Store) {
		appendRaw(t, store, s.resolve, signedBy(t, s.root, version.Seed1, "claim.taken", "c-3", `{}`, tipOf(t, store)))
	})
	if err != nil {
		t.Fatalf("root claim: %v", err)
	}
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/seed/c-3", false, "refs/heads/main", map[string]string{"Makefile": "check: root\n"})
	mustAdmit(t, "operator standing touching the surface on a branch it holds", out, err)

	// A declaration that does not parse refuses agent pushes naming
	// the repair, and operator pushes still land.
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "refs/heads/main", map[string]string{posture.DeclarationPath: `{"posture": "anarchy"}`})
	mustAdmit(t, "operator breaking the declaration", out, err)
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", false, "refs/heads/seed/c-1", map[string]string{"src/b.go": "package a\n"})
	mustRefuse(t, "holder pushing under a broken declaration", out, err, "does not parse")
}

// conformance: III.B statelessness on the code side — the hook has no
// standing to authorize a code push until the guarded ref carries an
// admitted genesis, and a repository with no declaration on its
// default branch protects the declaration path alone.
func TestCodeRefNoLedgerAndNoDeclaration(t *testing.T) {
	remote := guardedRemote(t)
	if out, err := exec.Command("git", "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("symbolic-ref: %v %s", err, out)
	}
	root := fpFor(t, fixtureKey(t))
	out, err := pushCode(t, remote, root, "refs/heads/main", false, "", map[string]string{"README": "a"})
	mustRefuse(t, "a code push before any genesis", out, err, "holds no admitted ledger")

	s := newCodeStand(t)
	// No declaration on main: the surface is the declaration path.
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "", map[string]string{"README": "a"})
	mustAdmit(t, "operator creating main without a declaration", out, err)
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", false, "refs/heads/main", map[string]string{posture.DeclarationPath: `{"posture": "cooperative"}`})
	mustRefuse(t, "holder introducing a declaration on its branch", out, err, "on the protected surface")
	out, err = pushCode(t, s.remote, s.aFP, "refs/heads/seed/c-1", false, "refs/heads/main", map[string]string{"Makefile": "anything\n"})
	mustAdmit(t, "holder touching a path no declaration protects", out, err)
}

// The two halves in one push: a ledger update and a code update are
// judged separately, and any refusal fails the whole push, so a code
// ref never lands beside a refused ledger update.
func TestCodeRefAtomicWithLedgerHalf(t *testing.T) {
	s := newCodeStand(t)
	work := t.TempDir()
	git := func(env []string, args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", work}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		o, err := cmd.CombinedOutput()
		return string(o), err
	}
	must := func(args ...string) {
		t.Helper()
		if o, err := git(nil, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, o)
		}
	}
	must("init", "-q")
	hardenGitRepo(t, work)
	must("config", "user.email", "t@t")
	must("config", "user.name", "t")
	must("commit", "-q", "--allow-empty", "-m", "work")
	code := strings.TrimSpace(func() string { o, _ := git(nil, "rev-parse", "HEAD"); return o }())

	// A refused ledger append (hostile payload) beside a legal code push.
	must("fetch", "-q", s.remote, guardedRef+":refs/heads/ledger")
	must("checkout", "-q", "ledger")
	hostile := fmt.Sprintf("%s\n", strings.Repeat("all work and no play ", 40))
	if err := os.WriteFile(filepath.Join(work, "segments", "hostile.jsonl"), []byte(hostile), 0o644); err != nil {
		t.Fatal(err)
	}
	must("add", "-A")
	must("commit", "-q", "-m", "hostile")
	before := refTip(t, s.remote, guardedRef)
	out, err := git([]string{pusherEnv + "=" + s.aFP}, "push", s.remote, "HEAD:"+guardedRef, code+":refs/heads/seed/c-1")
	if err == nil {
		t.Fatalf("a push carrying a refused ledger update must fail whole: %s", out)
	}
	if refTip(t, s.remote, guardedRef) != before || refTip(t, s.remote, "refs/heads/seed/c-1") != "" {
		t.Fatal("a refused push moved a ref")
	}
	// The same code push alone lands.
	out, err = git([]string{pusherEnv + "=" + s.aFP}, "push", s.remote, code+":refs/heads/seed/c-1")
	if err != nil {
		t.Fatalf("the legal half alone must land: %v %s", err, out)
	}
}

// conformance: III.L §II.14 — the protected surface is the governance
// root's alone: a non-root operator (the maintenance lane holds operator
// and is an agent key) may fast-forward the default branch with ordinary
// changes but is refused on a commit touching a protected path; only the
// governance root is exempt (Copilot review on #247).
func TestCodeRefProtectedSurfaceIsRootOnly(t *testing.T) {
	s := newCodeStand(t)
	decl := `{"posture": "enforced-self-hosted", "protected": ["Makefile"]}`
	out, err := pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "", map[string]string{posture.DeclarationPath: decl, "Makefile": "check:\n", "README": "r"})
	mustAdmit(t, "root committing the declaration and surface", out, err)

	// The operator advances main with a non-protected change: admitted.
	out, err = pushCode(t, s.remote, s.opFP, "refs/heads/main", false, "refs/heads/main", map[string]string{"README": "operator edit"})
	mustAdmit(t, "operator fast-forwarding main off the surface", out, err)
	// The operator touches the protected Makefile on main: refused.
	out, err = pushCode(t, s.remote, s.opFP, "refs/heads/main", false, "refs/heads/main", map[string]string{"Makefile": "check: evil\n"})
	mustRefuse(t, "operator touching the protected surface on main", out, err, "on the protected surface")
	// Root may.
	out, err = pushCode(t, s.remote, s.rootFP, "refs/heads/main", false, "refs/heads/main", map[string]string{"Makefile": "check: root\n"})
	mustAdmit(t, "root touching the protected surface on main", out, err)
	// The operator on an arbitrary branch it may create is still
	// protected-checked (root exempt everywhere, operator nowhere).
	out, err = pushCode(t, s.remote, s.opFP, "refs/heads/feature", false, "refs/heads/main", map[string]string{"Makefile": "check: sneaky\n"})
	mustRefuse(t, "operator touching the surface on a feature branch", out, err, "on the protected surface")
}
