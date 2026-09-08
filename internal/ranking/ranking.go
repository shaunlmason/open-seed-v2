// Package ranking derives the strongest qualified configurations
// (plans/os-c7554f18.md; build plan Phase 13 item 7; charter III.J row
// 3, §II.9): from the keyring's qualifications and the chain's own
// eval facts alone, an ordered list per capability of the qualified
// tuples and the evidence behind each. It is policy the supervisor
// writes into offers, never an admission rule: nothing here refuses a
// record, and a tuple's absence from the list changes nothing about
// what the boundary admits. The table in spec/ranking.md is the
// authority; Rules mirrors it and a drill holds the two together.
package ranking

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/tuple"
)

// The evidence kinds: the first pass since a tuple last held is its
// mint, later passes are spot checks, and a verdict tuple's passes are
// calibrations (plans/os-c7554f18.md D1).
const (
	KindMint        = "mint"
	KindSpotCheck   = "spot_check"
	KindCalibration = "calibration"
)

// Capabilities are the two the derivation ranks: claim tuples are
// qualified by evals, verdict tuples by calibrations.
var Capabilities = []string{keyring.CapClaim, keyring.CapVerdict}

// Rule is one row of the policy table. The spec states the same rows
// in the same words; TestRulesAreTheSpecsTable holds them together so
// the policy cannot change in one place only.
type Rule struct {
	Name   string
	Policy string
}

// Rules is the policy table (spec/ranking.md "The policy table").
var Rules = []Rule{
	{"score", "the count of qualifying evidence since the tuple last held: the first pass is the mint, later passes are spot checks, and a verdict tuple's passes are calibrations"},
	{"tie", "the latest pass's ts, newer first, then the tuple's canonical JSON, lower first"},
	{"excluded", "a tuple whose latest fact is a disqualification, or whose every holder is suspended or revoked: absent, not last"},
	{"agreement", "with the gold supplied, verdict tuples of equal score order by their mean calibration agreement, higher first, unrefined entries after; without it the field is null and the ranking says so"},
	{"instant", "the declared instant, never a clock: the projection derives at the latest qualification fact's ts, the verbs at --as-of or the offer's own instant"},
}

// Evidence is one qualifying fact behind a ranked tuple: the
// actor.qualified record by chain position, the holder it qualified,
// the eval contract and verdict it cited, and, for a calibration with
// the gold supplied, the agreement figure as a three-decimal string.
type Evidence struct {
	Kind      string  `json:"kind"`
	Actor     string  `json:"actor"`
	Contract  string  `json:"contract"`
	Verdict   int     `json:"verdict"`
	Position  int     `json:"position"`
	TS        string  `json:"ts"`
	Agreement *string `json:"agreement,omitempty"`
}

// Entry is one ranked tuple: its score, the ts of its latest pass, the
// active holders whose admissible set carries it (sorted), the
// evidence in chain order, and the mean agreement over its
// calibrations when the gold refined the ranking (null otherwise).
type Entry struct {
	Tuple     tuple.Tuple `json:"tuple"`
	Score     int         `json:"score"`
	Latest    string      `json:"latest"`
	Holders   []string    `json:"holders"`
	Evidence  []Evidence  `json:"evidence"`
	Agreement *string     `json:"agreement"`
	// mean is the unrounded agreement the order reads (review finding
	// on the task PR): two means that round to one display string are
	// still two means, and the higher ranks first.
	mean    float64
	hasMean bool
}

// Ranking is one derivation: the declared instant it was derived at,
// whether the gold refined the verdict ranking, and the ordered
// entries per capability (every capability present, empty when
// nothing ranks).
type Ranking struct {
	AsOf         string             `json:"as_of"`
	Refined      bool               `json:"agreement_refined"`
	Capabilities map[string][]Entry `json:"capabilities"`
}

// Inputs is everything Derive reads. Records is the verified prefix;
// Ring is its keyring (derived from Records when nil); AsOf is the
// DECLARED instant, recorded and never read from a clock; Agreement,
// when supplied, answers the calibration agreement of a verdict cited
// by contract and position, and its presence is what refines the
// verdict ranking.
type Inputs struct {
	Records   []*event.Record
	Ring      *keyring.State
	AsOf      string
	Agreement func(contract string, verdict int) (float64, bool)
}

// fact is one actor.qualified or actor.disqualified read off the chain.
type fact struct {
	pos          int
	ts           string
	actor        string
	capability   string
	tuple        tuple.Tuple
	contract     string
	verdict      int
	disqualified bool
}

// Canonical is the tuple's canonical JSON, the tie rule's last word:
// the five fields in their declared order.
func Canonical(t tuple.Tuple) string {
	b, _ := json.Marshal(t)
	return string(b)
}

func key(capability string, t tuple.Tuple) string {
	return capability + "\x00" + Canonical(t)
}

// Derive ranks the qualified tuples per capability from the chain's
// facts (D1). It is a pure function of Records, Ring and the supplied
// agreement: the same chain derives the same bytes.
func Derive(in Inputs) (Ranking, error) {
	ring := in.Ring
	if ring == nil {
		var err error
		ring, _, err = keyring.StateAt(in.Records)
		if err != nil {
			return Ranking{}, err
		}
	}
	facts, err := readFacts(in.Records)
	if err != nil {
		return Ranking{}, err
	}
	byKey := map[string][]fact{}
	var order []string
	for _, f := range facts {
		k := key(f.capability, f.tuple)
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], f)
	}
	holders := activeHolders(ring)
	out := Ranking{AsOf: in.AsOf, Refined: in.Agreement != nil, Capabilities: map[string][]Entry{}}
	for _, capability := range Capabilities {
		entries := []Entry{}
		for _, k := range order {
			fs := byKey[k]
			if fs[0].capability != capability {
				continue
			}
			if e, ok := entry(capability, fs, holders[k], in.Agreement); ok {
				entries = append(entries, e)
			}
		}
		sortEntries(entries, capability == keyring.CapVerdict && in.Agreement != nil)
		out.Capabilities[capability] = entries
	}
	return out, nil
}

