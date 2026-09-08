// Package refusals is the attempts journal behind the report's
// refusal-rate metric (plans/os-edf73d66.md; SEED-NEXT.md §II.10,
// charter III.I row 4). Refusals never reach the chain — admission
// refuses before anything is written — so the metric's source is a
// local, append-only JSONL journal of admission-boundary ATTEMPTS,
// both outcomes, kept beside the ledger: one population for the
// rate's numerator and denominator alike. The journal is operator
// telemetry — never synced by gitref, never part of verification —
// and it enters the report only as a declared, digest-covered input
// (the observations pattern).
package refusals

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gowebpki/jcs"
)

// File is the journal's name inside the ledger directory.
const File = "attempts.jsonl"

// Outcomes an attempt line may carry.
const (
	OutcomeAdmitted = "admitted"
	OutcomeRefused  = "refused"
)

// Entry is one admission-boundary attempt: the instant, the
// tip-ordinal position the response envelope stamped, the signing
// actor, the verb and subject attempted, the outcome, on refusals
// only the envelope's machine code, and the digest of the act
// attempted (plans/os-a9e715dc.md D1), which is what tells a blind
// retry from a corrected one. Absent on lines written before the
// field existed, which still load.
type Entry struct {
	TS       string `json:"ts"`
	Position string `json:"position"`
	Actor    string `json:"actor"`
	Verb     string `json:"verb"`
	Subject  string `json:"subject"`
	Outcome  string `json:"outcome"`
	Code     string `json:"code,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

// AttemptDigest is the identity of an ACT, not of a record
// (plans/os-a9e715dc.md D1): the lowercase hex SHA-256 of the RFC 8785
// canonical form of {actor, verb, subject, payload}. The boundary's
// coordinates are excluded on purpose: ts, prev and the version change
// on every retry by construction, so a record hash can never match
// across one, while two attempts of the same act by the same key
// digest alike wherever the tip stood. A payload that is not a JSON
// object (which the boundary refuses at the shape rule) is embedded
// as a string, so the digest still exists and still differs from any
// object's. An empty payload digests as an empty object.
func AttemptDigest(actor, verb, subject string, payload []byte) string {
	var body any
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		body = json.RawMessage(`{}`)
	} else if json.Valid(trimmed) && trimmed[0] == '{' {
		body = json.RawMessage(trimmed)
	} else {
		body = string(payload)
	}
	b, err := json.Marshal(map[string]any{"actor": actor, "verb": verb, "subject": subject, "payload": body})
	if err != nil {
		return ""
	}
	canonical, err := jcs.Transform(b)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// digestShape is what a present digest must look like: a sha256 in
// lowercase hex, the shape AttemptDigest writes.
func digestShape(d string) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Note appends one attempt line to the journal in dir, best-effort:
// journaling must never fail or slow the verb it rides, so every
// error — an unwritable directory, a full disk, a marshal failure —
// is swallowed, exactly the affordance-stamping posture. A failure
// must also never poison the journal for later builds (review
// finding on the task PR): a short write (quota, full disk) would
// leave a truncated fragment for the next append to glue onto, so
// Note restores the previous length when the fragment is provably
// the file's tail — with O_APPEND a rival process's line may land
// around ours, and truncating an ambiguous size would destroy it,
// so anything else is left for Load's torn-tail rule.
func Note(dir string, e Entry) {
	if dir == "" {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	line := append(b, '\n')
	f, err := os.OpenFile(filepath.Join(dir, File), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	pre, err := f.Stat()
	if err != nil {
		return
	}
	n, err := f.Write(line)
	if err == nil && n == len(line) {
		return
	}
	if now, statErr := f.Stat(); statErr == nil && now.Size() == pre.Size()+int64(n) {
		_ = f.Truncate(pre.Size())
	}
}

// Journal is a loaded attempts journal.
type Journal struct {
	Entries []Entry
}

// Load parses the journal at path. Inputs are declared, so garbage
// is the declarer's error, never silently skipped telemetry: every
// line must decode strictly to the entry shape, carry a known
// outcome and a numeric position, and pair code with outcome
// (refusals carry one, admissions never do). Errors name the line.
// The one exception is the commit-marker rule (review finding on
// the task PR): the terminating newline is what commits a line, so
// a final unterminated fragment — a torn short write or a crash
// mid-append — is an uncommitted attempt, ignored rather than
// allowed to poison every future build of a journal whose writer
// is best-effort by design. Terminated lines stay strict.
func Load(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	j := &Journal{Entries: []Entry{}}
	lines := strings.Split(string(data), "\n")
	for i, raw := range lines {
		if i == len(lines)-1 {
			// Empty when the file is newline-terminated; a torn
			// uncommitted fragment otherwise. Skipped either way.
			break
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var e Entry
		dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("line %d does not parse as an attempt: %v", i+1, err)
		}
		if dec.More() {
			return nil, fmt.Errorf("line %d carries trailing data", i+1)
		}
		if err := e.check(); err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		j.Entries = append(j.Entries, e)
	}
	return j, nil
}

func (e Entry) check() error {
	switch e.Outcome {
	case OutcomeAdmitted:
		if e.Code != "" {
			return fmt.Errorf("admitted attempts carry no code, got %q", e.Code)
		}
	case OutcomeRefused:
		if e.Code == "" {
			return errors.New("refused attempts carry the envelope's machine code")
		}
	default:
		return fmt.Errorf("outcome must be %q or %q, got %q", OutcomeAdmitted, OutcomeRefused, e.Outcome)
	}
	if _, err := strconv.Atoi(e.Position); err != nil {
		return fmt.Errorf("position must be the envelope's decimal tip-ordinal stamp, got %q", e.Position)
	}
	if e.Verb == "" || e.Subject == "" || e.Actor == "" {
		return errors.New("attempts name actor, verb, and subject")
	}
	if e.Digest != "" && !digestShape(e.Digest) {
		return fmt.Errorf("digest is not a sha256, got %q", e.Digest)
	}
	return nil
}

// Digest is the journal's declared-input identity: the RFC 8785
// digest over the entry list, so any change to any line rekeys the
// build that consumed it.
func (j *Journal) Digest() (string, error) {
	b, err := json.Marshal(j.Entries)
	if err != nil {
		return "", err
	}
	canonical, err := jcs.Transform(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
