// Chain verification: full replay from the empty hash (charter Part II
// section 1; conformance III.A). Every failure names its position and a
// distinct reason, and a HEAD that is merely behind the stream (the healed
// crash window) is reported apart from a HEAD that contradicts it.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// Reason codes for verification failures.
const (
	ReasonBadParse           = "bad_parse"
	ReasonBadPayload         = "bad_payload"
	ReasonBadSignature       = "bad_signature"
	ReasonBadPrev            = "bad_prev"
	ReasonUnknownActor       = "unknown_actor"
	ReasonHeadBehind         = "head_behind"
	ReasonHeadWrong          = "head_wrong"
	ReasonVersionMismatch    = "version_mismatch"
	ReasonVersionUnsupported = "version_unsupported"
	ReasonActorEvent         = "bad_actor_event"
)

// UpgradeVerb switches the active protocol version: the upgrade event is
// the last event of the old version and names the new one in its payload
// (spec/protocol.md, "Protocol version").
const UpgradeVerb = "system.protocol.upgraded"

// Failure is one verification finding.
type Failure struct {
	Position int
	Reason   string
	Detail   string
}

func (f *Failure) Error() string {
	return fmt.Sprintf("position %d: %s: %s", f.Position, f.Reason, f.Detail)
}

// Report is a successful verification's summary. ActiveVersion is the
// protocol version active after the last record: what the next appended
// event must carry (spec/protocol.md, "Protocol version"). It can
// name a version outside the verifier's supported set when the chain ends
// with an upgrade event; history verifies, and appending is then the new
// version's business.
type Report struct {
	Count         int
	Tip           string
	ActiveVersion string
	// Trusted is how many leading records were replayed WITHOUT
	// signature verification under WithTrustedPrefix: parsed, linked,
	// hashed and folded, but taken on the checkpoint signer's word.
	// Zero for a full replay.
	Trusted int
}

// VerifyOption configures a verification replay.
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	supported map[string]bool
	observe   func(pos int, rec *event.Record)
	trustPos  int
	trustTip  string
}

// ReasonTrustMismatch marks a trusted prefix whose chain hash at the
// trusted position is not the tip the checkpoint attested: the
// checkpoint and the chain disagree, and the reader replays instead.
const ReasonTrustMismatch = "trusted_prefix_mismatch"

// WithTrustedPrefix makes the replay trust the first position records
// on a checkpoint signer's word (spec/checkpoints.md, the
// `signers` declaration): they are still parsed, linked, hashed, held
// to the version discipline and folded into the keyring — everything
// but their signatures is verified — and at the trusted position the
// chain hash must equal tip, or the replay fails with
// ReasonTrustMismatch. Records after the prefix are verified in full.
// What this skips is exactly the prefix's signature checks, which is
// what "explicit trust in the signer set" means and all it means.
func WithTrustedPrefix(position int, tip string) VerifyOption {
	return func(c *verifyConfig) {
		c.trustPos = position
		c.trustTip = tip
	}
}

// WithSupportedVersions declares the protocol versions this verification
// accepts as active anywhere in the chain (spec/protocol.md,
// "Verification across history"). The default is the implementation's own
// protocol version.
func WithSupportedVersions(versions ...string) VerifyOption {
	return func(c *verifyConfig) {
		c.supported = map[string]bool{}
		for _, v := range versions {
			c.supported[v] = true
		}
	}
}

// WithObserver registers a callback invoked with each record after it
// fully verifies: in order, exactly once each, and never past a failure.
// Admission context construction (internal/admit) uses it to project
// chain state (halt) in the same replay that proves the chain, instead
// of re-scanning the store.
func WithObserver(fn func(pos int, rec *event.Record)) VerifyOption {
	return func(c *verifyConfig) { c.observe = fn }
}

