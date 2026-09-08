package lane

// The lane validation drills (plans/os-cf1c9688.md): each check fails
// with its own finding, and the shipped set validates clean. The point
// of every case below is that the finding comes from an authority
// elsewhere in the tree rather than from a list this package keeps.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/loopverb"
)

// good is a manifest that passes every check, for mutation by each
// case: a drill that builds its own broken manifest from scratch can
// pass for the wrong reason when a second field is broken too.
func good() Manifest {
	return Manifest{
		Lane:         "implementer",
		Kind:         KindLane,
		Summary:      "takes a card to a PR",
		Grants:       []string{keyring.CapClaim},
		OrientsFrom:  "seed situation --remote <repo> --key <key> --since <position>",
		ActsThrough:  []string{"claim take", "budget settle"},
		LivenessFrom: []string{"budget settle"},
		Inbox:        "wakes on push, convinces on the read",
		Fragments:    []string{"a.md"},
	}
}

// fixture writes a lanes directory holding one manifest and the
// fragments it names.
func fixture(t *testing.T, m Manifest, fragments map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, FragmentDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range fragments {
		p := filepath.Join(dir, FragmentDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, m.Lane+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func findingFields(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Field)
	}
	return out
}

// conformance: III.J — the declarations are checked, and each failure
// names the field it failed on.
func TestEachCheckHasItsOwnFinding(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*Manifest)
		field  string
		says   string
	}{
		"a grant outside the keyring vocabulary": {
			mutate: func(m *Manifest) { m.Grants = []string{"wizard"} },
			field:  "grants",
			says:   "not a capability",
		},
		"an act that is not a loop act": {
			mutate: func(m *Manifest) { m.ActsThrough = []string{"claim yeet"}; m.LivenessFrom = []string{"claim yeet"} },
			field:  "acts_through",
			says:   "not a loop act",
		},
		"grants that do not intersect what the act accepts": {
			mutate: func(m *Manifest) { m.Grants = []string{keyring.CapObserver} },
			field:  "acts_through",
			says:   "admits for any of",
		},
		"an empty liveness declaration": {
			mutate: func(m *Manifest) { m.LivenessFrom = nil },
			field:  "liveness_from",
			says:   "declares no liveness source",
		},
		"a liveness source that is not a work step": {
			mutate: func(m *Manifest) { m.LivenessFrom = []string{"claim park"} },
			field:  "liveness_from",
			says:   "heartbeat by another name",
		},
		"no orienting read": {
			mutate: func(m *Manifest) { m.OrientsFrom = "" },
			field:  "orients_from",
			says:   "names no orienting read",
		},
		"an orienting read that is not the situation read": {
			mutate: func(m *Manifest) { m.OrientsFrom = "seed project rebuild" },
			field:  "orients_from",
			says:   "is not the situation read",
		},
		"a flag the situation read does not take": {
			mutate: func(m *Manifest) { m.OrientsFrom = "seed situation --remote <repo> --everything" },
			field:  "orients_from",
			says:   "is not a flag",
		},
		"an orienting read naming no posture": {
			// Naming only real flags is not enough: this command
			// exits 64 without reaching the ledger, so the lane never
			// orients at all.
			mutate: func(m *Manifest) { m.OrientsFrom = "seed situation --key <key> --since <position>" },
			field:  "orients_from",
			says:   "names no posture",
		},
		"an orienting read naming both postures": {
			// The other arm of the same exclusive-or, and the one a
			// required-flag model could not express: a read citing
			// both cannot say which view its position stamps.
			mutate: func(m *Manifest) {
				m.OrientsFrom = "seed situation --ledger <dir> --remote <repo> --key <key>"
			},
			field: "orients_from",
			says:  "names --ledger and --remote",
		},
		"no inbox declaration": {
			mutate: func(m *Manifest) { m.Inbox = "" },
			field:  "inbox",
			says:   "declares no inbox",
		},
		"a missing fragment": {
			mutate: func(m *Manifest) { m.Fragments = []string{"absent.md"} },
			field:  "fragments",
			says:   "absent.md",
		},
		"a lane granted claim that declares no acts": {
			mutate: func(m *Manifest) { m.ActsThrough = nil; m.LivenessFrom = nil },
			field:  "acts_through",
			says:   "claiming is a loop act",
		},
	} {
		m := good()
		tc.mutate(&m)
		dir := fixture(t, m, map[string]string{"a.md": "prose\n"})
		got := ValidateEach(dir, []Manifest{m})
		found := false
		for _, f := range got {
			if f.Field == tc.field && strings.Contains(f.Message, tc.says) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want a %s finding saying %q, got %v (%v)", name, tc.field, tc.says, findingFields(got), got)
		}
	}
}

