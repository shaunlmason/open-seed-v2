package ranking_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// tupleJSON is a configuration whose model field is a placeholder
// lineage, never a real model name.
func tupleJSON(model, env string) string {
	return fmt.Sprintf(`{"principal": "acme", "harness": "local-worktree/v0", "model": %q, "tool_policy": "default", "environment": %q}`, model, env)
}

func parse(t *testing.T, raw string) tuple.Tuple {
	t.Helper()
	tu, err := tuple.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return tu
}

// stand is a real chain: genesis, the upgrades to seed/4, three
// enrolled workers granted claim and one verifier granted verdict, so
// every qualification fact below is an admitted record the keyring
// applied, never a hand-built struct.
type stand struct {
	t       *testing.T
	store   *ledger.Store
	keys    map[string]ed25519.PrivateKey
	fps     map[string]string
	resolve ledger.Resolver
	clock   int
}

func keyOf(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

func newStand(t *testing.T) *stand {
	t.Helper()
	s := &stand{t: t, keys: map[string]ed25519.PrivateKey{}, fps: map[string]string{}}
	for name, first := range map[string]byte{"root": 1, "a": 2, "b": 5, "c": 7, "v": 3} {
		s.keys[name] = keyOf(first)
		fp, err := event.Fingerprint(s.keys[name].Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		s.fps[name] = fp
	}
	store, err := ledger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.store = store
	g, err := genesis.Build(s.keys["root"], nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(g)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(g.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	s.resolve = func(fp string) (ed25519.PublicKey, bool) {
		for _, p := range s.keys {
			if f, _ := event.Fingerprint(p.Public().(ed25519.PublicKey)); f == fp {
				return p.Public().(ed25519.PublicKey), true
			}
		}
		return resolve(fp)
	}
	if _, err := store.Append(g, s.resolve); err != nil {
		t.Fatal(err)
	}
	s.addAt("root", version.Protocol, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	for _, e := range []struct{ name, capability string }{{"a", keyring.CapClaim}, {"b", keyring.CapClaim}, {"c", keyring.CapClaim}, {"v", keyring.CapVerdict}} {
		pub := s.keys[e.name].Public().(ed25519.PublicKey)
		s.addAt("root", version.Seed1, "2026-09-01T00:00:00Z", keyring.VerbEnrolled, s.fps[e.name], fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), e.name))
		s.addAt("root", version.Seed1, "2026-09-01T00:00:00Z", keyring.VerbGranted, s.fps[e.name], `{"capability": "`+e.capability+`"}`)
	}
	s.addAt("root", version.Seed1, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	s.addAt("root", version.Seed2, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)
	s.addAt("root", version.Seed3, "2026-09-01T00:00:00Z", ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`)
	return s
}

func (s *stand) addAt(who, v, ts, verb, subject, payload string) int {
	s.t.Helper()
	tip, count, err := s.store.Tip()
	if err != nil {
		s.t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{V: v, TS: ts, Actor: s.fps[who], Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip}, s.keys[who])
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.store.Append(rec, s.resolve); err != nil {
		s.t.Fatalf("%s %s: %v", verb, subject, err)
	}
	return count
}

// next appends at seed/4 with a ts one hour later than the last.
func (s *stand) next(who, verb, subject, payload string) int {
	s.t.Helper()
	s.clock++
	return s.addAt(who, version.Seed4, time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC).Add(time.Duration(s.clock)*time.Hour).Format(time.RFC3339), verb, subject, payload)
}

// qualify appends actor.qualified for who's capability and tuple,
// citing a contract and a verdict position.
func (s *stand) qualify(who, capability, tu, contract string, verdict int) int {
	s.t.Helper()
	return s.next("root", keyring.VerbQualified, s.fps[who], fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": %q, "verdict": "%d"}`, capability, tu, contract, verdict))
}

func (s *stand) disqualify(who, capability, tu, contract string, verdict int) int {
	s.t.Helper()
	return s.next("root", keyring.VerbDisqualified, s.fps[who], fmt.Sprintf(`{"capability": %q, "tuple": %s, "contract": %q, "verdict": "%d", "reason": "the eval failed"}`, capability, tu, contract, verdict))
}

func (s *stand) records() []*event.Record {
	s.t.Helper()
	var recs []*event.Record
	if err := s.store.Records(func(_ int, rec *event.Record) error {
		recs = append(recs, rec)
		return nil
	}); err != nil {
		s.t.Fatal(err)
	}
	return recs
}

func (s *stand) derive(agreement func(string, int) (float64, bool)) ranking.Ranking {
	s.t.Helper()
	r, err := ranking.Derive(ranking.Inputs{Records: s.records(), AsOf: "2026-09-02T00:00:00Z", Agreement: agreement})
	if err != nil {
		s.t.Fatal(err)
	}
	return r
}

func order(r ranking.Ranking, capability string) string {
	var out []string
	for _, e := range r.Capabilities[capability] {
		out = append(out, e.Tuple.Model+":"+fmt.Sprint(e.Score))
	}
	return strings.Join(out, " ")
}

// conformance: plans/os-c7554f18.md D1, AC1, AC4 — the score is the
// count of qualifying evidence; equal scores order by the latest
// pass, newer first; equal instants by the canonical JSON; the first
// pass is the mint and later ones spot checks.
func TestRankingOrdersByScoreThenLatestThenCanonical(t *testing.T) {
	s := newStand(t)
	one, two, three := tupleJSON("m/1", "e"), tupleJSON("m/2", "e"), tupleJSON("m/3", "e")
	s.qualify("a", keyring.CapClaim, one, "e1", 10)
	s.qualify("b", keyring.CapClaim, two, "e2", 11)
	s.qualify("a", keyring.CapClaim, one, "e3", 12) // one: mint + spot check
	s.qualify("c", keyring.CapClaim, three, "e4", 13)
	r := s.derive(nil)
	if got := order(r, keyring.CapClaim); got != "m/1:2 m/3:1 m/2:1" {
		t.Fatalf("score first, then the newer pass: %s", got)
	}
	first := r.Capabilities[keyring.CapClaim][0]
	if first.Evidence[0].Kind != ranking.KindMint || first.Evidence[1].Kind != ranking.KindSpotCheck || first.Latest != first.Evidence[1].TS || first.Evidence[1].Position <= first.Evidence[0].Position {
		t.Fatalf("the first pass is the mint, the second a spot check, the latest their newest: %+v", first)
	}
	if first.Agreement != nil || r.Refined || r.AsOf != "2026-09-02T00:00:00Z" || len(r.Capabilities[keyring.CapVerdict]) != 0 {
		t.Fatalf("no gold: unrefined, agreement null, the instant as declared: %+v", r)
	}
	// Equal instants: two tuples minted in one record's ts (the same
	// hour) order by canonical JSON, lower first, never by map order.
	t.Run("equal instants", func(t *testing.T) {
		s := newStand(t)
		ts := "2026-09-01T05:00:00Z"
		for _, m := range []string{"m/9", "m/2", "m/5"} {
			s.addAt("root", version.Seed4, ts, keyring.VerbQualified, s.fps["a"], fmt.Sprintf(`{"capability": "claim", "tuple": %s, "contract": "e", "verdict": "3"}`, tupleJSON(m, "e")))
		}
		for i := 0; i < 20; i++ {
			if got := order(s.derive(nil), keyring.CapClaim); got != "m/2:1 m/5:1 m/9:1" {
				t.Fatalf("a tie at one instant breaks by the canonical JSON: %s", got)
			}
		}
	})
}

// conformance: D1, AC3 — a tuple whose latest fact is a
// disqualification is absent, not last; the evidence since it last
// held is what scores once it is re-qualified; a tuple whose every
// holder is suspended leaves and returns with re-enrollment.
func TestDisqualifiedAndSuspendedNeverRank(t *testing.T) {
	s := newStand(t)
	one, two := tupleJSON("m/1", "e"), tupleJSON("m/2", "e")
	s.qualify("a", keyring.CapClaim, one, "e1", 10)
	s.qualify("a", keyring.CapClaim, one, "e2", 11)
	s.qualify("b", keyring.CapClaim, two, "e3", 12)
	if got := order(s.derive(nil), keyring.CapClaim); got != "m/1:2 m/2:1" {
		t.Fatalf("both rank: %s", got)
	}
	s.disqualify("a", keyring.CapClaim, one, "e4", 13)
	if got := order(s.derive(nil), keyring.CapClaim); got != "m/2:1" {
		t.Fatalf("a disqualified tuple is absent, not last: %s", got)
	}
	// A hand grant re-admits the tuple for a holder, so the holder
	// rule alone would rank it again; the latest fact is still the
	// disqualification, so it stays absent until a pass re-qualifies it.
	s.next("root", keyring.VerbGranted, s.fps["c"], `{"capability": "claim", "tuple": `+one+`}`)
	if got := order(s.derive(nil), keyring.CapClaim); got != "m/2:1" {
		t.Fatalf("a re-granted tuple whose latest fact is a disqualification is absent, not ranked at zero: %s", got)
	}
	// Re-qualified: only the evidence since it last held scores.
	s.qualify("a", keyring.CapClaim, one, "e5", 14)
	r := s.derive(nil)
	if got := order(r, keyring.CapClaim); got != "m/1:1 m/2:1" {
		t.Fatalf("re-qualified, the score restarts at the new mint (newer first): %s", got)
	}
	if e := r.Capabilities[keyring.CapClaim][0]; len(e.Evidence) != 1 || e.Evidence[0].Kind != ranking.KindMint || e.Evidence[0].Contract != "e5" {
		t.Fatalf("the evidence is the new mint alone: %+v", e)
	}
	// The only holder of two suspended: two leaves; re-enrolled, it
	// returns with the same evidence.
	s.next("root", keyring.VerbSuspended, s.fps["b"], `{"reason": "rotation"}`)
	if got := order(s.derive(nil), keyring.CapClaim); got != "m/1:1" {
		t.Fatalf("a suspended holder's tuple leaves: %s", got)
	}
	pub := s.keys["b"].Public().(ed25519.PublicKey)
	s.next("root", keyring.VerbEnrolled, s.fps["b"], fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "b"}`, hex.EncodeToString(pub)))
	r = s.derive(nil)
	if got := order(r, keyring.CapClaim); got != "m/1:1 m/2:1" {
		t.Fatalf("re-enrolled, the tuple returns: %s", got)
	}
	if h := r.Capabilities[keyring.CapClaim][1].Holders; len(h) != 1 || h[0] != s.fps["b"] {
		t.Fatalf("the holders are the active actors whose admissible set carries the tuple: %v", h)
	}
}

// conformance: D4 — the ranking changes only when eval facts change:
// an unrelated append leaves the derivation byte-identical.
func TestRankingIsByteIdenticalAcrossUnrelatedAppends(t *testing.T) {
	s := newStand(t)
	s.qualify("a", keyring.CapClaim, tupleJSON("m/1", "e"), "e1", 10)
	before, _ := json.Marshal(s.derive(nil))
	s.next("root", "intent.filed", "c-1", `{"intent": "unrelated work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	s.next("root", keyring.VerbGranted, s.fps["c"], `{"capability": "claim"}`)
	after, _ := json.Marshal(s.derive(nil))
	if string(before) != string(after) {
		t.Fatalf("an unrelated append changed the ranking:\n%s\n%s", before, after)
	}
}

// conformance: D1, D4 — verdict tuples rank by calibration passes; the
// agreement figure refines the order only when the gold is supplied,
// and the ranking says whether it was.
func TestAgreementRefinesOnlyWithGold(t *testing.T) {
	s := newStand(t)
	v1, v2 := tupleJSON("m/1", "v"), tupleJSON("m/2", "v")
	s.next("root", keyring.VerbGranted, s.fps["v"], `{"capability": "verdict", "tuple": `+v2+`}`)
	s.qualify("v", keyring.CapVerdict, v1, "cal-1", 20)
	s.qualify("v", keyring.CapVerdict, v2, "cal-2", 21)
	plain := s.derive(nil)
	if got := order(plain, keyring.CapVerdict); got != "m/2:1 m/1:1" || plain.Refined {
		t.Fatalf("without gold, equal scores order by the newer pass and the ranking is unrefined: %s %v", got, plain.Refined)
	}
	if plain.Capabilities[keyring.CapVerdict][0].Evidence[0].Kind != ranking.KindCalibration {
		t.Fatal("a verdict tuple's passes are calibrations")
	}
	agreement := func(contract string, verdict int) (float64, bool) {
		switch contract {
		case "cal-1":
			return 0.9, verdict == 20
		case "cal-2":
			return 0.6, verdict == 21
		}
		return 0, false
	}
	refined := s.derive(agreement)
	if got := order(refined, keyring.CapVerdict); got != "m/1:1 m/2:1" || !refined.Refined {
		t.Fatalf("with gold, the higher agreement ranks first at equal score: %s %v", got, refined.Refined)
	}
	top := refined.Capabilities[keyring.CapVerdict][0]
	if top.Agreement == nil || *top.Agreement != "0.900" || top.Evidence[0].Agreement == nil || *top.Evidence[0].Agreement != "0.900" {
		t.Fatalf("the agreement is carried on the entry and its evidence: %+v", top)
	}
	// Two means that round to one display string are still two means:
	// the higher ranks first even when the lower is the newer pass
	// (review finding on the task PR).
	t.Run("unrounded means", func(t *testing.T) {
		s := newStand(t)
		hi, lo := tupleJSON("m/hi", "v"), tupleJSON("m/lo", "v")
		s.next("root", keyring.VerbGranted, s.fps["v"], `{"capability": "verdict", "tuple": `+hi+`}`)
		s.next("root", keyring.VerbGranted, s.fps["v"], `{"capability": "verdict", "tuple": `+lo+`}`)
		s.qualify("v", keyring.CapVerdict, hi, "cal-hi", 30)
		s.qualify("v", keyring.CapVerdict, lo, "cal-lo", 31) // newer
		close := func(contract string, verdict int) (float64, bool) {
			switch contract {
			case "cal-hi":
				return 0.90049, true
			case "cal-lo":
				return 0.90041, true
			}
			return 0, false
		}
		r := s.derive(close)
		if got := order(r, keyring.CapVerdict); got != "m/hi:1 m/lo:1" {
			t.Fatalf("the unrounded mean orders, not the display string: %s", got)
		}
		if a, b := r.Capabilities[keyring.CapVerdict][0].Agreement, r.Capabilities[keyring.CapVerdict][1].Agreement; a == nil || b == nil || *a != *b {
			t.Fatalf("the display strings round equal: %v %v", a, b)
		}
	})
	// A pass the gold cannot score sorts after the scored ones.
	s.qualify("v", keyring.CapVerdict, tupleJSON("m/3", "v"), "cal-3", 22)
	s.next("root", keyring.VerbGranted, s.fps["v"], `{"capability": "verdict", "tuple": `+tupleJSON("m/3", "v")+`}`)
	if got := order(s.derive(agreement), keyring.CapVerdict); got != "m/1:1 m/2:1 m/3:1" {
		t.Fatalf("unrefined entries sort after refined ones at equal score: %s", got)
	}
	// The claim ranking is never refined by gold.
	s.qualify("a", keyring.CapClaim, tupleJSON("m/1", "e"), "e1", 30)
	if e := s.derive(agreement).Capabilities[keyring.CapClaim][0]; e.Agreement != nil {
		t.Fatal("gold refines the verdict ranking only")
	}
}

// conformance: D2 — Top is the first n tuples, nil when nothing ranks.
func TestTopTakesTheFirstN(t *testing.T) {
	s := newStand(t)
	if got := ranking.Top(s.derive(nil), keyring.CapClaim, 3); got != nil {
		t.Fatalf("nothing ranks on a chain with no qualification: %v", got)
	}
	s.qualify("a", keyring.CapClaim, tupleJSON("m/1", "e"), "e1", 10)
	s.qualify("b", keyring.CapClaim, tupleJSON("m/2", "e"), "e2", 11)
	r := s.derive(nil)
	if got := ranking.Top(r, keyring.CapClaim, 1); len(got) != 1 || got[0].Model != "m/2" {
		t.Fatalf("the top one is the newest pass at equal score: %v", got)
	}
	if got := ranking.Top(r, keyring.CapClaim, 5); len(got) != 2 {
		t.Fatalf("n past the end takes what ranks: %v", got)
	}
	if got := ranking.Top(r, keyring.CapVerdict, 1); got != nil {
		t.Fatalf("an empty capability yields nil: %v", got)
	}
}

// conformance: D1 — a malformed qualification payload in an admitted
// prefix is surfaced, never skipped.
func TestMalformedFactIsAnError(t *testing.T) {
	recs := []*event.Record{{Event: event.Event{Verb: keyring.VerbQualified, Subject: "x", Payload: json.RawMessage(`{"capability": "claim", "verdict": "seven"}`)}}}
	if _, err := ranking.Derive(ranking.Inputs{Records: recs, Ring: keyring.New()}); err == nil || !strings.Contains(err.Error(), "not a position") {
		t.Fatalf("a verdict that is not a position is an error: %v", err)
	}
	recs[0].Event.Payload = json.RawMessage(`not json`)
	if _, err := ranking.Derive(ranking.Inputs{Records: recs, Ring: keyring.New()}); err == nil {
		t.Fatal("an unreadable payload is an error")
	}
}

// conformance: D4 — the policy table is pinned: the rows
// spec/ranking.md states are Rules, word for word, so the policy
// cannot change in the code without the spec or in the spec without
// the code.
func TestRulesAreTheSpecsTable(t *testing.T) {
	b, err := os.ReadFile("../../spec/ranking.md")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `([a-z]+)` \\| (.+) \\|$")
	var got []ranking.Rule
	for _, m := range row.FindAllStringSubmatch(string(b), -1) {
		got = append(got, ranking.Rule{Name: m[1], Policy: m[2]})
	}
	if len(got) == 0 {
		t.Fatal("no policy rows parsed from spec/ranking.md")
	}
	if len(got) != len(ranking.Rules) {
		t.Fatalf("the spec states %d rows, the code %d", len(got), len(ranking.Rules))
	}
	for i, r := range ranking.Rules {
		if got[i] != r {
			t.Fatalf("row %d differs:\n spec: %+v\n code: %+v", i, got[i], r)
		}
	}
}

// conformance: the canonical JSON is the five fields in declared order.
func TestCanonicalIsTheDeclaredOrder(t *testing.T) {
	if got := ranking.Canonical(parse(t, tupleJSON("m/1", "e"))); got != `{"principal":"acme","harness":"local-worktree/v0","model":"m/1","tool_policy":"default","environment":"e"}` {
		t.Fatalf("canonical: %s", got)
	}
}
