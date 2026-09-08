package boundary

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

const declaration = `{"posture": "cooperative", "protocol": "seed/7", "guardrails": {"squads": {"core": {"default": "standard", "max_agent": "standard"}, "edge": {"default": "trivial", "max_agent": "critical"}}}, "teams": {"squads": [{"name": "core", "lanes": ["implementer"]}, {"name": "edge", "lanes": ["implementer"]}]}, "boundary": {"accepts": ["cross-repo"], "ingress": "git@forge:org/ledger.git"}}`

func key(first byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = first
	return ed25519.NewKeyFromSeed(seed)
}

// conformance: plans/os-40ed0ca0.md AC1 — the card renders from the
// declaration (names only), signs with the operator key, verifies
// against it and no other, parses strictly, and refuses a card that
// names internals or carries no signature.
func TestCardSaysWhatIsOfferedAndNothingElse(t *testing.T) {
	cfg, err := posture.Parse([]byte(declaration))
	if err != nil {
		t.Fatal(err)
	}
	card, err := Render(cfg, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if card.Protocol != "seed/7" || len(card.Squads) != 2 || card.Squads[1].Name != "edge" || strings.Join(card.Squads[1].Tiers, ",") != "critical,trivial" || !card.Accepts("cross-repo") || card.Accepts("mirror-edit") {
		t.Fatalf("the render: %+v", card)
	}
	if card.Signature != "" || Check(card) == nil {
		t.Fatal("an unsigned card offers nothing")
	}
	op, other := key(1), key(2)
	if err := Sign(card, op); err != nil {
		t.Fatal(err)
	}
	if err := Verify(card, op.Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("the operator key verifies: %v", err)
	}
	if err := Verify(card, other.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("another key does not")
	}
	b, _ := json.Marshal(card)
	parsed, err := Parse(b)
	if err != nil || parsed.Signer != card.Signer {
		t.Fatalf("the published card parses: %v", err)
	}
	fields, _ := FieldsOf(b)
	if strings.Join(fields, ",") != strings.Join(CardFields, ",") {
		t.Fatalf("the card's fields are the pin: %v", fields)
	}
	tampered := *card
	tampered.Name = "acme-2"
	if err := Verify(&tampered, op.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("a changed card no longer verifies")
	}
	for name, body := range map[string]string{
		"an extra field":  strings.Replace(string(b), `{"name"`, `{"lanes": ["lane/dispatcher.md"], "name"`, 1),
		"an internal":     strings.Replace(string(b), `"acme"`, `"acme (see lanes/dispatcher manifest)"`, 1),
		"unsigned":        strings.Replace(string(b), `"signature":"`+card.Signature+`"`, `"signature":""`, 1),
		"a foreign kind":  strings.Replace(string(b), `"cross-repo"`, `"wish"`, 1),
		"a foreign kind2": strings.Replace(string(b), `"receipt"`, `"ledger"`, 1),
		"no protocol":     strings.Replace(string(b), `"seed/7"`, `"seed/99"`, 1),
		"trailing data":   string(b) + ` {"more": true}`,
		"duplicate kind":  strings.Replace(string(b), `["cross-repo"]`, `["cross-repo","cross-repo"]`, 1),
		"duplicate kind2": strings.Replace(string(b), `"receipt","plan"`, `"receipt","receipt"`, 1),
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: the card parsed", name)
		}
	}
	noBoundary, _ := posture.Parse([]byte(`{"posture": "cooperative"}`))
	if _, err := Render(noBoundary, "acme"); err == nil {
		t.Fatal("a declaration without a boundary block publishes no card")
	}
	if _, err := Render(cfg, " "); err == nil {
		t.Fatal("the card names the deployment")
	}
	fp, _ := event.Fingerprint(op.Public().(ed25519.PublicKey))
	if card.Signer != fp {
		t.Fatal("the signer is the operator key's fingerprint")
	}
}

func rec(v, verb, subject, actor, payload string) *event.Record {
	return &event.Record{Event: event.Event{V: v, TS: "2026-09-03T00:00:00Z", Actor: actor, Verb: verb, Subject: subject, Payload: json.RawMessage(payload)}}
}

