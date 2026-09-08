// The SQLite cache projection (plans/os-acc1ac78.md, amended per the
// #110 review): a registered standard projection, byte-identical like
// every view, publishing one queryable database for single-machine
// read throughput with zero authority. Byte-determinism is engineered
// by closing the variance sources — one connection, one ordered
// transaction, rollback journal (never WAL, whose headers embed
// salts), fixed page_size, no auto_vacuum, no ANALYZE, no
// AUTOINCREMENT — and the registry's byte-identical drill enforces it
// against the go.sum-pinned driver.

package project

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/transition"

	_ "modernc.org/sqlite"
)

// CacheFile is the cache projection's one view file: the database is
// the API, opened read-only by any SQL client.
const CacheFile = "cache.db"

// cacheSchemaVersion is the database's own schema generation, stamped
// via PRAGMA user_version (12: ts and ts_unix on every per-event table
// and the ts_unparsed report row, plans/os-74ce2261.md); it bumps with
// the table set, not with the
// chain position. Generation 2 added contract_state and the derived
// queue rows (plans/os-d69a6c91.md); generation 3 the claim columns
// (plans/os-5dc16a7c.md); generation 4 the acceptance columns
// (plans/os-73c00a50.md); generation 5 the reconciliation-chain
// columns (plans/os-6cdc15be.md); generation 6 the sealed-commitment
// columns (plans/os-3128535a.md); generation 7 the override columns
// (plans/os-d2497eb7.md); generation 8 the offers table and the
// last_claim consumption-boundary column (plans/os-c61c3392.md);
// generation 9 the reservations table and the budget columns
// (plans/os-cecac5de.md); generation 10 the runs table
// (plans/os-1dad487d.md); generation 11 the interrupts table
// (plans/os-0f718b4e.md); generation 13 the relations,
// topology_state, topology_anomalies and goal_ancestry_warnings
// tables (plans/os-f0ae2cdf.md D8).
const cacheSchemaVersion = 13

// cacheVersion is the projection's derivation version, carried in the
// stamp table and the build id alike.
// Generation 13 split the report's lanes by kind (plans/os-0d4f2af3.md
// D6) and landed first; generation 14 adds `ts` and `ts_unix` to every
// per-event table (plans/os-74ce2261.md; charter III.G row 10), so
// evidence is queryable by time. Generation 15 mirrors the graph
// (plans/os-f0ae2cdf.md D8): the relation facts, the derived
// effective-readiness, hold, mission and rollup fields, the anomalies
// and the goal-ancestry warnings, and the queue's effective derivation.
const cacheVersion = "15"

// Cache returns the cache projection.
func Cache() Projection {
	return Projection{Name: "cache", Version: cacheVersion, Build: buildCache}
}

