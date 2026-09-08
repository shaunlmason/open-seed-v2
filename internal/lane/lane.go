// Package lane is the role surface: six lane definitions, each a
// manifest plus an ordered list of prose fragments, resolved by
// concatenation and CHECKED against the tables that already exist
// (SEED-NEXT.md §II.11 and III.J; plans/os-cf1c9688.md).
//
// The point is not that Seed has role documents. It is that their
// claims are decidable. A role file is prose, and nothing checks prose:
// v1 has four role documents and no validator, which is survivable
// where a human reads them and not survivable for a promotion criterion
// that asserts a property OF THE LANE ("runs entirely through Seed
// verbs, orienting from one position-stamped read") that only the file's
// author ever verified.
//
// So the four obligations docs/build-plan.md Phase 9 item 1 binds
// are DECLARED FIELDS, not paragraphs, and every field is checked
// against an authority elsewhere in the tree: capabilities against
// internal/keyring, the acts against internal/loopverb, and the
// capability a verb accepts against keyring.AcceptedCapabilities. This
// package holds no policy of its own and invents no legality; a
// hand-written list of capability or verb names here would be the bug
// it exists to prevent.
package lane

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
	"github.com/shaunlmason/open-seed-v2/internal/topology"
)

// Manifest is one lane's declaration. The four obligation fields are
// named for what they oblige rather than for what they contain, so a
// fragment author reading the schema meets the obligation, not a
// datatype.
type Manifest struct {
	// Lane is the manifest's name: one of the charter's six work-loop
	// lanes (§II.11) or one of its non-loop roles. The JSON key is
	// `lane` for both kinds, because the machinery is the same; Kind
	// says which the name belongs to.
	Lane string `json:"lane"`
	// Kind is REQUIRED and one of KindLane or KindRole
	// (plans/os-d6a52784.md D2). A lane is one of the charter's six
	// and no other name may claim to be; a role is a part the charter
	// defines outside the work loop (the supervisor, §II.9; governed
	// observers, §8). Required rather than defaulted, so the six say
	// what they are in their own files and the enumeration is a
	// property of the manifests rather than of a sentence elsewhere.
	Kind string `json:"kind"`
	// Summary is the one-line statement of what the lane is for.
	Summary string `json:"summary"`
	// Grants are the capabilities the lane's key holds.
	Grants []string `json:"grants"`
	// OrientsFrom is the SINGLE position-stamped read the lane wakes
	// on, written as the command a lane runs.
	OrientsFrom string `json:"orients_from"`
	// ActsThrough names the loop acts the lane performs, in
	// internal/loopverb's spelling. A lane that acts through the raw
	// append seam declares nothing here and is refused.
	ActsThrough []string `json:"acts_through"`
	// LivenessFrom names the lane's own work steps whose execution
	// emits observations. Every entry must appear in ActsThrough:
	// liveness rides the work, and a step that is not work is a
	// heartbeat by another name.
	LivenessFrom []string `json:"liveness_from"`
	// Inbox is the lane's one-inbox declaration: what wakes it, and
	// what convinces it.
	Inbox string `json:"inbox"`
	// Fragments is the ORDERED list of prose files composing the role,
	// relative to the lanes directory. Order is declared, never
	// inferred from a directory listing, which would change under a
	// rename.
	Fragments []string `json:"fragments"`
}

// The two manifest kinds.
const (
	KindLane = "lane"
	KindRole = "role"
)

// CharterLanes is the charter's closed enumeration, §II.11: "Six
// lanes", numbered one through six. It is the ONE hand-written name
// list this package holds, and it is the charter's rather than this
// package's: Validate refuses a seventh lane by name, because a
// directory anyone can drop a file into would otherwise enforce a
// normative enumeration with nothing at all (plans/os-d6a52784.md D3).
func CharterLanes() []string {
	return []string{"dispatcher", "planner", "implementer", "verifier", "curator", "maintenance"}
}

