// Package packet is the four-part handoff packet (plans/os-b07b0f59.md;
// SEED-NEXT.md Part II §6 "Handoff packets"): the ONLY interface
// between executors, written on every deliberate exit and every reap.
// Four bounded, mechanical parts: acceptance criteria; settled
// decisions each marked verified or asserted (an unmarked assertion is
// a shape violation, not a shield); commit-anchored references with the
// mandatory diff-vs-merge-base range (bare paths assume a shared
// filesystem disposable executors don't have); and investigation
// findings, the negative knowledge a successor must not rediscover.
// Packets are bounded findings, never transcripts.
package packet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/classify"
)

// MaxCanonicalBytes bounds the packet's RFC 8785 canonical form. The
// arithmetic (spec/packets.md): the landed whole-payload cap is
// 4096 bytes, and the packet must FIT it with room for the wrapper key
// and the verb's sibling fields (fence, branch, evidence refs) — an
// 8 KiB packet could never admit.
const MaxCanonicalBytes = 3072

// Key is the payload key carrying the inline v0 packet. RefKey is the
// reserved artifact-store reference form: it lands with the artifact
// store (Phase 6), and carrying it today refuses with that reason so
// the migration adds a branch, not a reshape.
const (
	Key    = "packet"
	RefKey = "packet_ref"
)

// ExitVerbs are the four deliberate exits, every one of which carries
// a packet — submission included: a submission that fails verification
// returns the contract to the pool, and the packet is what the next
// executor resumes from. Pinned against the transition table by test.
var ExitVerbs = []string{"claim.parked", "claim.released", "claim.reaped", "submission.made"}

// EscalationVerbs also carry a packet without being exits. The
// charter's §II.7 says an escalation carries "the packet, the
// question, and the minimal decision", and packets.md anticipated
// exactly this reuse. They are a SEPARATE list because ExitVerbs is
// pinned against the transition table's in_progress outgoing set:
// folding a non-exit into it would break the invariant that pinning
// exists to hold (plans/os-f781f0da.md).
//
// A raise from ready or review has no work to hand off, which the
// schema already spells: base is the zero-length range, refs and
// findings may be empty, and acceptance is the contract's own anchor.
var EscalationVerbs = []string{"escalation.raised"}

// Required reports whether the verb's payload must carry a packet.
func Required(verb string) bool {
	for _, v := range ExitVerbs {
		if v == verb {
			return true
		}
	}
	for _, v := range EscalationVerbs {
		if v == verb {
			return true
		}
	}
	return false
}

// Decision is one settled decision, structurally marked.
type Decision struct {
	Decision string `json:"decision"`
	Basis    string `json:"basis"`
}

// Finding is one recorded dead end: what was tried, why it failed, and
// an optional pointer into a durable artifact.
type Finding struct {
	Tried   string `json:"tried"`
	Outcome string `json:"outcome"`
	Pointer string `json:"pointer,omitempty"`
}

// Packet is the schema (v1): exactly the four parts, all keys present.
type Packet struct {
	Acceptance []string   `json:"acceptance"`
	Decisions  []Decision `json:"decisions"`
	Base       string     `json:"base"`
	Refs       []string   `json:"refs"`
	Findings   []Finding  `json:"findings"`
}

// Error names the offending part and why (plans/os-b07b0f59.md). It
// is a shape refusal: the envelope layer keeps the established
// invalid-payload mapping, no new exit codes.
type Error struct {
	Subject string
	Part    string
	Reason  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("handoff packet on %s: part %s: %s (spec/packets.md)", e.Subject, e.Part, e.Reason)
}

// FromPayload extracts and validates the packet a deliberate exit's
// payload must carry.
func FromPayload(subject string, payload []byte) (*Packet, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, &Error{Subject: subject, Part: "packet", Reason: "payload is not an object"}
	}
	if _, ok := m[RefKey]; ok {
		return nil, &Error{Subject: subject, Part: RefKey, Reason: "the artifact-store reference form lands with the artifact store (Phase 6); v0 packets are inline under \"packet\""}
	}
	raw, ok := m[Key]
	if !ok {
		return nil, &Error{Subject: subject, Part: Key, Reason: "every deliberate exit carries a packet — silent abandonment is impossible by construction"}
	}
	return Parse(subject, raw)
}

