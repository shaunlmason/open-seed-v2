package checkpoint

// The snapshot citation's drills (plans/os-8a5f14bb.md D4.5). Every
// refusal here is one a checkpoint could previously have been signed
// and admitted with: before this rule the boundary took an arbitrary
// payload, so an unusable checkpoint was indistinguishable from a
// usable one until a reader tried to start from it and could not.

import (
	"encoding/json"
	"strings"
	"testing"
)

func good(t *testing.T, position int) []byte {
	t.Helper()
	b, err := Payload(strings.Repeat("a", 64), position)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseAcceptsAWellFormedCitation(t *testing.T) {
	c, err := Parse("seed/0", good(t, 42))
	if err != nil {
		t.Fatalf("a well-formed citation must parse: %v", err)
	}
	if c.Format != Format || c.Location != Location || c.Snapshot != strings.Repeat("a", 64) {
		t.Errorf("the payload must round-trip: %+v", c)
	}
	if c.At() != 42 {
		t.Errorf("At reads the position back: %d", c.At())
	}
}

func TestParseRefusals(t *testing.T) {
	base := string(good(t, 1))
	for _, tc := range []struct{ name, payload, says string }{
		{"the pre-rule payload", `{"n": 1}`, "strict object"},
		{"not an object", `"checkpoint"`, "strict object"},
		{"an unknown field", strings.Replace(base, `{"format"`, `{"extra": 1, "format"`, 1), "strict object"},
		{"an unknown format", strings.Replace(base, Format, "made-up/v9", 1), "versioned"},
		{"an empty format", strings.Replace(base, Format, "", 1), "versioned"},
		{"a snapshot that is not a digest", strings.Replace(base, strings.Repeat("a", 64), "nope", 1), "digest"},
		{"an uppercase digest", strings.Replace(base, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), "digest"},
		{"a location nothing can fetch", strings.Replace(base, `"`+Location+`"`, `"https://elsewhere"`, 1), "fetch"},
		{"no position", strings.Replace(base, `"position":"1"`, `"position":""`, 1), "non-negative"},
		{"a negative position", strings.Replace(base, `"position":"1"`, `"position":"-1"`, 1), "non-negative"},
		{"a position that is not a number", strings.Replace(base, `"position":"1"`, `"position":"tip"`, 1), "non-negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("seed/0", []byte(tc.payload))
			if err == nil {
				t.Fatalf("must refuse: %s", tc.payload)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal must say %q, got %v", tc.says, err)
			}
			if !strings.Contains(err.Error(), "seed/0") {
				t.Errorf("the refusal names its subject: %v", err)
			}
		})
	}
}

// The materialization is deterministic: the same files at the same
// position produce the same bytes, so two readers materializing one
// prefix agree and a fetched snapshot can be checked against a hash.
// This is the property the whole citation rests on.
func TestMaterializeIsDeterministic(t *testing.T) {
	files := map[string][]byte{"queue/queue.json": []byte(`{"a":1}`), "actors/actors.json": []byte(`{"b":2}`)}
	first, err := Materialize(7, "tip-hash", files)
	if err != nil {
		t.Fatal(err)
	}
	// A DIFFERENT map with the same contents, built in the other
	// insertion order: Go randomizes map iteration, so this is the
	// case that would break a naive serializer.
	other := map[string][]byte{}
	other["actors/actors.json"] = []byte(`{"b":2}`)
	other["queue/queue.json"] = []byte(`{"a":1}`)
	second, err := Materialize(7, "tip-hash", other)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("the same state must serialize identically:\n%s\n%s", first, second)
	}
	if changed, _ := Materialize(8, "tip-hash", files); string(changed) == string(first) {
		t.Error("a different position is a different snapshot")
	}
}

func TestReadSnapshotRoundTrips(t *testing.T) {
	files := map[string][]byte{"queue/queue.json": []byte(`{"a":1}`)}
	body, err := Materialize(3, "tip", files)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := ReadSnapshot(body)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Position != "3" || snap.Tip != "tip" {
		t.Errorf("the snapshot carries what it materializes: %+v", snap)
	}
	if string(snap.Files["queue/queue.json"]) != `{"a":1}` {
		t.Errorf("the files survive verbatim: %q", snap.Files["queue/queue.json"])
	}
}

// A snapshot in a format this build does not know is REFUSED rather
// than read optimistically: the reader replays instead, which is the
// safe direction. Guessing at an unknown layout is how a reader starts
// from a state it has misread.
func TestReadSnapshotRefusesAnUnknownFormat(t *testing.T) {
	body, err := json.Marshal(Snapshot{Format: "made-up/v9", Position: "1", Files: map[string][]byte{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(body); err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("an unknown format must refuse and say to replay: %v", err)
	}
	if _, err := ReadSnapshot([]byte("not json")); err == nil {
		t.Fatal("unparseable bytes must refuse")
	}
}

// Materialize accepts an absent file set and produces an empty one
// rather than a JSON null, which ReadSnapshot would then hand back as
// a nil map that reads as "no projections" instead of "none recorded".
func TestMaterializeWithNoFiles(t *testing.T) {
	body, err := Materialize(0, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "null") {
		t.Errorf("an empty file set is an empty object, never null: %s", body)
	}
	snap, err := ReadSnapshot(body)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Files == nil {
		t.Error("the reader gets an empty map rather than nil")
	}
}
