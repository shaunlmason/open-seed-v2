package curation

// The curation package's own drills (plans/os-f30ee0d3.md AC6,
// plans/os-96850e5a.md AC1, AC5, step 7): the shapes, the id
// derivation over claim and exceptions, the predicate, the gate
// registry, the fold with the contested stage, anomalies, unbound, the
// lint's two halves, and the shipped store.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func testKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func signed(t *testing.T, key ed25519.PrivateKey, v, verb, subject, payload string) *event.Record {
	t.Helper()
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{V: v, TS: "2026-09-02T00:00:00Z", Actor: fp, Verb: verb, Subject: subject,
		Payload: json.RawMessage(payload), Prev: strings.Repeat("0", 64)}, key)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

const applies = `{"routing": "core"}`

func proposal(claim string, support []string, exceptions []string) string {
	q := func(xs []string) string {
		out := make([]string, len(xs))
		for i, x := range xs {
			out[i] = fmt.Sprintf("%q", x)
		}
		return "[" + strings.Join(out, ", ") + "]"
	}
	return fmt.Sprintf(`{"claim": %q, "applies_when": %s, "support": %s, "exceptions": %s, "provenance": ["plans/x.md @ 0123456"]}`, claim, applies, q(support), q(exceptions))
}

func lesson(id string, pos int) string {
	return fmt.Sprintf(`{"lesson": "%s/x.md @ 0123456", "hypothesis": "%s@%d", "pr": "pr/1 @ 0123456", "carrier": "knowledge", "adversarial": {"eval": "fix-the-check", "verdict": "40"}, "last_validated": "2026-09-01T00:00:00Z", "expires": "2026-12-01T00:00:00Z", "digest": "%s"}`,
		LessonsDir, id, pos, strings.Repeat("a", 64))
}

func gateOf(t *testing.T, err error) string {
	t.Helper()
	ge, ok := err.(*GateError)
	if !ok {
		t.Fatalf("a curation refusal is a GateError, got %T: %v", err, err)
	}
	if _, registered := GateDescription(ge.Gate); !registered {
		t.Fatalf("gate %q is not registered", ge.Gate)
	}
	return ge.Gate
}

func TestHypothesisIDIsDerivedFromClaimAndExceptions(t *testing.T) {
	a := HypothesisID("retry the fetch once", nil)
	if !IsHypothesisSubject(a) {
		t.Fatalf("the id has the h-<12 hex> shape: %s", a)
	}
	if b := HypothesisID("  retry   the fetch\nonce ", []string{}); b != a {
		t.Fatalf("whitespace does not change the claim: %s vs %s", a, b)
	}
	if c := HypothesisID("retry the fetch twice", nil); c == a {
		t.Fatal("a different claim derives a different subject")
	}
	if d := HypothesisID("retry the fetch once", []string{"a warm mirror"}); d == a {
		t.Fatal("an exception is part of the subject: adding one is the road out of a contest")
	}
	if HypothesisID("x", []string{"b", "a"}) != HypothesisID("x", []string{"a", "b"}) {
		t.Fatal("the exception set is unordered")
	}
	if IsHypothesisSubject("c-1") || IsHypothesisSubject("h-xyz") {
		t.Fatal("only the derived shape is a hypothesis subject")
	}
}

// conformance: plans/os-96850e5a.md AC1 — the predicate parses on at
// least one record-derivable field and matches on every present one.
func TestAppliesWhenIsAPredicateOverRecordFields(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":     `{}`,
		"unknown":   `{"routing": "core", "squad": "x"}`,
		"no paths":  `{"paths": []}`,
		"number":    `{"tier": 3}`,
		"blank":     `{"routing": " "}`,
		"string":    `"flaky"`,
		"empty-elt": `{"paths": [""]}`,
	} {
		if _, err := ParseAppliesWhen(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: refuses", name)
		}
	}
	a, err := ParseAppliesWhen(json.RawMessage(`{"routing": "core", "paths": ["specs/", "docs/"]}`))
	if err != nil {
		t.Fatal(err)
	}
	subject := transition.SubjectState{Routing: "core", Tier: "trivial", Acceptance: &transition.AcceptanceInfo{Ref: "specs/thing.md @ abc1234"}}
	if !a.Selects(subject) {
		t.Fatal("every present field matches: selected")
	}
	if (AppliesWhen{Routing: "other"}).Selects(subject) || (AppliesWhen{Tier: "critical"}).Selects(subject) || (AppliesWhen{Paths: []string{"src/"}}).Selects(subject) {
		t.Fatal("a present field that differs deselects")
	}
	if (AppliesWhen{}).Selects(subject) {
		t.Fatal("an empty predicate selects nothing")
	}
	if !(AppliesWhen{Tier: "trivial"}).Selects(subject) {
		t.Fatal("an absent field matches nothing on its own, and a present one matches")
	}
	if (AppliesWhen{Paths: []string{"specs/"}}).Selects(transition.SubjectState{Routing: "core"}) {
		t.Fatal("paths need an acceptance to match")
	}
	if !a.Equal(a) || a.Equal(AppliesWhen{Routing: "core"}) {
		t.Fatal("Equal compares field for field")
	}
}