// Top returns the first n tuples of a capability's ranking: the
// scheduling input an offer carries (D2). Nil when nothing ranks.
func Top(r Ranking, capability string, n int) []tuple.Tuple {
	var out []tuple.Tuple
	for i, e := range r.Capabilities[capability] {
		if i >= n {
			break
		}
		out = append(out, e.Tuple)
	}
	return out
}

// readFacts reads every applied qualification fact in chain order.
// The prefix is admitted, so a payload that does not parse is a
// malformed admitted record, surfaced rather than skipped.
func readFacts(records []*event.Record) ([]fact, error) {
	var out []fact
	for pos, rec := range records {
		if rec == nil {
			continue
		}
		verb := rec.Event.Verb
		if verb != keyring.VerbQualified && verb != keyring.VerbDisqualified {
			continue
		}
		var p struct {
			Capability string      `json:"capability"`
			Tuple      tuple.Tuple `json:"tuple"`
			Contract   string      `json:"contract"`
			Verdict    string      `json:"verdict"`
		}
		if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
			return nil, fmt.Errorf("position %d: %s carries no readable qualification payload: %v", pos, verb, err)
		}
		verdict, err := strconv.Atoi(p.Verdict)
		if err != nil {
			return nil, fmt.Errorf("position %d: %s cites verdict %q, not a position", pos, verb, p.Verdict)
		}
		out = append(out, fact{
			pos: pos, ts: rec.Event.TS, actor: rec.Event.Subject, capability: p.Capability, tuple: p.Tuple,
			contract: p.Contract, verdict: verdict, disqualified: verb == keyring.VerbDisqualified,
		})
	}
	return out, nil
}

// activeHolders maps each (capability, tuple) to the active actors
// whose admissible set carries it, sorted: a suspended or revoked
// holder is not one (the excluded row).
func activeHolders(ring *keyring.State) map[string][]string {
	out := map[string][]string{}
	for _, fp := range ring.Actors() {
		e, ok := ring.Get(fp)
		if !ok || e.Standing != keyring.StandingActive {
			continue
		}
		for _, capability := range Capabilities {
			for _, t := range ring.GrantTuples(fp, capability) {
				k := key(capability, t)
				out[k] = append(out[k], fp)
			}
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// entry builds one tuple's entry from its facts in chain order, or
// reports that the tuple does not rank: its latest fact is a
// disqualification, or no active holder carries it.
func entry(capability string, fs []fact, holders []string, agreement func(string, int) (float64, bool)) (Entry, bool) {
	if len(fs) == 0 || fs[len(fs)-1].disqualified || len(holders) == 0 {
		return Entry{}, false
	}
	start := 0
	for i, f := range fs {
		if f.disqualified {
			start = i + 1
		}
	}
	e := Entry{Tuple: fs[0].tuple, Holders: append([]string(nil), holders...), Evidence: []Evidence{}}
	var sum float64
	var scored int
	for i, f := range fs[start:] {
		ev := Evidence{Kind: KindSpotCheck, Actor: f.actor, Contract: f.contract, Verdict: f.verdict, Position: f.pos, TS: f.ts}
		switch {
		case capability == keyring.CapVerdict:
			ev.Kind = KindCalibration
		case i == 0:
			ev.Kind = KindMint
		}
		if agreement != nil && capability == keyring.CapVerdict {
			if a, ok := agreement(f.contract, f.verdict); ok {
				s := fmt.Sprintf("%.3f", a)
				ev.Agreement = &s
				sum += a
				scored++
			}
		}
		e.Evidence = append(e.Evidence, ev)
		e.Latest = f.ts
	}
	e.Score = len(e.Evidence)
	if scored > 0 {
		e.mean, e.hasMean = sum/float64(scored), true
		mean := fmt.Sprintf("%.3f", e.mean)
		e.Agreement = &mean
	}
	return e, true
}

// sortEntries orders by the table: score, then (refined verdict
// rankings only) agreement with unrefined entries after, then the
// latest pass newer first, then the canonical JSON lower first. Every
// key is stated, so map order never decides.
func sortEntries(entries []Entry, refined bool) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if refined {
			switch {
			case a.hasMean && !b.hasMean:
				return true
			case !a.hasMean && b.hasMean:
				return false
			case a.hasMean && b.hasMean && a.mean != b.mean:
				return a.mean > b.mean
			}
		}
		if c := compareTS(a.Latest, b.Latest); c != 0 {
			return c > 0
		}
		return Canonical(a.Tuple) < Canonical(b.Tuple)
	})
}

// compareTS orders two RFC3339 instants, +1 when a is newer; an
// unparseable ts compares as its string, so the order is still total.
func compareTS(a, b string) int {
	ta, ea := time.Parse(time.RFC3339, a)
	tb, eb := time.Parse(time.RFC3339, b)
	if ea == nil && eb == nil {
		switch {
		case ta.After(tb):
			return 1
		case tb.After(ta):
			return -1
		}
		return 0
	}
	return strings.Compare(a, b)
}