// conformance: plans/os-40ed0ca0.md AC2 — the five states derive from
// the target chain as it moves, requested through done or declined,
// with the artifact digests the contract published and no other
// field; a request that is not cross-repo is no task at all.
func TestTaskLifecycleIsDerivedAndFiveStated(t *testing.T) {
	table, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	receipt := strings.Repeat("a", 64)
	records := []*event.Record{
		rec(version.Seed7, "request.filed", "system", "ingress", `{"origin": "src", "kind": "cross-repo", "reference": "src/c-9 @ 0123456", "summary": "shared work"}`),
		rec(version.Seed7, "request.filed", "system", "dash", `{"origin": "dash", "kind": "dashboard-action", "reference": "a @ 0123456", "summary": "not a task"}`),
	}
	states := func(recs []*event.Record) []Task { return Tasks(recs, table.FoldRecords(recs)) }
	tasks := states(records)
	if len(tasks) != 1 || tasks[0].State != StateRequested || tasks[0].Request != 0 || tasks[0].Answer != nil || len(tasks[0].Artifacts) != 0 {
		t.Fatalf("requested, and the dashboard request is no task: %+v", tasks)
	}
	declined := append(records, rec(version.Seed7, "request.answered", "system", "dispatcher", `{"request": "0", "outcome": "declined", "reason": "no"}`))
	if tasks := states(declined); tasks[0].State != StateDeclined || *tasks[0].Answer != 2 {
		t.Fatalf("declined: %+v", tasks)
	}
	accepted := append(records,
		rec(version.Seed7, "intent.filed", "c-10", "dispatcher", `{"intent": "shared work", "tier": "trivial", "budget": "small", "routing": "core"}`),
		rec(version.Seed7, "request.answered", "system", "dispatcher", `{"request": "0", "outcome": "filed", "intent": "2"}`),
	)
	if tasks := states(accepted); tasks[0].State != StateAccepted || *tasks[0].Answer != 3 {
		t.Fatalf("accepted: %+v", tasks)
	}
	working := append(accepted,
		rec(version.Seed7, "contract.specified", "c-10", "dispatcher", `{"acceptance": {"ref": "spec.md @ 0123456789abcdef", "executable": false}}`),
		rec(version.Seed7, "claim.taken", "c-10", "worker", `{}`),
	)
	if tasks := states(working); tasks[0].State != StateWorking {
		t.Fatalf("working: %+v", tasks)
	}
	packet := `{"acceptance": ["resume"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	done := append(working,
		rec(version.Seed7, "submission.made", "c-10", "worker", `{"fence": "5", "packet": `+packet+`}`),
		rec(version.Seed7, "verdict.rendered", "c-10", "verifier", `{"verdict": "pass", "receipt": "`+receipt+`", "submission": "6", "independence": "L1"}`),
		rec(version.Seed7, "merge.requested", "c-10", "worker", `{"verdict": "7"}`),
		rec(version.Seed7, "merge.observed", "c-10", "root", `{"merged": "`+strings.Repeat("0", 40)+`", "pr": "pr/1"}`),
	)
	tasks = states(done)
	if tasks[0].State != StateDone || len(tasks[0].Artifacts) != 1 || tasks[0].Artifacts[0] != receipt {
		t.Fatalf("done with the receipt by digest: %+v", tasks)
	}
	b, _ := json.Marshal(tasks[0])
	fields, _ := FieldsOf(b)
	if strings.Join(fields, ",") != strings.Join(TaskFields, ",") {
		t.Fatalf("the task's fields are the pin: %v", fields)
	}
	if err := Sweep(b, []string{"worker", "dispatcher", "c-10", "shared work", packet}); err != nil {
		t.Fatalf("the task carries no actor, contract, payload or packet: %v", err)
	}
	if err := Sweep([]byte(`{"state": "done", "actor": "worker"}`), []string{"worker"}); err == nil {
		t.Fatal("the sweep catches ledger material")
	}
	if _, err := FieldsOf([]byte(`[1]`)); err == nil {
		t.Fatal("a non-object has no fields")
	}
	if Tasks(nil, nil) == nil {
		t.Fatal("no fold, no tasks, never nil")
	}
}