func TestShapesRefuseAtRegisteredGates(t *testing.T) {
	good := `{"fence": "3", "tried": "x", "outcome": "y", "condition": "z", "environment": "w", "pointer": "a/b.md @ 0123456"}`
	if _, err := ParseDeadEnd("c-1", []byte(good)); err != nil {
		t.Fatalf("a complete dead end parses: %v", err)
	}
	for name, body := range map[string]string{
		"unknown":  `{"fence": "3", "tried": "x", "outcome": "y", "condition": "z", "environment": "w", "conclusion": "so"}`,
		"bare":     `{"fence": "3", "tried": "x", "outcome": "y", "condition": "z", "environment": "w", "pointer": "a/b.md"}`,
		"fence":    `{"fence": "three", "tried": "x", "outcome": "y", "condition": "z", "environment": "w"}`,
		"not-json": `nope`,
		"trailing": good + ` {}`,
	} {
		if _, err := ParseDeadEnd("c-1", []byte(body)); err == nil {
			t.Errorf("%s: a malformed dead end refuses", name)
		} else if gateOf(t, err) != GateDeadEndShape {
			t.Errorf("%s: refuses at the shape gate, got %v", name, err)
		}
	}
	if _, err := ParseDeadEnd("c-1", []byte(`{"fence": "3", "tried": "x", "outcome": "y", "condition": "", "environment": "w"}`)); err == nil {
		t.Fatal("a missing field refuses")
	}
	claim := "retry once"
	id := HypothesisID(claim, nil)
	if _, err := ParseHypothesis(id, []byte(proposal(claim, []string{"c-1@4", "c-2@9"}, nil))); err != nil {
		t.Fatalf("a well-formed proposal parses: %v", err)
	}
	for name, c := range map[string]struct{ subject, body, gate string }{
		"subject":     {"h-000000000000", proposal(claim, []string{"c-1@4"}, nil), GateProposalSubject},
		"exceptions":  {id, proposal(claim, []string{"c-1@4"}, []string{"warm"}), GateProposalSubject},
		"citation":    {id, proposal(claim, []string{"c-1"}, nil), GateProposalShape},
		"provenance":  {id, strings.Replace(proposal(claim, nil, nil), "plans/x.md @ 0123456", "plans/x.md", 1), GateProposalShape},
		"unknown":     {id, strings.Replace(proposal(claim, nil, nil), `"claim"`, `"why": "x", "claim"`, 1), GateProposalShape},
		"applies":     {id, strings.Replace(proposal(claim, nil, nil), applies, `{}`, 1), GateAppliesWhen},
		"applies-str": {id, strings.Replace(proposal(claim, nil, nil), applies, `"flaky"`, 1), GateAppliesWhen},
	} {
		_, err := ParseHypothesis(c.subject, []byte(c.body))
		if err == nil {
			t.Errorf("%s: a malformed proposal refuses", name)
			continue
		}
		if got := gateOf(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s", name, c.gate, got)
		}
	}
	if _, err := ParseHypothesis(id, []byte(fmt.Sprintf(`{"claim": %q, "applies_when": %s, "exceptions": [], "provenance": []}`, claim, applies))); err == nil {
		t.Fatal("a missing support list refuses")
	}

	if _, err := ParseContest(id, []byte(fmt.Sprintf(`{"hypothesis": "%s@7", "evidence": ["c-3@11"], "reason": "the mirror was warm"}`, id))); err != nil {
		t.Fatalf("a well-formed contest parses: %v", err)
	}
	for name, body := range map[string]string{
		"other":    `{"hypothesis": "h-000000000000@7", "evidence": ["c-3@11"], "reason": "r"}`,
		"contract": `{"hypothesis": "c-1@7", "evidence": ["c-3@11"], "reason": "r"}`,
		"evidence": fmt.Sprintf(`{"hypothesis": "%s@7", "evidence": ["c-3"], "reason": "r"}`, id),
		"unknown":  fmt.Sprintf(`{"hypothesis": "%s@7", "evidence": ["c-3@11"], "reason": "r", "score": 1}`, id),
	} {
		if _, err := ParseContest(id, []byte(body)); err == nil {
			t.Errorf("%s: a malformed contest refuses", name)
		} else if gateOf(t, err) != GateContestShape {
			t.Errorf("%s: refuses at the shape gate, got %v", name, err)
		}
	}
	if _, err := ParseContest(id, []byte(fmt.Sprintf(`{"hypothesis": "%s@7", "evidence": [], "reason": ""}`, id))); err == nil {
		t.Fatal("a contest without evidence or reason refuses")
	}

	if _, err := ParseLesson(id, []byte(lesson(id, 7))); err != nil {
		t.Fatalf("a well-formed promotion parses: %v", err)
	}
	for name, c := range map[string]struct{ body, gate string }{
		"elsewhere":   {strings.Replace(lesson(id, 7), LessonsDir+"/x.md", "docs/x.md", 1), GatePromotionShape},
		"bare":        {strings.Replace(lesson(id, 7), "/x.md @ 0123456", "/x.md", 1), GatePromotionShape},
		"mismatch":    {strings.Replace(lesson(id, 7), id+"@7", "h-000000000000@7", 1), GatePromotionHypothesis},
		"carrier":     {strings.Replace(lesson(id, 7), `"knowledge"`, `"prompt"`, 1), GatePromotionCarrier},
		"stamps":      {strings.Replace(lesson(id, 7), "2026-12-01", "2026-08-01", 1), GatePromotionStamps},
		"stamp-shape": {strings.Replace(lesson(id, 7), "2026-12-01T00:00:00Z", "december", 1), GatePromotionStamps},
		"digest":      {strings.Replace(lesson(id, 7), strings.Repeat("a", 64), "abc", 1), GatePromotionDigest},
		"adversary":   {strings.Replace(lesson(id, 7), `"eval": "fix-the-check"`, `"eval": ""`, 1), GatePromotionAdversary},
		"verdict":     {strings.Replace(lesson(id, 7), `"verdict": "40"`, `"verdict": "forty"`, 1), GatePromotionAdversary},
	} {
		_, err := ParseLesson(id, []byte(c.body))
		if err == nil {
			t.Errorf("%s: a malformed promotion refuses", name)
			continue
		}
		if got := gateOf(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s", name, c.gate, got)
		}
	}
	if _, err := ParseLesson(id, []byte(strings.Replace(lesson(id, 7), `"adversarial": {"eval": "fix-the-check", "verdict": "40"}, `, "", 1))); err == nil {
		t.Fatal("adversarial is required for every carrier")
	}
	if c, ok := ParseCitation("c-1@12"); !ok || c.Contract != "c-1" || c.Position != 12 || c.String() != "c-1@12" {
		t.Fatalf("a citation splits at the last @: %+v %v", c, ok)
	}
	for _, bad := range []string{"c-1", "@3", "c-1@", "c-1@-1", "c-1@x"} {
		if _, ok := ParseCitation(bad); ok {
			t.Errorf("%q is not a citation", bad)
		}
	}
	if err := NewGateError("nonesuch.gate", LessonVerb, id, "x"); err == nil {
		t.Fatal("an unregistered gate is refused")
	} else if _, isGate := err.(*GateError); isGate {
		t.Fatal("an unregistered gate never yields a GateError")
	}
	if len(Gates()) < 30 {
		t.Fatalf("the registry lists the gates: %v", Gates())
	}
}

