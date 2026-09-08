// Package boundary is the A2A-shaped cross-organization boundary
// (plans/os-40ed0ca0.md; spec/boundary.md; SEED-NEXT.md III.N):
// the capability card a deployment publishes, the five-state task
// lifecycle a stranger derives from the target's own read, and the
// pins that make opacity a checked property rather than a habit.
//
// Nothing here is a dependency on any A2A library: the shape is the
// card, the task states and artifacts by digest; discovery is out of
// band, and the only write across the boundary is the request ingress
// (spec/requests.md).
package boundary

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/request"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// ArtifactKinds is what a task publishes across the boundary: a
// receipt, a plan, a body — each by digest, never by path.
var ArtifactKinds = []string{"receipt", "plan", "body"}

// CardIngress names the request kinds the deployment accepts and the
// remote or endpoint a request is filed through.
type CardIngress struct {
	Kinds   []string `json:"kinds"`
	Through string   `json:"through"`
}

// CardSquad is one squad and the tiers it accepts work at: the names
// only, never the lanes, the manifests or the budgets.
type CardSquad struct {
	Name  string   `json:"name"`
	Tiers []string `json:"tiers"`
}

// Card is the signed, published statement of what a deployment
// offers, and nothing else (D1). Its field set is pinned
// (CardFields): a field added here is a deliberate change to the pin.
type Card struct {
	Name      string      `json:"name"`
	Protocol  string      `json:"protocol"`
	Ingress   CardIngress `json:"ingress"`
	Squads    []CardSquad `json:"squads"`
	Artifacts []string    `json:"artifacts"`
	// Signer is the operator key's fingerprint; Signature the
	// lowercase-hex ed25519 signature over the card's canonical bytes
	// without it.
	Signer    string `json:"signer"`
	Signature string `json:"signature"`
}

// CardFields, TaskFields and Routes are the pins (D4): every route
// the surface serves and every field each response carries, sorted.
var (
	CardFields = []string{"artifacts", "ingress", "name", "protocol", "signature", "signer", "squads"}
	TaskFields = []string{"answer", "artifacts", "request", "state"}
	Routes     = []string{"GET /artifacts/{digest}", "GET /card", "GET /tasks", "GET /tasks/{request}"}
)

// internals is what a card must never name: a card that spoke of a
// deployment's lane manifests, fragments, prompts, budgets or models
// would be publishing how it works rather than what it offers.
var internals = []string{"manifest", "fragment", "prompt", "budget", "model", "lanes/", "fingerprint"}

// Error is a boundary refusal.
type Error struct{ Reason string }

func (e *Error) Error() string { return "boundary: " + e.Reason + " (spec/boundary.md)" }

// Render derives the unsigned card from the declaration: the ingress
// from the boundary block, the squads and the tiers each accepts
// from teams and guardrails (names only), the protocol from the
// declaration or this build's newest registered version, the
// artifact kinds fixed.
func Render(cfg *posture.Config, name string) (*Card, error) {
	if cfg == nil {
		return nil, &Error{Reason: "no declaration to render the card from"}
	}
	if strings.TrimSpace(name) == "" {
		return nil, &Error{Reason: "the card names the deployment"}
	}
	if cfg.Boundary == nil {
		return nil, &Error{Reason: "the declaration has no boundary block: a card publishes an ingress or nothing"}
	}
	c := &Card{Name: name, Protocol: cfg.Protocol, Artifacts: append([]string{}, ArtifactKinds...)}
	if c.Protocol == "" {
		supported := version.Supported()
		c.Protocol = supported[len(supported)-1]
	}
	c.Ingress = CardIngress{Kinds: append([]string{}, cfg.Boundary.Accepts...), Through: cfg.Boundary.Ingress}
	sort.Strings(c.Ingress.Kinds)
	c.Squads = []CardSquad{}
	for _, squad := range cfg.SquadNames() {
		tiers := map[string]bool{}
		if cfg.Guardrails != nil {
			if g, ok := cfg.Guardrails.Squads[squad]; ok {
				if g.Default != "" {
					tiers[g.Default] = true
				}
				if g.MaxAgent != "" {
					tiers[g.MaxAgent] = true
				}
			}
		}
		names := make([]string, 0, len(tiers))
		for t := range tiers {
			names = append(names, t)
		}
		sort.Strings(names)
		c.Squads = append(c.Squads, CardSquad{Name: squad, Tiers: names})
	}
	if err := c.check(false); err != nil {
		return nil, err
	}
	return c, nil
}

