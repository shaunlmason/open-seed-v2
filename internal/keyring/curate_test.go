package keyring_test

// The curate grant-disjointness drills (plans/os-f30ee0d3.md D2):
// hypotheses are proposed under a grant disjoint from the lanes that
// write observations, and the keyring is where the disjointness binds:
// curate cannot join claim or operator (a root's implicit operator
// standing included), and neither can join curate, in either grant
// order. Plus the rows the three curation verbs accept.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

func TestCurateGrantDisjointness(t *testing.T) {
	root := key(t, 1)
	cases := []struct {
		name     string
		held     string
		granting string
	}{
		{"curate onto a claim holder", keyring.CapClaim, keyring.CapCurate},
		{"curate onto an operator", keyring.CapOperator, keyring.CapCurate},
		{"claim onto a curator", keyring.CapCurate, keyring.CapClaim},
		{"operator onto a curator", keyring.CapCurate, keyring.CapOperator},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent := key(t, 44)
			s := seeded(t, root)
			if err := s.Advance(rec(t, root, keyring.VerbEnrolled, fp(t, agent), enrollPayload(t, agent, "agent", "a"))); err != nil {
				t.Fatal(err)
			}
			if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, agent), `{"capability": "`+c.held+`"}`)); err != nil {
				t.Fatal(err)
			}
			err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, agent), `{"capability": "`+c.granting+`"}`))
			if err == nil {
				t.Fatalf("granting %s to a key holding %s must refuse", c.granting, c.held)
			}
			if !strings.Contains(err.Error(), "disjoint") {
				t.Fatalf("the refusal names the disjointness rule: %v", err)
			}
		})
	}
	// A governance root holds operator implicitly, so curate cannot
	// land on it: a root concluding from its own observations is
	// refused at the grant.
	s := seeded(t, root)
	if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, root), `{"capability": "`+keyring.CapCurate+`"}`)); err == nil || !strings.Contains(err.Error(), "disjoint") {
		t.Fatalf("curate onto a root refuses as the operator case: %v", err)
	}
	// The verdict and observer lanes stay compatible with curation:
	// none of them writes observations.
	agent := key(t, 44)
	s = seeded(t, root)
	if err := s.Advance(rec(t, root, keyring.VerbEnrolled, fp(t, agent), enrollPayload(t, agent, "agent", "a"))); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{keyring.CapVerdict, keyring.CapObserver, keyring.CapCurate} {
		if err := s.Advance(rec(t, root, keyring.VerbGranted, fp(t, agent), `{"capability": "`+capability+`"}`)); err != nil {
			t.Fatalf("verdict, observer and curate may co-hold: %v", err)
		}
	}
}

func TestCurationRowsAndTheRaise(t *testing.T) {
	if !keyring.Known(keyring.CapCurate) {
		t.Fatal("curate is in the vocabulary")
	}
	for verb, want := range map[string][]string{
		"curation.deadend.recorded":     {keyring.CapClaim, keyring.CapOperator},
		"curation.hypothesis.proposed":  {keyring.CapCurate},
		"curation.hypothesis.contested": {keyring.CapCurate},
		"curation.lesson.promoted":      {keyring.CapObserver, keyring.CapOperator},
		"escalation.raised":             {keyring.CapClaim, keyring.CapDispatch, keyring.CapVerdict, keyring.CapSupervise, keyring.CapOperator, keyring.CapCurate},
		// The flywheel's two rows (plans/os-9075c308.md D4): the
		// proposal is curate alone, the same no-fallback posture as
		// the hypothesis; the merge observation is the observer's
		// with the operator fallback merge.observed has.
		"workflow.proposed": {keyring.CapCurate},
		"workflow.merged":   {keyring.CapObserver, keyring.CapOperator},
	} {
		if got := keyring.AcceptedCapabilities(verb); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s accepts %v, want %v", verb, got, want)
		}
	}
}