// The fold renders the dead ends, counts a malformed raw fact as an
// anomaly, and on a bare chain (no keyring, so no proposal can have
// passed the boundary) counts every proposal and every contest an
// anomaly and folds every promotion unbound: a hypothesis folds only
// once admission re-judges it (review finding on the item 1 PR), and
// the admitted fold, its promotion and its contest are drilled where
// a boundary stands, in internal/admit. Facts before the keyring
// boundary and lifecycle verbs alike are ignored.
func TestFoldRendersStagesAndCountsAnomalies(t *testing.T) {
	holder, curator, observer := testKey(1), testKey(2), testKey(3)
	claim, other := "retry once", "retry twice"
	id, otherID := HypothesisID(claim, nil), HypothesisID(other, nil)
	dead := `{"fence": "3", "tried": "x", "outcome": "y", "condition": "z", "environment": "w"}`
	records := []*event.Record{
		signed(t, holder, version.Seed1, DeadEndVerb, "c-1", dead),
		signed(t, holder, version.Seed1, DeadEndVerb, "c-2", dead),
		signed(t, holder, version.Seed1, DeadEndVerb, "c-2", `{"fence": "3"}`),
		signed(t, holder, version.Protocol, DeadEndVerb, "c-3", dead),
		signed(t, curator, version.Seed1, HypothesisVerb, id, proposal(claim, []string{"c-1@0", "c-2@1"}, nil)),
		signed(t, curator, version.Seed1, HypothesisVerb, id, proposal(claim, []string{"c-1@0", "c-2@1"}, nil)),
		signed(t, holder, version.Seed1, "claim.taken", "c-1", `{}`),
		signed(t, observer, version.Seed1, LessonVerb, "h-000000000000", lesson("h-000000000000", 9)),
		signed(t, observer, version.Seed1, LessonVerb, id, lesson(id, 4)),
		signed(t, curator, version.Seed1, HypothesisVerb, otherID, proposal(other, []string{"c-1@0", "c-2@1"}, nil)),
		signed(t, curator, version.Seed1, ContestVerb, otherID, fmt.Sprintf(`{"hypothesis": "%s@9", "evidence": ["c-3@3"], "reason": "no"}`, otherID)),
		signed(t, curator, version.Seed1, ContestVerb, id, fmt.Sprintf(`{"hypothesis": "%s@4", "evidence": ["c-3@3"], "reason": "no"}`, id)),
		signed(t, curator, version.Seed1, ContestVerb, "h-000000000000", `{"hypothesis": "h-000000000000@0", "evidence": ["c-3@3"], "reason": "no"}`),
	}
	st := Fold(records)
	if len(st.DeadEnds["c-1"]) != 1 || len(st.DeadEnds["c-2"]) != 1 || len(st.DeadEnds["c-3"]) != 0 {
		t.Fatalf("dead ends fold by contract, malformed and pre-boundary ones excluded: %+v", st.DeadEnds)
	}
	if st.DeadEnds["c-1"][0].Actor == "" || st.DeadEnds["c-1"][0].Pos != 0 || st.DeadEnds["c-1"][0].Condition != "z" {
		t.Fatalf("a dead end carries its actor, position and fields: %+v", st.DeadEnds["c-1"][0])
	}
	if _, ok := st.Hypothesis(id); ok || len(st.HypothesisIDs()) != 0 || st.Contested(id) || st.Contested(otherID) {
		t.Fatalf("no proposal passed a boundary, so none folds as a hypothesis and nothing is contested: %v", st.HypothesisIDs())
	}
	if len(st.Lessons) != 0 || len(st.Unbound) != 2 || st.Unbound[0].Pos != 7 || st.Unbound[1].Pos != 8 || len(st.LessonsOf(id)) != 0 {
		t.Fatalf("a promotion citing no admitted hypothesis is unbound, never a lesson: %+v %+v", st.Lessons, st.Unbound)
	}
	// One malformed dead end, three proposals that passed no boundary,
	// three contests of nothing.
	if st.Anomalies != 7 {
		t.Fatalf("malformed and unadmitted raw facts count anomalies, %d", st.Anomalies)
	}
	if !st.Any() || Fold(nil).Any() {
		t.Fatal("Any reports whether the prefix carries a curation fact")
	}
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(Candidates(st, table.FoldRecords(records), "")) != 0 {
		t.Fatal("an unbound promotion is no candidate")
	}
}

