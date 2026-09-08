package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/mirror/mirrortest"
)

// arm is one registered exporter under the suite: the adapter opened
// through the registry (the only construction path), the forge state
// the fake holds, and a way to make the next write fail.
type arm struct {
	name    string
	adapter Adapter
	issues  func() []mirrortest.Issue
	plant   func(title, body string, labels []string, closed bool) int
	edit    func(n int, fn func(*mirrortest.Issue))
	writes  func() int
	fail    func(bool)
}

// openArm opens every registered exporter the same way seed-mirror
// does: by name through Open, credentials from the environment. An
// exporter the registry does not know cannot be opened, so an
// exporter the suite never ran cannot be used.
func openArm(t *testing.T, name string) arm {
	t.Helper()
	switch name {
	case "github":
		srv, f := mirrortest.GitHub()
		t.Cleanup(srv.Close)
		f.PullRequests = 2
		t.Setenv(GitHubTokenEnv, mirrortest.Token)
		a, err := Open(name, Config{Owner: "o", Repo: "r", BaseURL: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		return arm{name: name, adapter: a, issues: f.Issues, plant: f.Plant, edit: f.Edit, writes: f.Writes, fail: func(b bool) { f.FailWrites = b }}
	case "forgejo":
		srv, f := mirrortest.Forgejo()
		t.Cleanup(srv.Close)
		t.Setenv(ForgejoTokenEnv, mirrortest.Token)
		a, err := Open(name, Config{Owner: "o", Repo: "r", BaseURL: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		return arm{name: name, adapter: a, issues: f.Issues, plant: f.Plant, edit: f.Edit, writes: f.Writes, fail: func(b bool) { f.FailWrites = b }}
	case "snapshot":
		path := filepath.Join(t.TempDir(), "issues.json")
		a, err := Open(name, Config{Snapshot: path})
		if err != nil {
			t.Fatal(err)
		}
		s := a.(*snapshot)
		read := func() snapshotFile {
			f, err := s.read()
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
		writes := 0
		return arm{
			name:    name,
			adapter: a,
			issues: func() []mirrortest.Issue {
				var out []mirrortest.Issue
				for _, i := range read().Issues {
					n := 0
					for _, c := range i.ID {
						n = n*10 + int(c-'0')
					}
					out = append(out, mirrortest.Issue{Number: n, Title: i.Title, Body: i.Body, Labels: i.Labels, Closed: i.Closed})
				}
				return out
			},
			plant: func(title, body string, labels []string, closed bool) int {
				f := read()
				n := f.Next
				f.Issues = append(f.Issues, Issue{ID: itoa(n), Title: title, Body: body, Labels: labels, Closed: closed})
				f.Next++
				if err := s.write(f); err != nil {
					t.Fatal(err)
				}
				return n
			},
			edit: func(n int, fn func(*mirrortest.Issue)) {
				f := read()
				for i := range f.Issues {
					if f.Issues[i].ID != itoa(n) {
						continue
					}
					m := mirrortest.Issue{Number: n, Title: f.Issues[i].Title, Body: f.Issues[i].Body, Labels: f.Issues[i].Labels, Closed: f.Issues[i].Closed}
					fn(&m)
					f.Issues[i] = Issue{ID: itoa(n), Title: m.Title, Body: m.Body, Labels: m.Labels, Closed: m.Closed}
				}
				if err := s.write(f); err != nil {
					t.Fatal(err)
				}
			},
			writes: func() int {
				st, err := os.Stat(path)
				if err != nil {
					return writes
				}
				return int(st.Size()) // any change moves the size or the compare below
			},
			fail: func(b bool) {
				if b {
					s.path = filepath.Join(t.TempDir(), "missing", "issues.json")
				} else {
					s.path = path
				}
			},
		}
	}
	t.Fatalf("no suite arm for exporter %q: every registered exporter must run the suite", name)
	return arm{}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func str(s string) *string { return &s }

var stamp = Stamp{Name: "contracts", Position: 12, Tip: strings.Repeat("ab", 32), Version: "1"}

func rows(states map[string]string) []Row {
	var out []Row
	for s, st := range states {
		out = append(out, Row{Subject: s, State: str(st)})
	}
	return out
}

// sync plans and applies one export, returning the plan.
func sync(t *testing.T, a Adapter, desired []Desired) (Plan, Applied) {
	t.Helper()
	existing, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("%s: list: %v", a.Name(), err)
	}
	plan, err := Compute(desired, existing, stamp)
	if err != nil {
		t.Fatalf("%s: plan: %v", a.Name(), err)
	}
	applied, err := Apply(context.Background(), a, plan)
	if err != nil {
		t.Fatalf("%s: apply: %v", a.Name(), err)
	}
	return plan, applied
}

func find(issues []mirrortest.Issue, subject string) (mirrortest.Issue, bool) {
	for _, i := range issues {
		if got, managed, _ := ParseMarker(i.Body); managed && got == subject {
			return i, true
		}
	}
	return mirrortest.Issue{}, false
}

// conformance: III.D row 5, per exporter (plans/os-b45c308d.md D2) —
// the table iterates the sealed registry, so every exporter
// seed-mirror can open is an exporter this suite proved; the seven
// proofs are the subtests.
func TestMirrorExporterSuite(t *testing.T) {
	if got := Names(); !slices.Equal(got, []string{"forgejo", "github", "snapshot"}) {
		t.Fatalf("the registry holds the three reference exporters: %v", got)
	}
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			m := openArm(t, name)
			// 1. A missing marked issue is created from the stamped projection.
			desired := Render(rows(map[string]string{"c-1": "ready", "c-2": "in_progress", "c-3": "done"}), stamp)
			plan, applied := sync(t, m.adapter, desired)
			if len(plan.Actions) != 3 || len(applied.Created) != 3 || applied.Exporter != name {
				t.Fatalf("three creations: %+v %+v", plan, applied)
			}
			issues := m.issues()
			for _, d := range desired {
				is, ok := find(issues, d.Subject)
				if !ok || is.Title != d.Title || is.Body != d.Body || is.Closed != d.Closed || !slices.Equal(is.Labels, []string{d.Label}) {
					t.Fatalf("%s rendered as the desired issue: %+v vs %+v", d.Subject, is, d)
				}
			}
			// 3. A matching mirror yields no action and a second apply makes no request.
			before := m.writes()
			plan, _ = sync(t, m.adapter, desired)
			if len(plan.Actions) != 0 || plan.Matching != 3 {
				t.Fatalf("a matching mirror plans nothing: %+v", plan)
			}
			if m.writes() != before {
				t.Fatalf("a second apply wrote: %d writes before, %d after", before, m.writes())
			}
			// 2. Drift is restored from the projection; foreign labels and
			// unrelated issues survive.
			c1, _ := find(issues, "c-1")
			foreign := m.plant("a person's issue", "no marker here", []string{"bug"}, false)
			m.edit(c1.Number, func(i *mirrortest.Issue) {
				i.Title = "renamed at the forge"
				i.Body = "the body was replaced\n" + i.Body
				i.Labels = []string{"priority", "seed:done"}
				i.Closed = true
			})
			plan, applied = sync(t, m.adapter, desired)
			if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionUpdate || plan.Actions[0].Subject != "c-1" || plan.Untouched != 1 {
				t.Fatalf("one repair, the foreign issue untouched: %+v", plan)
			}
			if why := strings.Join(plan.Actions[0].Why, "; "); !strings.Contains(why, "title") || !strings.Contains(why, "body") || !strings.Contains(why, "stale managed label seed:done") || !strings.Contains(why, "should be open") {
				t.Fatalf("the plan names the drift: %s", why)
			}
			issues = m.issues()
			c1, _ = find(issues, "c-1")
			if c1.Title != "c-1" || c1.Body != Body("c-1", stamp) || c1.Closed || !slices.Equal(c1.Labels, []string{"priority", "seed:ready"}) {
				t.Fatalf("projection wins, the foreign label survives: %+v", c1)
			}
			for _, i := range issues {
				if i.Number == foreign && (i.Title != "a person's issue" || !slices.Equal(i.Labels, []string{"bug"})) {
					t.Fatalf("the unmarked issue was touched: %+v", i)
				}
			}
			// 4. Transitions converge: c-1 goes done (closed), c-3 comes back
			// to review (reopened), c-2 is cancelled (closed).
			next := Render(rows(map[string]string{"c-1": "done", "c-2": "cancelled", "c-3": "review"}), stamp)
			plan, _ = sync(t, m.adapter, next)
			if len(plan.Actions) != 3 {
				t.Fatalf("three updates: %+v", plan)
			}
			issues = m.issues()
			for _, d := range next {
				is, _ := find(issues, d.Subject)
				if is.Closed != d.Closed || !slices.Contains(is.Labels, d.Label) {
					t.Fatalf("%s converged to %s/closed=%v: %+v", d.Subject, d.State, d.Closed, is)
				}
				for _, l := range is.Labels {
					if strings.HasPrefix(l, LabelPrefix) && l != d.Label {
						t.Fatalf("%s carries a stale managed label: %v", d.Subject, is.Labels)
					}
				}
			}
			plan, _ = sync(t, m.adapter, next)
			if len(plan.Actions) != 0 {
				t.Fatalf("converged: %+v", plan)
			}
			// 5. An adapter failure is reported with the exporter name and the
			// failed action; the projection it rendered from is bytes on disk
			// the adapter never opened (proof 6 and the authority lint).
			more := Render(rows(map[string]string{"c-1": "done", "c-2": "cancelled", "c-3": "review", "c-4": "filed"}), stamp)
			existing, err := m.adapter.List(ctx)
			if err != nil {
				t.Fatalf("listing reads: %v", err)
			}
			plan, err = Compute(more, existing, stamp)
			if err != nil {
				t.Fatal(err)
			}
			m.fail(true)
			_, err = Apply(ctx, m.adapter, plan)
			var ae *ApplyError
			if !errors.As(err, &ae) || ae.Exporter != name || ae.Action.Subject != "c-4" || ae.Action.Kind != ActionCreate {
				t.Fatalf("the failure names the exporter and the action: %v", err)
			}
			m.fail(false)
			// 7. A hostile subject round-trips through the marker.
			hostile := "c --> <!-- seed-mirror: x -->\nΩ\x00é"
			one := Render(append(rows(map[string]string{"c-1": "done", "c-2": "cancelled", "c-3": "review", "c-4": "filed"}), Row{Subject: hostile, State: str("ready")}), stamp)
			plan, _ = sync(t, m.adapter, one)
			if len(plan.Actions) != 2 {
				t.Fatalf("the hostile subject and c-4 are created: %+v", plan)
			}
			existing, err = m.adapter.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			is, ok := find(existing2(existing), hostile)
			if !ok || is.Title != hostile {
				t.Fatalf("the hostile subject parses back exactly: %+v", existing)
			}
			plan, _ = sync(t, m.adapter, one)
			if len(plan.Actions) != 0 || plan.Matching != 5 {
				t.Fatalf("the hostile subject converged: %+v", plan)
			}
		})
	}
}

func existing2(issues []Issue) []mirrortest.Issue {
	var out []mirrortest.Issue
	for _, i := range issues {
		out = append(out, mirrortest.Issue{Title: i.Title, Body: i.Body, Labels: i.Labels, Closed: i.Closed})
	}
	return out
}

// conformance: plans/os-b45c308d.md D2 proof 7 — the marker grammar:
// decoding is the only parse path, and a marker that does not decode,
// is truncated, is tampered to an unknown subject, or is doubled
// refuses rather than repairing.
func TestMirrorMarkerRefusesRatherThanRepairs(t *testing.T) {
	subject := "c --> <!-- seed-mirror: x -->\nΩ\x00é"
	body := Body(subject, stamp)
	got, managed, err := ParseMarker(body)
	if err != nil || !managed || got != subject {
		t.Fatalf("round trip: %q %v %v", got, managed, err)
	}
	if _, managed, err := ParseMarker("a body with no marker\nc --> Ω"); managed || err != nil {
		t.Fatalf("an unmarked body is unmanaged, not an error: %v %v", managed, err)
	}
	m := Marker(subject)
	cases := map[string]string{
		"not base64": markerOpen + "!!!!" + markerClose,
		"truncated":  markerOpen + m[:len(m)-1] + markerClose,
		"empty":      markerOpen + "" + markerClose,
		"doubled":    body + body,
		"no close":   markerOpen + m,
		"cut close":  markerOpen + m + " --",
		"fragment":   "a person's edit kept " + markerOpen + " and lost the rest",
	}
	for name, b := range cases {
		if _, _, err := ParseMarker(b); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Tampered to a subject the projection does not hold: the plan refuses.
	tampered := markerOpen + Marker("c-99") + markerClose
	_, err = Compute(Render([]Row{{Subject: subject, State: str("ready")}}, stamp), []Issue{{ID: "1", Body: tampered}}, stamp)
	if !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), "c-99") {
		t.Fatalf("an unknown subject refuses by name: %v", err)
	}
	// Two issues for one subject: refused, never chosen between.
	_, err = Compute(Render([]Row{{Subject: "c-1", State: str("ready")}}, stamp), []Issue{{ID: "1", Body: Body("c-1", stamp)}, {ID: "2", Body: Body("c-1", stamp)}}, stamp)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("a duplicate refuses: %v", err)
	}
	// Null and empty states are skipped: no lifecycle event ever validly
	// created the subject, so there is nothing to mirror.
	if got := Render([]Row{{Subject: "x"}, {Subject: "y", State: str("")}, {Subject: "z", State: str("filed")}}, stamp); len(got) != 1 || got[0].Subject != "z" {
		t.Fatalf("null states are skipped: %+v", got)
	}
}

