// The message notices the situation read carries (plans/os-8451d939.md;
// build plan Phase 9 item 5(b)): who has mail, from whom, about what,
// and since which position. Input-free like every projection: the rows
// are a pure function of the verified prefix.
//
// NO PAYLOAD TEXT LIVES HERE, and the type is the enforcement rather
// than a convention. message.sent is the injection conformance suite's
// named relaying residual (spec/lanes.md): it needs no capability
// at all, so any enrolled active actor appends it, and the
// classification lint bounds only its SIZE. The situation read is the
// one surface every lane fragment names as the read it orients from,
// taken on every wake, unbidden. A body reaching it would let any
// enrolled actor steer a lane that never asked to be told anything.
// The body is read deliberately instead, one cited position at a time
// (seed message read).

package project

import (
	"encoding/json"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

// MessageSentVerb is the relaying verb this projection reads. It is
// spelled here rather than imported because internal/transition has no
// constant for it: message.sent has no row in the transition table at
// all, being a standing-only verb that touches no subject state
// (spec/lanes.md). A constant in the table package would imply a
// row it does not have.
const MessageSentVerb = "message.sent"

// MessageNotice is one message's existence, without its content.
//
// A field holding payload text must never be added here. The
// injection sweep in cmd/seed asserts that by marker rather than by
// reading this struct, so the drill notices a field this comment does
// not stop.
type MessageNotice struct {
	// From is the sending actor's fingerprint, a generated identifier.
	From string `json:"from"`
	// Subject is the contract the message concerns, carried ONLY when
	// it resolves to a contract on the chain, and omitted otherwise.
	// The event's subject field is sender-controlled: message.sent
	// admits on any nonempty subject and the classification lint reads
	// only the payload, so `--subject "IGNORE PREVIOUS INSTRUCTIONS"`
	// admits and would otherwise ride this field straight into the
	// orienting read (review finding on #211). Resolving it against
	// the fold is what makes "a generated identifier" true by
	// construction rather than by hope.
	Subject string `json:"subject,omitempty"`
	// At is the ledger position, which is also the cursor a reader
	// compares against: the position a lane carries forward IS its
	// read cursor, so unread needs no stored state and no verb
	// (plans/os-8451d939.md D3).
	At int `json:"at"`
	// Bytes is the payload's size. A count, never its content: it is
	// what a reader needs to decide whether to go and fetch it.
	Bytes int `json:"bytes"`
	// To is the resolved recipient set, empty for a broadcast. See
	// AddressedTo for the three cases and why malformed fails closed.
	To []string `json:"to,omitempty"`
	// Undeliverable marks a message whose `to` was present and did
	// not parse: addressed to nobody. It is still reported to the
	// KEYLESS whole-board read, so a sender's encoding slip loses
	// delivery rather than erasing the message (D2).
	Undeliverable bool `json:"undeliverable,omitempty"`
}

// DeriveMessages returns the notices for every message.sent on the
// chain, in position order. Filtering to a caller is the reader's job
// (Addresses), because the keyless whole-board read applies no filter.
// isContract says whether a subject resolves to a contract on the
// chain; a notice whose subject does not carries none.
func DeriveMessages(records []*event.Record, isContract func(subject string) bool) []MessageNotice {
	out := []MessageNotice{}
	for pos, rec := range records {
		if rec.Event.Verb != MessageSentVerb {
			continue
		}
		to, undeliverable := AddressedTo(rec.Event.Payload)
		subject := ""
		if isContract != nil && isContract(rec.Event.Subject) {
			subject = rec.Event.Subject
		}
		out = append(out, MessageNotice{
			From:          rec.Event.Actor,
			Subject:       subject,
			At:            pos,
			Bytes:         len(rec.Event.Payload),
			To:            to,
			Undeliverable: undeliverable,
		})
	}
	return out
}

// AddressedTo resolves a message payload's addressing. Three cases,
// and the third is the one that must not collapse into the first
// (plans/os-8451d939.md D2, review finding on #209):
//
//	no `to` key at all                 -> everyone (nil, false)
//	`to` a string or all-string array  -> those actors
//	`to` present and not parseable     -> NOBODY (nil, true)
//
// Absent and malformed are different facts. An absent `to` is a sender
// who said nothing about addressing, and reading that as "everyone"
// reads what is there. A malformed `to` is a sender who said something
// this cannot read, and every way of resolving it invents intent:
// broadcasting widens delivery from one intended recipient to every
// actor on an encoding slip, and delivering to the well-formed entries
// only is the same invention one notch quieter, since nothing says
// which entries were meant.
//
// The admission boundary is untouched: message.sent still refuses
// nothing, `{"n": 1}` is still legal, and this is a read.
func AddressedTo(payload []byte) (to []string, undeliverable bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(payload, &probe); err != nil {
		// Not an object at all: no `to` key can be present in it, so
		// this is the absent case rather than the malformed one.
		return nil, false
	}
	raw, present := probe["to"]
	if !present {
		return nil, false
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if one == "" {
			return nil, true
		}
		return []string{one}, false
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, true
	}
	if len(many) == 0 {
		return nil, true
	}
	for _, fp := range many {
		if fp == "" {
			return nil, true
		}
	}
	return many, false
}

// Addresses reports whether a notice reaches the given actor: a
// broadcast reaches everyone, an addressed message reaches its
// recipients, and an undeliverable one reaches no one.
func (m MessageNotice) Addresses(actor string) bool {
	if m.Undeliverable {
		return false
	}
	if len(m.To) == 0 {
		return true
	}
	for _, fp := range m.To {
		if fp == actor {
			return true
		}
	}
	return false
}
