package redteam

// The adversary (plans/os-465e356e.md D2, D3): a process holding a real
// enrolled key with a claim grant and a real credential — the identity
// the transport asserts for it — that runs git directly. It never calls
// the honest CLI to perform an attack, and it never writes the bare
// repository's filesystem: every primitive here ends in `git push`, so
// the only thing between the attacker and the ref is the hook.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// Outcome is what one attempt produced: whether the push landed, git's
// combined output (the hook's refusal is in it), and for a single-event
// ledger push the record and the records it was judged against, so the
// in-process rule set can be asked the same question.
type Outcome struct {
	Admitted bool
	Output   string
	Record   *event.Record
	Before   []*event.Record
}

// Refusal returns the hook's own line from the output, or the output
// when the hook said nothing (a transport error, say).
func (o Outcome) Refusal() string {
	for _, line := range strings.Split(o.Output, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "seed-admit:"); i >= 0 {
			return line[i:]
		}
	}
	return strings.TrimSpace(o.Output)
}

// Adversary is the compromised actor.
type Adversary struct {
	fx *Fixture
	// ID is the compromised identity: its key signs, its fingerprint
	// is the credential the transport asserts.
	ID *Identity
	// dir is the adversary's own scratch space, never the remote's.
	dir string
}

// NewAdversary hands the compromised key and credential to a process.
func NewAdversary(fx *Fixture, id *Identity) (*Adversary, error) {
	dir, err := os.MkdirTemp(fx.Dir, "adversary-*")
	if err != nil {
		return nil, err
	}
	return &Adversary{fx: fx, ID: id, dir: dir}, nil
}

// Sign produces a record signed by the compromised key, attributed to
// the given actor (its own fingerprint, or another's for an
// impersonation), linked to prev.
func (a *Adversary) Sign(actor, verb, subject, payload, prev string) (*event.Record, error) {
	return event.Sign(event.Event{
		V: a.fx.Active, TS: a.fx.TS(), Actor: actor, Verb: verb, Subject: subject,
		Payload: json.RawMessage(payload), Prev: prev,
	}, a.ID.Key)
}

// client is a fresh raw client over the guarded ref: fetch, materialize,
// commit, push — no validation anywhere, which is the point.
func (a *Adversary) client() (*gitref.Client, error) {
	state, err := os.MkdirTemp(a.dir, "state-*")
	if err != nil {
		return nil, err
	}
	return gitref.NewClient(state, a.fx.Remote, GuardedRef)
}

// Craft materializes the guarded ref's tip, lets the attacker rewrite
// the tree however it likes, and pushes the result on top of the tip
// as a fast-forward. The hook is the only judge.
func (a *Adversary) Craft(mutate func(dir string, store *ledger.Store, records []*event.Record) error) (Outcome, error) {
	c, err := a.client()
	if err != nil {
		return Outcome{}, err
	}
	tip, err := c.Fetch()
	if err != nil {
		return Outcome{}, err
	}
	dir, err := os.MkdirTemp(a.dir, "craft-*")
	if err != nil {
		return Outcome{}, err
	}
	if err := c.Materialize(tip, dir); err != nil {
		return Outcome{}, err
	}
	store, err := ledger.Open(dir)
	if err != nil {
		return Outcome{}, err
	}
	var before []*event.Record
	if err := store.Records(func(pos int, rec *event.Record) error {
		before = append(before, rec)
		return nil
	}); err != nil {
		return Outcome{}, err
	}
	if err := mutate(dir, store, before); err != nil {
		return Outcome{}, err
	}
	_, err = c.CommitAndPush(dir, tip, "adversary: crafted push")
	return outcomeOf(err, before), nil
}

func outcomeOf(err error, before []*event.Record) Outcome {
	if err == nil {
		return Outcome{Admitted: true, Before: before}
	}
	return Outcome{Admitted: false, Output: err.Error(), Before: before}
}