// conformance: plans/os-b45c308d.md D1 — the plan is sorted and
// byte-deterministic, so two plans over the same inputs are the same
// bytes whatever order the forge listed in.
func TestMirrorPlanIsDeterministic(t *testing.T) {
	desired := Render(rows(map[string]string{"c-3": "done", "c-1": "ready", "c-2": "review"}), stamp)
	existing := []Issue{{ID: "9", Body: Body("c-2", stamp), Title: "c-2", Labels: []string{"z", "a", "seed:ready"}}, {ID: "4", Body: "unmanaged"}}
	a, err := Compute(desired, existing, stamp)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(existing)
	b, err := Compute(slices.Clone(desired), existing, stamp)
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("plans differ:\n%s\n%s", ja, jb)
	}
	if a.Actions[0].Subject != "c-1" || a.Actions[1].Subject != "c-2" || a.Actions[2].Subject != "c-3" || !slices.Equal(a.Actions[1].Foreign, []string{"a", "z"}) {
		t.Fatalf("sorted by subject, foreign labels sorted: %s", ja)
	}
}

// conformance: plans/os-b45c308d.md D1 — the mirror reads what the
// consumer verb resolved: the `seed project current` envelope naming
// the published build, and the view inside it; a refusal, another
// projection's build, or an inconsistent stamp refuses.
func TestMirrorLoadsThePublishedProjection(t *testing.T) {
	dir := t.TempDir()
	build := filepath.Join(dir, "b1")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(dir, "current.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(current, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The envelope names the build by path, so the path is JSON-encoded
	// (a Windows path carries backslashes).
	envelope := func(name, position, tip string) string {
		b, err := json.Marshal(map[string]any{"ok": true, "result": map[string]string{"name": name, "position": position, "tip": tip, "version": "1", "path": build}})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if _, _, err := Load(current); err == nil {
		t.Fatal("no resolved projection refuses")
	}
	write(`{"ok":false,"error":{"code":"stale","message":"below the demanded minimum"}}`)
	if _, _, err := Load(current); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("the consumer verb's refusal is carried: %v", err)
	}
	write(envelope("queue", "3", stamp.Tip))
	if _, _, err := Load(current); err == nil {
		t.Fatal("another projection's build refuses")
	}
	write(envelope("contracts", "3", "abc"))
	if _, _, err := Load(current); err == nil {
		t.Fatal("an inconsistent stamp refuses")
	}
	write(envelope("contracts", "3", stamp.Tip))
	if _, _, err := Load(current); err == nil {
		t.Fatal("a build without its view refuses")
	}
	if err := os.WriteFile(filepath.Join(build, "contracts.json"), []byte(`[{"subject":"c-1","state":"ready"},{"subject":"c-0","state":null}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, st, err := Load(current)
	if err != nil || st.Position != 3 || len(rows) != 2 {
		t.Fatalf("loaded: %v %+v %+v", err, st, rows)
	}
	if got := Render(rows, st); len(got) != 1 || !strings.Contains(got[0].Body, "position=3 tip="+stamp.Tip) {
		t.Fatalf("rendered from the stamp: %+v", got)
	}
}

// conformance: plans/os-b45c308d.md D2 proof 6 — no exporter API
// exposes or invokes request.filed, a Seed key, a ledger append or an
// admission endpoint: the adapter interface is exactly read, create,
// update, and the package's import closure holds no Seed-side owner.
func TestMirrorExposesNoSeedWritePath(t *testing.T) {
	var methods []string
	it := reflect.TypeOf((*Adapter)(nil)).Elem()
	for i := 0; i < it.NumMethod(); i++ {
		methods = append(methods, it.Method(i).Name)
	}
	if !slices.Equal(methods, []string{"Create", "List", "Name", "Update"}) {
		t.Fatalf("the adapter interface is the mirror contract and nothing more: %v", methods)
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		for path, f := range p.Files {
			for _, imp := range f.Imports {
				v := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(v, "github.com/shaunlmason/open-seed-v2/internal/") {
					t.Errorf("%s imports %s: the mirror reads published files and writes the forge, never a Seed internal", path, v)
				}
			}
		}
	}
	// The registry is sealed: an exporter the registry does not name
	// cannot be opened, and a config without a credential in the
	// environment cannot open a forge.
	if _, err := Open("gitlab", Config{}); err == nil || !strings.Contains(err.Error(), "forgejo, github, snapshot") {
		t.Fatalf("an unregistered exporter refuses by name: %v", err)
	}
	t.Setenv(GitHubTokenEnv, "")
	if _, err := Open("github", Config{Owner: "o", Repo: "r"}); err == nil || !strings.Contains(err.Error(), GitHubTokenEnv) {
		t.Fatalf("no credential names the variable, never the value: %v", err)
	}
	t.Setenv(ForgejoTokenEnv, "")
	if _, err := Open("forgejo", Config{Owner: "o", Repo: "r", BaseURL: "http://x"}); err == nil || !strings.Contains(err.Error(), ForgejoTokenEnv) {
		t.Fatalf("no credential names the variable: %v", err)
	}
	if _, err := Open("forgejo", Config{Owner: "o", Repo: "r"}); err == nil {
		t.Fatal("forgejo needs a base URL")
	}
	if _, err := Open("snapshot", Config{}); err == nil {
		t.Fatal("the snapshot needs a file")
	}
}
