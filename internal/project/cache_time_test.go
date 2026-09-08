package project_test

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/project"
)

// conformance: charter III.G row 10 ("evidence, receipts, and verdicts
// are queryable by contract, actor, time, and outcome"), the row the
// Phase 10 exit recorded UNMET; plans/os-74ce2261.md D1 and D3 — every
// per-event cache table carries the envelope's ts verbatim beside the
// instant it names as an integer, a range compares the integer (RFC
// 3339 mixes fractional precision, which mis-orders as text), an
// unparseable ts folds NULL (queryable as such) and is counted in the
// report table's ts_unparsed row, and the four names of the row answer
// in one query.
func TestCacheIsQueryableByTime(t *testing.T) {
	root, worker := pKey(t, 1), pKey(t, 2)
	dir := t.TempDir()
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	g, err := genesis.Init(store, root, nil, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := genesis.Parse(g)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := payload.Resolver(g.Event.Actor)
	if err != nil {
		t.Fatal(err)
	}
	ring := map[string]ed25519.PublicKey{pFP(t, root): root.Public().(ed25519.PublicKey), pFP(t, worker): worker.Public().(ed25519.PublicKey)}
	loose := func(fp string) (ed25519.PublicKey, bool) { pub, ok := ring[fp]; return pub, ok }
	add := func(priv ed25519.PrivateKey, ts, v, verb, subject, body string) {
		t.Helper()
		tip, _, err := store.Tip()
		if err != nil {
			t.Fatal(err)
		}
		rec, err := event.Sign(event.Event{V: v, TS: ts, Actor: pFP(t, priv), Verb: verb, Subject: subject, Payload: json.RawMessage(body), Prev: tip}, priv)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Append(rec, loose); err != nil {
			t.Fatal(err)
		}
	}
	// Mixed fractional precision on purpose: text order and instant
	// order are different relations ("…02.25Z" sorts before "…02Z" as
	// text and after it as an instant), and the integer column is what
	// a range reads.
	add(root, "2026-09-01T01:00:00Z", "seed/0", "system.protocol.upgraded", "system", `{"to": "seed/1"}`)
	add(root, "2026-09-01T01:00:00.5Z", "seed/1", "actor.enrolled", pFP(t, worker), enrollJSON(t, worker, "agent", "worker"))
	add(root, "2026-09-01T01:00:01Z", "seed/1", "actor.granted", pFP(t, worker), `{"capability": "claim"}`)
	add(root, "2026-09-01T01:00:02Z", "seed/1", "intent.filed", "c-T", `{"intent": "time", "tier": "trivial", "budget": "small", "routing": "core"}`)
	add(root, "2026-09-01T01:00:02.25Z", "seed/1", "contract.specified", "c-T", `{"acceptance": {"ref": "spec.md @ 0123456789abcdef", "executable": false}}`)
	add(worker, "2026-09-01T01:00:03Z", "seed/1", "claim.taken", "c-T", `{}`)
	add(worker, "not-an-instant", "seed/1", "claim.parked", "c-T", `{"fence": "6", "packet": {"acceptance": ["resume"], "decisions": [], "base": "0123456789abcdef..0123456789abcdef", "refs": [], "findings": []}}`)

	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	db, _ := openCacheRO(t, out)
	defer db.Close()

	// Every per-event row carries the verbatim string and the parsed
	// instant; the unparseable one carries NULL.
	if got := one[string](t, db, `SELECT ts FROM contracts WHERE subject = 'c-T' AND position = 4`); got != "2026-09-01T01:00:02Z" {
		t.Fatalf("ts verbatim: %q", got)
	}
	want := time.Date(2026, 9, 1, 1, 0, 2, 250_000_000, time.UTC).UnixNano()
	if got := one[int64](t, db, `SELECT ts_unix FROM contracts WHERE subject = 'c-T' AND position = 5`); got != want {
		t.Fatalf("ts_unix parses the fractional instant: %d, want %d", got, want)
	}
	if n := one[int](t, db, `SELECT COUNT(*) FROM contracts WHERE subject = 'c-T' AND position = 7 AND ts_unix IS NULL AND ts = 'not-an-instant'`); n != 1 {
		t.Fatalf("an unparseable ts folds NULL beside the verbatim string, got %d rows", n)
	}
	if got := one[string](t, db, `SELECT value FROM report WHERE key = 'ts_unparsed'`); got != "1" {
		t.Fatalf("the report table counts the unparseable instant once, got %q", got)
	}
	// A range on the integer orders the fractional instant between its
	// neighbors: [01:00:02, 01:00:03) holds positions 4 and 5 and not 6.
	lo := time.Date(2026, 9, 1, 1, 0, 2, 0, time.UTC).UnixNano()
	hi := time.Date(2026, 9, 1, 1, 0, 3, 0, time.UTC).UnixNano()
	if n := one[int](t, db, `SELECT COUNT(*) FROM contracts WHERE subject = 'c-T' AND ts_unix >= ? AND ts_unix < ?`, lo, hi); n != 2 {
		t.Fatalf("the range holds the two instants inside it, got %d", n)
	}
	if got := one[int](t, db, `SELECT position FROM contracts WHERE subject = 'c-T' AND ts_unix >= ? AND ts_unix < ? ORDER BY ts_unix DESC LIMIT 1`, lo, hi); got != 5 {
		t.Fatalf("the fractional instant is the later of the two, got position %d", got)
	}
	// Contract, actor, time and outcome together, in one query: the
	// worker's claim on c-T inside the second that holds it.
	lo3 := time.Date(2026, 9, 1, 1, 0, 3, 0, time.UTC).UnixNano()
	if n := one[int](t, db, `SELECT COUNT(*) FROM contracts JOIN contract_state USING (subject) WHERE subject = 'c-T' AND actor = ? AND ts_unix >= ? AND ts_unix < ? AND verb = 'claim.taken' AND contract_state.state = 'blocked'`, pFP(t, worker), lo3, lo3+int64(time.Second)); n != 1 {
		t.Fatalf("contract, actor, time and outcome answer in one query, got %d rows", n)
	}
	// The other per-event tables carry the columns too.
	for _, table := range []string{"actor_history", "actor_signed"} {
		if n := one[int](t, db, `SELECT COUNT(*) FROM `+table+` WHERE ts_unix IS NOT NULL`); n == 0 {
			t.Fatalf("%s carries ts_unix", table)
		}
	}
	// The stamp names the generation that added the columns.
	if v := one[string](t, db, `SELECT version FROM stamp`); v != "15" {
		t.Fatalf("the cache generation is 14, got %s", v)
	}
}
