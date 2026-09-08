package admit

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/request"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: plans/os-48df10a2.md AC3 (the mirror arm; charter III.J
// row 2) — the corpus is fired at the ingress: every corpus file
// planted in summary, in reference, in origin and in the subject. A
// reference with hostile text is not a reference, so the shape refuses
// it; a hostile summary or origin is data the fact carries verbatim.
// With hostile requests pending, the dispatcher's reachable set on the
// contract and on system is exactly what it was, plus the answer the
// pending request affords; and answering a hostile request yields
// only what the dispatcher's allowlist already permitted: no state
// moves and no verb joins the set.
func TestNoHostileRequestWidensTheDispatcherSet(t *testing.T) {
	f := newRequestFixture(t, version.Seed7)
	corpus := hostileCorpus(t)
	before := map[string][]string{}
	for _, subject := range []string{"c-1", "system"} {
		before[subject] = Affordances(f.ctx, f.dispatcher, subject)
	}
	planted, refusedByShape := 0, 0
	// The boundary's refusal of hostile text is the shape rule's or
	// the classification rule's (a fixture over the payload budget
	// never reaches the request rule): either is the boundary, and
	// neither is an admission.
	boundary := func(err error) bool {
		var rerr *request.Error
		var cerr *ClassificationError
		return errors.As(err, &rerr) || errors.As(err, &cerr)
	}
	for name, hostile := range corpus {
		text := strings.TrimSpace(hostile)
		// The summary: bounded, so a long fixture refuses by size and
		// a short one admits as data.
		summary, _ := json.Marshal(text)
		payload := `{"origin": "mirror-a", "kind": "mirror-edit", "reference": "cards/c-1.md @ 0123456", "summary": ` + string(summary) + `}`
		err := f.check(t, f.service, request.FiledVerb, "system", payload)
		switch {
		case err == nil:
			f.step(f.service, request.FiledVerb, "system", payload)
			planted++
		case boundary(err):
			refusedByShape++
		default:
			t.Errorf("%s in summary: %v", name, err)
		}
		// The reference: a valid ref form with hostile text is not a
		// reference, and the shape refuses it.
		ref, _ := json.Marshal("cards/" + text + " @ 0123456")
		if err := f.check(t, f.service, request.FiledVerb, "system", `{"origin": "m", "kind": "mirror-edit", "reference": `+string(ref)+`, "summary": "s"}`); !boundary(err) {
			t.Errorf("%s in reference admitted: %v", name, err)
		}
		// The origin: one token or refused; a token admits as data.
		origin, _ := json.Marshal(text)
		err = f.check(t, f.service, request.FiledVerb, "system", `{"origin": `+string(origin)+`, "kind": "mirror-edit", "reference": "cards/c-1.md @ 0123456", "summary": "s"}`)
		if err == nil {
			f.step(f.service, request.FiledVerb, "system", `{"origin": `+string(origin)+`, "kind": "mirror-edit", "reference": "cards/c-1.md @ 0123456", "summary": "s"}`)
			planted++
		} else if !boundary(err) {
			t.Errorf("%s in origin: %v", name, err)
		}
		// The subject: a contract on the chain or system, nothing
		// else, so hostile text there never rides a notice.
		if err := f.check(t, f.service, request.FiledVerb, text, `{"origin": "m", "kind": "mirror-edit", "reference": "cards/c-1.md @ 0123456", "summary": "s"}`); err == nil {
			t.Errorf("%s as the subject admitted", name)
		}
	}
	if planted == 0 {
		t.Fatal("no hostile request was planted: the arm proved nothing")
	}
	t.Logf("planted %d hostile requests, %d summaries refused at the boundary", planted, refusedByShape)
	for _, subject := range []string{"c-1", "system"} {
		after := Affordances(f.ctx, f.dispatcher, subject)
		want := append([]string{}, before[subject]...)
		if subject == "system" {
			want = append(want, request.AnsweredVerb)
		}
		slices.Sort(want)
		if !slices.Equal(after, want) {
			t.Errorf("the dispatcher's reachable set on %s moved under hostile requests: before %v, after %v", subject, want, after)
		}
	}
	// Answering a hostile request is the dispatcher's own act under
	// its allowlist: declined, no state moves, nothing new lists.
	state, _ := f.ctx.Lifecycle.State("c-1")
	for _, r := range f.ctx.Lifecycle.Requests() {
		if r.Answered == nil {
			f.step(f.dispatcher, request.AnsweredVerb, r.Subject, fmt.Sprintf(`{"request": "%d", "outcome": "declined", "reason": "not a proposal this deployment takes"}`, r.Pos))
		}
	}
	if s, _ := f.ctx.Lifecycle.State("c-1"); s.State != state.State {
		t.Errorf("answering moved c-1 from %s to %s", state.State, s.State)
	}
	if after := Affordances(f.ctx, f.dispatcher, "c-1"); !slices.Equal(after, before["c-1"]) {
		t.Errorf("after the answers the set on c-1 is what it was: %v vs %v", after, before["c-1"])
	}
	if after := Affordances(f.ctx, f.dispatcher, "system"); !slices.Equal(after, before["system"]) {
		t.Errorf("after the answers the set on system is what it was: %v vs %v", after, before["system"])
	}
}