// conformance: the dispatcher's posture is an ALLOWLIST, so a
// capability nobody thought to exclude fails by default. operator is
// the case that motivated it: it is not an authoring, verdict or
// sealing grant, so the first draft's blocklist would have admitted
// the strongest capability in the keyring on the lane that reads the
// most hostile input.
func TestDispatcherPostureIsAnAllowlist(t *testing.T) {
	base := func() Manifest {
		m := good()
		m.Lane = "dispatcher"
		m.Grants = []string{keyring.CapDispatch}
		m.ActsThrough = nil
		m.LivenessFrom = nil
		return m
	}
	clean := base()
	dir := fixture(t, clean, map[string]string{"a.md": "prose\n"})
	if got := ValidateEach(dir, []Manifest{clean}); len(got) != 0 {
		t.Fatalf("the allowlisted dispatcher validates clean: %v", got)
	}

	for _, extra := range []string{keyring.CapOperator, keyring.CapClaim, keyring.CapVerdict, keyring.CapMaintenance} {
		m := base()
		m.Grants = append(m.Grants, extra)
		dir := fixture(t, m, map[string]string{"a.md": "prose\n"})
		found := false
		for _, f := range ValidateEach(dir, []Manifest{m}) {
			if f.Field == "grants" && strings.Contains(f.Message, extra) && strings.Contains(f.Message, "allowlist") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q on the dispatcher must fail by name against the allowlist", extra)
		}
	}

	// And the allowlist is required, not merely permitted.
	m := base()
	m.Grants = nil
	dir = fixture(t, m, map[string]string{"a.md": "prose\n"})
	found := false
	for _, f := range ValidateEach(dir, []Manifest{m}) {
		if f.Field == "grants" && strings.Contains(f.Message, "does not grant") {
			found = true
		}
	}
	if !found {
		t.Error("a dispatcher granting nothing must fail: the allowlist names what it needs")
	}
}

// conformance: the accepted-capability set is an OR-set, so holding
// ONE alternative passes. A containment check here would force
// operator onto every lane that claims or spends, which is the posture
// the charter forbids.
func TestAcceptedCapabilitiesAreAlternatives(t *testing.T) {
	act, ok := loopverb.ByName("budget settle")
	if !ok {
		t.Fatal("budget settle is a loop act")
	}
	accepted := keyring.AcceptedCapabilities(act.Verb)
	if len(accepted) < 2 {
		t.Fatalf("this drill needs a verb with alternatives, %s accepts %v", act.Verb, accepted)
	}
	for _, one := range accepted {
		m := good()
		m.Grants = []string{one}
		m.ActsThrough = []string{"budget settle"}
		m.LivenessFrom = []string{"budget settle"}
		dir := fixture(t, m, map[string]string{"a.md": "prose\n"})
		for _, f := range ValidateEach(dir, []Manifest{m}) {
			if f.Field == "acts_through" {
				t.Errorf("holding %q alone must satisfy %s, which accepts %v: %s", one, act.Verb, accepted, f.Message)
			}
		}
	}
}

// conformance: resolution is ordered and byte-stable, and the order is
// the manifest's rather than the directory's.
func TestResolveIsOrderedAndStable(t *testing.T) {
	m := good()
	m.Fragments = []string{"b.md", "a.md"}
	dir := fixture(t, m, map[string]string{"a.md": "alpha\n", "b.md": "beta\n"})
	first, err := Resolve(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "beta") {
		t.Fatalf("fragments resolve in DECLARED order, not directory order: %q", first)
	}
	second, err := Resolve(dir, m)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("resolution must be byte-stable across runs")
	}
}

// conformance: an unreferenced fragment is prose nobody reads, and the
// ordered lists are the only thing that makes a fragment part of a
// role.
func TestOrphanFragmentIsAFinding(t *testing.T) {
	m := good()
	dir := fixture(t, m, map[string]string{"a.md": "prose\n", "stray.md": "unreferenced\n"})
	found := false
	for _, f := range ValidateEach(dir, []Manifest{m}) {
		if f.Field == "fragments" && strings.Contains(f.Message, "stray.md") {
			found = true
		}
	}
	if !found {
		t.Error("an orphaned fragment must be named")
	}
}

// conformance: the prose sweep is the belt to the property check's
// braces — a fragment could tell an agent to heartbeat without
// declaring it.
func TestFragmentInstructingABareHeartbeatIsAFinding(t *testing.T) {
	m := good()
	dir := fixture(t, m, map[string]string{"a.md": "Every 30 seconds run `seed obs emit --count 1`.\n"})
	found := false
	for _, f := range ValidateEach(dir, []Manifest{m}) {
		if f.Field == "fragments" && strings.Contains(f.Message, "bare liveness emit") {
			found = true
		}
	}
	if !found {
		t.Error("a fragment instructing a bare emit must be named")
	}
}