// Finding is one validation failure, naming the lane, the field, and
// what refused it. A finding that does not name its authority is a
// finding a reader has to take on trust.
type Finding struct {
	Lane    string `json:"lane"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s", f.Lane, f.Field, f.Message)
}

// FragmentDir is the subdirectory of prose fragments.
const FragmentDir = "fragments"

// dispatcherAllowlist is the dispatcher's permitted grant set, stated
// POSITIVELY (plans/os-cf1c9688.md D5). A blocklist would have to be
// extended every time a capability is added, and one nobody thought to
// exclude would be admitted by default: that is how `operator` — the
// strongest grant in the keyring — passed the first draft's
// "no authoring, verdict or sealing" check. The dispatcher reads the
// most hostile input in the system, so its posture is the one that
// must be stated rather than inferred.
var dispatcherAllowlist = []string{keyring.CapDispatch}

// SituationFlag is one flag of the orienting read, with what the
// surface demands of it. Demand is the half that matters: a
// declaration naming only real flags can still be a command that exits
// 64, and a validator that passes it has certified prose (review
// finding on #188, reproduced — all six shipped manifests declared a
// read that could not run).
//
// Two kinds of demand exist. Required is unconditional. Posture marks
// membership of the `--ledger` xor `--remote` pair: exactly one must be
// named, because a read with neither has nothing to derive from and a
// read with both has no answer to which view it stamped.
type SituationFlag struct {
	Name     string
	Required bool
	Posture  bool
}

// situationFlags is the read surface orients_from is checked against.
// It lives here rather than in cmd/seed because package main is not
// importable; cmd/seed carries a drill asserting its own flag set and
// its demands match this exactly, so the two cannot drift without a
// red test.
//
// The posture pair arrived with the loop (plans/os-abb206c8.md D3):
// `claim take` is remote-only, so a lane that could only orient locally
// would read one view and act against another.
func situationFlags() []SituationFlag {
	return []SituationFlag{
		{Name: "ledger", Posture: true},
		{Name: "remote", Posture: true},
		{Name: "ref"},
		{Name: "state"},
		{Name: "supported"},
		{Name: "key"},
		{Name: "subject"},
		{Name: "since"},
		// The deployment declaration the remote path acts under
		// (plans/os-5c8a312c.md D3): optional, because its absence is
		// today's behavior and a lane names it only where the
		// deployment has one.
		{Name: "config"},
	}
}

// SituationFlags exposes that set for the CLI's agreement drill.
func SituationFlags() []SituationFlag { return situationFlags() }

// Load reads every manifest in dir, in lane-name order.
func Load(dir string) ([]Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var m Manifest
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if m.Lane == "" {
			return nil, fmt.Errorf("%s: manifest names no lane", e.Name())
		}
		if m.Kind == "" {
			return nil, fmt.Errorf("%s: manifest declares no kind: every manifest says whether it is one of "+
				"the charter's six lanes (%q) or a role the charter defines outside the work loop (%q), "+
				"and a default here would let the six acquire a claim nobody wrote", e.Name(), KindLane, KindRole)
		}
		if want := m.Lane + ".json"; e.Name() != want {
			return nil, fmt.Errorf("%s: lane %q must live in %s", e.Name(), m.Lane, want)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lane < out[j].Lane })
	return out, nil
}

// Resolve concatenates the manifest's fragments IN DECLARED ORDER. It
// is a pure function of the files on disk: no position to stamp, no
// rebuild semantics, nothing written back. A resolved role written to
// disk would be a second copy that can go stale, which is the failure
// the ordered list exists to prevent.
func Resolve(dir string, m Manifest) (string, error) {
	var b strings.Builder
	for _, f := range m.Fragments {
		body, err := os.ReadFile(filepath.Join(dir, FragmentDir, f))
		if err != nil {
			return "", err
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.Write(body)
	}
	return b.String(), nil
}

// Validate is the PRODUCTION validation of a role set: every
// per-manifest check (ValidateEach), plus the property only the whole
// set can answer, that the charter's six lanes are all present, each
// exactly once. This is what `seed lane validate` runs, and it is the
// path a deployment's --lanes directory goes through, so a directory
// that omits planner.json is refused here rather than certified with
// "lanes: 5" (review finding on #212: the first draft kept completeness
// in the shipped-set unit test, which protected the test and not the
// directory an operator actually supplies).
func Validate(dir string, ms []Manifest) []Finding {
	out := ValidateEach(dir, ms)
	seen := map[string]int{}
	for _, m := range ms {
		if m.Kind == KindLane {
			seen[m.Lane]++
		}
	}
	for _, name := range CharterLanes() {
		switch seen[name] {
		case 0:
			out = append(out, Finding{Lane: name, Field: "kind", Message: fmt.Sprintf(
				"one of the charter's six lanes has no manifest of kind lane (SEED-NEXT.md §II.11: %s)",
				strings.Join(CharterLanes(), ", "))})
		case 1:
		default:
			out = append(out, Finding{Lane: name, Field: "kind", Message: fmt.Sprintf(
				"%d manifests claim to be this lane; the charter's six are one manifest each", seen[name])})
		}
	}
	sortFindings(out)
	return out
}

// ValidateEach runs every PER-MANIFEST check over the set and returns
// the findings, in a stable order. It is the half a drill can exercise
// against a single fixture manifest: a fixture validating one manifest
// in isolation is not a deployment missing five lanes, and the
// completeness rule that would say so lives in Validate.
func ValidateEach(dir string, ms []Manifest) []Finding {
	var out []Finding
	add := func(lane, field, msg string) {
		out = append(out, Finding{Lane: lane, Field: field, Message: msg})
	}
	used := map[string]bool{}

	// The enumeration's CLOSED half, as a property of each manifest
	// (plans/os-d6a52784.md D3): no name outside the charter's six may
	// be a lane. A role is unconstrained in name, because §II.9 and §8
	// enumerate nothing. The COMPLETE half is Validate's.
	charter := map[string]bool{}
	for _, name := range CharterLanes() {
		charter[name] = true
	}
	for _, m := range ms {
		switch m.Kind {
		case KindLane:
			if !charter[m.Lane] {
				add(m.Lane, "kind", fmt.Sprintf("claims to be a lane, and the charter's six are closed "+
					"(SEED-NEXT.md §II.11: %s): a part outside the work loop is kind %q",
					strings.Join(CharterLanes(), ", "), KindRole))
			}
		case KindRole:
		default:
			add(m.Lane, "kind", fmt.Sprintf("%q is not a manifest kind: %q or %q", m.Kind, KindLane, KindRole))
		}
	}

	for _, m := range ms {
		// Grants: real capabilities, asked of the keyring.
		for _, g := range m.Grants {
			if !keyring.Known(g) {
				add(m.Lane, "grants", fmt.Sprintf("%q is not a capability in the keyring vocabulary (%s)",
					g, strings.Join(keyring.Capabilities(), ", ")))
			}
		}

		// The orienting read: one position-stamped read, named as a
		// command, with flags the surface actually takes.
		checkOrientsFrom(m, add)

		// The acts: real loop acts, and grants that INTERSECT what
		// each act's ledger verb accepts. Intersection, not
		// containment: AcceptedCapabilities is an OR-set consumed by
		// HasAnyCapability, so requiring every accepted capability
		// would hand `operator` to every lane that claims or spends
		// and dissolve the separation this check exists to protect.
		runsLoop := false
		for _, name := range m.ActsThrough {
			act, ok := loopverb.ByName(name)
			verb := act.Verb
			if !ok {
				// The lane acts outside the worker loop
				// (plans/os-48df10a2.md): the dispatcher's answer to an
				// inbound request is its one declared act, a fact the
				// loop never appends, held to the same grant
				// intersection as a loop act.
				lv, lane := laneActs[name]
				if !lane {
					add(m.Lane, "acts_through", fmt.Sprintf("%q is not a loop act (%s) nor a lane act (%s)",
						name, strings.Join(loopverb.Names(), ", "), strings.Join(laneActNames(), ", ")))
					continue
				}
				verb = lv
			} else {
				runsLoop = true
			}
			accepted := keyring.AcceptedCapabilities(verb)
			if len(accepted) == 0 {
				continue
			}
			if !intersects(m.Grants, accepted) {
				add(m.Lane, "acts_through", fmt.Sprintf(
					"%s appends %s, which admits for any of {%s}, and this lane grants {%s}",
					name, verb, strings.Join(accepted, ", "), strings.Join(m.Grants, ", ")))
			}
		}

		// Liveness rides the work, checked against whether this lane
		// runs a loop at all. Four of the charter's six do not: a
		// verifier acts through verdict.rendered and a dispatcher
		// through intent.filed, neither of which is a loop act, so
		// requiring loop acts of every lane would force four
		// manifests to claim work they never do.
		//
		// The obligation is therefore CONDITIONAL but not dodgeable.
		// A lane that runs a loop must say where its liveness comes
		// from, and a lane cannot escape that by declaring no acts:
		// holding the claim capability means it claims, and claiming
		// IS a loop act, so the grant it already declares decides
		// whether the obligation applies.
		if contains(m.Grants, keyring.CapClaim) && !runsLoop {
			add(m.Lane, "acts_through", "grants claim but declares no acts: claiming is a loop act, so a lane "+
				"holding the claim capability acts through the loop verbs and must say which")
		}
		switch {
		case runsLoop && len(m.LivenessFrom) == 0:
			add(m.Lane, "liveness_from", "declares no liveness source: observations ride the loop's own steps, "+
				"and an empty declaration would satisfy the subset rule without meaning anything")
		case !runsLoop && len(m.LivenessFrom) > 0:
			add(m.Lane, "liveness_from", "names liveness sources but no acts: liveness rides the work, so a "+
				"lane that performs no loop act has no work for it to ride")
		}
		for _, step := range m.LivenessFrom {
			if !contains(m.ActsThrough, step) {
				add(m.Lane, "liveness_from", fmt.Sprintf(
					"%q is not among this lane's acts: liveness rides the work, so a liveness source "+
						"that is not a work step is a heartbeat by another name", step))
			}
		}

		if strings.TrimSpace(m.Inbox) == "" {
			add(m.Lane, "inbox", "declares no inbox: push channels wake, position-stamped reads convince, "+
				"and a lane that does not say so has not adopted the doctrine")
		}

		// The dispatcher's posture, as an allowlist.
		if m.Lane == "dispatcher" {
			checkDispatcher(m, add)
		}

		// Fragments: declared, ordered, present.
		if len(m.Fragments) == 0 {
			add(m.Lane, "fragments", "composes from no fragments: a role is its ordered fragments")
		}
		for _, f := range m.Fragments {
			path := filepath.Join(dir, FragmentDir, f)
			body, err := os.ReadFile(path)
			if err != nil {
				add(m.Lane, "fragments", fmt.Sprintf("%s: %v", f, err))
				continue
			}
			used[f] = true
			// The belt to D4's braces: a fragment could instruct an
			// agent to heartbeat without declaring it. This IS a
			// spelling rule and is only ever the second line of
			// defence; the property check above is the argument.
			if line, found := bareHeartbeat(string(body)); found {
				add(m.Lane, "fragments", fmt.Sprintf(
					"%s instructs a bare liveness emit (%q): liveness rides the loop's own steps, "+
						"and the vocabulary carries no verb whose only purpose is to report it", f, line))
			}
		}
	}

	out = append(out, orphanFindings(dir, used)...)
	sortFindings(out)
	return out
}

func sortFindings(out []Finding) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Message < out[j].Message
	})
}

func checkOrientsFrom(m Manifest, add func(lane, field, msg string)) {
	read := strings.TrimSpace(m.OrientsFrom)
	if read == "" {
		add(m.Lane, "orients_from", "names no orienting read: a lane orients from ONE position-stamped read, "+
			"and promotion criterion 1 is a property of this file rather than of whoever writes the agent")
		return
	}
	if !strings.HasPrefix(read, "seed situation") {
		add(m.Lane, "orients_from", fmt.Sprintf(
			"%q is not the situation read: `seed situation` is the one position-stamped read a lane wakes on", read))
		return
	}
	known := map[string]bool{}
	var names []string
	for _, f := range situationFlags() {
		known[f.Name] = true
		names = append(names, f.Name)
	}
	cited := map[string]bool{}
	for _, tok := range strings.Fields(read) {
		if !strings.HasPrefix(tok, "--") {
			continue
		}
		name := strings.TrimPrefix(tok, "--")
		if i := strings.Index(name, "="); i >= 0 {
			name = name[:i]
		}
		cited[name] = true
		if !known[name] {
			add(m.Lane, "orients_from", fmt.Sprintf("--%s is not a flag `seed situation` takes (%s)",
				name, "--"+strings.Join(names, ", --")))
		}
	}
	// Naming only real flags is not enough: a command missing a
	// REQUIRED one exits 64 without ever reaching the ledger, and a
	// lane following it would fail on its first act. Checking the
	// surface's demands is what makes the declaration executable
	// rather than merely well-spelled.
	for _, f := range situationFlags() {
		if f.Required && !cited[f.Name] {
			add(m.Lane, "orients_from", fmt.Sprintf(
				"omits --%s, which `seed situation` requires: as written this read exits 64 and the lane "+
					"never orients", f.Name))
		}
	}
	// The posture pair is an exclusive-or, so both arms fail for the
	// same reason the surface refuses them: a read naming neither has
	// no ledger to derive from, and one naming both cannot say which
	// view its position stamps.
	var posture []string
	var namedPosture []string
	for _, f := range situationFlags() {
		if !f.Posture {
			continue
		}
		posture = append(posture, "--"+f.Name)
		if cited[f.Name] {
			namedPosture = append(namedPosture, "--"+f.Name)
		}
	}
	switch len(namedPosture) {
	case 1:
	case 0:
		add(m.Lane, "orients_from", fmt.Sprintf(
			"names no posture: `seed situation` takes exactly one of %s, and as written this read exits 64 "+
				"and the lane never orients", strings.Join(posture, " or ")))
	default:
		add(m.Lane, "orients_from", fmt.Sprintf(
			"names %s: `seed situation` takes exactly one of %s, since a read citing both cannot say which "+
				"view its position stamps", strings.Join(namedPosture, " and "), strings.Join(posture, " or ")))
	}
}

func checkDispatcher(m Manifest, add func(lane, field, msg string)) {
	allowed := map[string]bool{}
	for _, c := range dispatcherAllowlist {
		allowed[c] = true
	}
	for _, g := range m.Grants {
		if !allowed[g] {
			add(m.Lane, "grants", fmt.Sprintf(
				"%q is outside the dispatcher's allowlist {%s}: it touches the most untrusted text in the "+
					"system and runs with least standing capability (SEED-NEXT.md §II.11)",
				g, strings.Join(dispatcherAllowlist, ", ")))
		}
	}
	for _, want := range dispatcherAllowlist {
		if !contains(m.Grants, want) {
			add(m.Lane, "grants", fmt.Sprintf("the dispatcher's allowlist names %q, which this manifest does not grant", want))
		}
	}
}

// orphanFindings names fragments no manifest composes: an unreferenced
// fragment is prose nobody reads, and the ordered lists are the only
// thing that makes a fragment part of a role.
func orphanFindings(dir string, used map[string]bool) []Finding {
	var out []Finding
	root := filepath.Join(dir, FragmentDir)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel) // manifests name fragments with slashes on every platform
		if rerr != nil || used[rel] {
			return nil
		}
		out = append(out, Finding{Lane: "-", Field: "fragments",
			Message: fmt.Sprintf("%s is composed by no lane: an unreferenced fragment is prose nobody reads", rel)})
		return nil
	})
	return out
}

// bareHeartbeat finds a fragment line instructing a standalone
// observation emit.
func bareHeartbeat(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "seed obs emit") {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}

// laneActs is the closed set of acts a lane declares that are not
// loop acts: the CLI phrase and the ledger verb it appends
// (spec/requests.md). A lane that declares one alone runs no
// worker loop, so the liveness obligation does not apply to it.
var laneActs = map[string]string{
	"request answer":    "request.answered",
	"topology depend":   topology.DependVerb,
	"topology undepend": topology.UndependVerb,
	"topology parent":   topology.ParentVerb,
	"topology align":    topology.AlignVerb,
}

func laneActNames() []string {
	out := make([]string, 0, len(laneActs))
	for name := range laneActs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