// Canonical is the card's RFC 8785 (JCS) bytes without its signature,
// through the same transform the event model signs over
// (internal/event.Canonical). One canonicalizer for the tree: a
// signature made here is one an independent JCS verifier reconstructs.
//
// It was hand-rolled once, on the reasoning that a compact encoding
// with sorted members and no HTML escaping is JCS-canonical for a
// value domain of strings and arrays (docs/decisions.md,
// "Canonical bytes without a JCS library"). That premise is false for
// the string domain: encoding/json escapes U+2028 and U+2029, which
// JCS emits literally, and check constrains Name only to be
// non-empty, so a deployment name carrying either character signed
// over bytes no other implementation produces.
func (c *Card) Canonical() ([]byte, error) {
	unsigned := *c
	unsigned.Signature = ""
	b, err := json.Marshal(unsigned)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, err
	}
	delete(generic, "signature")
	stripped, err := json.Marshal(generic)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(stripped)
}

// Sign sets the signer and the signature from the operator key.
func Sign(c *Card, priv ed25519.PrivateKey) error {
	if len(priv) != ed25519.PrivateKeySize {
		return &Error{Reason: "the signing key is not an ed25519 private key"}
	}
	fp, err := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	if err != nil {
		return err
	}
	c.Signer = fp
	canon, err := c.Canonical()
	if err != nil {
		return err
	}
	c.Signature = hex.EncodeToString(ed25519.Sign(priv, canon))
	return c.check(true)
}

// Verify holds a card to the operator key a reader was given out of
// band: the signer is that key's fingerprint and the signature is
// its signature over the canonical bytes.
func Verify(c *Card, pub ed25519.PublicKey) error {
	if err := c.check(true); err != nil {
		return err
	}
	fp, err := event.Fingerprint(pub)
	if err != nil {
		return err
	}
	if c.Signer != fp {
		return &Error{Reason: fmt.Sprintf("the card is signed by %s, not the operator key given (%s)", c.Signer, fp)}
	}
	sig, err := hex.DecodeString(c.Signature)
	if err != nil {
		return &Error{Reason: "the signature is not hex"}
	}
	canon, err := c.Canonical()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, canon, sig) {
		return &Error{Reason: "the signature does not verify over the card's canonical bytes"}
	}
	return nil
}

// Parse reads a published card strictly: the pinned fields and no
// other, signed, naming no internals.
func Parse(b []byte) (*Card, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Card
	if err := dec.Decode(&c); err != nil {
		return nil, &Error{Reason: fmt.Sprintf("the card is the strict object of %s: %v", strings.Join(CardFields, ", "), err)}
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &Error{Reason: "the card is one object and nothing after it"}
	}
	if err := c.check(true); err != nil {
		return nil, err
	}
	return &c, nil
}

// Check is the content rule with the signature required.
func Check(c *Card) error { return c.check(true) }