// Parse validates the packet's strict shape, part by part.
func Parse(subject string, raw []byte) (*Packet, error) {
	var p Packet
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, &Error{Subject: subject, Part: "packet", Reason: fmt.Sprintf("strict shape: %v", err)}
	}
	// json.Decode with DisallowUnknownFields still tolerates missing
	// keys; presence is checked against the raw object so an absent
	// part and an empty one are distinguishable where it matters.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, &Error{Subject: subject, Part: "packet", Reason: "packet is not an object"}
	}
	for _, k := range []string{"acceptance", "decisions", "base", "refs", "findings"} {
		if _, ok := keys[k]; !ok {
			return nil, &Error{Subject: subject, Part: k, Reason: "the part is absent — all four parts are present in every packet, empty where honesty allows"}
		}
	}
	// Presence is not shape: a null decodes into the same nil slice an
	// absent key does, so each array part must literally be an array,
	// explicitly empty ("[]") where honesty allows, never null.
	for _, k := range []string{"acceptance", "decisions", "refs", "findings"} {
		v := bytes.TrimLeft(keys[k], " \t\r\n")
		if len(v) == 0 || v[0] != '[' {
			return nil, &Error{Subject: subject, Part: k, Reason: "the part is not an array — an empty part is an explicit [], never null"}
		}
	}
	if len(p.Acceptance) == 0 {
		return nil, &Error{Subject: subject, Part: "acceptance", Reason: "a packet a successor cannot be judged against resumes nothing"}
	}
	for i, a := range p.Acceptance {
		if strings.TrimSpace(a) == "" {
			return nil, &Error{Subject: subject, Part: "acceptance", Reason: fmt.Sprintf("entry %d is empty", i)}
		}
	}
	for i, d := range p.Decisions {
		if strings.TrimSpace(d.Decision) == "" {
			return nil, &Error{Subject: subject, Part: "decisions", Reason: fmt.Sprintf("entry %d has no decision text", i)}
		}
		if d.Basis != "verified" && d.Basis != "asserted" {
			return nil, &Error{Subject: subject, Part: "decisions", Reason: fmt.Sprintf("entry %d basis %q: every decision is marked verified or asserted — an unmarked assertion shields upstream errors", i, d.Basis)}
		}
	}
	if !classify.IsRange(p.Base) {
		return nil, &Error{Subject: subject, Part: "base", Reason: fmt.Sprintf("%q is not a commit range — the resume coordinate is \"<merge-base>..<head>\", zero-length (\"<mb>..<mb>\") when no work was pushed", p.Base)}
	}
	for i, r := range p.Refs {
		if !classify.IsAnchoredRef(r) {
			return nil, &Error{Subject: subject, Part: "refs", Reason: fmt.Sprintf("entry %d %q is not commit-anchored — bare paths assume a shared filesystem disposable executors don't have (\"path @ commit\" or \"ref @ range\")", i, r)}
		}
	}
	for i, f := range p.Findings {
		if strings.TrimSpace(f.Tried) == "" || strings.TrimSpace(f.Outcome) == "" {
			return nil, &Error{Subject: subject, Part: "findings", Reason: fmt.Sprintf("entry %d needs both tried and outcome — findings are the negative knowledge a successor must not rediscover", i)}
		}
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return nil, &Error{Subject: subject, Part: "packet", Reason: fmt.Sprintf("not canonicalizable: %v", err)}
	}
	if len(canonical) > MaxCanonicalBytes {
		return nil, &Error{Subject: subject, Part: "packet", Reason: fmt.Sprintf("canonical form is %d bytes, bound %d — packets are bounded findings, never transcripts", len(canonical), MaxCanonicalBytes)}
	}
	return &p, nil
}

// ZeroRange is the resume coordinate for a packet written when NO
// pushed work is known: the zero-length range at git's null commit.
// A forced reap composes its packet "from what is known"
// (spec/executors.md), and what is known about a killed worker's
// branch is nothing — so the honest coordinate says nothing rather
// than guessing at a head.
const ZeroRange = "0000000000000000000000000000000000000000..0000000000000000000000000000000000000000"

// Render marshals a packet and re-parses it, so a caller cannot emit a
// packet the boundary would refuse. The round trip is the point: it is
// the same Parse admission runs, so "it built" and "it would be
// admitted" cannot come apart.
func Render(p Packet) ([]byte, error) {
	if p.Acceptance == nil {
		p.Acceptance = []string{}
	}
	if p.Decisions == nil {
		p.Decisions = []Decision{}
	}
	if p.Refs == nil {
		p.Refs = []string{}
	}
	if p.Findings == nil {
		p.Findings = []Finding{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if _, err := Parse("", b); err != nil {
		return nil, err
	}
	return b, nil
}
