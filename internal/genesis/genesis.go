// Package genesis builds and validates the system.genesis event: a ledger
// begins with a signed genesis naming the initial governance root and the
// protocol version (charter Part II section 1, "Genesis and halt";
// plans/os-d636299d.md). The genesis payload is also the trust bootstrap:
// it carries the root public keys, so a fresh reader can resolve the
// genesis signer without any prior keyring.
package genesis

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Verb is the genesis verb name from the charter's catalog.
const Verb = "system.genesis"

// RootKey is one governance-root entry: the protocol fingerprint and the
// raw 32-byte Ed25519 public key, hex-encoded per the spec's uniform rule.
type RootKey struct {
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
}

// Payload is the system.genesis payload: the protocol version and the
// governance root (at least one operator key).
type Payload struct {
	Protocol       string    `json:"protocol"`
	GovernanceRoot []RootKey `json:"governance_root"`
}

// Build signs a genesis record: the signer's key always joins the
// governance root (the operator running init is a root member), extra
// operator keys are optional, and prev is the empty hash.
func Build(signer ed25519.PrivateKey, extraOperators []ed25519.PublicKey, now time.Time) (*event.Record, error) {
	signerPub := signer.Public().(ed25519.PublicKey)
	keys := append([]ed25519.PublicKey{signerPub}, extraOperators...)
	var roots []RootKey
	seen := map[string]bool{}
	for _, pub := range keys {
		fp, err := event.Fingerprint(pub)
		if err != nil {
			return nil, err
		}
		if seen[fp] {
			continue
		}
		seen[fp] = true
		roots = append(roots, RootKey{Fingerprint: fp, PublicKey: hex.EncodeToString(pub)})
	}
	payload, err := json.Marshal(Payload{Protocol: version.Protocol, GovernanceRoot: roots})
	if err != nil {
		return nil, err
	}
	signerFP, err := event.Fingerprint(signerPub)
	if err != nil {
		return nil, err
	}
	e := event.Event{
		V:       version.Protocol,
		TS:      now.UTC().Format(time.RFC3339),
		Actor:   signerFP,
		Verb:    Verb,
		Subject: "system",
		Payload: payload,
		Prev:    event.EmptyHash,
	}
	return event.Sign(e, signer)
}

// Parse validates a record as a genesis event and returns its payload.
func Parse(rec *event.Record) (*Payload, error) {
	if rec.Event.Verb != Verb {
		return nil, fmt.Errorf("verb %q is not %s", rec.Event.Verb, Verb)
	}
	if rec.Event.Subject != "system" {
		return nil, fmt.Errorf("genesis subject %q is not system", rec.Event.Subject)
	}
	if rec.Event.Prev != event.EmptyHash {
		return nil, fmt.Errorf("genesis prev must be the empty hash")
	}
	var p Payload
	if err := json.Unmarshal(rec.Event.Payload, &p); err != nil {
		return nil, fmt.Errorf("genesis payload does not parse: %w", err)
	}
	if p.Protocol == "" || len(p.GovernanceRoot) == 0 {
		return nil, fmt.Errorf("genesis payload must name a protocol and at least one governance-root key")
	}
	return &p, nil
}

// Resolver builds a fingerprint resolver from the genesis payload's root
// keys: the trust bootstrap for a fresh chain. The genesis signer must be
// one of the named roots or the resolver refuses to build.
func (p *Payload) Resolver(genesisActor string) (ledger.Resolver, error) {
	ring := map[string]ed25519.PublicKey{}
	for _, rk := range p.GovernanceRoot {
		raw, err := hex.DecodeString(rk.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("governance-root key %s is not raw ed25519 hex", rk.Fingerprint)
		}
		pub := ed25519.PublicKey(raw)
		fp, err := event.Fingerprint(pub)
		if err != nil {
			return nil, err
		}
		if fp != rk.Fingerprint {
			return nil, fmt.Errorf("governance-root fingerprint %s does not match its key (computed %s)", rk.Fingerprint, fp)
		}
		ring[fp] = pub
	}
	if _, ok := ring[genesisActor]; !ok {
		return nil, fmt.Errorf("genesis signer %s is not in the governance root", genesisActor)
	}
	return func(fp string) (ed25519.PublicKey, bool) {
		pub, ok := ring[fp]
		return pub, ok
	}, nil
}

// Init writes a signed genesis into an empty store. A non-empty ledger
// refuses: import and re-init paths never overwrite history.
func Init(store *ledger.Store, signer ed25519.PrivateKey, extraOperators []ed25519.PublicKey, now time.Time) (*event.Record, error) {
	tip, count, err := store.Tip()
	if err != nil {
		return nil, err
	}
	if count != 0 || tip != event.EmptyHash {
		return nil, ledger.ErrNotEmpty
	}
	rec, err := Build(signer, extraOperators, now)
	if err != nil {
		return nil, err
	}
	payload, err := Parse(rec)
	if err != nil {
		return nil, err
	}
	resolve, err := payload.Resolver(rec.Event.Actor)
	if err != nil {
		return nil, err
	}
	if _, err := store.Append(rec, resolve); err != nil {
		return nil, err
	}
	return rec, nil
}

// ErrNoGenesis names the refusal for chains that do not begin with a
// system.genesis event.
var ErrNoGenesis = errors.New("chain does not start with a system.genesis event")

// Bootstrap reads the chain's first record, validates it as genesis, and
// returns the governance-root resolver plus the parsed payload: the trust
// bootstrap every ledger-aware CLI verb starts from.
func Bootstrap(store *ledger.Store) (ledger.Resolver, *Payload, error) {
	var first *event.Record
	stop := errors.New("stop")
	err := store.Records(func(pos int, rec *event.Record) error {
		first = rec
		return stop
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, nil, err
	}
	if first == nil {
		return nil, nil, ErrNoGenesis
	}
	payload, err := Parse(first)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNoGenesis, err)
	}
	resolve, err := payload.Resolver(first.Event.Actor)
	if err != nil {
		return nil, nil, err
	}
	return resolve, payload, nil
}
