// Package history generates a deterministic representative ledger
// (plans/os-7508ab9e.md D5): a seeded root key, the upgrade to seed/1,
// four lane identities enrolled and granted, then N contracts driven
// through the full loop — filed, specified, claimed, reserved, run and
// settled, submitted, passed, merge requested and observed — so the
// drills and the benchmarks measure one chain two machines agree on
// byte for byte. Keys derive from the seed; every instant is fixed.
package history

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Spec sizes a history.
type Spec struct {
	Seed      int64
	Contracts int
	Dir       string // the ledger directory to write (created if absent)
	Writers   int    // agent identities enrolled for a storm, one per writer (plans/os-a00d3f34.md D1); zero enrolls none
}

// Keys are the identities the history is signed by. Writers are the
// storm's actors, one enrolled key each with the grant intent.filed
// accepts, so a storm of N writers is N keypairs, which is what the
// charter means by N concurrent actors.
type Keys struct {
	Root, Holder, Supervisor, Verifier, Observer ed25519.PrivateKey
	Writers                                      []ed25519.PrivateKey
}

// Result reports what was generated.
type Result struct {
	Records int
	Keys    Keys
	Resolve ledger.Resolver
	Tip     string
}

// RecordsPerContract is the loop's length in records.
const RecordsPerContract = 10

// Preamble is the record count before the first contract: genesis,
// the upgrade, and four enrollments with their grants.
const Preamble = 2 + 4*2

