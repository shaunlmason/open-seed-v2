package transition_test

// Self-validation drills (plans/os-d69a6c91.md step 7): planted bad
// tables refuse by name, the shipped table loads clean, and the
// normative spec copy is byte-identical to the embedded one (the
// classify.json precedent).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

// table builds a minimal valid table string the planted cases mutate.
const validTable = `{
  "schema_version": "1",
  "states": [
    {"name": "backlog", "initial": true},
    {"name": "ready"},
    {"name": "in_progress"},
    {"name": "review"},
    {"name": "blocked"},
    {"name": "done", "terminal": true},
    {"name": "cancelled", "terminal": true}
  ],
  "transitions": [
    {"verb": "intent.filed", "from": null, "to": "backlog"},
    {"verb": "contract.specified", "from": ["backlog"], "to": "ready"},
    {"verb": "claim.taken", "from": ["ready"], "to": "in_progress"},
    {"verb": "submission.made", "from": ["in_progress"], "to": "review"},
    {"verb": "claim.released", "from": ["in_progress"], "to": "ready"},
    {"verb": "claim.parked", "from": ["in_progress"], "to": "blocked"},
    {"verb": "claim.reaped", "from": ["in_progress"], "to": "ready"},
    {"verb": "merge.observed", "from": ["review"], "to": "done"},
    {"verb": "contract.blocked", "from": ["ready"], "to": "blocked"},
    {"verb": "contract.unblocked", "from": ["blocked"], "to": "ready"},
    {"verb": "contract.cancelled", "from": ["backlog", "ready", "blocked", "review"], "to": "cancelled"}
  ]
}`

func TestEmbeddedTableMatchesTheSpecCopy(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "spec", "transitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(spec) != string(transition.TableJSON()) {
		t.Fatal("spec/transitions.json and the embedded table must be byte-identical")
	}
	if _, err := transition.Default(); err != nil {
		t.Fatalf("the shipped table must self-validate: %v", err)
	}
}

func TestSelfValidationRefusesByName(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"unknown to-state", `"to": "ready"`, `"to": "nowhere"`, "unknown state"},
		{"unknown from-state", `["backlog"], "to": "ready"`, `["limbo"], "to": "ready"`, "unknown state"},
		{"two birth verbs", `{"verb": "contract.specified", "from": ["backlog"], "to": "ready"}`, `{"verb": "contract.specified", "from": null, "to": "backlog"}`, "two birth verbs"},
		{"no birth verb", `{"verb": "intent.filed", "from": null, "to": "backlog"}`, `{"verb": "intent.filed", "from": ["backlog"], "to": "backlog"}`, "no birth verb"},
		{"terminal outgoing", `{"verb": "contract.unblocked", "from": ["blocked"], "to": "ready"}`, `{"verb": "contract.unblocked", "from": ["done"], "to": "ready"}`, "terminal state"},
		{"duplicate verb", `{"verb": "contract.blocked", "from": ["ready"], "to": "blocked"}`, `{"verb": "claim.taken", "from": ["ready"], "to": "blocked"}`, "duplicate verb"},
		{"two initial states", `{"name": "ready"}`, `{"name": "ready", "initial": true}`, "two initial states"},
		{"birth off the initial state", `{"verb": "intent.filed", "from": null, "to": "backlog"}`, `{"verb": "intent.filed", "from": null, "to": "ready"}`, "initial state"},
	}
	for _, c := range cases {
		mutated := strings.Replace(validTable, c.old, c.new, 1)
		if mutated == validTable {
			t.Fatalf("%s: the mutation did not apply", c.name)
		}
		_, err := transition.Parse([]byte(mutated))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want a named refusal containing %q, got %v", c.name, c.want, err)
		}
	}

	// The unreachable invariant needs a state no transition enters but
	// that itself exits cleanly (so the wedge check stays quiet); the
	// pinned in_progress exits stay untouched.
	unreachable := strings.Replace(validTable, `{"name": "blocked"},`,
		`{"name": "blocked"}, {"name": "limbo"},`, 1)
	unreachable = strings.Replace(unreachable,
		`{"verb": "contract.unblocked", "from": ["blocked"], "to": "ready"},`,
		`{"verb": "contract.unblocked", "from": ["blocked"], "to": "ready"},
    {"verb": "limbo.exited", "from": ["limbo"], "to": "ready"},`, 1)
	if _, err := transition.Parse([]byte(unreachable)); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("a state with no inflow must refuse as unreachable, got %v", err)
	}

	// The wedge invariant needs a state that exists and is entered but
	// never exits: park into a blocked state with no outgoing rows.
	wedged := strings.Replace(validTable,
		`{"verb": "contract.unblocked", "from": ["blocked"], "to": "ready"},`, "", 1)
	wedged = strings.Replace(wedged, `"blocked", `, "", 1)
	_, err := transition.Parse([]byte(wedged))
	if err == nil || !strings.Contains(err.Error(), "wedge") {
		t.Fatalf("a state that cannot reach a terminal one must refuse as a wedge, got %v", err)
	}

	// The pinned invariant: the in_progress exits are exactly the four
	// deliberate ones — adding a fifth refuses.
	extra := strings.Replace(validTable,
		`{"verb": "contract.cancelled", "from": ["backlog", "ready", "blocked", "review"], "to": "cancelled"}`,
		`{"verb": "contract.cancelled", "from": ["backlog", "ready", "blocked", "review", "in_progress"], "to": "cancelled"}`, 1)
	_, err = transition.Parse([]byte(extra))
	if err == nil || !strings.Contains(err.Error(), "deliberate exits") {
		t.Fatalf("a fifth in_progress exit must refuse against the pinned set, got %v", err)
	}
	// And removing one refuses the same way.
	fewer := strings.Replace(validTable,
		`{"verb": "claim.parked", "from": ["in_progress"], "to": "blocked"},`, "", 1)
	_, err = transition.Parse([]byte(fewer))
	if err == nil || !strings.Contains(err.Error(), "deliberate exits") {
		t.Fatalf("a missing deliberate exit must refuse against the pinned set, got %v", err)
	}
}

