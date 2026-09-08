package redteam

// The corpus (plans/os-465e356e.md D4, D5): one table, keyed to the
// §I.2 ceiling clause and side each entry attacks, run by the adversary
// with its own key and raw git against the enforced fixture. A
// prohibition entry must be refused at the push with its named reason; a
// permission's primary entry must be admitted; a permission's negatives
// (present but not primary) are refused. Coverage is derived from this
// table and the ceiling both ways (coverage_test.go).

import "fmt"

// attack is one corpus entry.
type attack struct {
	name    string
	clause  string
	side    Side
	primary bool   // the entry that covers its (clause, side) target
	admit   bool   // expected: admitted (true) or refused (false)
	reason  string // required substring of the hook's refusal (refused entries)
	order   int    // run order: admitted ledger mutations run last, in a safe sequence
	run     func(fx *Fixture, adv *Adversary) Outcome
}

// corpus returns the attacks. Every ceiling (clause, side) target has
// exactly one primary entry whose polarity matches the clause's kind;
// permission clauses carry their refused negatives beside it.
func corpus() []attack {
	held := func(fx *Fixture) string { return fmt.Sprintf("%d", fx.Fence[ContractHeld]) }
	return []attack{
		// --- prohibitions, ledger ---
		{
			name: "claim a contract the peer holds", clause: "claim", side: Ledger, primary: true,
			reason: "already claimed by",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("claim.taken", ContractPeer, `{}`)
				return o
			},
		},
		{
			// The ceiling refuses the adversary's own raw claim on a
			// contract above its squad's agent ceiling, at the enforced
			// boundary (os-0f924157 D4.2) — a negative under the claim
			// clause beside the contention primary, and the one the
			// one-derivation invariant now asserts under the declaration.
			name:   "claim a contract above its squad's agent ceiling",
			clause: "claim", side: Ledger,
			reason: "agent ceiling is trivial",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("claim.taken", ContractAbove, `{}`)
				return o
			},
		},
		{
			name: "render a verdict on its own submission", clause: "approve", side: Ledger, primary: true,
			reason: "is not granted any of",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				body := fmt.Sprintf(`{"verdict": "pass", "receipt": "%s", "submission": "%d", "independence": "L1"}`,
					"abababababababababababababababababababababababababababababababab", fx.Submission)
				o, _ := adv.As("verdict.rendered", ContractReview, body)
				return o
			},
		},
		{
			name: "rewrite an admitted record", clause: "rewrite", side: Ledger, primary: true,
			reason: "rewrites admitted history at or before position",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				// Rewrite the admitted tip's payload, re-signed so the
				// stream still verifies from genesis: the hook's
				// record-level prefix check catches the divergence a
				// commit-graph fast-forward check alone would miss.
				o, _ := adv.RewriteTip(`{"fence": "0", "packet": {"acceptance": ["forged"], "decisions": [], "base": "` + Anchor + `..` + Anchor + `", "refs": [], "findings": []}}`)
				return o
			},
		},
		{
			name: "grant itself operator", clause: "gates", side: Ledger, primary: true,
			reason: "is not granted any of",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("actor.granted", adv.ID.FP, `{"capability": "operator"}`)
				return o
			},
		},
		{
			name: "sign as the peer", clause: "impersonate", side: Ledger, primary: true,
			reason: "signature does not verify",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.PushEvent(fx.Peer.FP, "message.sent", ContractReady, `{"n": 1}`)
				return o
			},
		},
		{
			name: "act on a released contract citing its stale fence", clause: "lease", side: Ledger, primary: true,
			reason: "a fence dies with its claim window",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				body := fmt.Sprintf(`{"fence": "%d", "count": 1, "step": "past the lease"}`, fx.Fence[ContractReleased])
				o, _ := adv.As("progress.milestone", ContractReleased, body)
				return o
			},
		},
		{
			name: "submit a contract it does not hold", clause: "transition", side: Ledger, primary: true,
			reason: "is illegal for subject",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("submission.made", ContractReady, fmt.Sprintf(`{"packet": %s}`, Packet("not mine")))
				return o
			},
		},
		{
			name: "reserve beyond the class capacity", clause: "spend", side: Ledger, primary: true,
			reason: "exceeds remaining",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("budget.reserve", ContractHeld, fmt.Sprintf(`{"amount": "9999", "fence": "%s"}`, held(fx)))
				return o
			},
		},
		{
			name: "start a run it is not the supervisor for", clause: "spend", side: Ledger,
			reason: "is not granted any of",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("run.started", ContractHeld, fmt.Sprintf(`{"fence": "%s", "reservation": "1", "tuple": {"principal": "p", "harness": "h/0", "model": "m/0", "tool_policy": "t", "environment": "e"}}`, held(fx)))
				return o
			},
		},

		// --- prohibitions, code ---
		{
			name: "force-update a tag", clause: "rewrite", side: Code, primary: true,
			reason: "tags are immutable",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				return adv.PushBranch("refs/tags/v0", true, DefaultBranch, map[string]string{"x": "1"})
			},
		},
		{
			name: "push the default branch", clause: "gates", side: Code, primary: true,
			reason: "operator standing only",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				return adv.PushBranch(DefaultBranch, false, DefaultBranch, map[string]string{"README.md": "hijacked\n"})
			},
		},
		{
			name: "touch a protected path on its own branch", clause: "gates", side: Code,
			reason: "on the protected surface",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				return adv.PushBranch("refs/heads/seed/"+ContractHeld, false, DefaultBranch, map[string]string{"Makefile": "check: evil\n"})
			},
		},
		{
			name: "push a contract branch whose window closed", clause: "lease", side: Code, primary: true,
			reason: "no claim is active on " + ContractReleased,
			run: func(fx *Fixture, adv *Adversary) Outcome {
				return adv.PushBranch("refs/heads/seed/"+ContractReleased, false, DefaultBranch, map[string]string{"late.txt": "1"})
			},
		},

		// --- permissions, ledger (primary = admitted) + negatives ---
		{
			name: "reserve within its class capacity", clause: "spend-reserved", side: Ledger, primary: true,
			admit: true, order: 10,
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("budget.reserve", ContractHeld, fmt.Sprintf(`{"amount": "10", "fence": "%s"}`, held(fx)))
				return o
			},
		},
		{
			name: "reserve on a contract it does not hold", clause: "spend-reserved", side: Ledger,
			reason: "only the active claim holder",
			run: func(fx *Fixture, adv *Adversary) Outcome {
				o, _ := adv.As("budget.reserve", ContractPeer, fmt.Sprintf(`{"amount": "10", "fence": "%d"}`, fx.Fence[ContractPeer]))
				return o
			},
		},
		{
			name: "record a false dead end attributed to itself", clause: "signed-lie", side: Ledger, primary: true,
			admit: true, order: 20,
			run: func(fx *Fixture, adv *Adversary) Outcome {
				body := fmt.Sprintf(`{"fence": "%s", "tried": "a fabricated approach", "outcome": "invented failure", "condition": "never true", "environment": "fixture"}`, held(fx))
				o, _ := adv.As("curation.deadend.recorded", ContractHeld, body)
				return o
			},
		},
		{
			name: "submit its own work for verification", clause: "submit", side: Ledger, primary: true,
			admit: true, order: 30,
			run: func(fx *Fixture, adv *Adversary) Outcome {
				body := fmt.Sprintf(`{"fence": "%s", "packet": %s}`, held(fx), Packet("submitted for verification"))
				o, _ := adv.As("submission.made", ContractHeld, body)
				return o
			},
		},

		// --- permission, code (primary = admitted) ---
		{
			name: "push its own contract branch", clause: "code-surface", side: Code, primary: true,
			admit: true, order: 25,
			run: func(fx *Fixture, adv *Adversary) Outcome {
				return adv.PushBranch("refs/heads/seed/"+ContractHeld, false, DefaultBranch, map[string]string{"work.txt": "the implementer's own work\n"})
			},
		},
		{
			name: "push another actor's contract branch", clause: "code-surface", side: Code,
			reason: "does not hold the active claim on " + ContractPeer,
			run: func(fx *Fixture, adv *Adversary) Outcome {
				return adv.PushBranch("refs/heads/seed/"+ContractPeer, true, DefaultBranch, map[string]string{"steal.txt": "1"})
			},
		},
	}
}
