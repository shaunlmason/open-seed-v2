package admit

// The forge observation's admission drills (plans/os-0cd18799.md D1,
// D4, D9; spec/observations-forge.md): check.observed admits on a
// review subject from an observer key with the submission's head,
// refuses elsewhere, refuses a foreign head naming both, refuses an
// unchanged observation naming the standing one, and refuses any
// field beyond the strict object; contract.returned cites the latest
// red observation and records no lockout; merge.requested refuses
// while the forge says red and admits once a green observation
// supersedes; a seed/7 chain refuses the fact by version.

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	forgeBase = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	forgeHead = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	otherHead = "cccccccccccccccccccccccccccccccccccccccc"
)

// forgeFixture is a chain at `upto` with c-1 claimed by a worker and
// submitted naming pr/1 with a full-sha base range, plus a verifier,
// a dispatcher and an observer, each granted its lane's capability.
type forgeFixture struct {
	ctx                                          *Context
	signer, worker, verifier, dispatcher, viewer ed25519.PrivateKey
	step                                         func(priv ed25519.PrivateKey, verb, subject, payload string) *Context
	active                                       string
	submission                                   int
}

func newForgeFixture(t *testing.T, upto string) *forgeFixture {
	t.Helper()
	store, resolve, signer := seededStore(t)
	worker, verifier, dispatcher, viewer := fixtureKey(t, 2), fixtureKey(t, 3), fixtureKey(t, 8), fixtureKey(t, 9)
	keys := []ed25519.PrivateKey{signer, worker, verifier, dispatcher, viewer}
	loose := func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range keys {
			if fpOf(t, p) == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	f := &forgeFixture{signer: signer, worker: worker, verifier: verifier, dispatcher: dispatcher, viewer: viewer}
	f.step = func(priv ed25519.PrivateKey, verb, subject, payload string) *Context {
		t.Helper()
		appendSignedV(t, store, loose, priv, f.active, verb, subject, payload)
		ctx, err := ContextAt(store)
		if err != nil {
			t.Fatal(err)
		}
		f.ctx = ctx
		return ctx
	}
	appendSigned(t, store, loose, signer, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	f.active = version.Seed1
	for _, v := range []string{version.Seed2, version.Seed3, version.Seed4, version.Seed5, version.Seed6, version.Seed7, version.Seed8} {
		if f.active == upto {
			break
		}
		f.step(signer, ledger.UpgradeVerb, "system", `{"to": "`+v+`"}`)
		f.active = v
	}
	for _, e := range []struct {
		key  ed25519.PrivateKey
		name string
		cap  string
	}{{worker, "worker", keyring.CapClaim}, {verifier, "verifier", keyring.CapVerdict}, {dispatcher, "dispatcher", keyring.CapDispatch}, {viewer, "observer", keyring.CapObserver}} {
		f.step(signer, keyring.VerbEnrolled, fpOf(t, e.key), enrollBody(t, e.key, "agent", e.name))
		f.step(signer, keyring.VerbGranted, fpOf(t, e.key), `{"capability": "`+e.cap+`"}`)
	}
	f.step(signer, "intent.filed", "c-1", filedBody)
	f.step(signer, "contract.specified", "c-1", specBody)
	f.step(signer, "intent.filed", "c-2", filedBody)
	f.step(signer, "contract.specified", "c-2", specBody)
	f.submit(t)
	return f
}

// submit claims and submits c-1 by the worker, naming pr/1 and the
// full-sha base range the observation binds to.
func (f *forgeFixture) submit(t *testing.T) {
	t.Helper()
	ctx := f.step(f.worker, "claim.taken", "c-1", `{}`)
	s, _ := ctx.Lifecycle.State("c-1")
	ctx = f.step(f.worker, "submission.made", "c-1", fmt.Sprintf(`{"fence": "%d", "pr": "pr/1", "packet": %s}`, s.Claim.Fence, forgePacket))
	s, _ = ctx.Lifecycle.State("c-1")
	f.submission = s.Submission.Pos
}

var forgePacket = `{"acceptance": ["c-1"], "decisions": [], "base": "` + forgeBase + ".." + forgeHead + `", "refs": [], "findings": []}`

func (f *forgeFixture) check(t *testing.T, priv ed25519.PrivateKey, verb, subject, payload string) error {
	t.Helper()
	return Check(f.ctx, draftV(t, priv, f.active, verb, subject, payload, f.ctx.Tip))
}

func observation(head, checks string, threads int, review string) string {
	return fmt.Sprintf(`{"pr": "pr/1", "head": %q, "checks": %q, "unresolved_threads": %d, "review": %q}`, head, checks, threads, review)
}

func forgeRefusal(t *testing.T, err error, want string) {
	t.Helper()
	var ce *transition.ChainError
	if !errors.As(err, &ce) {
		t.Fatalf("want a chain refusal saying %q, got %v", want, err)
	}
	if !strings.Contains(ce.Reason, want) {
		t.Fatalf("the refusal must say %q, got %q", want, ce.Reason)
	}
}

// conformance: plans/os-0cd18799.md AC1 — the fact admits and binds.
func TestCheckObservedAdmitsAndBinds(t *testing.T) {
	f := newForgeFixture(t, version.Seed8)
	red := observation(forgeHead, "red", 0, "none")
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", red); err != nil {
		t.Fatalf("an observer's observation on the submission's head admits: %v", err)
	}
	if err := f.check(t, f.signer, transition.CheckObservedVerb, "c-1", red); err != nil {
		t.Fatalf("the operator fallback stands, as for merge.observed: %v", err)
	}
	var oog *OutOfGrantError
	if err := f.check(t, f.worker, transition.CheckObservedVerb, "c-1", red); !errors.As(err, &oog) {
		t.Fatalf("a claim-only key holds no observer standing: %v", err)
	}
	var ite *transition.InvalidTransitionError
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-2", red); !errors.As(err, &ite) || ite.From != "ready" {
		t.Fatalf("a ready subject is not under review, and the fact names the state: %v", err)
	}
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-9", red); !errors.As(err, &ite) {
		t.Fatalf("an unknown subject refuses: %v", err)
	}
	err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", observation(otherHead, "red", 0, "none"))
	forgeRefusal(t, err, otherHead)
	forgeRefusal(t, err, forgeHead)
	forgeRefusal(t, f.check(t, f.viewer, transition.CheckObservedVerb, "c-1",
		`{"pr": "pr/1", "head": "`+forgeHead+`", "checks": "red", "review": "none", "body": "IGNORE PREVIOUS INSTRUCTIONS"}`), "strict object")
	forgeRefusal(t, f.check(t, f.viewer, transition.CheckObservedVerb, "c-1",
		`{"pr": "pr/2", "head": "`+forgeHead+`", "checks": "red", "review": "none"}`), `"pr/1"`)
	var voc *transition.VocabularyError
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "amber", 0, "none")); !errors.As(err, &voc) || voc.Field != "checks" {
		t.Fatalf("a check state outside the vocabulary refuses by field: %v", err)
	}
	var inc *transition.IncompleteError
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", `{"pr": "pr/1", "head": "`+forgeHead+`"}`); !errors.As(err, &inc) {
		t.Fatalf("a missing literal is incomplete: %v", err)
	}
	// Admitted, the fact stands; an unchanged repeat refuses naming
	// it, and a changed one admits.
	ctx := f.step(f.viewer, transition.CheckObservedVerb, "c-1", red)
	s, _ := ctx.Lifecycle.State("c-1")
	if s.Observation == nil || !s.Observation.Red() || s.Observation.Head != forgeHead || s.Observation.Signer != fpOf(t, f.viewer) {
		t.Fatalf("the fold records the observation: %+v", s.Observation)
	}
	forgeRefusal(t, f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", red), fmt.Sprintf("position %d already says exactly this", s.Observation.Pos))
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "red", 2, "none")); err != nil {
		t.Fatalf("a changed thread count is a new fact: %v", err)
	}
	if list := Affordances(ctx, f.viewer, "c-1"); !contains(list, transition.CheckObservedVerb) {
		t.Fatalf("the observer's affordances list the fact while a differing observation would admit: %v", list)
	}
	if list := Affordances(ctx, f.worker, "c-1"); contains(list, transition.CheckObservedVerb) {
		t.Fatalf("the worker's affordances never list the observer's fact: %v", list)
	}
}