const (
	filedBody = `{"intent": "representative work", "tier": "trivial", "budget": "small", "routing": "core"}`
	specBody  = `{"acceptance": {"ref": "specs/thing.md @ abc1234", "executable": false}}`
	minPacket = `{"acceptance": ["resume from here"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
)

var epoch = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// keyFrom derives an Ed25519 key from the seed and a label: SHA-256 of
// both, so two runs from one seed produce one key and two labels never
// collide.
func keyFrom(seed int64, label string) ed25519.PrivateKey {
	h := sha256.New()
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(seed))
	h.Write(b[:])
	h.Write([]byte(label))
	return ed25519.NewKeyFromSeed(h.Sum(nil))
}

func fpOf(priv ed25519.PrivateKey) string {
	fp, _ := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	return fp
}

// Generate writes the history into spec.Dir and returns its keys and
// resolver. The chain is admissible by construction — it is the
// lifecycle happy path the admission drills replay — and the package's
// drill holds it to the boundary at a small size.
func Generate(spec Spec) (*Result, error) {
	if spec.Contracts < 0 {
		return nil, fmt.Errorf("contracts must be zero or more, got %d", spec.Contracts)
	}
	store, err := ledger.Open(spec.Dir)
	if err != nil {
		return nil, err
	}
	if spec.Writers < 0 {
		return nil, fmt.Errorf("writers must be zero or more, got %d", spec.Writers)
	}
	keys := Keys{
		Root:       keyFrom(spec.Seed, "root"),
		Holder:     keyFrom(spec.Seed, "holder"),
		Supervisor: keyFrom(spec.Seed, "supervisor"),
		Verifier:   keyFrom(spec.Seed, "verifier"),
		Observer:   keyFrom(spec.Seed, "observer"),
	}
	for w := 0; w < spec.Writers; w++ {
		keys.Writers = append(keys.Writers, keyFrom(spec.Seed, fmt.Sprintf("writer-%04d", w)))
	}
	gen, err := genesis.Build(keys.Root, nil, epoch)
	if err != nil {
		return nil, err
	}
	payload, err := genesis.Parse(gen)
	if err != nil {
		return nil, err
	}
	rootResolve, err := payload.Resolver(gen.Event.Actor)
	if err != nil {
		return nil, err
	}
	lanes := []ed25519.PrivateKey{keys.Holder, keys.Supervisor, keys.Verifier, keys.Observer}
	byFP := map[string]ed25519.PublicKey{}
	for _, p := range append(append([]ed25519.PrivateKey{}, lanes...), keys.Writers...) {
		byFP[fpOf(p)] = p.Public().(ed25519.PublicKey)
	}
	resolve := func(fp string) (ed25519.PublicKey, bool) {
		if pub, ok := byFP[fp]; ok {
			return pub, true
		}
		return rootResolve(fp)
	}
	if _, err := store.Append(gen, resolve); err != nil {
		return nil, err
	}
	n := 1
	tip, _, err := store.Tip()
	if err != nil {
		return nil, err
	}
	sign := func(priv ed25519.PrivateKey, v, verb, subject, body string) (int, error) {
		rec, err := event.Sign(event.Event{
			V: v, TS: epoch.Add(time.Duration(n) * time.Second).UTC().Format(time.RFC3339), Actor: fpOf(priv),
			Verb: verb, Subject: subject, Payload: json.RawMessage(body), Prev: tip,
		}, priv)
		if err != nil {
			return 0, err
		}
		pos, err := store.Append(rec, resolve)
		if err != nil {
			return 0, fmt.Errorf("%s on %s at %d: %w", verb, subject, n, err)
		}
		tip, err = rec.Event.Hash()
		if err != nil {
			return 0, err
		}
		n++
		return pos, nil
	}
	if _, err := sign(keys.Root, version.Protocol, ledger.UpgradeVerb, "system", `{"to": "`+version.Seed1+`"}`); err != nil {
		return nil, err
	}
	for _, lane := range []struct {
		key  ed25519.PrivateKey
		name string
		cap  string
	}{{keys.Holder, "holder", keyring.CapClaim}, {keys.Supervisor, "supervisor", keyring.CapSupervise}, {keys.Verifier, "verifier", keyring.CapVerdict}, {keys.Observer, "observer", keyring.CapObserver}} {
		pub := lane.key.Public().(ed25519.PublicKey)
		enroll := fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), lane.name)
		if _, err := sign(keys.Root, version.Seed1, keyring.VerbEnrolled, fpOf(lane.key), enroll); err != nil {
			return nil, err
		}
		if _, err := sign(keys.Root, version.Seed1, keyring.VerbGranted, fpOf(lane.key), `{"capability": "`+lane.cap+`"}`); err != nil {
			return nil, err
		}
	}
	for w, key := range keys.Writers {
		pub := key.Public().(ed25519.PublicKey)
		enroll := fmt.Sprintf(`{"key": %q, "kind": "agent", "name": %q}`, hex.EncodeToString(pub), fmt.Sprintf("writer-%04d", w))
		if _, err := sign(keys.Root, version.Seed1, keyring.VerbEnrolled, fpOf(key), enroll); err != nil {
			return nil, err
		}
		if _, err := sign(keys.Root, version.Seed1, keyring.VerbGranted, fpOf(key), `{"capability": "`+keyring.CapDispatch+`"}`); err != nil {
			return nil, err
		}
	}
	for i := 0; i < spec.Contracts; i++ {
		subject := fmt.Sprintf("c-%04d", i+1)
		if _, err := sign(keys.Root, version.Seed1, "intent.filed", subject, filedBody); err != nil {
			return nil, err
		}
		if _, err := sign(keys.Root, version.Seed1, "contract.specified", subject, specBody); err != nil {
			return nil, err
		}
		fence, err := sign(keys.Holder, version.Seed1, "claim.taken", subject, `{}`)
		if err != nil {
			return nil, err
		}
		reservation, err := sign(keys.Holder, version.Seed1, "budget.reserve", subject, fmt.Sprintf(`{"amount": "2", "fence": "%d"}`, fence))
		if err != nil {
			return nil, err
		}
		if _, err := sign(keys.Supervisor, version.Seed1, "run.started", subject, fmt.Sprintf(`{"fence": "%d", "reservation": "%d"}`, fence, reservation)); err != nil {
			return nil, err
		}
		if _, err := sign(keys.Supervisor, version.Seed1, "run.settled", subject, fmt.Sprintf(`{"fence": "%d", "units": "1", "lines": "1"}`, fence)); err != nil {
			return nil, err
		}
		submission, err := sign(keys.Holder, version.Seed1, "submission.made", subject, fmt.Sprintf(`{"branch": "seed/%s", "fence": "%d", "packet": %s}`, subject, fence, minPacket))
		if err != nil {
			return nil, err
		}
		receipt := sha256.Sum256([]byte(subject))
		verdict, err := sign(keys.Verifier, version.Seed1, "verdict.rendered", subject, fmt.Sprintf(`{"verdict": "pass", "receipt": %q, "submission": "%d", "independence": "L1"}`, hex.EncodeToString(receipt[:]), submission))
		if err != nil {
			return nil, err
		}
		if _, err := sign(keys.Holder, version.Seed1, "merge.requested", subject, fmt.Sprintf(`{"verdict": "%d"}`, verdict)); err != nil {
			return nil, err
		}
		merged := sha256.Sum256([]byte("merged " + subject))
		if _, err := sign(keys.Observer, version.Seed1, "merge.observed", subject, fmt.Sprintf(`{"merged": %q, "pr": "%d"}`, hex.EncodeToString(merged[:])[:40], i+1)); err != nil {
			return nil, err
		}
	}
	return &Result{Records: n, Keys: keys, Resolve: resolve, Tip: tip}, nil
}
