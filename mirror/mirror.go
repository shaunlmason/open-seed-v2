// Package mirror is the projection-only issue mirror (plans/os-b45c308d.md
// D1, D2; SEED-NEXT.md III.D rows 5 and 6; spec/projections.md
// "The mirror"): one desired issue per contract, rendered from a
// PUBLISHED contracts projection and its stamp, planned as a sorted,
// byte-deterministic set of actions against what a forge holds, and
// applied outward through one adapter interface. The package never
// opens a ledger, holds a Seed key, contacts admission or imports the
// request writer: its only write is the forge's, and the forge's only
// way back into Seed is the governed request ingress, a different
// component. Every registered exporter runs the same conformance suite
// (mirror_test.go), so a forge the registry knows is a forge the suite
// proved.
package mirror

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The marker grammar: the subject's UTF-8 bytes base64-encoded, so an
// opaque subject (admission restricts no character) never terminates
// or re-opens the comment; decoding is the only parse path.
const (
	markerOpen      = "<!-- seed-mirror: "
	markerClose     = " -->"
	projectionOpen  = "<!-- seed-mirror-projection: "
	projectionClose = " -->"
	// LabelPrefix is the managed label's prefix; exactly one
	// `seed:<state>` label is managed per issue, every other label is
	// foreign and survives.
	LabelPrefix = "seed:"
)

var (
	markerRE     = regexp.MustCompile(`<!-- seed-mirror: ([^ ]*) -->`)
	projectionRE = regexp.MustCompile(`<!-- seed-mirror-projection: position=(\d+) tip=([0-9a-f]{64}) -->`)
)

// Marker encodes a subject.
func Marker(subject string) string {
	return base64.StdEncoding.EncodeToString([]byte(subject))
}

// ErrMalformed is a marker that does not decode, a body carrying more
// than one marker, or a marker naming a subject the projection does
// not hold: refused, never repaired, never silently chosen between.
var ErrMalformed = errors.New("malformed seed-mirror marker")

// ParseMarker finds the one seed-mirror marker in a body and decodes
// its subject. managed is false when the body carries none (an
// unmanaged issue, untouched); an error is a body with two markers or
// one that does not decode.
func ParseMarker(body string) (subject string, managed bool, err error) {
	found := markerRE.FindAllStringSubmatch(body, -1)
	// A fragment of the marker (an opener with no complete marker
	// behind it, a truncated close) is a managed issue whose marker was
	// damaged, never an unmanaged one: refused, so the planner cannot
	// create a second issue beside it.
	if strings.Count(body, markerOpen) != len(found) {
		return "", true, fmt.Errorf("%w: a marker fragment that is not one complete marker", ErrMalformed)
	}
	switch len(found) {
	case 0:
		return "", false, nil
	case 1:
	default:
		return "", true, fmt.Errorf("%w: %d markers in one body", ErrMalformed, len(found))
	}
	raw, derr := base64.StdEncoding.Strict().DecodeString(found[0][1])
	if derr != nil || len(raw) == 0 {
		return "", true, fmt.Errorf("%w: %q does not decode", ErrMalformed, found[0][1])
	}
	return string(raw), true, nil
}

// Stamp is the projection stamp the mirror was rendered from: the
// contracts view's position and tip, carried into every body so a
// reader can name which projection the mirror reflects.
type Stamp struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
	Tip      string `json:"tip"`
	Version  string `json:"version"`
}

