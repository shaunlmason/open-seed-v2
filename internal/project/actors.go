// The actor-view projection (plans/os-fecfb3f7.md step 3): the
// per-actor drill-down the roster summarizes. For every roster
// candidate it adds standing_history (each actor.* event on the
// subject, with the acting signer) and signed (every record this
// fingerprint signed): the attribution surface, which is how the view
// shows a revoked key's history surviving revocation.

package project

import (
	"encoding/json"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

// ActorsFile is the actor-view projection's one view file.
const ActorsFile = "actors.json"

// StandingEvent is one actor.* event applied to a subject.
type StandingEvent struct {
	Position int    `json:"position"`
	Verb     string `json:"verb"`
	Acting   string `json:"acting"`
}

// SignedEvent is one record a fingerprint signed.
type SignedEvent struct {
	Position int    `json:"position"`
	Verb     string `json:"verb"`
	Subject  string `json:"subject"`
}

// ActorEntry is one actor in the view: the roster fields plus the two
// streams.
type ActorEntry struct {
	Fingerprint     string          `json:"fingerprint"`
	Kind            string          `json:"kind"`
	Name            string          `json:"name"`
	Standing        string          `json:"standing"`
	Root            bool            `json:"root"`
	Grants          []string        `json:"grants"`
	StandingHistory []StandingEvent `json:"standing_history"`
	Signed          []SignedEvent   `json:"signed"`
}

// Actors returns the actor-view projection.
func Actors() Projection {
	return Projection{Name: "actors", Build: buildActors}
}

func buildActors(records []*event.Record, _ Inputs) (map[string][]byte, error) {
	state, _, err := keyring.StateAt(records)
	if err != nil {
		return nil, err
	}
	order := candidateFingerprints(records)
	entries := make([]ActorEntry, 0, len(order))
	for _, fp := range order {
		e, ok := state.Get(fp)
		if !ok {
			continue
		}
		grants := e.Grants
		if grants == nil {
			grants = []string{}
		}
		entry := ActorEntry{
			Fingerprint:     fp,
			Kind:            e.Kind,
			Name:            e.Name,
			Standing:        string(e.Standing),
			Root:            e.Root,
			Grants:          grants,
			StandingHistory: []StandingEvent{},
			Signed:          []SignedEvent{},
		}
		for pos, rec := range records {
			ev := &rec.Event
			if keyring.IsActorVerb(ev.Verb) && ev.Subject == fp {
				entry.StandingHistory = append(entry.StandingHistory, StandingEvent{
					Position: pos, Verb: ev.Verb, Acting: ev.Actor,
				})
			}
			if ev.Actor == fp {
				entry.Signed = append(entry.Signed, SignedEvent{
					Position: pos, Verb: ev.Verb, Subject: ev.Subject,
				})
			}
		}
		entries = append(entries, entry)
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ActorsFile: append(b, '\n')}, nil
}