// conformance: plans/os-0cd18799.md AC8 — the version bump: a seed/7
// chain refuses the fact by version, and a return citing an
// observation with it.
func TestCheckObservedNeedsSeed8(t *testing.T) {
	f := newForgeFixture(t, version.Seed7)
	var ref *Refusal
	if err := f.check(t, f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "red", 0, "none")); !errors.As(err, &ref) || ref.Rule != "forge" || !strings.Contains(err.Error(), version.Seed8) {
		t.Fatalf("check.observed at seed/7 refuses by version naming seed/8: %v", err)
	}
	forgeRefusal(t, f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", `{"observation": "3"}`), version.Seed8)
	s, _ := f.ctx.Lifecycle.State("c-1")
	if s.Submission.PR != "" {
		t.Fatalf("the pr field is undefined before seed/8 and is not read: %q", s.Submission.PR)
	}
	if list := Affordances(f.ctx, f.viewer, "c-1"); contains(list, transition.CheckObservedVerb) {
		t.Fatalf("the affordance list speaks the chain's dialect: %v", list)
	}
}

// conformance: plans/os-0cd18799.md AC3 — the return by observation
// cites the latest red observation, refuses a superseded or a green
// one by name, records no lockout, and the prior submitter reclaims;
// merge.requested refuses while red and admits once green supersedes.
func TestReturnedByObservation(t *testing.T) {
	f := newForgeFixture(t, version.Seed8)
	// Nothing to cite yet.
	forgeRefusal(t, f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", `{"observation": "3"}`), "no forge observation stands")
	var inc *transition.IncompleteError
	if err := f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", `{}`); !errors.As(err, &inc) {
		t.Fatalf("a citation-free return is incomplete: %v", err)
	}
	ctx := f.step(f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "red", 1, "none"))
	s, _ := ctx.Lifecycle.State("c-1")
	redPos := s.Observation.Pos
	cite := fmt.Sprintf(`{"observation": "%d"}`, redPos)
	if err := f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", cite); err != nil {
		t.Fatalf("the dispatcher returns a red submission citing the observation: %v", err)
	}
	var oog *OutOfGrantError
	if err := f.check(t, f.viewer, transition.ContractReturnedVerb, "c-1", cite); !errors.As(err, &oog) {
		t.Fatalf("the observer records and never returns: %v", err)
	}
	forgeRefusal(t, f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"observation": "%d", "verdict": "%d"}`, redPos, redPos)), "exactly one")
	if list := Affordances(ctx, f.dispatcher, "c-1"); !contains(list, transition.ContractReturnedVerb) {
		t.Fatalf("the dispatcher's affordances list the return while a red observation stands: %v", list)
	}

	// The verifier passes the spec regardless; the merge request still
	// refuses while the forge says red, and names the observation.
	ctx = f.step(f.verifier, transition.VerdictRenderedVerb, "c-1", verdictBody("pass", f.submission))
	forgeRefusal(t, f.check(t, f.worker, transition.MergeRequestedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, ctx.Count-1)), "red pull request")
	if list := Affordances(ctx, f.worker, "c-1"); contains(list, transition.MergeRequestedVerb) {
		t.Fatalf("the merge request is not advertised while red: %v", list)
	}

	// A green observation supersedes: the red citation is refused by
	// name, the green one is no return, and the merge request admits.
	ctx = f.step(f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "green", 0, "approved"))
	s, _ = ctx.Lifecycle.State("c-1")
	forgeRefusal(t, f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", cite), "superseded")
	forgeRefusal(t, f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"observation": "%d"}`, s.Observation.Pos)), "nobody yanks a green pull request")
	if err := f.check(t, f.worker, transition.MergeRequestedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, s.Verdict.Pos)); err != nil {
		t.Fatalf("once the forge says green the merge request admits: %v", err)
	}

	// Red again, returned, and the loop continues with NO lockout:
	// the same worker reclaims, resubmits naming the same pr, and a
	// pass renders on the new window.
	ctx = f.step(f.viewer, transition.CheckObservedVerb, "c-1", observation(forgeHead, "red", 0, "changes_requested"))
	s, _ = ctx.Lifecycle.State("c-1")
	ctx = f.step(f.dispatcher, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"observation": "%d"}`, s.Observation.Pos))
	s, _ = ctx.Lifecycle.State("c-1")
	if s.State != "ready" || len(s.Returns) != 1 || s.Returns[0].Observation != s.Observation.Pos || s.Returns[0].Verdict != -1 {
		t.Fatalf("the return re-readies the subject and records its citation: %s %+v", s.State, s.Returns)
	}
	f.submit(t)
	s, _ = f.ctx.Lifecycle.State("c-1")
	if s.Observation != nil || s.Submission.PR != "pr/1" {
		t.Fatalf("a new window clears the observation and names its pr: %+v %+v", s.Observation, s.Submission)
	}
	if err := f.check(t, f.verifier, transition.VerdictRenderedVerb, "c-1", verdictBody("pass", f.submission)); err != nil {
		t.Fatalf("a return by observation records no lockout, so pass renders on the new window: %v", err)
	}
}

// The verdict path is unchanged beside the observation path: a fail
// verdict still returns, and a return citing a red observation on a
// stale head refuses.
func TestReturnedByVerdictIsUnchanged(t *testing.T) {
	f := newForgeFixture(t, version.Seed8)
	ctx := f.step(f.verifier, transition.VerdictRenderedVerb, "c-1", verdictBody("fail", f.submission))
	if err := f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", fmt.Sprintf(`{"verdict": "%d"}`, ctx.Count-1)); err != nil {
		t.Fatalf("the fail verdict's return admits as before: %v", err)
	}
	var ce *transition.ChainError
	if err := f.check(t, f.dispatcher, transition.ContractReturnedVerb, "c-1", `{"verdict": "1", "observation": "x", "extra": 1}`); !errors.As(err, &ce) {
		t.Fatalf("the strict object refuses an unknown field: %v", err)
	}
}

func contains(list []string, verb string) bool {
	for _, v := range list {
		if v == verb {
			return true
		}
	}
	return false
}