// cacheDDL creates the tables in spec order (spec/projections.md
// "Cache"). The stamp row is written last in the build transaction and
// carries exactly the tree stamp's fields.
var cacheDDL = []string{
	`CREATE TABLE stamp (name TEXT NOT NULL, position INTEGER NOT NULL, tip TEXT NOT NULL, version TEXT NOT NULL)`,
	`CREATE TABLE roster (fingerprint TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, standing TEXT NOT NULL, root INTEGER NOT NULL, grants TEXT NOT NULL) WITHOUT ROWID`,
	`CREATE TABLE contracts (subject TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, verb TEXT NOT NULL, actor TEXT NOT NULL, payload TEXT NOT NULL)`,
	`CREATE INDEX contracts_subject ON contracts(subject)`,
	`CREATE TABLE queue (subject TEXT NOT NULL, since_position INTEGER NOT NULL)`,
	`CREATE TABLE contract_state (subject TEXT PRIMARY KEY, state TEXT, anomalies INTEGER NOT NULL, holder TEXT, fence TEXT, acc_ref TEXT, acc_executable INTEGER, acc_gated INTEGER, verdict_position INTEGER, verdict TEXT, verdict_receipt TEXT, requested_position INTEGER, merged_position INTEGER, merged_sha TEXT, sealed_position INTEGER, sealed_commitment TEXT, override_position INTEGER, override_reason TEXT, last_claim INTEGER, budget_class TEXT, budget_capacity INTEGER, budget_remaining INTEGER) WITHOUT ROWID`,
	`CREATE TABLE offers (subject TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, signer TEXT NOT NULL, capabilities TEXT NOT NULL, tiers TEXT NOT NULL, expires TEXT NOT NULL)`,
	`CREATE INDEX offers_subject ON offers(subject)`,
	`CREATE TABLE reservations (subject TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, signer TEXT NOT NULL, amount INTEGER NOT NULL, closed_position INTEGER, closed_kind TEXT, closed_actuals INTEGER)`,
	`CREATE INDEX reservations_subject ON reservations(subject)`,
	`CREATE TABLE runs (subject TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, signer TEXT NOT NULL, fence INTEGER NOT NULL, kind TEXT NOT NULL, reservation INTEGER, units INTEGER, lines INTEGER)`,
	`CREATE TABLE interrupts (subject TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, signer TEXT NOT NULL, fence INTEGER NOT NULL)`,
	`CREATE INDEX interrupts_subject ON interrupts(subject)`,
	`CREATE INDEX runs_subject ON runs(subject)`,
	`CREATE TABLE queue_meta (schema_version TEXT NOT NULL, derivation TEXT NOT NULL)`,
	`CREATE TABLE actor_history (fingerprint TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, verb TEXT NOT NULL, acting TEXT NOT NULL)`,
	`CREATE INDEX actor_history_fp ON actor_history(fingerprint)`,
	`CREATE TABLE actor_signed (fingerprint TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, verb TEXT NOT NULL, subject TEXT NOT NULL)`,
	`CREATE INDEX actor_signed_fp ON actor_signed(fingerprint)`,
	`CREATE TABLE report (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID`,
	`CREATE TABLE relations (subject TEXT NOT NULL, position INTEGER NOT NULL, ts TEXT NOT NULL, ts_unix INTEGER, actor TEXT NOT NULL, kind TEXT NOT NULL, target TEXT NOT NULL)`,
	`CREATE INDEX relations_subject ON relations(subject)`,
	`CREATE TABLE topology_state (subject TEXT PRIMARY KEY, effective_ready INTEGER NOT NULL, unresolved TEXT NOT NULL, held_by TEXT NOT NULL, parent TEXT, mission_anchor TEXT, mission_from TEXT, initiative INTEGER NOT NULL, rollup TEXT) WITHOUT ROWID`,
	`CREATE TABLE topology_anomalies (position INTEGER NOT NULL, verb TEXT NOT NULL, subject TEXT NOT NULL, reason TEXT NOT NULL)`,
	`CREATE TABLE goal_ancestry_warnings (subject TEXT PRIMARY KEY, ancestry TEXT NOT NULL) WITHOUT ROWID`,
}