// PushEvent forges one event — signed by the compromised key, attributed
// to actor — appends it raw to the materialized tip and pushes. The
// local append uses a resolver that maps the claimed actor to the
// adversary's OWN key, so a forged attribution (an impersonation) leaves
// the client happily; the hook's replay resolves the claimed actor
// through the admitted keyring, which is where the impersonation fails.
func (a *Adversary) PushEvent(actor, verb, subject, payload string) (Outcome, error) {
	var rec *event.Record
	out, err := a.Craft(func(dir string, store *ledger.Store, records []*event.Record) error {
		tip, _, err := store.Tip()
		if err != nil {
			return err
		}
		r, err := a.Sign(actor, verb, subject, payload, tip)
		if err != nil {
			return err
		}
		rec = r
		local := func(fp string) (ed25519.PublicKey, bool) {
			return a.ID.Key.Public().(ed25519.PublicKey), true
		}
		if _, err := store.Append(r, local); err != nil {
			return fmt.Errorf("the adversary's own append refused before it could push (the attack is malformed, not that the hook caught it): %w", err)
		}
		return nil
	})
	out.Record = rec
	return out, err
}

// As forges an event attributed to the adversary itself — the ordinary
// case, where the credential and the signature agree.
func (a *Adversary) As(verb, subject, payload string) (Outcome, error) {
	return a.PushEvent(a.ID.FP, verb, subject, payload)
}

// Rewrite re-signs the record at position pos with a mutated payload,
// keeping its prev, and force-pushes the whole ledger tree with that
// record swapped in: the hook re-derives the chain hash and catches
// the swap. Force, because a rewrite is never a fast-forward.
func (a *Adversary) Rewrite(pos int, payload string) (Outcome, error) {
	before, err := a.fetchRecords()
	if err != nil {
		return Outcome{}, err
	}
	if pos < 0 || pos >= len(before) {
		return Outcome{}, fmt.Errorf("position %d is outside the staged chain of %d", pos, len(before))
	}
	target := before[pos]
	forged, err := event.Sign(event.Event{
		V: target.Event.V, TS: target.Event.TS, Actor: target.Event.Actor,
		Verb: target.Event.Verb, Subject: target.Event.Subject,
		Payload: []byte(payload), Prev: target.Event.Prev,
	}, a.ID.Key)
	if err != nil {
		return Outcome{}, err
	}
	out := make([]*event.Record, len(before))
	copy(out, before)
	out[pos] = forged
	o := a.forcePushTree(treeFiles(out), a.tipCommit())
	o.Before = before
	return o, nil
}

// RewriteTip rewrites the LAST admitted record's payload, re-signed with
// its own version and prev so the pushed stream still verifies from
// genesis: every prior record is untouched and the tip re-links, so the
// only thing wrong is that the tip's hash no longer equals the admitted
// tip. That is exactly the divergence a commit-graph fast-forward check
// would wave through, and the hook's record-level prefix check catches.
func (a *Adversary) RewriteTip(payload string) (Outcome, error) {
	before, err := a.fetchRecords()
	if err != nil {
		return Outcome{}, err
	}
	if len(before) == 0 {
		return Outcome{}, fmt.Errorf("empty chain")
	}
	return a.Rewrite(len(before)-1, payload)
}

// DropTip force-pushes the chain with its last record removed: a
// truncation a commit-graph fast-forward check alone would miss.
func (a *Adversary) DropTip() (Outcome, error) {
	before, err := a.fetchRecords()
	if err != nil {
		return Outcome{}, err
	}
	if len(before) < 2 {
		return Outcome{}, fmt.Errorf("chain too short to drop a tip")
	}
	o := a.forcePushTree(treeFiles(before[:len(before)-1]), a.tipCommit())
	o.Before = before
	return o, nil
}