// A promotion names a file inside the validated store and nothing
// else: an anchor whose path climbs out of it matches the anchor
// grammar and must still refuse (review finding on the item 1 PR).
func TestLessonAnchorsStayUnderTheStore(t *testing.T) {
	for anchor, want := range map[string]bool{
		LessonsDir + "/retry.md @ 0123456":         true,
		LessonsDir + "/nested/retry.md @ 0123456":  true,
		LessonsDir + "/../../secrets.md @ 0123456": false,
		LessonsDir + "/a/../../../x.md @ 0123456":  false,
		LessonsDir + "/./retry.md @ 0123456":       false,
		LessonsDir + "//retry.md @ 0123456":        false,
		LessonsDir + " @ 0123456":                  false,
		LessonsDir + "/ @ 0123456":                 false,
		"/" + LessonsDir + "/retry.md @ 0123456":   false,
		LessonsDir + "x/retry.md @ 0123456":        false,
		"knowledge/other/retry.md @ 0123456":       false,
		"../" + LessonsDir + "/retry.md @ 0123456": false,
	} {
		if got := UnderLessonsDir(anchor); got != want {
			t.Errorf("UnderLessonsDir(%q) = %v, want %v", anchor, got, want)
		}
	}
	id := HypothesisID("retry once", nil)
	body := strings.Replace(lesson(id, 4), LessonsDir+"/x.md", LessonsDir+"/../../x.md", 1)
	if _, err := ParseLesson(id, []byte(body)); err == nil || gateOf(t, err) != GatePromotionShape || !strings.Contains(err.Error(), "climbs out") {
		t.Fatalf("a traversal path refuses at the shape gate naming the store: %v", err)
	}
}

