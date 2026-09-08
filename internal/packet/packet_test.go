package packet_test

// Shape drills (plans/os-b07b0f59.md step 4): each part missing, an
// unmarked decision, a bare path, a bad base, an unknown key, and an
// oversize packet refuse with the part named; the minimal honest
// packet admits; the exit-verb set is pinned to the transition table.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/packet"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

const valid = `{
  "acceptance": ["the thing works", "the drill passes"],
  "decisions": [
    {"decision": "use the existing seam", "basis": "verified"},
    {"decision": "the flag is unused downstream", "basis": "asserted"}
  ],
  "base": "aaaaaaaaaaaa..bbbbbbbbbbbb",
  "refs": ["internal/packet/packet.go @ aaaaaaaaaaaa", "seed/c-1 @ aaaaaaa..bbbbbbb"],
  "findings": [{"tried": "approach-X", "outcome": "fails: the cache locks first", "pointer": "notes/x.md @ aaaaaaaaaaaa"}]
}`

func TestPacketShape(t *testing.T) {
	if _, err := packet.Parse("c-1", []byte(valid)); err != nil {
		t.Fatalf("the reference packet must parse: %v", err)
	}
	minimal := `{"acceptance": ["done"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	if _, err := packet.Parse("c-1", []byte(minimal)); err != nil {
		t.Fatalf("the minimal honest packet (empty findings and refs, zero-length base) must parse: %v", err)
	}

	cases := []struct{ name, old, new, part string }{
		{"missing acceptance", `"acceptance": ["the thing works", "the drill passes"],`, ``, "acceptance"},
		{"empty acceptance", `["the thing works", "the drill passes"]`, `[]`, "acceptance"},
		{"missing decisions", `"decisions": [
    {"decision": "use the existing seam", "basis": "verified"},
    {"decision": "the flag is unused downstream", "basis": "asserted"}
  ],`, ``, "decisions"},
		{"unmarked decision", `"basis": "asserted"`, `"basis": "probably"`, "decisions"},
		{"missing base", `"base": "aaaaaaaaaaaa..bbbbbbbbbbbb",`, ``, "base"},
		{"prose base", `"aaaaaaaaaaaa..bbbbbbbbbbbb"`, `"the main branch"`, "base"},
		{"bare path ref", `"internal/packet/packet.go @ aaaaaaaaaaaa"`, `"internal/packet/packet.go"`, "refs"},
		{"missing findings", `,
  "findings": [{"tried": "approach-X", "outcome": "fails: the cache locks first", "pointer": "notes/x.md @ aaaaaaaaaaaa"}]`, ``, "findings"},
		{"finding without outcome", `"outcome": "fails: the cache locks first", `, `"outcome": " ", `, "findings"},
	}
	for _, c := range cases {
		mutated := strings.Replace(valid, c.old, c.new, 1)
		if mutated == valid {
			t.Fatalf("%s: mutation did not apply", c.name)
		}
		_, err := packet.Parse("c-1", []byte(mutated))
		pe, ok := err.(*packet.Error)
		if !ok {
			t.Fatalf("%s: want a typed packet error, got %v", c.name, err)
		}
		if pe.Part != c.part {
			t.Fatalf("%s: refusal must name part %q, got %q (%v)", c.name, c.part, pe.Part, err)
		}
	}

	// A null array part decodes into the same nil slice an absent key
	// does and would slip past presence plus the range loops; the shape
	// requires literal arrays, explicitly empty where honesty allows.
	for _, c := range []struct{ name, old, new, part string }{
		{"null acceptance", `"acceptance": ["done"]`, `"acceptance": null`, "acceptance"},
		{"null decisions", `"decisions": []`, `"decisions": null`, "decisions"},
		{"null refs", `"refs": []`, `"refs": null`, "refs"},
		{"null findings", `"findings": []`, `"findings": null`, "findings"},
	} {
		mutated := strings.Replace(minimal, c.old, c.new, 1)
		if mutated == minimal {
			t.Fatalf("%s: mutation did not apply", c.name)
		}
		_, err := packet.Parse("c-1", []byte(mutated))
		pe, ok := err.(*packet.Error)
		if !ok || pe.Part != c.part {
			t.Fatalf("%s: a null part must refuse naming part %q, got %v", c.name, c.part, err)
		}
	}

	// Unknown keys refuse (strict shape), at the top level and inside
	// entries.
	for _, m := range []string{
		strings.Replace(valid, `"base"`, `"transcript": "...", "base"`, 1),
		strings.Replace(valid, `"basis": "verified"`, `"basis": "verified", "mood": "good"`, 1),
	} {
		if _, err := packet.Parse("c-1", []byte(m)); err == nil {
			t.Fatal("unknown keys must refuse")
		}
	}

	// The size bound is on canonical bytes, and the refusal says so.
	big := strings.Replace(valid, "the thing works", strings.Repeat("w", packet.MaxCanonicalBytes), 1)
	_, err := packet.Parse("c-1", []byte(big))
	pe, ok := err.(*packet.Error)
	if !ok || !strings.Contains(pe.Reason, "bound") {
		t.Fatalf("an oversize packet must refuse on the canonical bound, got %v", err)
	}

	// FromPayload: a missing packet names the obligation; the reserved
	// ref form names its phase.
	if _, err := packet.FromPayload("c-1", []byte(`{"fence": "4"}`)); err == nil {
		t.Fatal("a deliberate exit without a packet must refuse")
	}
	_, err = packet.FromPayload("c-1", []byte(`{"packet_ref": "store://x"}`))
	pe, ok = err.(*packet.Error)
	if !ok || pe.Part != packet.RefKey {
		t.Fatalf("the reserved ref form must refuse naming itself, got %v", err)
	}
	if _, err := packet.FromPayload("c-1", []byte(`{"fence": "4", "packet": `+minimal+`}`)); err != nil {
		t.Fatalf("a payload carrying fence and packet side by side must parse: %v", err)
	}
}

// The exit-verb set is the transition table's in_progress outgoing set,
// pinned from both sides: the packet obligation and the pinned
// deliberate exits cannot drift apart.
func TestExitVerbsMatchTheTable(t *testing.T) {
	tab, err := transition.Default()
	if err != nil {
		t.Fatal(err)
	}
	var fromTable []string
	for _, v := range tab.Verbs() {
		if tab.Allows("in_progress", v) {
			fromTable = append(fromTable, v)
		}
	}
	sort.Strings(fromTable)
	pinned := append([]string(nil), packet.ExitVerbs...)
	sort.Strings(pinned)
	if fmt.Sprint(fromTable) != fmt.Sprint(pinned) {
		t.Fatalf("packet.ExitVerbs %v drifted from the table's deliberate exits %v", pinned, fromTable)
	}
	if packet.Required("message.sent") || !packet.Required("submission.made") {
		t.Fatal("Required must gate the exit verbs")
	}
	// A packet-carrying verb that is NOT an exit: the escalation.
	// Asserted from both sides, because the two lists mean different
	// things and folding one into the other would break the pinning
	// above rather than extend it (plans/os-f781f0da.md).
	for _, v := range packet.EscalationVerbs {
		if !packet.Required(v) {
			t.Fatalf("%s carries a packet: the charter says an escalation carries one", v)
		}
		if tab.Allows("in_progress", v) {
			t.Fatalf("%s must not leave in_progress: the deliberate exits are exactly four", v)
		}
		for _, e := range packet.ExitVerbs {
			if e == v {
				t.Fatalf("%s is not an exit and must stay out of ExitVerbs", v)
			}
		}
	}
}
