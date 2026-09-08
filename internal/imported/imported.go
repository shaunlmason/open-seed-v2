// Package imported is the shape of the import's provenance record
// (plans/os-cf13fb51.md D2; SEED-NEXT.md §II.17, Appendix D.2):
// `system.imported`, the one fact a genesis import appends before the
// replayed history, naming the predecessor, the export it came from,
// the anchor that vouched for it, and the mapping manifest that gives
// every export record its disposition. Like internal/checkpoint it
// holds the payload and nothing else: admission validates the shape
// and the once-per-ledger rule, and the reader fetches the manifest.
package imported

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Verb is the provenance record's verb; Subject its only subject.
const (
	Verb    = "system.imported"
	Subject = "system"
)

// SourceOpenSeed is the one predecessor this build understands.
const SourceOpenSeed = "open-seed"

// Payload is the strict object {source, export_head, anchor, manifest}.
type Payload struct {
	Source     string `json:"source"`
	ExportHead string `json:"export_head"`
	Anchor     string `json:"anchor"`
	Manifest   string `json:"manifest"`
}

var (
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Error is a malformed provenance payload.
type Error struct{ Reason string }

func (e *Error) Error() string { return "system.imported: " + e.Reason }

// Parse validates the payload's shape: a known source, the export's
// head commit, the anchor tag that covered it, and the manifest's
// artifact digest.
func Parse(subject string, raw []byte) (*Payload, error) {
	if subject != Subject {
		return nil, &Error{Reason: fmt.Sprintf("subject %q is not %q", subject, Subject)}
	}
	var p Payload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, &Error{Reason: fmt.Sprintf("the payload is the strict object {source, export_head, anchor, manifest}: %v", err)}
	}
	if dec.More() {
		return nil, &Error{Reason: "the payload carries trailing data"}
	}
	if p.Source != SourceOpenSeed {
		return nil, &Error{Reason: fmt.Sprintf("source %q is not %q, the one predecessor this build imports", p.Source, SourceOpenSeed)}
	}
	if !commitRE.MatchString(p.ExportHead) {
		return nil, &Error{Reason: "export_head is the export's state commit, forty lowercase hex characters"}
	}
	if strings.TrimSpace(p.Anchor) == "" || strings.ContainsAny(p.Anchor, " \t\n") {
		return nil, &Error{Reason: "anchor names the tag that covered the export"}
	}
	if !digestRE.MatchString(p.Manifest) {
		return nil, &Error{Reason: "manifest is the mapping manifest's sha256 digest in the artifact store"}
	}
	return &p, nil
}

// Render renders the strict payload.
func Render(exportHead, anchor, manifest string) ([]byte, error) {
	return json.Marshal(Payload{Source: SourceOpenSeed, ExportHead: exportHead, Anchor: anchor, Manifest: manifest})
}
