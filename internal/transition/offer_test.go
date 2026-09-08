package transition_test

// The offer fold drills (plans/os-c61c3392.md; spec/offers.md):
// well-shaped offers fold tolerantly (raw pushes included), malformed
// payloads fold to nothing, and liveness is derived — ready subject,
// unexpired, and unconsumed by any later applied claim, behind every
// re-ready path.

import (
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

func offerEvent(subject, expires string) *event.Record {
	return payloadEvent("seed/1", "offer.published", subject,
		`{"eligibility": {"capabilities": ["claim"], "tiers": ["trivial"]}, "expires": "`+expires+`"}`)
}

// conformance: III.H — offers expire and are consumed by claims;
// liveness is derived, never stored, and no re-ready path resurrects
// a taken offer.
func TestOfferFoldAndLiveness(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339, "2026-09-01T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	records := []*event.Record{
		lifecycleEvent("intent.filed", "c-1"),                                                                         // 0
		lifecycleEvent("contract.specified", "c-1"),                                                                   // 1
		offerEvent("c-1", "2026-09-02T00:00:00Z"),                                                                     // 2: live
		offerEvent("c-1", "2026-09-01T00:00:00Z"),                                                                     // 3: expired at now
		payloadEvent("seed/1", "offer.published", "c-1", `{"expires": "x"}`),                                          // 4: malformed, no fact
		payloadEvent("seed/1", "offer.published", "c-none", `{"eligibility": {}, "expires": "2026-09-02T00:00:00Z"}`), // 5: no subject, binds nothing
	}
	fold := tab.FoldRecords(records)
	s, ok := fold.State("c-1")
	if !ok || len(s.Offers) != 2 {
		t.Fatalf("two well-shaped offers fold, the malformed one to nothing: %+v", s.Offers)
	}
	if o := s.Offers[0]; o.Pos != 2 || o.Signer != "aa" || len(o.Capabilities) != 1 || o.Capabilities[0] != "claim" ||
		len(o.Tiers) != 1 || o.Tiers[0] != "trivial" || o.Expires != "2026-09-02T00:00:00Z" {
		t.Fatalf("the offer fact carries position, signer, scopes, and expiry: %+v", o)
	}
	if _, ok := fold.State("c-none"); ok {
		t.Fatal("an offer on a subject no lifecycle event created binds nothing")
	}
	if live := s.LiveOffers(now); len(live) != 1 || live[0].Pos != 2 {
		t.Fatalf("liveness keeps the unexpired offer only: %+v", live)
	}

	// A claim consumes every offer at or before it: after release the
	// subject is ready again inside the expiry window, and the taken
	// offer stays dead while a fresh publication lists.
	records = append(records,
		lifecycleEvent("claim.taken", "c-1"),    // 6
		lifecycleEvent("claim.released", "c-1"), // 7
	)
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	if s.State != "ready" || s.LastClaim != 6 {
		t.Fatalf("release re-readies with the consumption boundary at the claim: state %s, last claim %d", s.State, s.LastClaim)
	}
	if live := s.LiveOffers(now); len(live) != 0 {
		t.Fatalf("the taken offer never resurrects on re-ready: %+v", live)
	}
	records = append(records, offerEvent("c-1", "2026-09-02T00:00:00Z")) // 8: fresh intent
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	if live := s.LiveOffers(now); len(live) != 1 || live[0].Pos != 8 {
		t.Fatalf("a fresh publication on the re-readied subject lists: %+v", live)
	}

	// The same boundary holds behind park + unblock: the second claim
	// consumes the fresh offer too.
	records = append(records,
		lifecycleEvent("claim.taken", "c-1"),                // 9
		payloadEvent("seed/1", "claim.parked", "c-1", `{}`), // 10: packetless, tolerated visibly
		lifecycleEvent("contract.unblocked", "c-1"),         // 11
	)
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	if s.State != "ready" || s.LastClaim != 9 {
		t.Fatalf("park + unblock re-readies with the boundary advanced: state %s, last claim %d", s.State, s.LastClaim)
	}
	if live := s.LiveOffers(now); len(live) != 0 {
		t.Fatalf("the park path resurrects nothing either: %+v", live)
	}

	// Liveness needs ready: an offer standing while the subject is
	// claimed lists nothing.
	records = append(records,
		offerEvent("c-1", "2026-09-02T00:00:00Z"), // 12
		lifecycleEvent("claim.taken", "c-1"),      // 13
	)
	fold = tab.FoldRecords(records)
	s, _ = fold.State("c-1")
	if live := s.LiveOffers(now); live != nil {
		t.Fatalf("no offer is live off ready: %+v", live)
	}
}