// conformance: the manifest is strict data — an unknown field is a
// typo that would otherwise be silently ignored, and the filename
// carries the lane's identity.
func TestManifestsAreStrict(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, FragmentDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "implementer.json"),
		[]byte(`{"lane": "implementer", "kind": "lane", "grants": [], "acts_thru": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("an unknown field must refuse rather than be ignored")
	}
	if err := os.WriteFile(filepath.Join(dir, "implementer.json"),
		[]byte(`{"lane": "planner", "kind": "lane"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "must live in") {
		t.Fatalf("the filename carries the lane's identity: %v", err)
	}
}

// conformance: plans/os-d6a52784.md D2, AC5 — kind is REQUIRED, never
// defaulted. A default would let the six existing manifests keep
// passing while silently acquiring a claim nobody wrote.
func TestKindIsRequired(t *testing.T) {
	m := good()
	m.Kind = ""
	dir := fixture(t, m, map[string]string{"a.md": "role text"})
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "declares no kind") {
		t.Fatalf("a manifest with no kind is refused at load, not defaulted: %v", err)
	}
	m.Kind = "squad"
	dir = fixture(t, m, map[string]string{"a.md": "role text"})
	ms, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fs := ValidateEach(dir, ms); len(fs) != 1 || fs[0].Field != "kind" {
		t.Fatalf("an unknown kind is a finding on the kind field: %v", fs)
	}
}

// conformance: D3, AC4 — the charter's six are CLOSED. A seventh lane
// is refused BY NAME with a message citing §II.11; a role of any name
// is accepted, because §II.9 and §8 enumerate nothing.
func TestTheCharterSixAreClosed(t *testing.T) {
	seventh := good()
	seventh.Lane = "auditor"
	dir := fixture(t, seventh, map[string]string{"a.md": "role text"})
	ms, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	fs := ValidateEach(dir, ms)
	if len(fs) != 1 || fs[0].Field != "kind" || !strings.Contains(fs[0].Message, "II.11") {
		t.Fatalf("a seventh lane is refused by name, citing the charter: %v", fs)
	}
	role := good()
	role.Lane = "auditor"
	role.Kind = KindRole
	role.Grants = []string{keyring.CapObserver}
	role.ActsThrough, role.LivenessFrom = nil, nil
	dir = fixture(t, role, map[string]string{"a.md": "role text"})
	ms, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fs := ValidateEach(dir, ms); len(fs) != 0 {
		t.Fatalf("a role may take any name: %v", fs)
	}
	// And every charter name IS accepted as a lane, so the list is
	// the charter's and not shorter.
	for _, name := range CharterLanes() {
		m := good()
		m.Lane = name
		if name == "dispatcher" {
			m.Grants = []string{keyring.CapDispatch}
			m.ActsThrough, m.LivenessFrom = nil, nil
		}
		dir := fixture(t, m, map[string]string{"a.md": "role text"})
		ms, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range ValidateEach(dir, ms) {
			if f.Field == "kind" {
				t.Errorf("%s is one of the charter's six and must be accepted as a lane: %v", name, f)
			}
		}
	}
}

// conformance: D4, AC6 — the acts_through obligation turns on CapClaim,
// not on kind. A role holding no claim declares no acts and is clean;
// a role that DID hold claim would be obliged like a lane, so the rule
// is shown to be about the grant rather than about the label.
func TestActsThroughTurnsOnClaimNotKind(t *testing.T) {
	role := good()
	role.Lane, role.Kind = "supervisor", KindRole
	role.Grants = []string{keyring.CapSupervise}
	role.ActsThrough, role.LivenessFrom = nil, nil
	dir := fixture(t, role, map[string]string{"a.md": "role text"})
	ms, _ := Load(dir)
	if fs := ValidateEach(dir, ms); len(fs) != 0 {
		t.Fatalf("a role holding no claim owes no acts: %v", fs)
	}
	role.Grants = []string{keyring.CapClaim}
	dir = fixture(t, role, map[string]string{"a.md": "role text"})
	ms, _ = Load(dir)
	found := false
	for _, f := range ValidateEach(dir, ms) {
		if f.Field == "acts_through" && strings.Contains(f.Message, "grants claim but declares no acts") {
			found = true
		}
	}
	if !found {
		t.Fatal("a ROLE holding claim is obliged exactly like a lane: the rule is about the grant, not the kind")
	}
}