func lifecycleEvent(verb, subject string) *event.Record {
	return &event.Record{Event: event.Event{V: "seed/1", TS: "2026-09-01T00:00:00Z", Actor: "aa", Verb: verb, Subject: subject, Payload: json.RawMessage(`{}`)}}
}

func payloadEvent(v, verb, subject, payload string) *event.Record {
	return &event.Record{Event: event.Event{V: v, TS: "2026-09-01T00:00:00Z", Actor: "aa", Verb: verb, Subject: subject, Payload: json.RawMessage(payload)}}
}

// A grandfathered seed/0 record whose payload happens to carry a
// count must not become the milestone high-water mark: the
// summarization boundary activates at seed/1 with the rest of the
// lifecycle semantics.
func TestMilestoneFoldHonorsActivationBoundary(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	pre := payloadEvent("seed/0", "progress.milestone", "c-1", `{"count": 999, "step": "old"}`)
	fold := tab.FoldRecords([]*event.Record{pre})
	if err := fold.CheckMilestone("c-1", 40, []byte(`{"count": 1, "step": "fresh"}`)); err != nil {
		t.Fatalf("a post-upgrade milestone must not be wedged by grandfathered history: %v", err)
	}
	live := tab.FoldRecords([]*event.Record{payloadEvent("seed/1", "progress.milestone", "c-1", `{"count": 999, "step": "new"}`)})
	if err := live.CheckMilestone("c-1", 40, []byte(`{"count": 1, "step": "fresh"}`)); err == nil {
		t.Fatal("a seed/1 milestone must set the high-water mark")
	}
}

