package main

// The messages section of the situation read, and the deliberate body
// read beside it (plans/os-8451d939.md; build plan Phase 9 item 5(b)).
//
// The containment half — that no payload text reaches situation — is
// swept by marker in injection_cli_test.go rather than here, because a
// drill that read this package's structs would not notice a field
// added later. What lives here is the behavior: who sees what, when a
// message counts as unread, and what a body read gives back.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// mailLedger stands up a ledger with two enrolled workers and a
// supervisor, and returns the pieces the drills address each other by.
func mailLedger(t *testing.T) (ld string, keys, fps map[string]string, send func(payload string) int) {
	t.Helper()
	ld, _, _, specCommit, _, priv, _, keys, fps := offerLedger(t)
	offerFile(t, ld, priv, specCommit, "c-1")
	send = func(payload string) int {
		t.Helper()
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
			"--verb", "message.sent", "--subject", "c-1", "--payload", payload); code != 0 {
			t.Fatalf("message.sent %s: %d %+v", payload, code, e)
		}
		// The position the message landed at is the tip's ordinal,
		// which the envelope of the NEXT read reports; reading it back
		// here keeps the drills from counting appends by hand.
		e, code := runEnv(t, "situation", "--ledger", ld, "--key", keys["workerA"])
		if code != 0 {
			t.Fatalf("situation after send: %d %+v", code, e)
		}
		if e.Position == nil {
			t.Fatal("the read must stamp a position")
		}
		pos, err := strconv.Atoi(*e.Position)
		if err != nil {
			t.Fatalf("the stamped position must be a number: %q", *e.Position)
		}
		return pos
	}
	return ld, keys, fps, send
}

func messagesIn(t *testing.T, ld, key string, extra ...string) []map[string]any {
	t.Helper()
	args := append([]string{"situation", "--ledger", ld, "--key", key}, extra...)
	e, code := runEnv(t, args...)
	if code != 0 {
		t.Fatalf("situation: %d %+v", code, e)
	}
	raw, ok := e.Result["messages"]
	if !ok {
		t.Fatal("the messages section is present whether or not it is empty: an absent section and an " +
			"empty one are the same question a lane should not have to ask twice (D4)")
	}
	rows, _ := raw.([]any)
	out := []map[string]any{}
	for _, r := range rows {
		m, _ := r.(map[string]any)
		out = append(out, m)
	}
	return out
}

// conformance: AC1 — the read reports the messages addressed to the
// caller, and ONLY those. A broadcast reaches everyone; a message
// addressed to another actor reaches nobody else.
func TestSituationCarriesTheCallersMessages(t *testing.T) {
	ld, keys, fps, send := mailLedger(t)
	send(fmt.Sprintf(`{"to": %q, "n": 1}`, fps["workerA"]))
	send(fmt.Sprintf(`{"to": %q, "n": 2}`, fps["workerB"]))
	send(`{"n": 3}`)

	a := messagesIn(t, ld, keys["workerA"])
	if len(a) != 2 {
		t.Fatalf("workerA sees its own message and the broadcast, saw %d: %+v", len(a), a)
	}
	b := messagesIn(t, ld, keys["workerB"])
	if len(b) != 2 {
		t.Fatalf("workerB sees its own message and the broadcast, saw %d: %+v", len(b), b)
	}
	// The one that matters: neither sees the other's.
	for _, row := range a {
		if row["bytes"] == fmt.Sprintf("%d", len(fmt.Sprintf(`{"to": %q, "n": 2}`, fps["workerB"]))) &&
			row["at"] == b[0]["at"] {
			t.Errorf("workerA must not see a message addressed to workerB: %+v", row)
		}
	}
	// The sender and the contract are reported, because a notice a
	// lane cannot act on is not worth carrying.
	for _, row := range a {
		if row["from"] != fps["supervisor"] || row["subject"] != "c-1" {
			t.Errorf("a notice names who sent it and what it concerns: %+v", row)
		}
		if _, has := row["body"]; has {
			t.Errorf("NO BODY in the orienting read (D1): %+v", row)
		}
	}
}