// tipCommit is the guarded ref's current commit, the parent a rewrite
// commits on top of so the push is a fast-forward and the hook judges
// the CONTENT (the rewritten or dropped record) rather than bouncing a
// non-fast-forward before its rules run.
func (a *Adversary) tipCommit() string {
	out, err := exec.Command("git", "--git-dir", a.fx.Remote, "rev-parse", "--verify", "--quiet", GuardedRef).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DeleteLedger force-pushes a deletion of the guarded ref.
func (a *Adversary) DeleteLedger() Outcome {
	cmd := exec.Command("git", "--git-dir", a.fx.Remote, "push", a.fx.Remote, ":"+GuardedRef)
	out, err := cmd.CombinedOutput()
	return Outcome{Admitted: err == nil, Output: string(out)}
}

// PushBranch pushes files to a code ref under the adversary's asserted
// identity; force chooses whether it is a force-update.
func (a *Adversary) PushBranch(ref string, force bool, base string, files map[string]string) Outcome {
	out, err := PushCode(a.fx.Remote, a.ID.FP, ref, force, base, files, a.dir)
	return Outcome{Admitted: err == nil, Output: out}
}

// DeleteRef pushes a deletion of a code ref under the adversary's
// identity.
func (a *Adversary) DeleteRef(ref string) Outcome {
	cmd := exec.Command("git", "--git-dir", a.fx.Remote, "push", a.fx.Remote, ":"+ref)
	cmd.Env = append(os.Environ(), PusherEnv+"="+a.ID.FP)
	out, err := cmd.CombinedOutput()
	return Outcome{Admitted: err == nil, Output: string(out)}
}

// fetchRecords materializes the guarded ref's current tip and returns
// its records, the base a rewrite mutates.
func (a *Adversary) fetchRecords() ([]*event.Record, error) {
	c, err := a.client()
	if err != nil {
		return nil, err
	}
	tip, err := c.Fetch()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(a.dir, "read-*")
	if err != nil {
		return nil, err
	}
	if err := c.Materialize(tip, dir); err != nil {
		return nil, err
	}
	store, err := ledger.Open(dir)
	if err != nil {
		return nil, err
	}
	var before []*event.Record
	if err := store.Records(func(p int, rec *event.Record) error {
		before = append(before, rec)
		return nil
	}); err != nil {
		return nil, err
	}
	return before, nil
}

// treeFiles renders a record slice as the ledger tree the guarded ref
// carries: one JCS line per record in a single day segment, and a HEAD
// naming the tip and count. A malformed chain is the attacker's to
// push; the hook verifies from genesis and catches it.
func treeFiles(records []*event.Record) map[string]string {
	var b strings.Builder
	for _, rec := range records {
		line, err := rec.Marshal()
		if err != nil {
			continue
		}
		b.Write(line)
	}
	tip := event.EmptyHash
	if len(records) > 0 {
		if h, err := records[len(records)-1].Event.Hash(); err == nil {
			tip = h
		}
	}
	return map[string]string{
		"segments/2026-09-01.jsonl": b.String(),
		"HEAD":                      fmt.Sprintf(`{"tip": %q, "count": %d}`, tip, len(records)) + "\n",
	}
}

// forcePushTree builds a fresh orphan commit carrying exactly files and
// force-pushes it to the guarded ref: the hook is the only judge, and
// force is what makes it judge a rewrite rather than git bouncing it as
// a non-fast-forward before the hook ever runs.
func (a *Adversary) forcePushTree(files map[string]string, parent string) Outcome {
	work, err := os.MkdirTemp(a.dir, "force-*")
	if err != nil {
		return Outcome{Output: err.Error()}
	}
	run := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", work}, args...)...).CombinedOutput()
		return string(out), err
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "gc.auto", "0"}, {"config", "gc.autoDetach", "false"}, {"config", "user.email", "a@f"}, {"config", "user.name", "adversary"}} {
		if out, err := run(args...); err != nil {
			return Outcome{Output: out}
		}
	}
	for name, body := range files {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Outcome{Output: err.Error()}
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return Outcome{Output: err.Error()}
		}
	}
	if out, err := run("add", "-A"); err != nil {
		return Outcome{Output: out}
	}
	// Committing on top of the guarded ref's current commit makes the
	// push a fast-forward, so the hook's content rules (prefix rewrite,
	// dropped records) judge it; an orphan would bounce as a
	// non-fast-forward before those rules run.
	if parent != "" {
		if out, err := run("fetch", "-q", a.fx.Remote, GuardedRef); err != nil {
			return Outcome{Output: out}
		}
		if out, err := run("reset", "--soft", parent); err != nil {
			return Outcome{Output: out}
		}
	}
	if out, err := run("commit", "-q", "-m", "adversary rewrite"); err != nil {
		return Outcome{Output: out}
	}
	push := []string{"-C", work, "push"}
	if parent == "" {
		push = append(push, "--force")
	}
	push = append(push, a.fx.Remote, "HEAD:"+GuardedRef)
	out, err := exec.Command("git", push...).CombinedOutput()
	return Outcome{Admitted: err == nil, Output: string(out)}
}
