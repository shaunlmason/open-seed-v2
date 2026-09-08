package project

// The message notice derivation (plans/os-8451d939.md D1, D2). The
// containment claim — that no body reaches the situation read — is
// swept by marker in cmd/seed, because a drill that reads this
// package's structs cannot notice a field the CLI adds downstream.
// What this file pins is the derivation itself.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAddressedToResolvesTheThreeCases(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		to            []string
		undeliverable bool
	}{
		{"no to key", `{"n": 1}`, nil, false},
		{"not an object at all", `[1, 2]`, nil, false},
		{"a string", `{"to": "aa"}`, []string{"aa"}, false},
		{"an array", `{"to": ["aa", "bb"]}`, []string{"aa", "bb"}, false},
		// Present and unreadable: nobody, never everybody. Each of
		// these would otherwise widen delivery to every actor on an
		// encoding slip (review finding on #209).
		{"a mixed array", `{"to": ["aa", 7]}`, nil, true},
		{"a number", `{"to": 7}`, nil, true},
		{"an object", `{"to": {"fp": "aa"}}`, nil, true},
		{"null", `{"to": null}`, nil, true},
		{"an empty array", `{"to": []}`, nil, true},
		{"an empty string", `{"to": ""}`, nil, true},
		{"an array with an empty string", `{"to": ["aa", ""]}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			to, undeliverable := AddressedTo([]byte(tc.payload))
			if !reflect.DeepEqual(to, tc.to) || undeliverable != tc.undeliverable {
				t.Fatalf("got (%v, %v), want (%v, %v)", to, undeliverable, tc.to, tc.undeliverable)
			}
			// And the resolution agrees with the reach: an
			// undeliverable message reaches nobody, including the
			// actor whose fingerprint appears in the malformed field.
			n := MessageNotice{To: to, Undeliverable: undeliverable}
			if n.Addresses("aa") == tc.undeliverable {
				t.Errorf("an undeliverable message must reach no one, including %q", "aa")
			}
		})
	}
}

// conformance: the notice type carries no field a body could occupy.
// This is a structural complement to cmd/seed's marker sweep: the sweep
// catches a body reaching the READ, and this catches one being added to
// the TYPE, which is where it would come from.
func TestTheNoticeTypeHoldsNoPayloadText(t *testing.T) {
	// The whitelist is the contract: every field is a generated
	// identifier, a count, or a flag. Adding one means deciding, on
	// purpose, that it cannot carry sender-controlled prose.
	allowed := map[string]bool{
		"From": true, "Subject": true, "At": true,
		"Bytes": true, "To": true, "Undeliverable": true,
	}
	rt := reflect.TypeOf(MessageNotice{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !allowed[f.Name] {
			t.Errorf("MessageNotice grew the field %q (%s). If it can carry text the SENDER chose, "+
				"it must not exist: the situation read is taken on every wake, unbidden, and "+
				"message.sent needs no capability (spec/lanes.md). If it cannot, add it here "+
				"deliberately.", f.Name, f.Type)
		}
	}
	// Subject and From are ledger-generated identifiers rather than
	// prose, which is the whole reason they are safe to carry: a
	// serialized notice of a hostile payload holds none of its text.
	b, err := json.Marshal(MessageNotice{
		From: "aa", Subject: "c-1", At: 3, Bytes: 900, To: []string{"bb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "900") && !strings.Contains(string(b), `"bytes":900`) {
		t.Errorf("the size is a count, not content: %s", b)
	}
}