// conformance: D6, AC2, AC3 — every capability the contract path
// requires is granted by some shipped manifest. This is the drill whose
// absence let three capabilities go ungranted through an entire phase.
//
// The verbs come from the CAPABILITY TABLE'S OWN SOURCE, not a list
// here: a hand-listed drill cannot notice a verb it was never told
// about, which is exactly how the gap survived. `operator` is excluded
// from satisfying the check because it satisfies everything by
// construction, so counting it would let the maintenance lane paper
// over every future gap: the shape of the bug rather than the fix.
func TestEveryRequiredCapabilityIsGrantedBySomeManifest(t *testing.T) {
	dir := filepath.Join("..", "..", "lanes")
	findings := requiredCapabilityGaps(t, dir)
	if len(findings) != 0 {
		t.Fatalf("capabilities the contract path requires and no shipped manifest grants:\n  %s",
			strings.Join(findings, "\n  "))
	}
}

// requiredCapabilityGaps is the derivation, exposed so the drill can be
// run against a tree other than the shipped one (AC2: it must fail on
// main's pre-fix lanes directory, naming all three gaps).
func requiredCapabilityGaps(t *testing.T, dir string) []string {
	t.Helper()
	ms, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	granted := map[string]bool{}
	for _, m := range ms {
		for _, g := range m.Grants {
			granted[g] = true
		}
	}
	var out []string
	for _, verb := range capabilityTableVerbs(t) {
		accepted := keyring.AcceptedCapabilities(verb)
		var need []string
		for _, c := range accepted {
			if c != keyring.CapOperator {
				need = append(need, c)
			}
		}
		if len(need) == 0 {
			continue // operator-only by design: a human gate, not a gap
		}
		ok := false
		for _, c := range need {
			if granted[c] {
				ok = true
			}
		}
		if !ok {
			out = append(out, fmt.Sprintf("%s needs one of {%s} and nothing grants any (operator excluded)",
				verb, strings.Join(need, ", ")))
		}
	}
	return out
}

// capabilityTableVerbs reads the verb literals out of
// keyring.AcceptedCapabilities' own source. Verbs the table names by
// constant (halt, upgrade, checkpoint) are operator- or
// maintenance-gated system acts rather than contract-path ones and are
// not read here; everything on the contract path is a literal case.
func capabilityTableVerbs(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "keyring", "keyring.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "func AcceptedCapabilities(")
	if i < 0 {
		t.Fatal("keyring.AcceptedCapabilities is no longer where this drill reads it")
	}
	body := src[i:]
	if j := strings.Index(body, "\n}\n"); j >= 0 {
		body = body[:j]
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range regexp.MustCompile(`"([a-z]+\.[a-z.]+)"`).FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	if len(out) < 20 {
		t.Fatalf("the capability table read too few verbs (%d): the scan is broken, and a scan that finds "+
			"nothing agrees with everything", len(out))
	}
	return out
}

// conformance: plans/os-d6a52784.md D3, the COMPLETE half — the
// production path refuses a set missing a charter lane, so a --lanes
// directory that omits planner.json is refused rather than certified
// with five (review finding on #212). ValidateEach is what the fixture
// drills above use precisely because they are not deployments.
func TestValidateRefusesAnIncompleteSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, FragmentDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FragmentDir, "a.md"), []byte("role text"), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(name string) {
		m := good()
		m.Lane = name
		if name == "dispatcher" {
			m.Grants = []string{keyring.CapDispatch}
		}
		if name != "implementer" && name != "planner" {
			m.Grants = []string{}
			if name == "dispatcher" {
				m.Grants = []string{keyring.CapDispatch}
			}
			m.ActsThrough, m.LivenessFrom = nil, nil
		}
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range CharterLanes() {
		if name != "planner" {
			write(name)
		}
	}
	ms, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var missing []Finding
	for _, f := range Validate(dir, ms) {
		if f.Lane == "planner" && f.Field == "kind" {
			missing = append(missing, f)
		}
	}
	if len(missing) != 1 || !strings.Contains(missing[0].Message, "II.11") {
		t.Fatalf("five of six must be refused, naming the absent lane and the charter: %v", Validate(dir, ms))
	}
	// And the same set through ValidateEach is silent about it, which
	// is the split: per-manifest rules there, the set property here.
	for _, f := range ValidateEach(dir, ms) {
		if f.Lane == "planner" {
			t.Errorf("ValidateEach must not judge completeness: %v", f)
		}
	}
	write("planner")
	ms, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range Validate(dir, ms) {
		if f.Field == "kind" {
			t.Errorf("all six present: no completeness finding: %v", f)
		}
	}
}