func TestLintRequiresTheFrontmatterKeys(t *testing.T) {
	good := "---\nhypothesis: h-0123456789ab@4\napplies-when: {\"routing\": \"core\"}\nsupport: c-1@4, c-2@9\nprovenance: plans/x.md @ 0123456\nlast-validated: 2026-09-02T00:00:00Z\nexpires: 2026-12-02T00:00:00Z\ncarrier: knowledge\n---\n\n# Lesson\n"
	if err := Lint([]byte(good)); err != nil {
		t.Fatalf("a complete frontmatter lints: %v", err)
	}
	for name, body := range map[string]string{
		"no-block": "# Lesson\n",
		"unclosed": "---\nhypothesis: x\n",
		"missing":  "---\nhypothesis: h-0123456789ab@4\napplies-when: {}\n---\n",
		"empty":    strings.Replace(good, "expires: 2026-12-02T00:00:00Z", "expires:", 1),
	} {
		if err := Lint([]byte(body)); err == nil {
			t.Errorf("%s: an incomplete frontmatter refuses", name)
		}
	}
}

// conformance: plans/os-96850e5a.md AC5 — the file half refuses each
// disagreement between the file, the fact and the repository at a
// registered gate, and passes a lesson that agrees with all three.
func TestLintFileIsTheGatesFileHalf(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	hardenGitRepo(t, repo)
	if err := os.MkdirAll(filepath.Join(repo, LessonsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "plans", "x.md"), nil, 0o644); err != nil {
		if err := os.MkdirAll(filepath.Join(repo, "plans"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "plans", "x.md"), []byte("plan\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "plan")
	planCommit := git("rev-parse", "HEAD")
	id := HypothesisID("retry once", nil)
	h := &HypothesisFact{ID: id, Pos: 4, AppliesWhen: AppliesWhen{Routing: "core"}, Support: []string{"c-1@4", "c-2@9"}, Provenance: []string{"plans/x.md @ " + planCommit}}
	body := "---\nhypothesis: " + id + "@4\napplies-when: {\"routing\": \"core\"}\nsupport: c-1@4, c-2@9\nprovenance: plans/x.md @ " + planCommit + "\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: knowledge\n---\n\n# Retry once\n"
	path := LessonsDir + "/retry.md"
	if err := os.WriteFile(filepath.Join(repo, path), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "--quiet", "-m", "lesson")
	anchor := git("rev-parse", "HEAD")
	fact := LessonFact{Lesson: path + " @ " + anchor, Hypothesis: id + "@4", Carrier: "knowledge", Digest: Digest([]byte(body)),
		LastValidated: "2026-09-01T00:00:00Z", Expires: "2026-12-01T00:00:00Z"}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := LintFile(repo, []byte(body), fact, h, now); err != nil {
		t.Fatalf("a lesson that agrees with the fact, the hypothesis and the repository lints: %v", err)
	}
	if err := Verify(repo, fact); err != nil {
		t.Fatalf("the fact resolves: %v", err)
	}
	// A later commit keeps the anchor an ancestor; a rewritten file at
	// the same anchor cannot exist, so the digest check reads the
	// anchor's bytes, not the working tree's.
	if err := os.WriteFile(filepath.Join(repo, path), []byte(body+"\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "--quiet", "-am", "edit")
	if err := LintFile(repo, []byte(body), fact, h, now); err != nil {
		t.Fatalf("the digest binds the anchor's bytes: %v", err)
	}
	altered := fact
	altered.Digest = Digest([]byte(body + "\nedited\n"))
	if err := LintFile(repo, []byte(body), altered, h, now); err == nil || gateOf(t, err) != GateLintDigest {
		t.Fatalf("a fact whose digest is not the anchor's bytes refuses at the digest gate: %v", err)
	}
	// The fact's stamps are the reviewed file's: a promotion that
	// recorded other dates carries dates nobody reviewed (review
	// finding on the item 3 PR).
	drifted := fact
	drifted.Expires = "2027-06-01T00:00:00Z"
	if err := LintFile(repo, []byte(body), drifted, h, now); err == nil || gateOf(t, err) != GateLintStamps || !strings.Contains(err.Error(), "differ from the fact") {
		t.Fatalf("a fact whose stamps differ from the frontmatter refuses at the stamps gate: %v", err)
	}
	unmerged := fact
	unmerged.Lesson = path + " @ " + strings.Repeat("f", 40)
	if err := Verify(repo, unmerged); err == nil {
		t.Fatal("an anchor that is no ancestor does not resolve")
	}
	if err := Verify(repo, altered); err == nil {
		t.Fatal("a fact whose digest is not the anchor's bytes does not resolve")
	}
	// A commit that exists and is not an ancestor of the head: the
	// promotion PR was opened and never merged here. Its bytes resolve
	// and hash, so the ancestry check alone stands between it and the
	// delivery (the mutation dropping that check surfaces it).
	git("checkout", "--quiet", "-b", "side")
	sideBody := body + "\nside\n"
	if err := os.WriteFile(filepath.Join(repo, path), []byte(sideBody), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "--quiet", "-am", "side")
	side := git("rev-parse", "HEAD")
	git("checkout", "--quiet", "main")
	unmergedReal := fact
	unmergedReal.Lesson = path + " @ " + side
	unmergedReal.Digest = Digest([]byte(sideBody))
	if err := Verify(repo, unmergedReal); err == nil || !strings.Contains(err.Error(), "not an ancestor") {
		t.Fatalf("an anchor on an unmerged branch does not resolve, naming the ancestry: %v", err)
	}
	// The lint judges one revision, the bytes at the promoted anchor:
	// a working file that differs from them refuses at the digest gate
	// whatever its frontmatter says, so a valid frontmatter in a later
	// edit cannot stand in for the promoted one (review finding on the
	// task PR). Each bent case is therefore promoted as its own anchor.
	if err := LintFile(repo, []byte(body+"\nedited\n"), fact, h, now); err == nil || gateOf(t, err) != GateLintDigest {
		t.Fatalf("a working file that differs from the promoted bytes refuses at the digest gate: %v", err)
	}
	promote := func(bent string) LessonFact {
		t.Helper()
		if bent == body {
			return fact
		}
		if err := os.WriteFile(filepath.Join(repo, path), []byte(bent), 0o644); err != nil {
			t.Fatal(err)
		}
		git("commit", "--quiet", "-am", "bent")
		return LessonFact{Lesson: path + " @ " + git("rev-parse", "HEAD"), Hypothesis: id + "@4", Carrier: "knowledge", Digest: Digest([]byte(bent)),
			LastValidated: "2026-09-01T00:00:00Z", Expires: "2026-12-01T00:00:00Z"}
	}
	for _, c := range []struct {
		name string
		body string
		hyp  *HypothesisFact
		now  time.Time
		gate string
	}{
		{"frontmatter", "# no block\n", h, now, GateLintFrontmatter},
		{"hypothesis", strings.Replace(body, id+"@4", "h-000000000000@4", 1), h, now, GateLintHypothesis},
		{"applies", strings.Replace(body, `{"routing": "core"}`, `{"routing": "other"}`, 1), h, now, GateLintAppliesWhen},
		{"applies-bad", strings.Replace(body, `{"routing": "core"}`, `flaky`, 1), h, now, GateLintAppliesWhen},
		{"support", strings.Replace(body, "c-2@9", "c-2@10", 1), h, now, GateLintSupport},
		{"provenance", strings.Replace(body, "plans/x.md @ "+planCommit, "plans/y.md @ "+planCommit, 1), h, now, GateLintProvenance},
		{"stale", body, h, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), GateLintStamps},
		{"expired", body, h, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), GateLintStamps},
		{"carrier", strings.Replace(body, "carrier: knowledge", "carrier: role", 1), h, now, GateLintCarrier},
	} {
		bentFact := promote(c.body)
		err := LintFile(repo, []byte(c.body), bentFact, c.hyp, c.now)
		if err == nil {
			t.Errorf("%s: refuses", c.name)
			continue
		}
		if got := gateOf(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s (%v)", c.name, c.gate, got, err)
		}
	}
}

// Every file in the store honors the contract its README states.
func TestEveryLessonInTheStoreLints(t *testing.T) {
	dir := filepath.Join("..", "..", "knowledge", "lessons")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	readme := false
	for _, e := range entries {
		if e.Name() == "README.md" {
			readme = true
			continue
		}
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := Lint(b); err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
	}
	if !readme {
		t.Fatal("the store's README states the frontmatter contract")
	}
}

// hardenGitRepo disables auto-gc in a fixture repository so a detached
// collector never outlives the test that made it (the fixture guard in
// internal/gitref).
func hardenGitRepo(t testing.TB, repo string) {
	t.Helper()
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"gc.autoDetach", "false"}, {"receive.autoGC", "false"},
		{"core.autocrlf", "false"}, // the ledger is LF-only on every platform (spec/platform.md)
		{"core.eol", "lf"}} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("hardening %s (%s): %v %s", repo, kv[0], err, out)
		}
	}
}

