// The ranking projection (plans/os-c7554f18.md D3; spec/ranking.md):
// the strongest qualified tuples per capability, derived from the
// verified prefix alone at the tip record's own instant. Input-free
// like every projection but the report: it reads no gold (the gold is
// outside the tree), so its agreement fields are null and it says so.

package project

import (
	"encoding/json"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ranking"
)

// RankingFile is the projection's one view file.
const RankingFile = "ranking.json"

// Ranking returns the ranking projection.
func Ranking() Projection {
	return Projection{Name: "ranking", Version: "1", Build: buildRanking}
}

// DeriveRanking is the shared derivation: the projection builds from
// it and the doctor reads it at a ledger's tip. The instant is the ts
// of the latest qualification fact, never a clock and never the tip's
// own ts (review finding on the task PR: an unrelated append would
// move the tip's instant and change the bytes of a ranking whose
// evidence did not change), so the same evidence derives the same
// bytes; a prefix carrying no qualification derives at no instant.
func DeriveRanking(records []*event.Record) (ranking.Ranking, error) {
	ring, _, err := keyring.StateAt(records)
	if err != nil {
		return ranking.Ranking{}, err
	}
	asOf := ""
	for _, rec := range records {
		if rec != nil && (rec.Event.Verb == keyring.VerbQualified || rec.Event.Verb == keyring.VerbDisqualified) {
			asOf = rec.Event.TS
		}
	}
	return ranking.Derive(ranking.Inputs{Records: records, Ring: ring, AsOf: asOf})
}

func buildRanking(records []*event.Record, _ Inputs) (map[string][]byte, error) {
	r, err := DeriveRanking(records)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{RankingFile: append(b, '\n')}, nil
}
