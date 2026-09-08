package project

// Builders receive verified prefixes in real flow; these internal
// drills pin the defensive branch anyway: an unfoldable actor history
// surfaces a derivation error from every keyring-consuming builder
// instead of a silent partial view.

import (
	"encoding/json"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

func unfoldableChain() []*event.Record {
	return []*event.Record{
		{Event: event.Event{V: "seed/0", TS: "2026-09-01T00:00:00Z", Actor: "aa", Verb: "system.protocol.upgraded", Subject: "system", Payload: json.RawMessage(`{"to": "seed/1"}`)}},
		{Event: event.Event{V: "seed/1", TS: "2026-09-01T00:00:01Z", Actor: "aa", Verb: "actor.enrolled", Subject: "bb", Payload: json.RawMessage(`{}`)}},
	}
}

func TestBuildersSurfaceDerivationErrors(t *testing.T) {
	builders := map[string]Builder{
		"roster": buildRoster,
		"actors": buildActors,
		"report": buildReport,
		"cache":  buildCache,
	}
	for name, build := range builders {
		if _, err := build(unfoldableChain(), Inputs{}); err == nil {
			t.Errorf("builder %s must surface a derivation error on unfoldable history", name)
		}
	}
}
