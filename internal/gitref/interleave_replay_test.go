package gitref

// The replay that keeps the interleaving model honest
// (plans/os-07e6e76c.md D4): terminal traces from the explorer are
// driven through real clients, one per writer, over the loop's own
// steps against a bare remote, Fetch for a fetch step and the
// in-package attempt (the function AppendLoop iterates) for an attempt
// step, with the cooperative rollback as git update-ref on the remote
// the way the existing rollback drills do it. Every step's outcome
// class must match the model's, in the loop's own error vocabulary
// (D6); every landed record's prev must be the tip its client fetched
// (P2 for real); and the final chain must verify from genesis carrying
// the model's records in the model's order. Without this the model is
// evidence about itself.
//
// Sizes (D5): the fast gate replays -replay=8 traces, the two named P3
// shapes among them; the weekly schedule replays every trace
// (-replay=0). Times are recorded in docs/decisions.md.

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/halt"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

var replayCount = flag.Int("replay", 8, "terminal traces to replay through real clients (0: every trace of the replay configurations)")

// replayConfigs are the configurations whose traces are replayed: the
// two-writer enforced race, the halt, and the cooperative rollback.
func replayConfigs() []config {
	all := fastConfigs()
	return []config{all[0], all[2], all[3]}
}

type sampledTrace struct {
	cfg   config
	trace []step
	final *state
	label string
}

// sampleTraces picks the traces to replay: the first healing and the
// first fork trace by name, then every k-th remaining trace across the
// configurations to reach n (n <= 0: all).
func sampleTraces(t *testing.T, n int) []sampledTrace {
	t.Helper()
	var named []sampledTrace
	var perConfig [][]sampledTrace
	for _, c := range replayConfigs() {
		ex := walkModel(t, c)
		taken := map[int]bool{}
		if len(ex.healing) > 0 {
			i := ex.healing[0]
			named = append(named, sampledTrace{c, ex.traces[i], ex.finals[i], c.name + "/healing"})
			taken[i] = true
		}
		if len(ex.fork) > 0 {
			i := ex.fork[0]
			named = append(named, sampledTrace{c, ex.traces[i], ex.finals[i], c.name + "/fork"})
			taken[i] = true
		}
		var mine []sampledTrace
		for i := range ex.traces {
			if !taken[i] {
				mine = append(mine, sampledTrace{c, ex.traces[i], ex.finals[i], fmt.Sprintf("%s/%d", c.name, i)})
			}
		}
		perConfig = append(perConfig, mine)
	}
	total := len(named)
	for _, mine := range perConfig {
		total += len(mine)
	}
	if n <= 0 || n >= total {
		out := named
		for _, mine := range perConfig {
			out = append(out, mine...)
		}
		return out
	}
	// The remaining quota is dealt round-robin across the
	// configurations, and each configuration's share is spread evenly
	// over its own traces, so a sample of a few touches every
	// configuration rather than the largest alone.
	want := n - len(named)
	if want <= 0 {
		return named[:n]
	}
	quota := make([]int, len(perConfig))
	for i := 0; i < want; i++ {
		quota[i%len(perConfig)]++
	}
	out := named
	for ci, mine := range perConfig {
		q := min(quota[ci], len(mine))
		if q == 0 {
			continue
		}
		stride := len(mine) / q
		for i := 0; i < q; i++ {
			out = append(out, mine[i*stride])
		}
	}
	return out
}