func TestCheckAndFold(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	if tab.BirthVerb() != "intent.filed" || tab.Initial() != "backlog" {
		t.Fatalf("birth/initial wrong: %s/%s", tab.BirthVerb(), tab.Initial())
	}
	if !tab.Terminal("done") || !tab.Terminal("cancelled") || tab.Terminal("review") {
		t.Fatal("terminal markers wrong")
	}
	if !tab.IsLifecycleVerb("claim.taken") || tab.IsLifecycleVerb("progress.milestone") {
		t.Fatal("lifecycle vocabulary boundary wrong")
	}

	// Legality is the table, including the refusal fields.
	if to, err := tab.Check("c-1", "", "intent.filed"); err != nil || to != "backlog" {
		t.Fatalf("birth: %v %s", err, to)
	}
	_, err = tab.Check("c-1", "backlog", "intent.filed")
	var inv *transition.InvalidTransitionError
	if !asInvalid(err, &inv) || inv.From != "backlog" || inv.Verb != "intent.filed" {
		t.Fatalf("birth on an existing subject must refuse typed, got %v", err)
	}
	_, err = tab.Check("c-1", "", "claim.taken")
	if !asInvalid(err, &inv) || inv.From != "" {
		t.Fatalf("non-birth on a fresh subject must refuse typed, got %v", err)
	}
	_, err = tab.Check("c-1", "backlog", "claim.taken")
	if !asInvalid(err, &inv) || inv.Subject != "c-1" || inv.From != "backlog" || inv.Verb != "claim.taken" {
		t.Fatalf("claim on backlog must refuse naming subject, state, verb, got %v", err)
	}
	// contract.cancelled has no in_progress source, structurally.
	if _, err := tab.Check("c-1", "in_progress", "contract.cancelled"); err == nil {
		t.Fatal("cancelling an in_progress contract must be impossible")
	}

	// The fold: happy path, an anomalous event skipped visibly, a free
	// verb ignored, and an anomalous-only subject surfaced.
	records := []*event.Record{
		lifecycleEvent("intent.filed", "c-A"),
		lifecycleEvent("claim.taken", "c-A"), // anomaly: backlog cannot be claimed
		lifecycleEvent("contract.specified", "c-A"),
		lifecycleEvent("claim.taken", "c-A"),
		lifecycleEvent("submission.made", "c-B"), // anomaly: unknown subject
		lifecycleEvent("merge.observed", "c-A"),  // anomaly: in_progress cannot be done
		lifecycleEvent("submission.made", "c-A"),
		lifecycleEvent("merge.observed", "c-A"),
	}
	fold := tab.FoldRecords(records)
	a, ok := fold.State("c-A")
	// Six visible anomalies: the two table-illegal events, the raw
	// specification's empty acceptance, the raw submission's missing
	// fence citation and missing packet (the exits still apply —
	// skipping them would wedge the subject), and the applied
	// observation's skipped reconciliation chain
	// (plans/os-6cdc15be.md).
	if !ok || a.State != "done" || a.Anomalies != 6 || a.Since != 7 {
		t.Fatalf("c-A fold wrong: %+v ok=%v", a, ok)
	}
	if a.Claim != nil {
		t.Fatalf("the deliberate exit must clear the claim even when malformed: %+v", a.Claim)
	}
	b, ok := fold.State("c-B")
	if !ok || b.State != "" || b.Anomalies != 1 {
		t.Fatalf("an anomalous-only subject must surface with an empty state: %+v ok=%v", b, ok)
	}
	if _, ok := fold.State("c-C"); ok {
		t.Fatal("an unnamed subject folds to nothing")
	}
	if s, ok := tab.StateAt(records, "c-A"); !ok || s.State != "done" {
		t.Fatalf("StateAt mirrors the fold: %+v", s)
	}
	if subs := fold.Subjects(); len(subs) != 2 || subs[0] != "c-A" || subs[1] != "c-B" {
		t.Fatalf("subjects in first-appearance order: %v", subs)
	}

	// Pre-activation history is inert: an upgraded ledger's seed/0
	// events stay grandfathered even where their verbs later became
	// lifecycle verbs, so they occupy no state and the real seed/1
	// filing is the birth, not a duplicate.
	pre := lifecycleEvent("intent.filed", "c-G")
	pre.Event.V = "seed/0"
	gfold := tab.FoldRecords([]*event.Record{pre})
	if _, ok := gfold.State("c-G"); ok {
		t.Fatal("a pre-activation event must fold inert")
	}
	gfold = tab.FoldRecords([]*event.Record{pre, lifecycleEvent("intent.filed", "c-G")})
	g, ok := gfold.State("c-G")
	if !ok || g.State != "backlog" || g.Anomalies != 0 || g.Since != 1 {
		t.Fatalf("a seed/1 filing after grandfathered history must be the birth: %+v ok=%v", g, ok)
	}
}

func asInvalid(err error, target **transition.InvalidTransitionError) bool {
	e, ok := err.(*transition.InvalidTransitionError)
	if ok {
		*target = e
	}
	return ok
}

func TestCompleteness(t *testing.T) {
	full := `{"intent": "fix it", "tier": "standard", "budget": "small", "routing": "core"}`
	if err := transition.CheckCompleteness("intent.filed", "c-1", []byte(full)); err != nil {
		t.Fatalf("a complete filing passes: %v", err)
	}
	err := transition.CheckCompleteness("intent.filed", "c-1", []byte(`{"intent": "x", "tier": ""}`))
	ie, ok := err.(*transition.IncompleteError)
	if !ok || len(ie.Missing) != 3 {
		t.Fatalf("missing fields must be named: %v", err)
	}
	if err := transition.CheckCompleteness("contract.specified", "c-1", []byte(`{}`)); err == nil {
		t.Fatal("specification without an acceptance reference must refuse")
	}
	if err := transition.CheckCompleteness("contract.specified", "c-1", []byte(`{"acceptance": {"ref": "specs/a.md @ abc1234", "executable": false}}`)); err != nil {
		t.Fatalf("a present acceptance reference passes: %v", err)
	}
	if err := transition.CheckCompleteness("claim.taken", "c-1", []byte(`{}`)); err != nil {
		t.Fatalf("verbs outside the completeness map are unconstrained here: %v", err)
	}
}

// The spec prose and the table cannot drift: lifecycle.md quotes the
// table in a json fence, and this drill pins the quotation to the
// normative file until the generated-prose machinery lands.
func TestSpecProseQuotesTheTable(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "spec", "lifecycle.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(spec), "```json\n")
	if !ok {
		t.Fatal("lifecycle.md must quote the table in a json fence")
	}
	quoted, _, ok := strings.Cut(after, "```")
	if !ok {
		t.Fatal("unterminated json fence in lifecycle.md")
	}
	table, err := os.ReadFile(filepath.Join("..", "..", "spec", "transitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(quoted) != strings.TrimSpace(string(table)) {
		t.Fatal("lifecycle.md's quoted table drifted from transitions.json")
	}
}
