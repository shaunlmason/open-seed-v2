package main

// The escalation channel's CLI drills (plans/os-f781f0da.md steps 6
// and its criteria 6 through 8): the derivation refusals name what
// would establish the fact rather than picking one, and age is
// measured in ELAPSED TIME against both ledgers a position difference
// gets wrong.

import (
	"fmt"
	"strconv"
	"testing"
)

// raiseOn puts a well-shaped question on a ready subject and returns
// the position an answer must cite.
func raiseOn(t *testing.T, ld, key, subject string) string {
	t.Helper()
	e, code := runEnv(t, "escalation", "raise", "--ledger", ld, "--key", key,
		"--subject", subject, "--packet", writePacket(t, "aaaaaaa..bbbbbbb"),
		"--question", "which base?",
		"--option", "a=main", "--option", "b=the release branch")
	if code != 0 || !e.OK {
		t.Fatalf("raise: %d %+v", code, e)
	}
	pos := fmt.Sprint(e.Result["escalation"])
	if pos == "" || pos == "<nil>" {
		t.Fatalf("a raise names the position its answer must cite: %+v", e.Result)
	}
	return pos
}

// conformance: III — "escalations carry packet + question + minimal
// decision; waiting escalations surface with age". The whole channel
// through the CLI, with the cited position DERIVED rather than asked
// for.
func TestEscalationRaiseAndAnswerThroughTheCLI(t *testing.T) {
	ld, _, _, _, _, priv, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	worker := keys["workerA"]

	pos := raiseOn(t, ld, worker, "c-1")

	// It surfaces as an obligation owed by the human gate, and the row
	// cites the RAISE, not the state.
	_, s, _ := situationOf(t, "--ledger", ld, "--key", priv)
	var row map[string]any
	for _, r := range s.Obligations {
		if fmt.Sprint(r["kind"]) == "escalation.pending" {
			row = r
		}
	}
	if row == nil {
		t.Fatalf("a standing question surfaces: %+v", s.Obligations)
	}
	if fmt.Sprint(row["since"]) != pos {
		t.Errorf("the row cites the raise (%s), got %v", pos, row["since"])
	}
	if fmt.Sprint(row["owed_by"]) != "lane:operator" {
		t.Errorf("a human gate owes the answer: %v", row["owed_by"])
	}

	// No --escalation flag exists: the citation is derived from the
	// fold, and the landed payload carries it.
	e, code := runEnv(t, "decision", "record", "--ledger", ld, "--key", priv,
		"--subject", "c-1", "--choice", "b", "--because", "the release branch is frozen")
	if code != 0 || !e.OK {
		t.Fatalf("record: %d %+v", code, e)
	}
	p := payloadAt(t, ld, chainCount(t, ld)-1)
	if fmt.Sprint(p["escalation"]) != pos || fmt.Sprint(p["choice"]) != "b" {
		t.Fatalf("the derived citation and the choice must LAND: %+v", p)
	}
	if fmt.Sprint(p["because"]) != "the release branch is frozen" {
		t.Fatalf("the reasoning rides along: %+v", p)
	}
	// The answer names the RESOLUTION LATENCY it closes. The charter
	// requires it tracked, and the chain makes it derivable, but
	// nothing else surfaces it: the fold clears the standing question
	// on the answer, so a later read has no pair to subtract (review
	// finding on #200).
	if e.Result["resolved_after_seconds"] == nil {
		t.Fatalf("the answer reports how long the question waited: %+v", e.Result)
	}
	if _, err := strconv.Atoi(fmt.Sprint(e.Result["resolved_after_seconds"])); err != nil {
		t.Fatalf("the latency is a number of seconds: %v", e.Result["resolved_after_seconds"])
	}
	_, s, _ = situationOf(t, "--ledger", ld, "--key", priv)
	if kindsOf(s.Obligations)["escalation.pending"] {
		t.Fatalf("the answer discharges it: %+v", s.Obligations)
	}
	// The delta form reports the removal by (subject, kind), which is
	// the pair the situation read's identity is defined on: a resuming
	// lane matches rows it already holds against it, so a removal list
	// naming anything else would be unusable.
	_, d, _ := situationOf(t, "--ledger", ld, "--key", priv, "--since", pos)
	found := false
	for _, r := range d.Discharged {
		if fmt.Sprint(r["subject"]) == "c-1" && fmt.Sprint(r["kind"]) == "escalation.pending" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the delta reports the discharge by (subject, kind): %+v", d.Discharged)
	}
}