// The gate table in spec/curation.md is pinned to the registry
// in both directions: a gate the rules register must be in the spec,
// and a gate the spec names must be registered, so neither side can
// drift alone (plans/os-96850e5a.md D4; the poisoning drill derives
// its coverage from the same registry).
func TestGateTableMatchesTheRegistry(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "curation.md"))
	if err != nil {
		t.Fatal(err)
	}
	spec := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		if strings.Contains(name, ".") && !strings.Contains(name, " ") {
			spec[name] = true
		}
	}
	registered := map[string]bool{}
	for _, g := range Gates() {
		registered[g] = true
		if !spec[g] {
			t.Errorf("gate %s is registered and the spec's table does not name it", g)
		}
	}
	for g := range spec {
		if !registered[g] {
			t.Errorf("the spec's table names gate %s and no rule registers it", g)
		}
	}
	if len(registered) == 0 || len(spec) == 0 {
		t.Fatal("an empty registry or table would pass vacuously")
	}
}

// conformance: plans/os-0d537fbd.md D1, D2, D3, D4 — the shapes the
// retirement verbs carry, expiry at an instant, and the structure and
// dedup lints, each refusing at a registered gate naming the part.
func TestRetirementShapesRefuseAtTheirGates(t *testing.T) {
	id := HypothesisID("retry once", nil)
	good := fmt.Sprintf(`{"lesson": "%s/x.md @ 0123456", "hypothesis": "%s@4", "reason": "regression", "pr": "pr/9 @ 0123456"}`, LessonsDir, id)
	if _, err := ParseRetirement(id, []byte(good)); err != nil {
		t.Fatalf("a regression retirement with its pr parses: %v", err)
	}
	if _, err := ParseRetirement(id, []byte(fmt.Sprintf(`{"lesson": "%s/x.md @ 0123456", "hypothesis": "%s@4", "reason": "superseded", "superseded_by": "9"}`, LessonsDir, id))); err != nil {
		t.Fatalf("a superseded retirement with its position parses: %v", err)
	}
	if _, err := ParseRetirement(id, []byte(fmt.Sprintf(`{"lesson": "%s/x.md @ 0123456", "hypothesis": "%s@4", "reason": "expired"}`, LessonsDir, id))); err != nil {
		t.Fatalf("an expired retirement with neither field parses: %v", err)
	}
	for name, c := range map[string]struct{ body, gate string }{
		"unknown key":        {strings.Replace(good, `"reason"`, `"note": "x", "reason"`, 1), GateRetireShape},
		"bare lesson":        {strings.Replace(good, "/x.md @ 0123456", "/x.md", 1), GateRetireShape},
		"outside the store":  {strings.Replace(good, LessonsDir+"/x.md", LessonsDir+"/../x.md", 1), GateRetireShape},
		"other subject":      {strings.Replace(good, id+"@4", "h-000000000000@4", 1), GateRetireShape},
		"unknown reason":     {strings.Replace(good, `"regression"`, `"tired"`, 1), GateRetireReason},
		"regression no pr":   {strings.Replace(good, `, "pr": "pr/9 @ 0123456"`, "", 1), GateRetireReason},
		"regression with by": {strings.Replace(good, `"pr": "pr/9 @ 0123456"`, `"pr": "pr/9 @ 0123456", "superseded_by": "9"`, 1), GateRetireReason},
		"superseded no by":   {strings.Replace(good, `"regression", "pr": "pr/9 @ 0123456"`, `"superseded"`, 1), GateRetireReason},
		"superseded with pr": {strings.Replace(good, `"regression"`, `"superseded"`, 1), GateRetireReason},
		"expired with pr":    {strings.Replace(good, `"regression"`, `"expired"`, 1), GateRetireReason},
		"pr not anchored":    {strings.Replace(good, "pr/9 @ 0123456", "pr/9", 1), GateRetireShape},
		"by not a position":  {strings.Replace(good, `"regression", "pr": "pr/9 @ 0123456"`, `"superseded", "superseded_by": "nine"`, 1), GateRetireShape},
	} {
		_, err := ParseRetirement(id, []byte(c.body))
		if err == nil {
			t.Errorf("%s: refuses", name)
			continue
		}
		if got := gateOf(t, err); got != c.gate {
			t.Errorf("%s: refuses at %s, got %s (%v)", name, c.gate, got, err)
		}
	}
	var inc *transition.IncompleteError
	if _, err := ParseRetirement(id, []byte(`{"lesson": "", "hypothesis": "", "reason": ""}`)); !errors.As(err, &inc) {
		t.Fatalf("empty fields refuse as incomplete naming them: %v", err)
	}
	dead := `{"deadend": "c-1@7", "environment": "ci-runner/v1", "reason": "the runner moved"}`
	if _, err := ParseDeadEndRetirement(DeadEndRetireVerb, "c-1", []byte(dead)); err != nil {
		t.Fatalf("a dead-end retirement parses: %v", err)
	}
	for name, body := range map[string]string{
		"unknown key":   strings.Replace(dead, `"reason"`, `"note": "x", "reason"`, 1),
		"bad citation":  strings.Replace(dead, "c-1@7", "c-1", 1),
		"other subject": strings.Replace(dead, "c-1@7", "c-2@7", 1),
	} {
		if _, err := ParseDeadEndRetirement(DeadEndUnretireVerb, "c-1", []byte(body)); err == nil || gateOf(t, err) != GateDeadEndRetireShape {
			t.Errorf("%s: refuses at the shape gate: %v", name, err)
		}
	}
	if _, err := ParseDeadEndRetirement(DeadEndRetireVerb, "c-1", []byte(`{"deadend": "c-1@7", "environment": "", "reason": ""}`)); !errors.As(err, &inc) {
		t.Fatalf("empty fields refuse as incomplete: %v", err)
	}
}