// Desired is the canonical issue for one contract: title the opaque
// subject, body the two markers and nothing else, one managed label,
// closed exactly for a terminal lifecycle state.
type Desired struct {
	Subject string `json:"subject"`
	State   string `json:"state"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Label   string `json:"label"`
	Closed  bool   `json:"closed"`
}

// Terminal states close the issue; every other lifecycle state is open.
var terminal = map[string]bool{"done": true, "cancelled": true}

// Body renders the desired body for a subject at a stamp.
func Body(subject string, stamp Stamp) string {
	return markerOpen + Marker(subject) + markerClose + "\n" +
		projectionOpen + fmt.Sprintf("position=%d tip=%s", stamp.Position, stamp.Tip) + projectionClose + "\n"
}

// Row is the slice of a contracts-view entry the mirror reads: the
// subject and its folded lifecycle state (null for a subject no
// lifecycle event ever validly created, which the mirror skips).
type Row struct {
	Subject string  `json:"subject"`
	State   *string `json:"state"`
}

// Render derives the desired set from the rows and the stamp, sorted
// by subject.
func Render(rows []Row, stamp Stamp) []Desired {
	var out []Desired
	for _, r := range rows {
		if r.State == nil || *r.State == "" {
			continue
		}
		out = append(out, Desired{Subject: r.Subject, State: *r.State, Title: r.Subject, Body: Body(r.Subject, stamp),
			Label: LabelPrefix + *r.State, Closed: terminal[*r.State]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// Load reads the contracts projection the consumer verb resolved:
// current is the envelope `seed project current --name contracts`
// printed (spec/projections.md "The consumer verb and
// staleness"), which names the published build's directory, position,
// tip and version; the view is `contracts.json` inside it. The mirror
// never resolves the published layout itself, so the projection
// engine's vocabulary stays the engine's, and a consumer demanding
// freshness passes --min-position to that verb, not to the mirror.
func Load(current string) ([]Row, Stamp, error) {
	raw, err := os.ReadFile(current)
	if err != nil {
		return nil, Stamp{}, fmt.Errorf("reading the resolved projection: %v", err)
	}
	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			Name     string `json:"name"`
			Position string `json:"position"`
			Tip      string `json:"tip"`
			Version  string `json:"version"`
			Path     string `json:"path"`
		} `json:"result"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, Stamp{}, fmt.Errorf("the resolved projection does not parse as a seed envelope: %v", err)
	}
	if !env.OK {
		msg := "the consumer verb refused"
		if env.Error != nil {
			msg = env.Error.Code + ": " + env.Error.Message
		}
		return nil, Stamp{}, fmt.Errorf("no published contracts projection to mirror: %s", msg)
	}
	r := env.Result
	pos, perr := strconv.Atoi(r.Position)
	if r.Name != "contracts" || r.Path == "" || r.Version == "" || perr != nil || pos < 0 || (pos > 0 && len(r.Tip) != 64) {
		return nil, Stamp{}, fmt.Errorf("the resolved projection is not a contracts build: %+v", r)
	}
	stamp := Stamp{Name: r.Name, Position: pos, Tip: r.Tip, Version: r.Version}
	vb, err := os.ReadFile(filepath.Join(r.Path, "contracts.json"))
	if err != nil {
		return nil, Stamp{}, err
	}
	var rows []Row
	if err := json.Unmarshal(vb, &rows); err != nil {
		return nil, Stamp{}, fmt.Errorf("the contracts view does not parse: %v", err)
	}
	return rows, stamp, nil
}

// Issue is an external issue as an adapter reads it: the adapter's
// own id, the title, the body, every label, and whether it is closed.
type Issue struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
	Closed bool     `json:"closed"`
}

// The action kinds.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
)

// Action is one planned change: a creation from the desired issue, or
// an update of an existing issue to it, carrying the foreign labels
// the update must keep.
type Action struct {
	Kind    string   `json:"kind"`
	Subject string   `json:"subject"`
	ID      string   `json:"id,omitempty"`
	Issue   Desired  `json:"issue"`
	Foreign []string `json:"foreign_labels,omitempty"`
	// Why names the drift the update repairs, for the operator.
	Why []string `json:"why,omitempty"`
}

// Plan is the sorted, byte-deterministic set of actions.
type Plan struct {
	Stamp     Stamp    `json:"stamp"`
	Actions   []Action `json:"actions"`
	Matching  int      `json:"matching"`
	Untouched int      `json:"untouched"`
}