// ValidateUpgradeShape checks the system.protocol.upgraded schema: the
// verb must ride subject system and its payload must name a non-empty
// new version in 'to'. The verifier and the admission rule set
// (internal/admit) share this one definition: a signed but schema-broken
// upgrade admitted to the chain would wedge every later verification at
// bad_payload, so admission refuses it up front. Non-upgrade verbs pass
// through untouched. The verifier applies it only to events it treats as
// upgrades (subject system): an off-system upgrade-verb event in
// admitted history is an ordinary event and stays verifiable, while
// admission refuses new ones at the boundary.
func ValidateUpgradeShape(e *event.Event) error {
	if e.Verb != UpgradeVerb {
		return nil
	}
	if e.Subject != "system" {
		return fmt.Errorf("%s subject %q is not system", UpgradeVerb, e.Subject)
	}
	var up struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(e.Payload, &up); err != nil || up.To == "" {
		return errors.New("protocol.upgraded payload must name the new version in 'to'")
	}
	return nil
}

// VerifyFromGenesis replays the whole stream: parse every record, enforce
// the version discipline (every event carries the version active at its
// position; UpgradeVerb switches it; every active version must be
// supported), recompute canonical bytes, check prev linkage from the empty
// hash, verify every signature against the resolver, then compare HEAD.
// The first failure returns as a *Failure. A HEAD that is merely behind
// the stream must still be *consistent*: its tip must equal the chain
// hash at its claimed position, or it is wrong, not recoverable; and a
// missing HEAD over a non-empty stream is itself a behind state, never a
// pass.
func (s *Store) VerifyFromGenesis(resolve Resolver, opts ...VerifyOption) (*Report, error) {
	cfg := verifyConfig{supported: map[string]bool{}}
	for _, v := range version.Supported() {
		cfg.supported[v] = true
	}
	for _, o := range opts {
		o(&cfg)
	}
	head, headExists, headErr := s.ReadHead()

	tip := event.EmptyHash
	count := 0
	claimedTip := event.EmptyHash
	active := ""
	ring := keyring.New()
	err := s.scan(func(pos int, segment string, line []byte) error {
		rec, err := event.ParseRecord(line)
		if err != nil {
			return &Failure{Position: pos, Reason: ReasonBadParse, Detail: err.Error()}
		}
		if active == "" {
			// The initial active version is the version genesis NAMES, not
			// the version the genesis event carries: seeding from the
			// event's own v would make the equality check below
			// tautological (#83 review finding). Chains that do not begin
			// with a genesis fall back to the first event's v (the store
			// stays generic; the CLI refuses genesis-less chains anyway).
			active = rec.Event.V
			if pos == 0 && rec.Event.Verb == "system.genesis" && rec.Event.Subject == "system" {
				var g struct {
					Protocol string `json:"protocol"`
				}
				if err := json.Unmarshal(rec.Event.Payload, &g); err == nil && g.Protocol != "" {
					active = g.Protocol
				}
				// The genesis governance root also seeds the keyring
				// projection, so standing-aware resolution has its trust
				// anchor once the chain upgrades to seed/1.
				ring.SeedGenesis(rec)
			}
		}
		if !cfg.supported[active] {
			return &Failure{Position: pos, Reason: ReasonVersionUnsupported,
				Detail: fmt.Sprintf("active version %q is not in this implementation's supported set", active)}
		}
		if rec.Event.V != active {
			return &Failure{Position: pos, Reason: ReasonVersionMismatch,
				Detail: fmt.Sprintf("event carries %q, the version active at this position is %q", rec.Event.V, active)}
		}
		if rec.Event.Prev != tip {
			return &Failure{Position: pos, Reason: ReasonBadPrev,
				Detail: fmt.Sprintf("prev %.12s does not cite tip %.12s", rec.Event.Prev, tip)}
		}
		// From seed/1 the seeded keyring is the authoritative resolver:
		// standing decides who signs at this position (enrolled keys
		// resolve, suspended and revoked ones do not), while earlier
		// positions keep the caller's root-seam resolver
		// (spec/actors.md; the seed/0 grandfathering).
		pub, ok := resolve(rec.Event.Actor)
		if keyring.Applies(active) && ring.Seeded() {
			pub, ok = ring.Resolve(rec.Event.Actor)
		}
		if !ok {
			detail := rec.Event.Actor
			if e, known := ring.Get(rec.Event.Actor); known && keyring.Applies(active) {
				detail = fmt.Sprintf("%s standing is %s at this position", rec.Event.Actor, e.Standing)
			}
			return &Failure{Position: pos, Reason: ReasonUnknownActor, Detail: detail}
		}
		// A trusted record (pos < trustPos) skips exactly this check:
		// it is still parsed, linked, canonicalized for its hash below,
		// held to the version discipline and folded into the keyring.
		if pos >= cfg.trustPos {
			if err := rec.Verify(pub); err != nil {
				reason := ReasonBadSignature
				if errors.Is(err, event.ErrBadPayload) {
					reason = ReasonBadPayload
				}
				return &Failure{Position: pos, Reason: reason, Detail: err.Error()}
			}
		}
		h, err := rec.Event.Hash()
		if err != nil {
			return &Failure{Position: pos, Reason: ReasonBadPayload, Detail: err.Error()}
		}
		if keyring.Applies(active) {
			// The keyring transition function enforces actor payload
			// shapes and standing legality for every seed/1 record: a
			// signed but broken actor event must fail here, at its
			// position, not wedge later readers (the upgrade-schema
			// discipline).
			if err := ring.Advance(rec); err != nil {
				return &Failure{Position: pos, Reason: ReasonActorEvent, Detail: err.Error()}
			}
		}
		if rec.Event.Verb == UpgradeVerb && rec.Event.Subject == "system" {
			if err := ValidateUpgradeShape(&rec.Event); err != nil {
				return &Failure{Position: pos, Reason: ReasonBadPayload, Detail: err.Error()}
			}
			var up struct {
				To string `json:"to"`
			}
			_ = json.Unmarshal(rec.Event.Payload, &up)
			active = up.To
		}
		if cfg.observe != nil {
			cfg.observe(pos, rec)
		}
		tip = h
		count = pos + 1
		if cfg.trustPos > 0 && count == cfg.trustPos && h != cfg.trustTip {
			return &Failure{Position: pos, Reason: ReasonTrustMismatch,
				Detail: fmt.Sprintf("the chain hash at the trusted position %d is %.12s, the checkpoint attested %.12s: replay from genesis instead", cfg.trustPos, h, cfg.trustTip)}
		}
		if headExists && count == head.Count {
			claimedTip = h
		}
		return nil
	})
	if err != nil {
		if f, ok := err.(*Failure); ok {
			return nil, f
		}
		return nil, err
	}

	if headErr != nil {
		return nil, &Failure{Position: count, Reason: ReasonHeadWrong, Detail: headErr.Error()}
	}
	switch {
	case !headExists && count == 0:
		// clean empty ledger
	case !headExists:
		return nil, &Failure{Position: count, Reason: ReasonHeadBehind,
			Detail: fmt.Sprintf("HEAD missing over a %d-event stream (healed on next open or append)", count)}
	case head.Tip == tip && head.Count == count:
		// clean
	case head.Count < count && head.Count >= 0 && head.Tip == claimedTip:
		return nil, &Failure{Position: count, Reason: ReasonHeadBehind,
			Detail: fmt.Sprintf("HEAD at count %d behind stream count %d, consistent with its position (healed on next open or append)", head.Count, count)}
	default:
		return nil, &Failure{Position: count, Reason: ReasonHeadWrong,
			Detail: fmt.Sprintf("HEAD claims tip %.12s count %d, stream has tip %.12s count %d", head.Tip, head.Count, tip, count)}
	}
	if cfg.trustPos > 0 && count < cfg.trustPos {
		return nil, &Failure{Position: count, Reason: ReasonTrustMismatch,
			Detail: fmt.Sprintf("the chain holds %d records, fewer than the trusted position %d", count, cfg.trustPos)}
	}
	trusted := 0
	if cfg.trustPos > 0 {
		trusted = cfg.trustPos
		if trusted > count {
			trusted = count
		}
	}
	return &Report{Count: count, Tip: tip, ActiveVersion: active, Trusted: trusted}, nil
}