// Expiry is derived at an instant and compared with >=: at the stamp
// the lesson is expired, a second before it is not, and a stamp that
// does not parse counts as expired.
func TestExpiryIsDerivedAtAnInstant(t *testing.T) {
	l := LessonFact{Expires: "2026-12-01T00:00:00Z"}
	at := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if !Expired(l, at) {
		t.Fatal("at the stamp the lesson is expired")
	}
	if Expired(l, at.Add(-time.Second)) {
		t.Fatal("a second before the stamp it is not")
	}
	if !Expired(LessonFact{Expires: "never"}, at) {
		t.Fatal("a stamp that does not parse counts as expired")
	}
	if !stampAfter("2026-12-02T00:00:00Z", "2026-12-01T00:00:00Z") || stampAfter("2026-12-01T00:00:00Z", "2026-12-01T00:00:00Z") || stampAfter("x", "2026-12-01T00:00:00Z") {
		t.Fatal("stampAfter is strict and refuses what does not parse")
	}
}

// The structure lint requires exactly the known keys and the README's
// sections in order; the dedup lint names the second file citing one
// hypothesis.
func TestStructureAndDedupLints(t *testing.T) {
	id := HypothesisID("retry once", nil)
	head := "---\nhypothesis: " + id + "@4\napplies-when: {\"routing\": \"core\"}\nsupport: c-1@4, c-2@9\nprovenance: plans/x.md @ 0123456\nlast-validated: 2026-09-01T00:00:00Z\nexpires: 2026-12-01T00:00:00Z\ncarrier: knowledge\n---\n"
	good := head + "\n# Retry once\n\n## Claim\n\nretry once\n\n## Evidence\n\nc-1@4, c-2@9\n\n## Applies when\n\ncore\n"
	if err := LintStructure([]byte(good)); err != nil {
		t.Fatalf("the README's shape lints: %v", err)
	}
	for name, body := range map[string]string{
		"unknown key":     strings.Replace(good, "carrier: knowledge\n", "carrier: knowledge\nowner: me\n", 1),
		"missing section": strings.Replace(good, "## Evidence\n\nc-1@4, c-2@9\n\n", "", 1),
		"out of order":    strings.Replace(strings.Replace(good, "## Claim", "## Zz", 1), "## Applies when", "## Claim", 1) + "\n## Applies when\n",
	} {
		if err := LintStructure([]byte(body)); err == nil || gateOf(t, err) != GateLintStructure {
			t.Errorf("%s: refuses at the structure gate: %v", name, err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LintDuplicates(dir, nil); err != nil {
		t.Fatalf("one file per hypothesis lints: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte(strings.Replace(good, id+"@4", id+"@9", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LintDuplicates(dir, nil); err == nil || gateOf(t, err) != GateLintDuplicate || !strings.Contains(err.Error(), "b.md is the duplicate") {
		t.Fatalf("a second file citing the hypothesis refuses naming the duplicate: %v", err)
	}
}