// stagedRemote builds one bare remote carrying genesis and the upgrade
// to seed/1 under the root key, and returns a copier: each trace gets a
// private copy of the staged repository, so a rollback in one trace
// touches nothing another sees, and the staging cost is paid once.
func stagedRemote(t *testing.T) (copyRemote func(*testing.T) string, resolve ledger.Resolver, signer ed25519.PrivateKey, root string, tipCommit string) {
	t.Helper()
	key := fixtureKey(t, 1)
	template := bareRemote(t)
	resolve = seedGenesis(t, template, key)
	fp, err := event.Fingerprint(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	setup, err := NewClient(t.TempDir(), template, ref)
	if err != nil {
		t.Fatal(err)
	}
	res, err := setup.AppendLoop(Draft{
		V: "seed/0", TS: "2026-09-01T01:00:00Z", Actor: fp,
		Verb: "system.protocol.upgraded", Subject: "system", Payload: []byte(`{"to": "seed/1"}`),
	}, func(e event.Event) (*event.Record, error) { return event.Sign(e, key) }, resolve, admit.Validate(), 5)
	if err != nil {
		t.Fatalf("staging the upgrade: %v", err)
	}
	copyRemote = func(t *testing.T) string {
		t.Helper()
		dst := filepath.Join(t.TempDir(), "remote.git")
		if err := copyTree(template, dst); err != nil {
			t.Fatal(err)
		}
		return dst
	}
	return copyRemote, resolve, key, fp, res.Commit
}

// writerDraft is the draft a model writer carries, distinct per writer
// so the race is real (byte-identical drafts converge on one commit).
func writerDraft(kind draftKind, w int, actor string) Draft {
	ts := fmt.Sprintf("2026-09-01T02:00:%02dZ", w)
	switch kind {
	case haltDraft:
		return Draft{V: "seed/1", TS: ts, Actor: actor, Verb: halt.DeclareVerb, Subject: "system", Payload: []byte(`{"reason": "model"}`)}
	case liftDraft:
		return Draft{V: "seed/1", TS: ts, Actor: actor, Verb: halt.LiftVerb, Subject: "system", Payload: []byte(`{}`)}
	}
	return Draft{V: "seed/1", TS: ts, Actor: actor, Verb: "intent.filed", Subject: fmt.Sprintf("w-%d", w),
		Payload: []byte(`{"intent": "model", "tier": "standard", "budget": "small", "routing": "core"}`)}
}

// conformance: charter II.1 and III.A row 7 (plans/os-07e6e76c.md D4,
// D6): the model's traces replay through the real client step for
// step, outcomes named by the loop's own errors, prev by the fetched
// tip, the final chain by the model's order.
func TestAppendInterleavingReplay(t *testing.T) {
	copyRemote, resolve, key, fp, staged := stagedRemote(t)
	sign := func(e event.Event) (*event.Record, error) { return event.Sign(e, key) }
	for _, st := range sampleTraces(t, *replayCount) {
		t.Run(st.label, func(t *testing.T) {
			remote := copyRemote(t)
			replayTrace(t, remote, resolve, sign, fp, staged, st)
		})
	}
}

func replayTrace(t *testing.T, remote string, resolve ledger.Resolver, sign Signer, fp, staged string, st sampledTrace) {
	t.Helper()
	trace := traceText(st.trace)
	nodeCommit := map[int]string{0: staged}
	clients := make([]*Client, len(st.cfg.kinds))
	drafts := make([]Draft, len(st.cfg.kinds))
	fetchedCommit := make([]string, len(st.cfg.kinds))
	for w, kind := range st.cfg.kinds {
		c, err := NewClient(t.TempDir(), remote, ref)
		if err != nil {
			t.Fatal(err)
		}
		clients[w] = c
		drafts[w] = writerDraft(kind, w, fp)
	}
	s := st.cfg.initial()
	for i, step := range st.trace {
		n := s.Apply(step)
		where := fmt.Sprintf("step %d %s of %s", i, step, trace)
		switch step.action {
		case "fetch":
			w := n.writers[step.writer]
			tip, err := clients[step.writer].Fetch()
			if w.outcome == headRegression {
				if !errors.Is(err, ErrHeadRegression) {
					t.Fatalf("%s: the model refuses head regression, the client returned %v", where, err)
				}
			} else {
				if err != nil {
					t.Fatalf("%s: the model fetches node %d, the client returned %v", where, w.fetched, err)
				}
				if tip != nodeCommit[w.fetched] {
					t.Fatalf("%s: the model fetches node %d (%.12s), the client fetched %.12s", where, w.fetched, nodeCommit[w.fetched], tip)
				}
				fetchedCommit[step.writer] = tip
			}
		case "attempt":
			w := n.writers[step.writer]
			workDir, err := os.MkdirTemp("", "seed-replay-*")
			if err != nil {
				t.Fatal(err)
			}
			res, err := clients[step.writer].attempt(drafts[step.writer], sign, resolve, admit.Validate(), fetchedCommit[step.writer], workDir, nil)
			os.RemoveAll(workDir)
			switch {
			case w.outcome == landed:
				if err != nil {
					t.Fatalf("%s: the model lands node %d, the client returned %v", where, w.landedAt, err)
				}
				nodeCommit[w.landedAt] = res.Commit
				checkPrev(t, clients[step.writer], fetchedCommit[step.writer], res, where)
			case w.outcome == haltedOut:
				var halted *halt.HaltedError
				if !errors.As(err, &halted) {
					t.Fatalf("%s: the model refuses as halted, the client returned %v", where, err)
				}
			default:
				// A lost race, whether the writer retries or has spent
				// its attempts: the loop's retryable outcome.
				if !errors.Is(err, ErrNonFastForward) {
					t.Fatalf("%s: the model loses the race, the client returned %v", where, err)
				}
			}
		case "rollback":
			if out, err := exec.Command("git", "--git-dir", remote, "update-ref", ref, nodeCommit[step.to]).CombinedOutput(); err != nil {
				t.Fatalf("%s: rollback: %v %s", where, err, out)
			}
		}
		s = n
	}
	// The final chain, read by a fresh client (no persisted head, so it
	// accepts a forked remote too): genesis, the upgrade, then the
	// model's records in the model's order.
	fresh, err := NewClient(t.TempDir(), remote, ref)
	if err != nil {
		t.Fatal(err)
	}
	tip, err := fresh.Fetch()
	if err != nil {
		t.Fatalf("final fetch: %v", err)
	}
	if tip != nodeCommit[s.tip] {
		t.Fatalf("final tip %.12s is not the model's node %d (%.12s) after %s", tip, s.tip, nodeCommit[s.tip], trace)
	}
	dir := filepath.Join(t.TempDir(), "final")
	if err := fresh.Materialize(tip, dir); err != nil {
		t.Fatal(err)
	}
	store, err := ledger.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyFromGenesis(resolve); err != nil {
		t.Fatalf("the final chain does not verify after %s: %v", trace, err)
	}
	var got []string
	if err := store.Records(func(pos int, r *event.Record) error {
		if pos >= 2 {
			got = append(got, r.Event.Verb+"@"+r.Event.Subject)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, id := range s.ancestry(s.tip)[1:] {
		d := drafts[s.nodes[id].writer]
		want = append(want, d.Verb+"@"+d.Subject)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the final chain carries %v, the model %v, after %s", got, want, trace)
	}
}

// checkPrev reads the landed record back from the pushed commit and
// the tip hash from the fetched commit, and requires prev to be that
// tip (P2 for real).
func checkPrev(t *testing.T, c *Client, fetchedCommit string, res *Result, where string) {
	t.Helper()
	before := filepath.Join(t.TempDir(), "before")
	if err := c.Materialize(fetchedCommit, before); err != nil {
		t.Fatal(err)
	}
	bs, err := ledger.Open(before)
	if err != nil {
		t.Fatal(err)
	}
	tipHash, _, err := bs.Tip()
	if err != nil {
		t.Fatal(err)
	}
	after := filepath.Join(t.TempDir(), "after")
	if err := c.Materialize(res.Commit, after); err != nil {
		t.Fatal(err)
	}
	as, err := ledger.Open(after)
	if err != nil {
		t.Fatal(err)
	}
	var rec *event.Record
	if err := as.Records(func(pos int, r *event.Record) error {
		if pos == res.Position {
			rec = r
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatalf("%s: the pushed commit carries no record at position %d", where, res.Position)
	}
	if rec.Event.Prev != tipHash {
		t.Fatalf("%s: P2: the landed record cites prev %.12s, the fetched tip was %.12s", where, rec.Event.Prev, tipHash)
	}
}