// conformance: AC4 — addressing resolves by D2's three cases, and a
// `to` that does not parse addresses NOBODY rather than everybody.
//
// Fail-closed is the point. Broadcasting a malformed address widens
// delivery from one intended recipient to every actor on an encoding
// slip, which contradicts "and only those" above (review finding on
// #209).
func TestMalformedAddressingReachesNobody(t *testing.T) {
	ld, keys, fps, send := mailLedger(t)
	for _, tc := range []struct {
		name, payload string
		reaches       bool
	}{
		{"no to key at all", `{"n": 1}`, true},
		{"a string recipient", fmt.Sprintf(`{"to": %q}`, fps["workerA"]), true},
		{"an all-string array", fmt.Sprintf(`{"to": [%q, %q]}`, fps["workerA"], fps["workerB"]), true},
		// The review finding's own example: the intended recipient is
		// visible in the array, and delivering to it anyway would be
		// inventing which half the sender meant.
		{"an array with a number in it", fmt.Sprintf(`{"to": [%q, 7]}`, fps["workerA"]), false},
		{"a number", `{"to": 7}`, false},
		{"an object", `{"to": {"fp": "x"}}`, false},
		{"an empty array", `{"to": []}`, false},
		{"an empty string", `{"to": ""}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(messagesIn(t, ld, keys["workerA"]))
			send(tc.payload)
			after := messagesIn(t, ld, keys["workerA"])
			got := len(after) > before
			if got != tc.reaches {
				t.Fatalf("reaches workerA = %v, want %v: %+v", got, tc.reaches, after)
			}
		})
	}
	// And the undeliverable ones are not ERASED: the keyless read
	// applies no caller filter, so an operator looking at the board
	// still finds them. A typo costs delivery, not the message (D2).
	e, code := runEnv(t, "situation", "--ledger", ld)
	if code != 0 {
		t.Fatalf("keyless situation: %d %+v", code, e)
	}
	rows, _ := e.Result["messages"].([]any)
	if len(rows) != 8 {
		t.Fatalf("the keyless whole-board read filters nothing and must show all eight, showed %d", len(rows))
	}
	undeliverable := 0
	for _, r := range rows {
		if m, _ := r.(map[string]any); m["undeliverable"] == true {
			undeliverable++
		}
	}
	if undeliverable != 5 {
		t.Errorf("the five unreadable addresses are marked undeliverable, not hidden: %d", undeliverable)
	}
}

// conformance: AC3 — unread is the cursor and nothing else. The
// boundary is drilled AT the cited position, which is where an
// off-by-one lives: --since cites a tip ordinal, so the message AT
// that position was already seen.
func TestUnreadIsTheCitedCursor(t *testing.T) {
	ld, keys, _, send := mailLedger(t)
	first := send(`{"n": 1}`)
	second := send(`{"n": 2}`)

	all := messagesIn(t, ld, keys["workerA"])
	if len(all) != 2 {
		t.Fatalf("with no cursor cited, everything the caller can see is unread: %+v", all)
	}
	// Position order, oldest first: the cursor a lane carries forward
	// is a position, so a section ordered any other way would make
	// "everything after my cursor" mean reading the list backwards.
	if all[0]["at"] != strconv.Itoa(first) || all[1]["at"] != strconv.Itoa(second) {
		t.Fatalf("notices are in position order, oldest first: %+v", all)
	}
	for _, row := range messagesIn(t, ld, keys["workerA"]) {
		if row["unread"] != true {
			t.Errorf("a caller that names no cursor has said nothing about what it has seen: %+v", row)
		}
	}
	// AT the first message's position: it was seen, the second was not.
	at := messagesIn(t, ld, keys["workerA"], "--since", strconv.Itoa(first))
	if len(at) != 1 || at[0]["at"] != strconv.Itoa(second) {
		t.Fatalf("--since %d must exclude the message AT %d and keep the one after: %+v", first, first, at)
	}
	// One BEFORE it: both are new.
	before := messagesIn(t, ld, keys["workerA"], "--since", strconv.Itoa(first-1))
	if len(before) != 2 {
		t.Fatalf("--since %d must keep both: %+v", first-1, before)
	}
	// At the tip: nothing is new, and the section is still present.
	if got := messagesIn(t, ld, keys["workerA"], "--since", strconv.Itoa(second)); len(got) != 0 {
		t.Fatalf("--since at the tip leaves nothing unread: %+v", got)
	}
}

// conformance: AC5 — the section is present and empty for a caller
// with no mail, so a lane never has to tell an absent section from an
// empty one.
func TestTheMessagesSectionIsAlwaysPresent(t *testing.T) {
	ld, keys, _, _ := mailLedger(t)
	if got := messagesIn(t, ld, keys["workerA"]); len(got) != 0 {
		t.Fatalf("no mail sent, so the section is present and empty: %+v", got)
	}
}

// conformance: AC6 — the deliberate body read. A recipient gets the
// body; everyone else gets not_found, byte for byte what a position
// holding no message gives, so the refusal discloses nothing about
// what is there.
func TestMessageReadGivesTheBodyToRecipientsOnly(t *testing.T) {
	ld, keys, fps, send := mailLedger(t)
	mine := send(fmt.Sprintf(`{"to": %q, "secret": "for A"}`, fps["workerA"]))
	theirs := send(fmt.Sprintf(`{"to": %q, "secret": "for B"}`, fps["workerB"]))
	cast := send(`{"secret": "for everyone"}`)
	bad := send(fmt.Sprintf(`{"to": [%q, 7], "secret": "for nobody"}`, fps["workerA"]))

	read := func(key string, at int) (map[string]any, int, string) {
		t.Helper()
		e, code := runEnv(t, "message", "read", "--ledger", ld, "--key", key, "--at", strconv.Itoa(at))
		msg := ""
		if e.Error != nil {
			msg = e.Error.Code + ": " + e.Error.Message
		}
		return e.Result, code, msg
	}

	res, code, _ := read(keys["workerA"], mine)
	if code != 0 {
		t.Fatalf("a recipient reads its own message: %d", code)
	}
	if body, _ := res["body"].(string); !strings.Contains(body, "for A") {
		t.Errorf("the body is what the read is for: %+v", res)
	}
	if res, code, _ := read(keys["workerA"], cast); code != 0 {
		t.Errorf("a broadcast is readable by anyone: %d %+v", code, res)
	}

	// The disclosure property: four different reasons, ONE refusal.
	// If these ever diverge, the refusal starts telling a caller
	// whether something is there.
	notMine, codeA, msgA := read(keys["workerA"], theirs)
	_ = notMine
	nothingThere, codeB, msgB := read(keys["workerA"], 0)
	_ = nothingThere
	pastTheTip, codeC, msgC := read(keys["workerA"], 9999)
	_ = pastTheTip
	nobodys, codeD, msgD := read(keys["workerA"], bad)
	_ = nobodys
	for _, c := range []int{codeA, codeB, codeC, codeD} {
		if c != envelopeNotFoundExit {
			t.Fatalf("every reason a caller gets no body is the same not_found: got %d", c)
		}
	}
	// Byte for byte, except for the position each names — which the
	// caller supplied, so it discloses nothing it did not already know.
	want := []string{
		fmt.Sprintf("not_found: no message addressed to you at position %d", theirs),
		"not_found: no message addressed to you at position 0",
		"not_found: no message addressed to you at position 9999",
		fmt.Sprintf("not_found: no message addressed to you at position %d", bad),
	}
	for i, got := range []string{msgA, msgB, msgC, msgD} {
		if got != want[i] {
			t.Errorf("refusal %d differs from the one shape: %q != %q", i, got, want[i])
		}
	}
	// A key is required: a body is read as somebody.
	if _, code := runEnv(t, "message", "read", "--ledger", ld, "--at", strconv.Itoa(cast)); code != envelopeUsageExit {
		t.Errorf("a keyless body read addresses no one and must refuse as usage: %d", code)
	}
}

// envelopeNotFoundExit is spec/envelope.md row 4.
const envelopeNotFoundExit = 4

// mailPositionAfter returns the tip ordinal, the position the append
// that just ran landed at, read back rather than counted by hand.
func mailPositionAfter(t *testing.T, ld, key string) int {
	t.Helper()
	e, code := runEnv(t, "situation", "--ledger", ld, "--key", key)
	if code != 0 || e.Position == nil {
		t.Fatalf("situation after send: %d %+v", code, e)
	}
	pos, err := strconv.Atoi(*e.Position)
	if err != nil {
		t.Fatalf("the stamped position must be a number: %q", *e.Position)
	}
	return pos
}

// conformance: the loop's relaying act has a verb of its own
// (docs/promotion.md, "The cutover and the rollback": mail was
// "the one loop act without a verb of its own", to be settled by
// naming the ledger-append form or adding the verb). An addressed send
// reaches its recipient and nobody else; an unaddressed one is a
// broadcast every reader can open.
func TestMessageSendRoundTrips(t *testing.T) {
	ld, keys, fps, _ := mailLedger(t)

	if e, code := runEnv(t, "message", "send", "--ledger", ld, "--key", keys["supervisor"],
		"--subject", "c-1", "--to", fps["workerA"], "--body", "rerun the explorer"); code != 0 {
		t.Fatalf("an addressed send: %d %+v", code, e)
	}
	at := mailPositionAfter(t, ld, keys["workerA"])

	e, code := runEnv(t, "message", "read", "--ledger", ld, "--key", keys["workerA"], "--at", strconv.Itoa(at))
	if code != 0 {
		t.Fatalf("the recipient reads the body: %d %+v", code, e)
	}
	body, _ := e.Result["body"].(string)
	if !strings.Contains(body, "rerun the explorer") {
		t.Fatalf("the body carries what was sent: %q", body)
	}
	if !strings.Contains(body, fps["workerA"]) {
		t.Fatalf("the payload addresses the recipient: %q", body)
	}
	if e.Result["subject"] != "c-1" || e.Result["from"] != fps["supervisor"] {
		t.Fatalf("the read names the contract and the sender: %+v", e.Result)
	}
	// A non-recipient gets the one refusal this surface gives.
	if _, code := runEnv(t, "message", "read", "--ledger", ld, "--key", keys["workerB"], "--at", strconv.Itoa(at)); code != envelopeNotFoundExit {
		t.Fatalf("a non-recipient gets not_found, not a body: %d", code)
	}

	// No --to at all is a broadcast, which every reader can open.
	if e, code := runEnv(t, "message", "send", "--ledger", ld, "--key", keys["supervisor"],
		"--subject", "c-1", "--body", "standup in ten"); code != 0 {
		t.Fatalf("a broadcast send: %d %+v", code, e)
	}
	bat := mailPositionAfter(t, ld, keys["workerA"])
	for _, who := range []string{"workerA", "workerB"} {
		if _, code := runEnv(t, "message", "read", "--ledger", ld, "--key", keys[who], "--at", strconv.Itoa(bat)); code != 0 {
			t.Fatalf("%s opens the broadcast: %d", who, code)
		}
	}
}

// The reason the verb exists rather than a documented `ledger append`
// form: `to` has three states at the reading end and the malformed one
// delivers to nobody, so a sender must not be able to reach it by
// mistyping JSON. The verb writes `to` or omits it, never anything
// else, and refuses a recipient that could not be a fingerprint.
func TestMessageSendCannotLandUndeliverable(t *testing.T) {
	ld, keys, fps, _ := mailLedger(t)

	// A recipient that could not be a fingerprint is refused before
	// anything is appended, so the tip does not move.
	before := mailPositionAfter(t, ld, keys["workerA"])
	for _, bad := range []string{
		"shaunlmason",
		strings.ToUpper(fps["workerA"]),
		fps["workerA"] + "extra",
		fps["workerA"][:63],
		"0x" + fps["workerA"][2:],
	} {
		e, code := runEnv(t, "message", "send", "--ledger", ld, "--key", keys["supervisor"],
			"--subject", "c-1", "--to", bad, "--body", "x")
		if code != envelopeUsageExit || e.Error == nil || !strings.Contains(e.Error.Message, "fingerprint") {
			t.Fatalf("--to %q must refuse by name: %d %+v", bad, code, e)
		}
	}
	if after := mailPositionAfter(t, ld, keys["workerA"]); after != before {
		t.Fatalf("a refused send appends nothing: tip moved %d -> %d", before, after)
	}

	// Every send this verb does write carries `to` as an all-string
	// array or omits it, which are exactly the two states that resolve
	// to somebody. The malformed third state is unreachable from here.
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"addressed", []string{"--to", fps["workerA"]}},
		{"two recipients", []string{"--to", fps["workerA"], "--to", fps["workerB"]}},
		{"broadcast", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"message", "send", "--ledger", ld, "--key", keys["supervisor"],
				"--subject", "c-1", "--body", "x"}, tc.args...)
			if e, code := runEnv(t, args...); code != 0 {
				t.Fatalf("send: %d %+v", code, e)
			}
			at := mailPositionAfter(t, ld, keys["workerA"])
			e, code := runEnv(t, "message", "read", "--ledger", ld, "--key", keys["workerA"], "--at", strconv.Itoa(at))
			if code != 0 {
				t.Fatalf("workerA is addressed in every case here: %d %+v", code, e)
			}
			body, _ := e.Result["body"].(string)
			if len(tc.args) == 0 {
				if strings.Contains(body, `"to"`) {
					t.Fatalf("a broadcast omits `to` entirely rather than writing an empty one: %q", body)
				}
				return
			}
			if !strings.Contains(body, `"to":[`) {
				t.Fatalf("an addressed send writes `to` as an array: %q", body)
			}
		})
	}
}

func TestMessageSendUsage(t *testing.T) {
	ld, keys, fps, _ := mailLedger(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no ledger and no remote", []string{"message", "send", "--key", keys["supervisor"], "--subject", "c-1", "--body", "x"}},
		{"both ledger and remote", []string{"message", "send", "--ledger", ld, "--remote", ld, "--key", keys["supervisor"], "--subject", "c-1", "--body", "x"}},
		{"no key", []string{"message", "send", "--ledger", ld, "--subject", "c-1", "--body", "x"}},
		{"no subject", []string{"message", "send", "--ledger", ld, "--key", keys["supervisor"], "--body", "x"}},
		{"no body", []string{"message", "send", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-1"}},
		{"a positional argument", []string{"message", "send", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-1", "--body", "x", "stray"}},
		{"an unknown flag", []string{"message", "send", "--ledger", ld, "--key", keys["supervisor"], "--subject", "c-1", "--body", "x", "--cc", fps["workerA"]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e, code := runEnv(t, tc.args...); code != envelopeUsageExit || e.Error == nil {
				t.Fatalf("want a usage refusal, got %d %+v", code, e)
			}
		})
	}
	if e, code := runEnv(t, "message", "post"); code != envelopeUsageExit || e.Error == nil || !strings.Contains(e.Error.Message, "send") {
		t.Fatalf("an unknown subverb names the real ones: %d %+v", code, e)
	}
}
