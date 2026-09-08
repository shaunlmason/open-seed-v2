package maintain_test

// The racing interleaving check (plans/os-873b5153.md D4; SEED-NEXT.md
// §II.6): duplicate execution is tolerated and the first VERIFIED
// success settles, so the settlement rule is a concurrency property and
// belongs to an enumeration rather than to hand-written sequences.
//
// Like the reconciliation model beside it, this enumerates over real
// records through the real admission rather than over an abstract fold,
// so no model-versus-code gap can open: every step is admitted by
// admit.Check as it is taken, and every fact a property reads is the
// real fold's.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/explore"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/maintain"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// raceDepth is the fast gate's size (plans/os-873b5153.md D7): 7 steps
// per trace is 2,007 terminal states, 70 of them settled, in about 2.5s.
// perf-scale.yml runs 9 (22,599 terminal states, 1,162 settled, about
// 33s) weekly, which is where the model found the settlement property's
// first counterexample.
var raceDepth = flag.Int("race-depth", 7, "steps per racing trace (plans/os-873b5153.md D7)")

const (
	raceSubject = "c-race"
	racers      = 2 // the declared cap
	zeros64     = "0000000000000000000000000000000000000000000000000000000000000000"
	zeros40     = "0000000000000000000000000000000000000000"
	minPacket   = `{"acceptance": ["done"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
)

// rstep is one move. Claims, submissions, verdicts and exits are per
// racer; the settlement and the reap are the lane's.
type rstep struct {
	name  string
	racer int
}

func (s rstep) String() string {
	if s.racer < 0 {
		return s.name
	}
	return fmt.Sprintf("%s(r%d)", s.name, s.racer)
}

// rcstate is one state: the records so far. Nothing here re-implements
// the fold; every read goes through the real one.
type rcstate struct {
	h       *rharness
	records []*event.Record
	steps   int
}

func (s *rcstate) Key() string {
	var b strings.Builder
	for _, r := range s.records[s.h.base:] {
		fmt.Fprintf(&b, "%s/%s/%x;", r.Event.Verb, r.Event.Actor[:6], sha256.Sum256(r.Event.Payload))
	}
	// The depth counter belongs in the key because Terminal() reads it:
	// a refused step is a no-op on the records and would otherwise
	// memoize back onto its own predecessor, losing every trace whose
	// tail is refusals (the reconciliation model's first draft did
	// exactly that).
	fmt.Fprintf(&b, "|depth=%d", s.steps)
	return b.String()
}

func (s *rcstate) Terminal() bool { return s.steps >= s.h.depth }

func (s *rcstate) fold() *transition.Fold { return s.h.table.FoldRecords(s.records) }

func (s *rcstate) subject() transition.SubjectState {
	st, _ := s.fold().State(raceSubject)
	return st
}

func (s *rcstate) Enabled() []rstep {
	if s.Terminal() {
		return nil
	}
	var out []rstep
	for r := 0; r < racers; r++ {
		out = append(out,
			rstep{"claim", r},
			rstep{"submit", r},
			rstep{"verdict.pass", r},
			rstep{"verdict.fail", r},
			rstep{"release", r},
		)
	}
	return append(out,
		rstep{"request", -1},
		rstep{"settle", -1},
	)
}

func (s *rcstate) clone() *rcstate {
	c := *s
	c.records = s.records[:len(s.records):len(s.records)]
	c.steps = s.steps + 1
	return &c
}

func (s *rcstate) Apply(st rstep) *rcstate {
	n := s.clone()
	rec, ok := s.h.draft(s, st)
	if !ok {
		return n
	}
	ctx, err := admit.ContextOver(s.records, admit.WithDeclaration(s.h.decl))
	if err != nil {
		return n
	}
	if admit.Check(ctx, rec) != nil {
		return n
	}
	n.records = append(n.records, rec)
	return n
}

// rharness stages one racing-squad contract standing ready, with two
// claim-capable racers, a verifier and an observer enrolled.
type rharness struct {
	t      *testing.T
	table  *transition.Table
	decl   *posture.Config
	keys   map[string]ed25519.PrivateKey
	fps    map[string]string
	base   int
	depth  int
	prefix []*event.Record
}

func racerName(i int) string { return fmt.Sprintf("racer%d", i) }

func rstage(t *testing.T, depth int) *rharness {
	t.Helper()
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	h := &rharness{t: t, table: table, depth: depth,
		keys: map[string]ed25519.PrivateKey{}, fps: map[string]string{}}
	// The declaration is what turns racing on: a squad with a racers cap
	// and the cost in the operator's words (spec: the opt-in is explicit
	// and per squad, never an accident of connectivity).
	h.decl = &posture.Config{
		Posture: "cooperative",
		Guardrails: &posture.Guardrails{
			Squads: map[string]posture.SquadGuardrail{
				"racing": {Racing: &posture.Racing{Racers: racers, Cost: "duplicate execution, declared"}},
			},
		},
	}
	h.keys["root"] = rfixtureKey(1)
	lanes := []struct{ name, cap string }{
		{racerName(0), keyring.CapClaim},
		{racerName(1), keyring.CapClaim},
		{"verifier", keyring.CapVerdict},
		{"observer", keyring.CapObserver},
		// The reaper: dispatch-granted, as the maintenance lane is what
		// reaps a settled-out claim (internal/admit's racing drills).
		{"maint", keyring.CapDispatch},
	}
	for i, l := range lanes {
		h.keys[l.name] = rfixtureKey(byte(i + 2))
	}
	for name, k := range h.keys {
		fp, err := event.Fingerprint(k.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		h.fps[name] = fp
	}
	gen, err := genesis.Build(h.keys["root"], nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	h.prefix = []*event.Record{gen}
	add := func(lane, v, verb, subject, payload string) {
		t.Helper()
		ctx, err := admit.ContextOver(h.prefix, admit.WithDeclaration(h.decl))
		if err != nil {
			t.Fatalf("staging context: %v", err)
		}
		rec, err := event.Sign(event.Event{
			V: v, TS: h.ts(len(h.prefix)), Actor: h.fps[lane], Verb: verb,
			Subject: subject, Payload: json.RawMessage(payload), Prev: ctx.Tip,
		}, h.keys[lane])
		if err != nil {
			t.Fatal(err)
		}
		if err := admit.Check(ctx, rec); err != nil {
			t.Fatalf("staging %s by %s: %v", verb, lane, err)
		}
		h.prefix = append(h.prefix, rec)
	}
	for i, v := range version.Supported() {
		if i == 0 {
			continue
		}
		add("root", version.Supported()[i-1], ledger.UpgradeVerb, "system", `{"to": "`+v+`"}`)
	}
	active := h.active()
	for _, l := range lanes {
		pub := h.keys[l.name].Public().(ed25519.PublicKey)
		add("root", active, keyring.VerbEnrolled, h.fps[l.name],
			fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), l.name))
		add("root", active, keyring.VerbGranted, h.fps[l.name], `{"capability": "`+l.cap+`"}`)
	}
	// Trivial tier: the plan gate and the sealed-checks gate both exempt
	// it, which keeps the alphabet about racing rather than about the
	// gates the reconciliation model already covers.
	add("root", active, "intent.filed", raceSubject,
		`{"intent": "racing model", "tier": "trivial", "budget": "small", "routing": "racing"}`)
	add("root", active, "contract.specified", raceSubject,
		`{"acceptance": {"ref": "specs/race.md @ abc1234", "executable": false}}`)
	h.base = len(h.prefix)
	return h
}

func (h *rharness) ts(i int) string {
	return time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
}

func (h *rharness) active() string {
	return version.Supported()[len(version.Supported())-1]
}

// draft builds the record a step proposes, reading its citations from
// the real fold.
func (h *rharness) draft(s *rcstate, st rstep) (*event.Record, bool) {
	sub := s.subject()
	lane, verb, payload := "", "", ""
	switch st.name {
	case "claim":
		lane, verb, payload = racerName(st.racer), "claim.taken", `{}`
	case "submit":
		fence, holds := sub.HolderFence(h.fps[racerName(st.racer)])
		if !holds {
			return nil, false
		}
		lane, verb = racerName(st.racer), "submission.made"
		payload = fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, minPacket)
	case "verdict.pass", "verdict.fail":
		// One verdict per submission: the racer's own.
		sf, ok := submissionOf(sub, h.fps[racerName(st.racer)])
		if !ok {
			return nil, false
		}
		outcome := "pass"
		if st.name == "verdict.fail" {
			outcome = "fail"
		}
		lane, verb = "verifier", transition.VerdictRenderedVerb
		payload = fmt.Sprintf(`{"verdict": %q, "receipt": %q, "submission": "%d", "independence": "L1"}`,
			outcome, zeros64, sf.Pos)
	case "milestone":
		// A probe rather than a move: the plainly non-exit act, used to
		// check what a settled-out racer may no longer do. submission.made
		// reads as an exit (it closes the window), so it is the wrong
		// instrument for that arm.
		fence, holds := sub.HolderFence(h.fps[racerName(st.racer)])
		if !holds {
			return nil, false
		}
		lane, verb = racerName(st.racer), "progress.milestone"
		payload = fmt.Sprintf(`{"fence": "%d", "count": 1, "step": "late"}`, fence)
	case "release":
		fence, holds := sub.HolderFence(h.fps[racerName(st.racer)])
		if !holds {
			return nil, false
		}
		lane, verb = racerName(st.racer), "claim.released"
		payload = fmt.Sprintf(`{"fence": "%d", "packet": %s}`, fence, minPacket)
	case "request":
		if sub.Verdict == nil {
			return nil, false
		}
		lane, verb = "root", transition.MergeRequestedVerb
		payload = fmt.Sprintf(`{"verdict": "%d"}`, sub.Verdict.Pos)
	case "settle":
		lane, verb = "observer", transition.MergeObservedVerb
		payload = fmt.Sprintf(`{"merged": %q, "pr": "race"}`, zeros40)
	default:
		return nil, false
	}
	return h.sign(lane, verb, payload, s.records), true
}

// sign builds one signed record on the tip of the records given.
func (h *rharness) sign(lane, verb, payload string, records []*event.Record) *event.Record {
	h.t.Helper()
	tip, err := records[len(records)-1].Event.Hash()
	if err != nil {
		h.t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{
		V: h.active(), TS: h.ts(len(records)), Actor: h.fps[lane], Verb: verb,
		Subject: raceSubject, Payload: json.RawMessage(payload), Prev: tip,
	}, h.keys[lane])
	if err != nil {
		h.t.Fatal(err)
	}
	return rec
}

// submissionOf finds the submission a racer made in the current window.
func submissionOf(s transition.SubjectState, signer string) (transition.SubmissionFact, bool) {
	for _, sf := range s.Submissions {
		if sf.Signer == signer {
			return sf, true
		}
	}
	return transition.SubmissionFact{}, false
}

func rfixtureKey(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func rtraceText(path []rstep) string {
	parts := make([]string, len(path))
	for i, s := range path {
		parts[i] = s.String()
	}
	return strings.Join(parts, " ")
}

// conformance: charter §II.6 racing (plans/os-873b5153.md D4). Every
// interleaving of two racers claiming, submitting, drawing verdicts,
// exiting and settling is enumerated, and four properties hold.
func TestRacingInterleavings(t *testing.T) {
	h := rstage(t, *raceDepth)
	settlements, capBreaches, terminals := 0, 0, 0
	sawTwoClaims, sawSettlement, sawSettledOutRefusal, sawReap := false, false, false, false

	res, err := explore.Walk[*rcstate, rstep](&rcstate{h: h, records: h.prefix}, explore.Hooks[*rcstate, rstep]{
		AtState: func(s *rcstate, path []rstep) {
			sub := s.subject()
			// P1: the declared cap is never exceeded. Racing admits a
			// further claim BELOW the cap and refuses at it, so more
			// active claims than racers would be the boundary failing.
			if len(sub.Claims) > racers {
				capBreaches++
				t.Fatalf("P1: %d active claims exceed the declared cap of %d\n%s",
					len(sub.Claims), racers, rtraceText(path))
			}
			if len(sub.Claims) == racers {
				sawTwoClaims = true
			}
			// P3: after settlement, a settled-out racer's own deliberate
			// exit still admits while every other act refuses. The
			// asymmetry is the rule's, and this checks both arms.
			if sub.RaceSettled == nil {
				return
			}
			sawSettlement = true
			ctx, err := admit.ContextOver(s.records, admit.WithDeclaration(h.decl))
			if err != nil {
				return
			}
			for r := 0; r < racers; r++ {
				fp := h.fps[racerName(r)]
				if _, holds := sub.HolderFence(fp); !holds {
					continue
				}
				// The non-exit arm: a milestone by a settled-out racer.
				if rec, ok := h.draft(s, rstep{"milestone", r}); ok {
					var settled *admit.RaceSettledError
					if err := admit.Check(ctx, rec); err == nil {
						t.Fatalf("P3: a settled-out racer's milestone admitted after the settlement at position %d\n%s",
							*sub.RaceSettled, rtraceText(path))
					} else if asRaceSettled(err, &settled) {
						sawSettledOutRefusal = true
					}
				}
				// The exit arm: its own release must still admit.
				if rec, ok := h.draft(s, rstep{"release", r}); ok {
					if err := admit.Check(ctx, rec); err != nil {
						t.Fatalf("P3: a settled-out racer's own deliberate exit was refused, so its packet could never be written: %v\n%s",
							err, rtraceText(path))
					}
				}
			}
		},
		AtTerminal: func(s *rcstate, path []rstep) {
			terminals++
			sub := s.subject()
			// P2: at most one settlement in any trace, and each one
			// standing on the pass verdict its own request cited. A race
			// whose every submission drew a fail settles nothing, which is
			// first-VERIFIED-success working (review on #366).
			n, request := 0, -1
			for i := h.base; i < len(s.records); i++ {
				switch s.records[i].Event.Verb {
				case transition.MergeRequestedVerb:
					request = i
				case transition.MergeObservedVerb:
					n++
				}
			}
			if n > 1 {
				t.Fatalf("P2: %d settlements in one trace\n%s", n, rtraceText(path))
			}
			if n == 1 {
				settlements++
				// What the settlement rests on is the position its
				// request CITED, not whichever verdict landed last: with
				// two racers a later fail on the LOSER's submission
				// follows the winner's pass without unseating it, and
				// reading the subject's singular verdict fact calls that
				// authentic chain unverified. The enumeration found it at
				// race-depth 9: claim(r0) claim(r1) submit(r0)
				// verdict.pass(r0) submit(r1) request verdict.fail(r1)
				// settle.
				if request < 0 {
					t.Fatalf("P2: a settlement with no request before it\n%s", rtraceText(path))
				}
				var cite struct {
					Verdict string `json:"verdict"`
				}
				if err := json.Unmarshal(s.records[request].Event.Payload, &cite); err != nil {
					t.Fatalf("P2: the request payload does not decode: %v\n%s", err, rtraceText(path))
				}
				pos, err := strconv.Atoi(cite.Verdict)
				if err != nil || pos < 0 || pos >= len(s.records) {
					t.Fatalf("P2: the request cites %q, which is no position in this chain\n%s", cite.Verdict, rtraceText(path))
				}
				cited := s.records[pos]
				var v struct {
					Verdict string `json:"verdict"`
				}
				if err := json.Unmarshal(cited.Event.Payload, &v); err != nil {
					t.Fatalf("P2: the cited record does not decode: %v\n%s", err, rtraceText(path))
				}
				if cited.Event.Verb != transition.VerdictRenderedVerb || v.Verdict != "pass" {
					t.Fatalf("P2: a settlement stands on %s at position %d, not on a pass verdict\n%s",
						cited.Event.Verb, pos, rtraceText(path))
				}
			}
			// P5: an authentic settlement is no anomaly. What the fold
			// and the classifier read as the chain's verdict must be the
			// position the request cited, exactly as admission and P2
			// read it: a settlement the boundary took, counted as an
			// anomaly or reported as a divergence, is the two halves of
			// one rule disagreeing (charter §II.10).
			if n == 1 && sub.Anomalies != 0 {
				t.Fatalf("P5: an admitted settlement counted %d anomalies\n%s", sub.Anomalies, rtraceText(path))
			}
			if n == 1 {
				for _, f := range reconcile.Subject(raceSubject, sub) {
					switch f.Class {
					case reconcile.ClassMergeWithoutVerdict, reconcile.ClassChainSkipped:
						t.Fatalf("P5: an admitted settlement is classified %s: %s\n%s",
							f.Class, f.Detail, rtraceText(path))
					}
				}
			}
			// P4: the reaper leaves no active claim of a settled race,
			// and the packet it writes names the settlement. The reap is
			// TAKEN here, not imagined: each record goes through the real
			// admission and onto the chain, and the refold is what says
			// the claims are gone (review on #370: composing a packet and
			// calling it a reap would leave an interleaving-specific
			// failure in the reaper's admission path invisible). It runs
			// from the terminal state rather than as a step in the
			// alphabet, which keeps the enumeration exhaustive at a
			// usable depth while still exercising every settled trace.
			if sub.RaceSettled == nil {
				return
			}
			records := s.records
			for _, c := range sub.Claims {
				payload, err := maintain.RaceReapPacket(sub, c.Fence, *sub.RaceSettled)
				if err != nil {
					t.Fatalf("P4: the reaper could not compose a packet for the settled-out claim at fence %d: %v\n%s",
						c.Fence, err, rtraceText(path))
				}
				if !strings.Contains(string(payload), fmt.Sprintf("%d", *sub.RaceSettled)) {
					t.Fatalf("P4: the reap packet for fence %d does not name the settlement at position %d\n%s",
						c.Fence, *sub.RaceSettled, rtraceText(path))
				}
				rec := h.sign("maint", "claim.reaped", string(payload), records)
				ctx, err := admit.ContextOver(records, admit.WithDeclaration(h.decl))
				if err != nil {
					t.Fatalf("P4: %v\n%s", err, rtraceText(path))
				}
				if err := admit.Check(ctx, rec); err != nil {
					t.Fatalf("P4: the reap of the settled-out claim at fence %d was refused: %v\n%s",
						c.Fence, err, rtraceText(path))
				}
				records = append(records, rec)
				sawReap = true
			}
			reaped, _ := h.table.FoldRecords(records).State(raceSubject)
			if len(reaped.Claims) != 0 {
				t.Fatalf("P4: %d claims of a settled race survived the reaper\n%s",
					len(reaped.Claims), rtraceText(path))
			}
			if reaped.State != sub.State {
				t.Fatalf("P4: the reap moved the subject from %s to %s\n%s",
					sub.State, reaped.State, rtraceText(path))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("racing: %d states, %d terminal, %d settled", res.States, terminals, settlements)

	// The instrument must have exercised what it claims to check: a
	// property that never fired is a test that proves nothing.
	if !sawTwoClaims {
		t.Error("no state reached the declared cap of two active claims: the enumeration never raced")
	}
	if !sawSettlement {
		t.Error("no trace settled: the settlement properties were never exercised")
	}
	if !sawSettledOutRefusal {
		t.Error("no settled-out act was refused with RaceSettledError: P3's non-exit arm never fired")
	}
	if !sawReap {
		t.Error("no settled race left an active claim for the reaper: P4 never fired")
	}
	if capBreaches != 0 {
		t.Errorf("%d cap breaches", capBreaches)
	}
}

func asRaceSettled(err error, target **admit.RaceSettledError) bool {
	return errors.As(err, target)
}

// conformance: charter §II.6 and §II.10 (one rule, two consumers), the
// named regression for the enumeration's second finding
// (plans/os-873b5153.md D8). Two racers submit; the winner's pass is
// requested and settled, and the loser's fail lands in between. Every
// step is admitted, so nothing here is a divergence: the fold's
// anomaly counter and the classifier must both read the verdict the
// request CITED rather than whichever landed last. The racing model
// reaches this trace at race-depth 9, past the fast gate's 7, which is
// why it is pinned by hand as well.
func TestSettlementSurvivesTheLosersLateFail(t *testing.T) {
	h := rstage(t, 32)
	s := &rcstate{h: h, records: h.prefix}
	for _, st := range []rstep{
		{"claim", 0}, {"claim", 1}, {"submit", 0}, {"verdict.pass", 0},
		{"submit", 1}, {"request", -1}, {"verdict.fail", 1}, {"settle", -1},
	} {
		before := len(s.records)
		s = s.Apply(st)
		if len(s.records) == before {
			rec, ok := h.draft(s, st)
			if !ok {
				t.Fatalf("%v: the step drafted nothing", st)
			}
			ctx, err := admit.ContextOver(s.records, admit.WithDeclaration(h.decl))
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("%v refused: %v", st, admit.Check(ctx, rec))
		}
	}
	sub := s.subject()
	// Both racers submitted, so no claim was active when the settlement
	// landed and nobody is settled out: the subject simply reaches done.
	if sub.State != "done" || len(sub.Claims) != 0 {
		t.Fatalf("the subject reached done with no claim left standing: %s %+v", sub.State, sub.Claims)
	}
	if sub.Verdict == nil || sub.Verdict.Verdict != "fail" {
		t.Fatalf("the loser's fail is the standing verdict fact: %+v", sub.Verdict)
	}
	if !sub.CitedPass() {
		t.Fatalf("the request cites the winner's pass: %+v", sub.Requested)
	}
	if sub.Anomalies != 0 {
		t.Fatalf("an admitted chain counted %d anomalies", sub.Anomalies)
	}
	for _, f := range reconcile.Subject(raceSubject, sub) {
		switch f.Class {
		case reconcile.ClassMergeWithoutVerdict, reconcile.ClassChainSkipped:
			t.Fatalf("an admitted chain is classified %s: %s", f.Class, f.Detail)
		}
	}
}