func buildCache(records []*event.Record, _ Inputs) (files map[string][]byte, err error) {
	tmpDir, err := os.MkdirTemp("", "seed-cache-build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	path := filepath.Join(tmpDir, CacheFile)

	// One connection; the pragmas close the variance sources before
	// any page is allocated.
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	closed := false
	defer func() {
		if !closed {
			_ = db.Close()
		}
	}()
	for _, pragma := range []string{
		"PRAGMA page_size = 4096",
		"PRAGMA journal_mode = OFF",
		"PRAGMA auto_vacuum = NONE",
		fmt.Sprintf("PRAGMA user_version = %d", cacheSchemaVersion),
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("cache pragma: %v", err)
		}
	}

	roster, err := rosterEntries(records)
	if err != nil {
		return nil, err
	}
	view, err := reportView(records)
	if err != nil {
		return nil, err
	}
	// The stamp row is written last and must equal the tree stamp the
	// engine writes beside this database (drilled).
	position := len(records)
	tip := ""
	if position > 0 {
		if tip, err = records[position-1].Event.Hash(); err != nil {
			return nil, err
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// One error captor for the whole ordered write sequence.
	w := &txWriter{tx: tx}
	for _, ddl := range cacheDDL {
		w.exec(ddl)
	}
	for _, r := range roster {
		root := 0
		if r.Root {
			root = 1
		}
		w.exec(`INSERT INTO roster VALUES (?, ?, ?, ?, ?, ?)`,
			r.Fingerprint, r.Kind, r.Name, r.Standing, root, w.jsonOf(r.Grants))
	}
	table, err := transition.Default()
	if err != nil {
		return nil, err
	}
	fold := table.FoldRecords(records)
	graph := deriveTopology(records, table, fold)
	// at is the event's instant for a per-event row (plans/os-74ce2261.md
	// D1; charter III.G row 10): the envelope's ts verbatim, and the
	// instant it names as nanoseconds since the epoch for range queries
	// — RFC 3339 permits mixed fractional precision, so the text mis-orders
	// as text and the integer is what a range compares. NULL when the
	// string does not parse; such rows stay queryable by their NULL, and
	// the report table carries their count under ts_unparsed (the fold's
	// anomaly count is the lifecycle's, not this).
	tsUnparsed := map[int]bool{}
	at := func(pos int) (string, any) {
		if pos < 0 || pos >= len(records) {
			return "", nil
		}
		ts := records[pos].Event.TS
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			return ts, t.UnixNano()
		}
		tsUnparsed[pos] = true
		return ts, nil
	}
	for _, c := range contractEntries(records) {
		for _, e := range c.Events {
			ts, tsUnix := at(e.Position)
			w.exec(`INSERT INTO contracts VALUES (?, ?, ?, ?, ?, ?, ?)`,
				c.Subject, e.Position, ts, tsUnix, e.Verb, e.Actor, string(e.Payload))
		}
		var state, holder, fence, accRef, accExec, accGated any
		var verdictPos, verdictVal, verdictReceipt, requestedPos, mergedPos, mergedSHA, sealedPos, sealedCommitment, overridePos, overrideReason, lastClaim any
		var budgetClass, budgetCapacity, budgetRemaining any
		anomalies := 0
		if s, ok := fold.State(c.Subject); ok {
			anomalies = s.Anomalies
			if s.State != "" {
				state = s.State
			}
			if s.Claim != nil {
				holder, fence = s.Claim.Holder, fmt.Sprintf("%d", s.Claim.Fence)
			}
			if s.Acceptance != nil {
				accRef = s.Acceptance.Ref
				accExec, accGated = boolInt(s.Acceptance.Executable), boolInt(s.Acceptance.Gated)
			}
			if s.Verdict != nil {
				verdictPos, verdictVal, verdictReceipt = s.Verdict.Pos, s.Verdict.Verdict, s.Verdict.Receipt
			}
			if s.Requested != nil {
				requestedPos = s.Requested.Pos
			}
			if s.Merged != nil {
				mergedPos, mergedSHA = s.Merged.Pos, s.Merged.SHA
			}
			if s.Sealed != nil {
				sealedPos, sealedCommitment = s.Sealed.Pos, s.Sealed.Commitment
			}
			if s.Override != nil {
				overridePos, overrideReason = s.Override.Pos, s.Override.Reason
			}
			if len(s.PriorClaimants) > 0 {
				lastClaim = s.LastClaim
			}
			for _, o := range s.Offers {
				ts, tsUnix := at(o.Pos)
				w.exec(`INSERT INTO offers VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					c.Subject, o.Pos, ts, tsUnix, o.Signer, w.jsonOf(o.Capabilities), w.jsonOf(o.Tiers), o.Expires)
			}
			if s.Budget != "" {
				budgetClass = s.Budget
			}
			for _, st := range s.RunStarts {
				ts, tsUnix := at(st.Pos)
				w.exec(`INSERT INTO runs VALUES (?, ?, ?, ?, ?, ?, 'started', ?, NULL, NULL)`,
					c.Subject, st.Pos, ts, tsUnix, st.Signer, st.Fence, st.Reservation)
			}
			for _, r := range s.Runs {
				ts, tsUnix := at(r.Pos)
				w.exec(`INSERT INTO runs VALUES (?, ?, ?, ?, ?, ?, 'settled', NULL, ?, ?)`,
					c.Subject, r.Pos, ts, tsUnix, r.Signer, r.Fence, r.Units, r.Lines)
			}
			for _, it := range s.Interrupts {
				ts, tsUnix := at(it.Pos)
				w.exec(`INSERT INTO interrupts VALUES (?, ?, ?, ?, ?, ?)`,
					c.Subject, it.Pos, ts, tsUnix, it.Signer, it.Fence)
			}
			if len(s.Reservations) > 0 || len(s.BudgetCloses) > 0 {
				// The view posture (plans/os-cecac5de.md D6): the
				// derived numbers appear only beside budget facts; an
				// unknown class stays NULL, stated never fudged.
				view := admit.BudgetViewAt(records, table, c.Subject, s)
				if view.Known {
					budgetCapacity, budgetRemaining = view.Capacity, view.Remaining
				}
				for _, r := range s.Reservations {
					var cp, ck, ca any
					if cl, ok := view.ClosedBy[r.Pos]; ok {
						cp, ck, ca = cl.Pos, cl.Kind, cl.Actuals
					}
					ts, tsUnix := at(r.Pos)
					w.exec(`INSERT INTO reservations VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
						c.Subject, r.Pos, ts, tsUnix, r.Signer, r.Amount, cp, ck, ca)
				}
			}
		}
		w.exec(`INSERT INTO contract_state VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.Subject, state, anomalies, holder, fence, accRef, accExec, accGated, verdictPos, verdictVal, verdictReceipt, requestedPos, mergedPos, mergedSHA, sealedPos, sealedCommitment, overridePos, overrideReason, lastClaim, budgetClass, budgetCapacity, budgetRemaining)
		// The graph, mirrored from the same derivation the contracts
		// view renders (plans/os-f0ae2cdf.md D8): rows only when the
		// prefix carries a trusted relation, so relation-free chains
		// keep their bodies apart from the version.
		if tv := contractTopology(graph, c.Subject); tv != nil {
			for _, r := range tv.Requires {
				pos, _ := strconv.Atoi(r.Position)
				ts, tsUnix := at(pos)
				w.exec(`INSERT INTO relations VALUES (?, ?, ?, ?, ?, 'requires', ?)`, c.Subject, pos, ts, tsUnix, r.Actor, r.Target)
			}
			var parent, missionAnchor, missionFrom, rollup any
			if tv.Parent != nil {
				pos, _ := strconv.Atoi(tv.Parent.Position)
				ts, tsUnix := at(pos)
				w.exec(`INSERT INTO relations VALUES (?, ?, ?, ?, ?, 'parent', ?)`, c.Subject, pos, ts, tsUnix, tv.Parent.Actor, tv.Parent.Target)
				parent = tv.Parent.Target
			}
			if tv.Mission != nil {
				pos, _ := strconv.Atoi(tv.Mission.Position)
				ts, tsUnix := at(pos)
				w.exec(`INSERT INTO relations VALUES (?, ?, ?, ?, ?, 'mission', ?)`, c.Subject, pos, ts, tsUnix, tv.Mission.Actor, tv.Mission.Target)
			}
			if tv.MissionAnchor != "" {
				missionAnchor, missionFrom = tv.MissionAnchor, tv.MissionFrom
			}
			if tv.Rollup != nil {
				rollup = w.jsonOf(tv.Rollup)
			}
			w.exec(`INSERT INTO topology_state VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.Subject, boolInt(tv.EffectiveReady), w.jsonOf(tv.Unresolved), w.jsonOf(tv.HeldBy), parent, missionAnchor, missionFrom, boolInt(tv.Rollup != nil), rollup)
			if tv.Warning != nil {
				w.exec(`INSERT INTO goal_ancestry_warnings VALUES (?, ?)`, c.Subject, w.jsonOf(tv.Warning.Ancestry))
			}
		}
	}
	for _, a := range graph.Graph.Anomalies {
		w.exec(`INSERT INTO topology_anomalies VALUES (?, ?, ?, ?)`, a.Pos, a.Verb, a.Subject, a.Reason)
	}
	// The queue mirrors the JSON view's derivation exactly: the
	// transition table's ready set, oldest first.
	ready, err := readyEntries(records)
	if err != nil {
		return nil, err
	}
	w.exec(`INSERT INTO queue_meta VALUES (?, ?)`, QueueSchemaVersion, QueueDerivationEffective)
	for _, q := range ready {
		w.exec(`INSERT INTO queue VALUES (?, ?)`, q.Subject, q.SincePosition)
	}
	for _, fp := range candidateFingerprints(records) {
		for pos, rec := range records {
			ev := &rec.Event
			ts, tsUnix := at(pos)
			if isActorVerbOn(ev, fp) {
				w.exec(`INSERT INTO actor_history VALUES (?, ?, ?, ?, ?, ?)`, fp, pos, ts, tsUnix, ev.Verb, ev.Actor)
			}
			if ev.Actor == fp {
				w.exec(`INSERT INTO actor_signed VALUES (?, ?, ?, ?, ?, ?)`, fp, pos, ts, tsUnix, ev.Verb, ev.Subject)
			}
		}
	}
	w.exec(`INSERT INTO report VALUES ('ts_unparsed', ?)`, fmt.Sprintf("%d", len(tsUnparsed)))
	w.exec(`INSERT INTO report VALUES ('chain', ?)`, w.jsonOf(view.Chain))
	w.exec(`INSERT INTO report VALUES ('actors', ?)`, w.jsonOf(view.Actors))
	w.exec(`INSERT INTO report VALUES ('halt', ?)`, w.jsonOf(view.Halt))
	w.exec(`INSERT INTO report VALUES ('checkpoints', ?)`, w.jsonOf(view.Checkpoints))
	w.exec(`INSERT INTO report VALUES ('contracts', ?)`, w.jsonOf(view.Contracts))
	if view.Reconciliation != nil {
		w.exec(`INSERT INTO report VALUES ('reconciliation', ?)`, w.jsonOf(view.Reconciliation))
	}
	if view.Topology != nil {
		w.exec(`INSERT INTO report VALUES ('topology', ?)`, w.jsonOf(view.Topology))
	}
	w.exec(`INSERT INTO stamp VALUES (?, ?, ?, ?)`, "cache", position, tip, cacheVersion)
	if err = w.err; err != nil {
		return nil, fmt.Errorf("cache write: %v", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	closed = true
	if err = db.Close(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{CacheFile: b}, nil
}

// txWriter runs an ordered write sequence capturing the first error,
// so the deterministic insert order reads as data, not error plumbing.
type txWriter struct {
	tx  *sql.Tx
	err error
}

func (w *txWriter) exec(q string, args ...any) {
	if w.err != nil {
		return
	}
	_, w.err = w.tx.Exec(q, args...)
}

// jsonOf marshals for a column value, capturing the first error.
func (w *txWriter) jsonOf(v any) string {
	if w.err != nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		w.err = err
		return ""
	}
	return string(b)
}

// boolInt renders a flag as a SQLite integer.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isActorVerbOn reports an actor.* event applied to the fingerprint,
// mirroring the actor view's standing-history derivation.
func isActorVerbOn(ev *event.Event, fp string) bool {
	return ev.Subject == fp && len(ev.Verb) > 6 && ev.Verb[:6] == "actor."
}
