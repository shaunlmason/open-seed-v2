package project_test

// The ranking projection drills (plans/os-c7554f18.md D3, D4;
// spec/ranking.md): the view orders the qualified tuples per
// capability from the chain's facts alone at the tip's instant, builds
// byte-identically, changes only when eval facts change, and the
// report's planner section carries the strongest the latest scoped
// offer named.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// appendAt signs and appends one record at a chosen ts, the one thing
// the fixture's add cannot do: an unrelated record at a later instant.
func appendAt(t *testing.T, dir string, resolve ledger.Resolver, priv ed25519.PrivateKey, v, ts, verb, subject, payload string) {
	t.Helper()
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tip, _, err := store.Tip()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := event.Sign(event.Event{V: v, TS: ts, Actor: pFP(t, priv), Verb: verb, Subject: subject, Payload: json.RawMessage(payload), Prev: tip}, priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(rec, resolve); err != nil {
		t.Fatal(err)
	}
}

func rankTuple(model string) string {
	return fmt.Sprintf(`{"principal": "acme", "harness": "local-worktree/v0", "model": %q, "tool_policy": "default", "environment": "detached-git-worktree"}`, model)
}

func TestRankingProjectionAndTheReportsStrongest(t *testing.T) {
	root, worker, other, supervisor := pKey(t, 1), pKey(t, 2), pKey(t, 3), pKey(t, 4)
	dir, resolve, add := fixtureChain(t, root, worker, other, supervisor)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, worker), `{"capability": "claim"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, other), enrollJSON(t, other, "agent", "other"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, other), `{"capability": "claim"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, supervisor), enrollJSON(t, supervisor, "agent", "supervisor"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, supervisor), `{"capability": "supervise"}`)
	add(root, version.Seed1, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed2+`"}`)
	add(root, version.Seed2, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed3+`"}`)

	rebuild := func() string {
		t.Helper()
		out := lockedTempOut(t, "views")
		if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
			t.Fatal(err)
		}
		return out
	}
	// A chain carrying no qualification builds two empty lists at the
	// tip's instant, unrefined.
	out := rebuild()
	var view ranking.Ranking
	readView(t, out, "ranking", project.RankingFile, &view)
	if len(view.Capabilities) != 2 || len(view.Capabilities["claim"]) != 0 || len(view.Capabilities["verdict"]) != 0 || view.Refined || view.AsOf != "" {
		t.Fatalf("no qualification: both capabilities present and empty, unrefined, at no instant: %+v", view)
	}

	// Two tuples qualified: the one with more evidence ranks first;
	// the evidence is the chain's own facts by position.
	one, two := rankTuple("m/1"), rankTuple("m/2")
	add(root, version.Seed3, keyring.VerbQualified, pFP(t, worker), `{"capability": "claim", "tuple": `+one+`, "contract": "e-1", "verdict": "7"}`)
	add(root, version.Seed3, keyring.VerbQualified, pFP(t, other), `{"capability": "claim", "tuple": `+two+`, "contract": "e-2", "verdict": "8"}`)
	add(root, version.Seed3, keyring.VerbQualified, pFP(t, other), `{"capability": "claim", "tuple": `+two+`, "contract": "e-3", "verdict": "9"}`)
	out2 := rebuild()
	readView(t, out2, "ranking", project.RankingFile, &view)
	claim := view.Capabilities["claim"]
	if len(claim) != 2 || claim[0].Tuple.Model != "m/2" || claim[0].Score != 2 || claim[1].Tuple.Model != "m/1" || claim[1].Score != 1 {
		t.Fatalf("the ranking orders by evidence: %+v", claim)
	}
	if view.AsOf != "2026-09-01T01:00:00Z" {
		t.Fatalf("the instant is the latest qualification fact's ts, never a clock: %q", view.AsOf)
	}
	if claim[0].Evidence[0].Kind != ranking.KindMint || claim[0].Evidence[1].Kind != ranking.KindSpotCheck || claim[0].Holders[0] != pFP(t, other) {
		t.Fatalf("the evidence and holders are the chain's: %+v", claim[0])
	}
	if claim[0].Agreement != nil || view.Refined {
		t.Fatal("the projection reads no gold: unrefined, agreement null")
	}

	// Byte-identical for the same prefix, and unchanged by an
	// unrelated append (the ranking changes only when eval facts do).
	out3 := rebuild()
	if !bytes.Equal(readRaw(t, out2, "ranking", project.RankingFile), readRaw(t, out3, "ranking", project.RankingFile)) {
		t.Fatal("the ranking projection is not byte-identical for the same prefix")
	}
	before := readRaw(t, out3, "ranking", project.RankingFile)
	add(root, version.Seed3, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	add(root, version.Seed3, "contract.specified", "c-1", `{"acceptance": {"ref": "specs/c1.md @ abc1234", "executable": false}}`)
	// An unrelated record at a LATER instant (review finding on the
	// task PR): the tip's ts moves, the ranking's bytes do not.
	appendAt(t, dir, resolve, root, version.Seed3, "2026-09-02T09:30:00Z", "intent.filed", "c-2", `{"intent": "later work", "tier": "trivial", "budget": "small", "routing": "core"}`)
	out4 := rebuild()
	after := readRaw(t, out4, "ranking", project.RankingFile)
	if !bytes.Equal(before, after) {
		t.Fatalf("an unrelated append changed the ranking:\n%s\n%s", before, after)
	}

	// A disqualification removes the tuple at once: absent, not last.
	add(root, version.Seed3, keyring.VerbDisqualified, pFP(t, other), `{"capability": "claim", "tuple": `+two+`, "contract": "e-4", "verdict": "12", "reason": "the eval failed"}`)
	out5 := rebuild()
	readView(t, out5, "ranking", project.RankingFile, &view)
	if claim := view.Capabilities["claim"]; len(claim) != 1 || claim[0].Tuple.Model != "m/1" {
		t.Fatalf("a disqualified tuple leaves the ranking: %+v", claim)
	}

	// The report's planner section carries the strongest the latest
	// scoped offer named, and nothing before one exists.
	var rep project.ReportView
	readView(t, out5, "report", project.ReportFile, &rep)
	if rep.Lanes == nil || rep.Lanes.Planner.Strongest != nil {
		t.Fatalf("no scoped offer yet: strongest absent: %+v", rep.Lanes)
	}
	add(supervisor, version.Seed3, "offer.published", "c-1", `{"eligibility": {"capabilities": ["claim"], "tuples": [`+one+`]}, "expires": "2027-01-01T00:00:00Z"}`)
	out6 := rebuild()
	readView(t, out6, "report", project.ReportFile, &rep)
	if got := rep.Lanes.Planner.Strongest; len(got) != 1 || got[0].Model != "m/1" {
		t.Fatalf("the report carries the latest scoped offer's tuples: %+v", got)
	}
	// A raw-pushed offer by a key holding no supervise grant folds as a
	// fact and counts for nothing here (review finding on the task PR).
	add(worker, version.Seed3, "offer.published", "c-1", `{"eligibility": {"capabilities": ["claim"], "tuples": [`+two+`]}, "expires": "2027-01-01T00:00:00Z"}`)
	out7 := rebuild()
	readView(t, out7, "report", project.ReportFile, &rep)
	if got := rep.Lanes.Planner.Strongest; len(got) != 1 || got[0].Model != "m/1" {
		t.Fatalf("an unauthorized offer's scope is not the planner's strongest: %+v", got)
	}
}