func (c *Card) check(signed bool) error {
	if strings.TrimSpace(c.Name) == "" {
		return &Error{Reason: "the card names the deployment"}
	}
	if !version.Activated(c.Protocol) && c.Protocol != version.Protocol {
		return &Error{Reason: fmt.Sprintf("protocol %q is not one this build registers", c.Protocol)}
	}
	if len(c.Ingress.Kinds) == 0 || strings.TrimSpace(c.Ingress.Through) == "" {
		return &Error{Reason: "the ingress names the request kinds accepted and what a request is filed through"}
	}
	seenKind := map[string]bool{}
	for _, k := range c.Ingress.Kinds {
		known := false
		for _, want := range request.Kinds {
			known = known || k == want
		}
		if !known {
			return &Error{Reason: fmt.Sprintf("ingress kind %q is not a request kind (%s)", k, strings.Join(request.Kinds, ", "))}
		}
		if seenKind[k] {
			return &Error{Reason: fmt.Sprintf("ingress kind %q is listed twice", k)}
		}
		seenKind[k] = true
	}
	if len(c.Artifacts) == 0 {
		return &Error{Reason: "the card names the artifact kinds a task returns"}
	}
	seenArtifact := map[string]bool{}
	for _, a := range c.Artifacts {
		known := false
		for _, want := range ArtifactKinds {
			known = known || a == want
		}
		if !known {
			return &Error{Reason: fmt.Sprintf("artifact kind %q is not one a task publishes (%s)", a, strings.Join(ArtifactKinds, ", "))}
		}
		if seenArtifact[a] {
			return &Error{Reason: fmt.Sprintf("artifact kind %q is listed twice", a)}
		}
		seenArtifact[a] = true
	}
	for _, s := range c.Squads {
		if strings.TrimSpace(s.Name) == "" {
			return &Error{Reason: "a squad has a name"}
		}
	}
	var words []string
	words = append(words, c.Name, c.Ingress.Through)
	words = append(words, c.Ingress.Kinds...)
	for _, s := range c.Squads {
		words = append(words, s.Name)
		words = append(words, s.Tiers...)
	}
	for _, w := range words {
		lower := strings.ToLower(w)
		for _, taboo := range internals {
			if strings.Contains(lower, taboo) {
				return &Error{Reason: fmt.Sprintf("%q names an internal (%s): a card says what is offered, never how", w, taboo)}
			}
		}
	}
	if signed {
		if c.Signer == "" || c.Signature == "" {
			return &Error{Reason: "the card is unsigned: a statement nobody signed offers nothing"}
		}
	}
	return nil
}

// Accepts reports whether the card's ingress takes a request kind.
func (c *Card) Accepts(kind string) bool {
	for _, k := range c.Ingress.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// The five task states (D2), derived from the target chain and
// nothing finer.
const (
	StateRequested = "requested"
	StateDeclined  = "declined"
	StateAccepted  = "accepted"
	StateWorking   = "working"
	StateDone      = "done"
)

// Task is a cross-org task as the boundary shows it: the request's
// position, its answer's, the state, and the artifact digests the
// contract published. No actor, no fence, no payload, no contract id.
type Task struct {
	Request   int      `json:"request"`
	Answer    *int     `json:"answer"`
	State     string   `json:"state"`
	Artifacts []string `json:"artifacts"`
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Tasks derives every cross-repo request's task from the verified
// records and their fold, in request order.
func Tasks(records []*event.Record, fold *transition.Fold) []Task {
	out := []Task{}
	if fold == nil {
		return out
	}
	for _, r := range fold.Requests() {
		if r.Kind != "cross-repo" {
			continue
		}
		t := Task{Request: r.Pos, State: StateRequested, Artifacts: []string{}}
		if r.Answered == nil {
			out = append(out, t)
			continue
		}
		at := *r.Answered
		t.Answer = &at
		if r.Outcome == "declined" {
			t.State = StateDeclined
			out = append(out, t)
			continue
		}
		t.State = StateAccepted
		if r.Intent >= 0 && r.Intent < len(records) {
			subject := records[r.Intent].Event.Subject
			if s, ok := fold.State(subject); ok {
				switch {
				case s.Merged != nil || s.State == "done":
					t.State = StateDone
				case s.Claim != nil || len(s.Claims) > 0 || s.State == "in_progress" || s.State == "review":
					t.State = StateWorking
				}
				seen := map[string]bool{}
				add := func(d string) {
					if digestRE.MatchString(d) && !seen[d] {
						seen[d] = true
						t.Artifacts = append(t.Artifacts, d)
					}
				}
				add(fold.PlanDigests(subject).Approved)
				for _, v := range s.Verdicts {
					add(v.Receipt)
				}
			}
		}
		out = append(out, t)
	}
	return out
}

// Sweep refuses a response body that carries any of the taboo strings
// (D4): the drill feeds it every fingerprint on the chain, every
// payload and every packet, and a body carrying one is ledger material
// crossing the boundary.
func Sweep(body []byte, taboo []string) error {
	text := string(body)
	for _, t := range taboo {
		if t != "" && strings.Contains(text, t) {
			return &Error{Reason: fmt.Sprintf("the response carries ledger material (%.16s…)", t)}
		}
	}
	return nil
}

// FieldsOf lists the top-level keys of a JSON object, sorted, for the
// pin comparison.
func FieldsOf(body []byte) ([]string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errors.New("the response is not a JSON object")
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}
