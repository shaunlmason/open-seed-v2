//go:build ignore

// Generates testdata/valid-chain: the committed fixture chain the
// corruption tests derive from (plans/os-ead12024.md step 4). Deterministic
// by construction (fixed seed key, fixed timestamps), so re-running it is
// idempotent; it contains only public material (events and signatures).
//
//	cd next && go run ./internal/ledger/gen_fixture.go
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

func main() {
	dir := filepath.Join("internal", "ledger", "testdata", "valid-chain")
	if err := os.RemoveAll(dir); err != nil {
		panic(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	fp, err := event.Fingerprint(pub)
	if err != nil {
		panic(err)
	}
	resolve := func(got string) (ed25519.PublicKey, bool) { return pub, got == fp }

	days := []string{
		"2026-09-01T09:00:00Z",
		"2026-09-01T18:30:00Z",
		"2026-09-02T08:15:00Z",
		"2026-09-03T11:45:00Z",
	}
	var now time.Time
	store, err := ledger.Open(dir, ledger.WithClock(func() time.Time { return now }))
	if err != nil {
		panic(err)
	}
	prev := event.EmptyHash
	for i, ts := range days {
		now, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			panic(err)
		}
		rec, err := event.Sign(event.Event{
			V:       "seed/0",
			TS:      ts,
			Actor:   fp,
			Verb:    "message.sent",
			Subject: "c-0001",
			Payload: json.RawMessage(fmt.Sprintf(`{"n": %d}`, i)),
			Prev:    prev,
		}, priv)
		if err != nil {
			panic(err)
		}
		if _, err := store.Append(rec, resolve); err != nil {
			panic(err)
		}
		if prev, err = rec.Event.Hash(); err != nil {
			panic(err)
		}
	}
	rep, err := store.VerifyFromGenesis(resolve)
	if err != nil {
		panic(err)
	}
	fmt.Printf("valid-chain: %d events, tip %.12s\n", rep.Count, rep.Tip)
}
