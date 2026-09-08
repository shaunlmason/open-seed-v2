package project_test

// The offer projection drills (plans/os-c61c3392.md): offer facts and
// the last-claim consumption boundary land in the contracts "9" view
// and the cache's generation-8 offers table, both omitted on
// offer-free, never-claimed subjects (the byte-identity of the
// pre-offer golden views pins that half).

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/project"

	_ "modernc.org/sqlite"
)

func TestOfferFactsInContractsAndCache(t *testing.T) {
	root := pKey(t, 1)
	dir, resolve, add := fixtureChain(t, root)
	add(root, "seed/0", "system.protocol.upgraded", "system", `{"to": "seed/1"}`)
	add(root, "seed/1", "intent.filed", "c-1", `{"tier": "trivial"}`)
	add(root, "seed/1", "contract.specified", "c-1", `{}`)
	add(root, "seed/1", "offer.published", "c-1", `{"eligibility": {"capabilities": ["claim"]}, "expires": "2027-01-01T00:00:00Z"}`)
	add(root, "seed/1", "claim.taken", "c-1", `{}`)
	out := lockedTempOut(t, "projections")
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}

	// The contracts view: the offer fact with its scopes, and the
	// consumption boundary one position after it.
	build, err := project.Current(out, "contracts")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(build, project.ContractsFile))
	if err != nil {
		t.Fatal(err)
	}
	var entries []project.ContractEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	var c1 *project.ContractEntry
	for i := range entries {
		if entries[i].Subject == "c-1" {
			c1 = &entries[i]
		}
	}
	if c1 == nil || len(c1.Offers) != 1 || c1.LastClaim == nil {
		t.Fatalf("c-1 carries its offer and last claim: %+v", c1)
	}
	o := c1.Offers[0]
	if o.Expires != "2027-01-01T00:00:00Z" || len(o.Capabilities) != 1 || o.Capabilities[0] != "claim" || o.Signer == "" {
		t.Fatalf("the offer row carries scopes, expiry, signer: %+v", o)
	}

	// The cache: the offers table and the last_claim column agree
	// with the view.
	db, _ := openCacheRO(t, out)
	defer db.Close()
	var pos int
	var expires, caps string
	if err := db.QueryRow(`SELECT position, expires, capabilities FROM offers WHERE subject = 'c-1'`).Scan(&pos, &expires, &caps); err != nil {
		t.Fatalf("offers table: %v", err)
	}
	if expires != "2027-01-01T00:00:00Z" || caps != `["claim"]` {
		t.Fatalf("offers row: pos %d expires %s caps %s", pos, expires, caps)
	}
	lastClaim := one[int](t, db, `SELECT last_claim FROM contract_state WHERE subject = 'c-1'`)
	if lastClaim != pos+1 {
		t.Fatalf("the consumption boundary is the applied claim right after the offer: offer %d, last_claim %d", pos, lastClaim)
	}
	if got := one[string](t, db, `SELECT position FROM stamp`); got == "" {
		t.Fatal("stamp present")
	}
	var never sql.NullInt64
	add(root, "seed/1", "intent.filed", "c-2", `{"tier": "trivial"}`)
	// c-3 is ever-claimed but offer-free: its view entry must keep
	// the v8 body — no offers array and no last_claim — while the
	// cache column stays full-fidelity under its new generation.
	add(root, "seed/1", "intent.filed", "c-3", `{"tier": "trivial"}`)
	add(root, "seed/1", "contract.specified", "c-3", `{}`)
	add(root, "seed/1", "claim.taken", "c-3", `{}`)
	if _, err := project.Rebuild(dir, out, project.Default(), resolve); err != nil {
		t.Fatal(err)
	}
	build2, err := project.Current(out, "contracts")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(filepath.Join(build2, project.ContractsFile))
	if err != nil {
		t.Fatal(err)
	}
	var entries2 []project.ContractEntry
	if err := json.Unmarshal(b2, &entries2); err != nil {
		t.Fatal(err)
	}
	for i := range entries2 {
		if entries2[i].Subject == "c-3" {
			if entries2[i].LastClaim != nil || len(entries2[i].Offers) != 0 {
				t.Fatalf("an ever-claimed, offer-free subject keeps the v8 body: %+v", entries2[i])
			}
		}
	}
	db2, _ := openCacheRO(t, out)
	defer db2.Close()
	if err := db2.QueryRow(`SELECT last_claim FROM contract_state WHERE subject = 'c-2'`).Scan(&never); err != nil {
		t.Fatal(err)
	}
	if never.Valid {
		t.Fatalf("a never-claimed subject carries NULL last_claim, got %d", never.Int64)
	}
}
