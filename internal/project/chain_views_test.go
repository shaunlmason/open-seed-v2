package project_test

// The reconciliation-chain view drills (plans/os-6cdc15be.md):
// contracts v6 carries the verdict, requested, and merged facts;
// report v3 carries the record-derivable divergence classes with the
// evidence-grade surface named; a clean full chain yields no findings.

import (
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
	"github.com/shaunlmason/open-seed-v2/internal/reconcile"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestReconciliationViews(t *testing.T) {
	root, worker, verifier, sealer := pKey(t, 1), pKey(t, 2), pKey(t, 3), pKey(t, 4)
	dir, resolve, add := fixtureChain(t, root, worker, verifier, sealer)
	add(root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, verifier), enrollJSON(t, verifier, "agent", "verifier"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, verifier), `{"capability": "verdict"}`)
	add(root, version.Seed1, keyring.VerbEnrolled, pFP(t, sealer), enrollJSON(t, sealer, "agent", "sealer"))
	add(root, version.Seed1, keyring.VerbGranted, pFP(t, sealer), `{"capability": "sealer"}`)
	packet := `{"acceptance": ["done means done"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	sha := strings.Repeat("ef", 20)
	receipt := strings.Repeat("ab", 32)

	// c-F walks the full chain with the granted, disjoint verifier,
	// sealed by the granted sealer in its ready window
	// (plans/os-3128535a.md): seal at 9, claim at 10, submission at
	// 11, pass verdict at 12, request at 13 citing it, observation at
	// 14.
	commitment := strings.Repeat("cd", 32)
	add(root, version.Seed1, "intent.filed", "c-F", `{"intent": "ship", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-F", `{"acceptance": {"ref": "specs/f.md @ abc1234", "executable": false}}`)
	add(sealer, version.Seed1, "check.sealed", "c-F", `{"commitment": "`+commitment+`"}`)
	add(worker, version.Seed1, "claim.taken", "c-F", `{}`)
	add(worker, version.Seed1, "submission.made", "c-F", `{"fence": "10", "packet": `+packet+`}`)
	add(verifier, version.Seed1, "verdict.rendered", "c-F", `{"verdict": "pass", "receipt": "`+receipt+`", "submission": "11", "independence": "L1"}`)
	add(worker, version.Seed1, "merge.requested", "c-F", `{"verdict": "12"}`)
	add(root, version.Seed1, "merge.observed", "c-F", `{"merged": "`+sha+`", "pr": "pr/1"}`)

	// c-G reaches done through a raw-pushed observation with no
	// verdict at all: merge_without_verdict.
	add(root, version.Seed1, "intent.filed", "c-G", `{"intent": "slip", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-G", `{"acceptance": {"ref": "specs/g.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-G", `{}`)
	add(worker, version.Seed1, "submission.made", "c-G", `{"fence": "17", "packet": `+packet+`}`)
	add(root, version.Seed1, "merge.observed", "c-G", `{"merged": "`+sha+`", "pr": "pr/2"}`)

	// c-H holds a pass verdict with no merge yet: unreconciled,
	// neutrally.
	add(root, version.Seed1, "intent.filed", "c-H", `{"intent": "wait", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-H", `{"acceptance": {"ref": "specs/h.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-H", `{}`)
	add(worker, version.Seed1, "submission.made", "c-H", `{"fence": "22", "packet": `+packet+`}`)
	add(verifier, version.Seed1, "verdict.rendered", "c-H", `{"verdict": "pass", "receipt": "`+receipt+`", "submission": "23", "independence": "L1"}`)

	// c-I is the laundered chain: a raw-pushed verdict signed by the
	// ungranted, implementing worker, then a facts-complete request
	// and observation. Every fold fact lines up, and the retroactive
	// boundary check is what surfaces it.
	add(root, version.Seed1, "intent.filed", "c-I", `{"intent": "launder", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-I", `{"acceptance": {"ref": "specs/i.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-I", `{}`)
	add(worker, version.Seed1, "submission.made", "c-I", `{"fence": "27", "packet": `+packet+`}`)
	add(worker, version.Seed1, "verdict.rendered", "c-I", `{"verdict": "pass", "receipt": "`+receipt+`", "submission": "28", "independence": "L1"}`)
	add(worker, version.Seed1, "merge.requested", "c-I", `{"verdict": "29"}`)
	add(root, version.Seed1, "merge.observed", "c-I", `{"merged": "`+sha+`", "pr": "pr/3"}`)

	// c-J is the override-backed chain: a validated fail, the
	// operator's cited override, the request citing it, and the
	// observation — done through the sanctioned alternative
	// (plans/os-d2497eb7.md).
	add(root, version.Seed1, "intent.filed", "c-J", `{"intent": "override", "tier": "standard", "budget": "s", "routing": "core"}`)
	add(root, version.Seed1, "contract.specified", "c-J", `{"acceptance": {"ref": "specs/j.md @ abc1234", "executable": false}}`)
	add(worker, version.Seed1, "claim.taken", "c-J", `{}`)
	add(worker, version.Seed1, "submission.made", "c-J", `{"fence": "34", "packet": `+packet+`}`)
	add(verifier, version.Seed1, "verdict.rendered", "c-J", `{"verdict": "fail", "receipt": "`+receipt+`", "submission": "35", "independence": "L1"}`)
	add(root, version.Seed1, "merge.overridden", "c-J", `{"reason": "verifier judged the wrong artifact", "verdict": "36"}`)
	add(worker, version.Seed1, "merge.requested", "c-J", `{"override": "37"}`)
	add(root, version.Seed1, "merge.observed", "c-J", `{"merged": "`+sha+`", "pr": "pr/4"}`)

	out := rebuildAll(t, dir, resolve)

	var entries []project.ContractEntry
	readView(t, out, "contracts", project.ContractsFile, &entries)
	byID := map[string]project.ContractEntry{}
	for _, e := range entries {
		byID[e.Subject] = e
	}
	f := byID["c-F"]
	if f.Verdict == nil || f.Verdict.Position != "12" || f.Verdict.Verdict != "pass" || f.Verdict.Receipt != receipt {
		t.Fatalf("c-F must carry the verdict fact: %+v", f.Verdict)
	}
	if f.Requested == nil || *f.Requested != "13" {
		t.Fatalf("c-F must carry the request position: %+v", f.Requested)
	}
	if f.Merged == nil || f.Merged.Position != "14" || f.Merged.SHA != sha {
		t.Fatalf("c-F must carry the merged fact: %+v", f.Merged)
	}
	if f.Sealed == nil || f.Sealed.Position != "9" || f.Sealed.Commitment != commitment {
		t.Fatalf("c-F must carry the sealed commitment: %+v", f.Sealed)
	}
	g := byID["c-G"]
	if g.State == nil || *g.State != "done" || g.Verdict != nil || g.Merged == nil || g.Anomalies == 0 {
		t.Fatalf("c-G is done with a recorded merge, no verdict, and a visible anomaly: %+v", g)
	}
	j := byID["c-J"]
	if j.Override == nil || j.Override.Position != "37" || j.Override.Reason == "" {
		t.Fatalf("c-J must carry the override under its own name: %+v", j.Override)
	}
	if j.Anomalies != 0 {
		t.Fatalf("a legitimate override chain is no anomaly (review finding on the task PR): %d", j.Anomalies)
	}
	if f.Override != nil {
		t.Fatalf("a chain with no override marshals it as explicit null: %+v", f.Override)
	}

	var rep project.ReportView
	readView(t, out, "report", project.ReportFile, &rep)
	if rep.Reconciliation == nil {
		t.Fatal("the report carries the reconciliation section when work subjects exist")
	}
	by := rep.Reconciliation.ByClass
	if by[reconcile.ClassMergeWithoutVerdict] != 1 || by[reconcile.ClassUnreconciled] != 1 ||
		by[reconcile.ClassChainSkipped] != 0 || by[reconcile.ClassVerdictUnverified] != 1 ||
		by[reconcile.ClassUnsealed] != 4 || by[reconcile.ClassOverridden] != 1 ||
		by[reconcile.ClassOverrideUnverified] != 0 {
		t.Fatalf("the classes must count c-G, c-H, the laundered c-I, the unsealed chains, and c-J's named override: %+v", by)
	}
	for _, fnd := range rep.Reconciliation.Findings {
		if fnd.Subject == "c-J" && fnd.Class == reconcile.ClassMergeWithoutVerdict {
			t.Fatal("the override-backed chain must never surface as merge_without_verdict")
		}
	}
	launderedSeen := false
	for _, fnd := range rep.Reconciliation.Findings {
		if fnd.Subject == "c-I" && fnd.Class == reconcile.ClassVerdictUnverified {
			launderedSeen = true
		}
	}
	if !launderedSeen {
		t.Fatal("the laundered chain must surface as verdict_unverified on c-I")
	}
	for _, fnd := range rep.Reconciliation.Findings {
		if fnd.Subject == "c-F" {
			t.Fatalf("a clean full chain yields no findings: %+v", fnd)
		}
	}
	if !strings.Contains(rep.Reconciliation.EvidenceGrade, "seed reconcile") {
		t.Fatalf("the evidence-grade surface is named: %q", rep.Reconciliation.EvidenceGrade)
	}
}
