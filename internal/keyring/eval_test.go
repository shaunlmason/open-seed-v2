package keyring_test

// The qualification verbs' keyring half (plans/os-03e47abb.md D1, D4,
// D8; spec/evals.md): actor.qualified and actor.disqualified are
// defined at seed/3 positions only and fail a chain at their position
// before it, exactly as a seed/2 validator fails them; the payload is
// strict; a qualification is a grant with evidence, adding to the
// admissible set and leaving the "ever cited" mark; a disqualification
// removes the tuple, keeps the mark (the bridge never reopens), and
// refuses when there is nothing to disqualify.

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const (
	mintBody = `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "7"}`
	dropBody = `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-2", "verdict": "9", "reason": "the eval failed under this configuration"}`
)

// seed3Chain is genesis, the three upgrades and one enrolled worker:
// the prefix every drill below appends to.
func seed3Chain(t *testing.T) (root, worker ed25519.PrivateKey, wfp string, base []*event.Record) {
	t.Helper()
	root, worker = key(t, 1), key(t, 2)
	g, err := genesis.Build(root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	wfp = fp(t, worker)
	base = []*event.Record{
		g,
		recAt(t, root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`),
		rec(t, root, "actor.enrolled", wfp, enrollPayload(t, worker, "agent", "worker")),
		rec(t, root, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`),
		recAt(t, root, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`),
	}
	return root, worker, wfp, base
}

func TestQualificationVerbsActivateAtSeed3(t *testing.T) {
	root, _, wfp, base := seed3Chain(t)
	at := func(v, verb, body string) *event.Record { return recAt(t, root, v, verb, wfp, body) }

	// At a seed/2 position the verb is unknown and the chain fails
	// there, naming the position and the version that defines it.
	prefix := base[:4] // through the seed/2 upgrade
	for _, verb := range []string{keyring.VerbQualified, keyring.VerbDisqualified} {
		body := mintBody
		if verb == keyring.VerbDisqualified {
			body = dropBody
		}
		_, _, err := keyring.StateAt(append(append([]*event.Record{}, prefix...), at(version.Seed2, verb, body)))
		if err == nil || !strings.Contains(err.Error(), "position 4") || !strings.Contains(err.Error(), version.Seed3) {
			t.Fatalf("%s at a seed/2 position fails at its position naming %s: %v", verb, version.Seed3, err)
		}
	}

	// At seed/3 the mint is a grant with evidence: the capability in
	// the string view, the tuple in the admissible set, the mark set,
	// and the record kept with the event's own ts.
	s, active, err := keyring.StateAt(append(append([]*event.Record{}, base...), at(version.Seed3, keyring.VerbQualified, mintBody)))
	if err != nil || active != version.Seed3 {
		t.Fatalf("a seed/3 mint folds: %v (%s)", err, active)
	}
	if !s.HasAnyCapability(wfp, []string{keyring.CapClaim}) {
		t.Fatal("a qualification grants the capability it names when absent")
	}
	if cited := s.GrantTuples(wfp, keyring.CapClaim); len(cited) != 1 || cited[0].Model != "fable/5.1" {
		t.Fatalf("the admissible set holds the qualified tuple: %+v", cited)
	}
	if !s.EverCited(wfp, keyring.CapClaim) {
		t.Fatal("a qualification marks the capability as ever cited")
	}
	qs := s.Qualifications(wfp)
	if len(qs) != 1 || qs[0].Contract != "eval-1" || qs[0].Verdict != 7 || qs[0].TS != "2026-09-01T00:00:00Z" || qs[0].Disqualified || qs[0].Reason != "" {
		t.Fatalf("the qualification record carries contract, verdict position and the event's ts: %+v", qs)
	}
	if s.EverCited("deadbeef", keyring.CapClaim) || s.Qualifications("deadbeef") != nil || s.EverCited(wfp, keyring.CapVerdict) {
		t.Fatal("marks and records are per actor and per capability; unknown ones are empty")
	}
	found := false
	for _, a := range s.Actors() {
		found = found || a == wfp
	}
	if !found {
		t.Fatalf("Actors lists the enrolled: %v", s.Actors())
	}

	// A disqualification removes the tuple from the admissible set,
	// keeps the mark and the capability's string view, and records the
	// reason: the bridge does not reopen.
	s, _, err = keyring.StateAt(append(append([]*event.Record{}, base...),
		at(version.Seed3, keyring.VerbQualified, mintBody), at(version.Seed3, keyring.VerbDisqualified, dropBody)))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.GrantTuples(wfp, keyring.CapClaim)) != 0 {
		t.Fatalf("the disqualified tuple leaves the admissible set: %+v", s.GrantTuples(wfp, keyring.CapClaim))
	}
	if !s.EverCited(wfp, keyring.CapClaim) || !s.HasAnyCapability(wfp, []string{keyring.CapClaim}) {
		t.Fatal("the mark and the string view survive a disqualification: an empty set that was once cited is the closed bridge")
	}
	qs = s.Qualifications(wfp)
	if len(qs) != 2 || !qs[1].Disqualified || qs[1].Reason == "" || qs[1].Contract != "eval-2" || qs[1].Verdict != 9 {
		t.Fatalf("the disqualification is recorded after the qualification with its reason: %+v", qs)
	}
	c := s.Clone()
	if !c.EverCited(wfp, keyring.CapClaim) || len(c.Qualifications(wfp)) != 2 {
		t.Fatal("Clone carries the mark and the records")
	}

	// Nothing to disqualify is a refusal: a disqualification always
	// names a configuration that was admissible.
	_, _, err = keyring.StateAt(append(append([]*event.Record{}, base...), at(version.Seed3, keyring.VerbDisqualified, dropBody)))
	if err == nil || !strings.Contains(err.Error(), "nothing to disqualify") {
		t.Fatalf("disqualifying a tuple the actor never held refuses by name: %v", err)
	}

	// Two configurations qualified, one disqualified: the other stays.
	second := strings.Replace(strings.Replace(mintBody, "fable/5.1", "fable/5.2", 1), `"verdict": "7"`, `"verdict": "8"`, 1)
	s, _, err = keyring.StateAt(append(append([]*event.Record{}, base...),
		at(version.Seed3, keyring.VerbQualified, mintBody), at(version.Seed3, keyring.VerbQualified, second),
		at(version.Seed3, keyring.VerbDisqualified, dropBody)))
	if err != nil {
		t.Fatal(err)
	}
	if cited := s.GrantTuples(wfp, keyring.CapClaim); len(cited) != 1 || cited[0].Model != "fable/5.2" {
		t.Fatalf("a disqualification is per tuple: %+v", cited)
	}
}

// conformance: actors.md makes actor payload shapes chain validity —
// each malformed qualification fails at its position, never folds as
// a partial grant.
func TestQualificationShapesAreStrict(t *testing.T) {
	root, _, wfp, base := seed3Chain(t)
	for name, c := range map[string]struct{ verb, body string }{
		"no capability":              {keyring.VerbQualified, `{"tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "7"}`},
		"no tuple":                   {keyring.VerbQualified, `{"capability": "claim", "contract": "eval-1", "verdict": "7"}`},
		"malformed tuple":            {keyring.VerbQualified, `{"capability": "claim", "tuple": {"principal": "acme"}, "contract": "eval-1", "verdict": "7"}`},
		"no contract":                {keyring.VerbQualified, `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "verdict": "7"}`},
		"non-numeric verdict":        {keyring.VerbQualified, `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "seven"}`},
		"negative verdict":           {keyring.VerbQualified, `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "-1"}`},
		"unknown field":              {keyring.VerbQualified, `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "7", "extra": 1}`},
		"qualified with a reason":    {keyring.VerbQualified, `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "7", "reason": "because"}`},
		"disqualified without one":   {keyring.VerbDisqualified, `{"capability": "claim", "tuple": ` + qualifiedTuple + `, "contract": "eval-2", "verdict": "9"}`},
		"qualified for operator":     {keyring.VerbQualified, `{"capability": "operator", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "7"}`},
		"qualified for verdict":      {keyring.VerbQualified, `{"capability": "verdict", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "7"}`},
		"disqualified from dispatch": {keyring.VerbDisqualified, `{"capability": "dispatch", "tuple": ` + qualifiedTuple + `, "contract": "eval-2", "verdict": "9", "reason": "r"}`},
		"sealer qualified for claim": {keyring.VerbQualified, mintBody},
	} {
		chain := append([]*event.Record{}, base...)
		subject := wfp
		if name == "sealer qualified for claim" {
			// Disjointness holds for a qualification as for a grant:
			// a key holding sealer cannot be qualified into claim.
			sealer := key(t, 9)
			subject = fp(t, sealer)
			chain = append(chain,
				recAt(t, root, version.Seed3, "actor.enrolled", subject, enrollPayload(t, sealer, "agent", "sealer")),
				recAt(t, root, version.Seed3, "actor.granted", subject, `{"capability": "sealer"}`))
		}
		pos := len(chain)
		_, _, err := keyring.StateAt(append(chain, recAt(t, root, version.Seed3, c.verb, subject, c.body)))
		if err == nil || !strings.Contains(err.Error(), "position "+itoa(pos)) {
			t.Errorf("%s: fails at position %d as an actor event, got %v", name, pos, err)
		}
	}
	// Standing: an unenrolled subject and a revoked one both refuse.
	stranger := key(t, 8)
	if _, _, err := keyring.StateAt(append(append([]*event.Record{}, base...), recAt(t, root, version.Seed3, keyring.VerbQualified, fp(t, stranger), mintBody))); err == nil || !strings.Contains(err.Error(), "not enrolled") {
		t.Fatalf("an unenrolled subject cannot be qualified: %v", err)
	}
	if _, _, err := keyring.StateAt(append(append([]*event.Record{}, base...),
		recAt(t, root, version.Seed3, "actor.revoked", wfp, `{"reason": "gone"}`),
		recAt(t, root, version.Seed3, keyring.VerbQualified, wfp, mintBody))); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("a revoked subject cannot be qualified: %v", err)
	}
}

// conformance: the capability table's first non-operator actor rows —
// the supervisor mints and suspends attributably (plans/os-03e47abb.md
// D1), operator staying the standing override.
func TestQualificationVerbsAcceptSuperviseOrOperator(t *testing.T) {
	for _, verb := range []string{keyring.VerbQualified, keyring.VerbDisqualified} {
		got := keyring.AcceptedCapabilities(verb)
		if len(got) != 2 || got[0] != keyring.CapSupervise || got[1] != keyring.CapOperator {
			t.Fatalf("%s accepts [supervise, operator], got %v", verb, got)
		}
	}
	if got := keyring.AcceptedCapabilities(keyring.VerbGranted); len(got) != 1 || got[0] != keyring.CapOperator {
		t.Fatalf("every other actor verb stays operator-only: %v", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// conformance: review finding on plans/os-2e34f66a.md's task PR — a
// verifier holding verdict by a bare grant renders under the bridge,
// and its first failed calibration closes it: disqualifying a tuple
// nothing had cited admits for verdict alone, marks the capability
// cited with an empty admissible set, and a second disqualification
// then finds nothing to remove. For claim the refusal stands as
// before: an eval's fail disqualifies a configuration a qualification
// cited.
func TestFirstFailedCalibrationClosesTheBareGrantsBridge(t *testing.T) {
	root, _, _, base := seed3Chain(t)
	verifier := key(t, 4)
	vfp := fp(t, verifier)
	at := func(verb, subject, payload string) *event.Record {
		return recAt(t, root, version.Seed4, verb, subject, payload)
	}
	base = append(base,
		recAt(t, root, version.Seed3, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed4+`"}`),
		at(keyring.VerbEnrolled, vfp, enrollPayload(t, verifier, "agent", "verifier")),
		at(keyring.VerbGranted, vfp, `{"capability": "verdict"}`),
	)
	drop := `{"capability": "verdict", "tuple": ` + qualifiedTuple + `, "contract": "eval-1", "verdict": "9", "reason": "drift"}`
	s, _, err := keyring.StateAt(append(append([]*event.Record{}, base...), at(keyring.VerbDisqualified, vfp, drop)))
	if err != nil {
		t.Fatalf("the first failed calibration's disqualification admits for a bare verdict grant: %v", err)
	}
	if !s.EverCited(vfp, keyring.CapVerdict) || len(s.GrantTuples(vfp, keyring.CapVerdict)) != 0 || !s.HasAnyCapability(vfp, []string{keyring.CapVerdict}) {
		t.Fatal("the bridge closes: cited, with an empty admissible set, the grant's string view kept")
	}
	if qs := s.Qualifications(vfp); len(qs) != 1 || !qs[0].Disqualified {
		t.Fatalf("the disqualification is recorded: %+v", qs)
	}
	second := strings.Replace(drop, `"verdict": "9"`, `"verdict": "12"`, 1)
	if _, _, err := keyring.StateAt(append(append([]*event.Record{}, base...), at(keyring.VerbDisqualified, vfp, drop), at(keyring.VerbDisqualified, vfp, second))); err == nil || !strings.Contains(err.Error(), "nothing to disqualify") {
		t.Fatalf("once cited, the bridge is closed and a second disqualification finds nothing: %v", err)
	}
	// Claim keeps the refusal: the bridge closes on calibration alone.
	claimDrop := strings.Replace(drop, `"capability": "verdict"`, `"capability": "claim"`, 1)
	withClaim := append(append([]*event.Record{}, base...), at(keyring.VerbGranted, vfp, `{"capability": "claim"}`))
	if _, _, err := keyring.StateAt(append(withClaim, at(keyring.VerbDisqualified, vfp, claimDrop))); err == nil || !strings.Contains(err.Error(), "nothing to disqualify") {
		t.Fatalf("a bare claim grant is not closed by a disqualification: %v", err)
	}
	// A key without the capability at all has nothing to close.
	other := key(t, 5)
	ofp := fp(t, other)
	withOther := append(append([]*event.Record{}, base...), at(keyring.VerbEnrolled, ofp, enrollPayload(t, other, "agent", "other")))
	if _, _, err := keyring.StateAt(append(withOther, at(keyring.VerbDisqualified, ofp, drop))); err == nil || !strings.Contains(err.Error(), "nothing to disqualify") {
		t.Fatalf("no grant, no bridge, nothing to disqualify: %v", err)
	}
}