// Compute plans the desired set against the issues a forge holds.
// Unmanaged issues are untouched; a managed issue whose state drifted
// (title, body, managed label, open/closed) is updated from the
// projection, foreign labels kept; a subject with no issue is created;
// two issues for one subject, or a marker naming a subject the
// projection does not hold, refuse.
func Compute(desired []Desired, existing []Issue, stamp Stamp) (Plan, error) {
	plan := Plan{Stamp: stamp, Actions: []Action{}}
	want := map[string]Desired{}
	for _, d := range desired {
		want[d.Subject] = d
	}
	managed := map[string]Issue{}
	for _, is := range existing {
		subject, isManaged, err := ParseMarker(is.Body)
		if err != nil {
			return Plan{}, fmt.Errorf("issue %s: %w", is.ID, err)
		}
		if !isManaged {
			plan.Untouched++
			continue
		}
		if _, known := want[subject]; !known {
			return Plan{}, fmt.Errorf("issue %s: %w: the marker names %q, which the projection does not hold", is.ID, ErrMalformed, subject)
		}
		if prior, dup := managed[subject]; dup {
			return Plan{}, fmt.Errorf("issues %s and %s: %w: both carry the marker for %q", prior.ID, is.ID, ErrMalformed, subject)
		}
		managed[subject] = is
	}
	for _, d := range desired {
		is, has := managed[d.Subject]
		if !has {
			plan.Actions = append(plan.Actions, Action{Kind: ActionCreate, Subject: d.Subject, Issue: d})
			continue
		}
		var why []string
		foreign := []string{}
		hasLabel := false
		for _, l := range is.Labels {
			switch {
			case l == d.Label:
				hasLabel = true
			case strings.HasPrefix(l, LabelPrefix):
				why = append(why, "stale managed label "+l)
			default:
				foreign = append(foreign, l)
			}
		}
		if !hasLabel {
			why = append(why, "missing managed label "+d.Label)
		}
		if is.Title != d.Title {
			why = append(why, "title")
		}
		if is.Body != d.Body {
			why = append(why, "body")
		}
		if is.Closed != d.Closed {
			if d.Closed {
				why = append(why, "open, should be closed")
			} else {
				why = append(why, "closed, should be open")
			}
		}
		if len(why) == 0 {
			plan.Matching++
			continue
		}
		sort.Strings(foreign)
		plan.Actions = append(plan.Actions, Action{Kind: ActionUpdate, Subject: d.Subject, ID: is.ID, Issue: d, Foreign: foreign, Why: why})
	}
	sort.Slice(plan.Actions, func(i, j int) bool { return plan.Actions[i].Subject < plan.Actions[j].Subject })
	return plan, nil
}

// Adapter is the one interface every exporter implements: read every
// issue, create one, update one. There is no method that reads or
// writes Seed.
type Adapter interface {
	Name() string
	List(ctx context.Context) ([]Issue, error)
	Create(ctx context.Context, d Desired) (Issue, error)
	Update(ctx context.Context, id string, d Desired, foreign []string) error
}

// Applied reports what Apply did.
type Applied struct {
	Exporter string   `json:"exporter"`
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
}

// ApplyError is an adapter failure: the exporter and the action that
// failed, so the operator knows exactly what the forge holds; nothing
// on the Seed side changed, since nothing here can change it.
type ApplyError struct {
	Exporter string
	Action   Action
	Err      error
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("%s: %s %s failed: %v", e.Exporter, e.Action.Kind, e.Action.Subject, e.Err)
}

func (e *ApplyError) Unwrap() error { return e.Err }

// Apply runs the plan's actions in order, stopping at the first
// failure. Applying a plan twice is a no-op the second time, because
// the second plan is empty.
func Apply(ctx context.Context, a Adapter, plan Plan) (Applied, error) {
	out := Applied{Exporter: a.Name(), Created: []string{}, Updated: []string{}}
	for _, act := range plan.Actions {
		switch act.Kind {
		case ActionCreate:
			if _, err := a.Create(ctx, act.Issue); err != nil {
				return out, &ApplyError{Exporter: a.Name(), Action: act, Err: err}
			}
			out.Created = append(out.Created, act.Subject)
		case ActionUpdate:
			if err := a.Update(ctx, act.ID, act.Issue, act.Foreign); err != nil {
				return out, &ApplyError{Exporter: a.Name(), Action: act, Err: err}
			}
			out.Updated = append(out.Updated, act.Subject)
		default:
			return out, &ApplyError{Exporter: a.Name(), Action: act, Err: fmt.Errorf("unknown action kind %q", act.Kind)}
		}
	}
	return out, nil
}

// Config is what an exporter is opened with: the forge's repository
// and, for a forge, the API base and the environment variable its
// token is read from; for the snapshot, the file it keeps.
type Config struct {
	Owner   string
	Repo    string
	BaseURL string
	// TokenEnv names the environment variable the credential is read
	// from; the credential itself is never a field, a flag, an output
	// or a fixture.
	TokenEnv string
	Snapshot string
}

// Constructor opens an adapter from a config.
type Constructor func(Config) (Adapter, error)

// registry is sealed: the three reference exporters are registered
// here and nowhere else, and `seed-mirror` opens adapters through
// Open alone, so an exporter the suite never ran cannot be used.
var registry = map[string]Constructor{
	"github":   newGitHub,
	"forgejo":  newForgejo,
	"snapshot": newSnapshot,
}

// Names lists the registered exporters, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Open constructs a registered exporter.
func Open(name string, cfg Config) (Adapter, error) {
	ctor, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("no exporter named %q: %s", name, strings.Join(Names(), ", "))
	}
	return ctor(cfg)
}