// The derivation refusals: absence names what would establish the
// fact, and a choice outside the set names what IS offered rather
// than picking one.
func TestDecisionRecordDerivationRefusals(t *testing.T) {
	ld, _, _, _, _, priv, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	before := chainCount(t, ld)

	e, code := runEnv(t, "decision", "record", "--ledger", ld, "--key", priv,
		"--subject", "c-1", "--choice", "a")
	if code == 0 || e.OK {
		t.Fatalf("an answer with no standing question must refuse: %d %+v", code, e)
	}
	if !containsStr(e.Error.Message, "escalation raise") {
		t.Errorf("the refusal names what would establish one: %q", e.Error.Message)
	}

	raiseOn(t, ld, keys["workerA"], "c-1")
	e, code = runEnv(t, "decision", "record", "--ledger", ld, "--key", priv,
		"--subject", "c-1", "--choice", "zzz")
	if code == 0 || e.OK {
		t.Fatalf("a choice outside the set must refuse: %d %+v", code, e)
	}
	for _, want := range []string{"a", "b", "which base?"} {
		if !containsStr(e.Error.Message, want) {
			t.Errorf("the refusal names what IS offered (%q): %q", want, e.Error.Message)
		}
	}
	if chainCount(t, ld) != before+1 {
		t.Fatalf("only the raise appended: refused acts sign nothing")
	}
}

// The question is validated AT THE DOOR, before a session opens, the
// packet's precedent: a malformed one refuses without appending.
//
// "At the door" is asserted by the CODE, not merely by the refusal.
// The boundary would refuse these too, so a drill that only checked
// "it refuses" would pass with the door check deleted — which is what
// the mutation showed. A door refusal is usage (exit 2); a boundary
// one carries the escalation rule's own code, and on the remote path
// costs a round-trip the caller never needed to spend.
func TestAMalformedQuestionRefusesBeforeSigning(t *testing.T) {
	ld, _, _, _, _, _, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	before := chainCount(t, ld)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"one option", []string{"--question", "q?", "--option", "a=only"}},
		{"no question", []string{"--option", "a=x", "--option", "b=y"}},
		{"malformed option", []string{"--question", "q?", "--option", "no-equals-sign"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"escalation", "raise", "--ledger", ld, "--key", keys["workerA"],
				"--subject", "c-1", "--packet", writePacket(t, "aaaaaaa..bbbbbbb")}, tc.args...)
			e, code := runEnv(t, args...)
			if code == 0 || e.OK {
				t.Fatalf("must refuse: %d %+v", code, e)
			}
			if e.Error == nil || e.Error.Code != "usage" {
				t.Fatalf("a malformed question refuses at the door as usage, not at the boundary: %d %+v", code, e)
			}
		})
	}
	if chainCount(t, ld) != before {
		t.Fatalf("a refused raise appends nothing: %d != %d", chainCount(t, ld), before)
	}
}

// conformance: age is ELAPSED TIME. Drilled against both ledgers a
// position difference gets wrong — an IDLE one, where the age must
// grow with --now although no event has landed, and a BUSY one, where
// unrelated traffic between the raise and the read must not change it.
func TestAgeIsElapsedTimeNotAPositionDifference(t *testing.T) {
	ld, _, _, _, _, priv, _, keys, _ := offerLedgerAndSubject(t, "c-1")
	raiseOn(t, ld, keys["workerA"], "c-1")

	ageAt := func(now string) int {
		t.Helper()
		_, s, _ := situationOf(t, "--ledger", ld, "--key", priv, "--now", now)
		for _, r := range s.Obligations {
			if fmt.Sprint(r["kind"]) == "escalation.pending" {
				n, err := strconv.Atoi(fmt.Sprint(r["age_seconds"]))
				if err != nil {
					t.Fatalf("age_seconds must be a number: %v", r["age_seconds"])
				}
				return n
			}
		}
		t.Fatalf("no standing escalation to age: %+v", s.Obligations)
		return 0
	}

	// IDLE: not one event lands between these two reads, so a position
	// difference would report the same number twice. Elapsed time does
	// not.
	early := ageAt("2030-01-01T00:00:00Z")
	late := ageAt("2030-01-01T01:00:00Z")
	if late-early != 3600 {
		t.Fatalf("an hour of idleness is an hour of age: %d then %d", early, late)
	}
	if early <= 0 {
		t.Fatalf("a question raised in the past has positive age, got %d", early)
	}

	// BUSY: unrelated traffic lands, moving every position, and the
	// age at a FIXED instant must not move with it.
	for i := 0; i < 3; i++ {
		if _, err := admitAppend(t, ld, workerRawKey(22), "message.sent", "c-2",
			fmt.Sprintf(`{"body": "unrelated %d"}`, i)); err != nil {
			t.Fatalf("the drill needs real traffic: %v", err)
		}
	}
	if got := ageAt("2030-01-01T00:00:00Z"); got != early {
		t.Fatalf("unrelated traffic changed the age from %d to %d — that is event count, not elapsed time", early, got)
	}
}

func containsStr(hay, needle string) bool {
	return len(needle) == 0 || (len(hay) >= len(needle) && indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
