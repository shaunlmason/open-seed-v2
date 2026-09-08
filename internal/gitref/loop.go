// The optimistic append loop: fetch, re-link, re-sign, validate, push,
// retry on races (plans/os-62e2aa1d.md step 3). Losing a race never
// silently re-appends: every retry re-derives the tip, re-signs, and
// re-runs validation against the refreshed store, and a draft that fails
// re-validation surfaces its reason to the caller.
package gitref

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// Draft is an unsigned event body: the loop owns prev (re-linked each
// attempt) and the caller owns everything else.
type Draft struct {
	V       string
	TS      string
	Actor   string
	Verb    string
	Subject string
	Payload []byte
}

// Signer signs one re-linked event; only the key holder can, which is what
// makes mutate-prev-without-resign unrepresentable.
type Signer func(e event.Event) (*event.Record, error)

// Validate inspects the refreshed store and the candidate record before
// the push; refusing here is how admission rules compose into the loop
// (Phase 2 wires the full rule set through this seam).
type Validate func(store *ledger.Store, rec *event.Record) error

// Result reports a won race. Relinked counts the attempts a hook
// refused as bad_prev at or beyond the position this client appended
// (plans/os-5063e8ba.md D1): zero on a clean loop, and never a budget.
type Result struct {
	Position int
	Commit   string
	Attempts int
	Relinked int
}

// ErrStaleTree is the seventh race shape (plans/os-5063e8ba.md D1): the
// hook refused the pushed chain as bad_prev at or beyond the position
// this attempt appended, which is either a tip that moved in a way the
// loop did not see or a tree the client built wrong, and re-linking
// from a fresh fetch is the loop's answer to both. The refused tree is
// kept under the client's RefusedDir before the retry.
var ErrStaleTree = errors.New("push refused: the pushed chain cites a stale tip at or beyond this append (re-linking)")

// badPrevRE reads the hook's verification refusal as seed-admit prints
// it: "seed-admit: rule verify: position N: bad_prev". The prefix is
// part of the shape (review finding on #300): a push rejection is
// unstructured text any hook can write, and a policy refusal that
// merely echoes "position N: bad_prev" is not the verifier's finding
// and stays the refusal it is.
var badPrevRE = regexp.MustCompile(`seed-admit: rule verify: position (\d+): bad_prev`)

// staleTreeRefusal reports whether a push rejection is the seventh shape
// for a record appended at pos: a bad_prev the hook found at pos or
// beyond it. A bad_prev below pos is the fetched prefix failing, which
// the client's own verification should have caught, and stays a refusal.
func staleTreeRefusal(err error, pos int) bool {
	if !errors.Is(err, ErrRemoteRejected) {
		return false
	}
	m := badPrevRE.FindStringSubmatch(err.Error())
	if m == nil {
		return false
	}
	n, convErr := strconv.Atoi(m[1])
	return convErr == nil && n >= pos
}

// AppendLoop lands one draft on the remote ref, retrying non-fast-forward
// losses up to maxAttempts. The resolver verifies the record's signature
// at append (the genesis bootstrap or, from Phase 3, the keyring
// projection supplies it). The persisted verified head advances after
// each successful validate, and again after a winning push. Trailing
// verify options (e.g. ledger.WithSupportedVersions) apply to the
// per-attempt stream re-verification, so a client appending past a
// protocol upgrade verifies with the same set it admits with
// (plans/os-895bf828.md step 1).
func (c *Client) AppendLoop(draft Draft, sign Signer, resolve ledger.Resolver, validate Validate, maxAttempts int, vopts ...ledger.VerifyOption) (*Result, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	relinked := 0
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		tipCommit, err := c.Fetch()
		if err != nil {
			return nil, err
		}
		workDir, err := os.MkdirTemp("", "seed-gitref-*")
		if err != nil {
			return nil, err
		}
		res, err := c.attempt(draft, sign, resolve, validate, tipCommit, workDir, vopts)
		os.RemoveAll(workDir)
		if err == nil {
			res.Attempts = attempt
			res.Relinked = relinked
			return res, nil
		}
		if errors.Is(err, ErrStaleTree) {
			relinked++
			lastErr = err
			continue
		}
		if errors.Is(err, ErrNonFastForward) {
			lastErr = err
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w after %d attempts: %v", ErrRetriesSpent, maxAttempts, lastErr)
}

func (c *Client) attempt(draft Draft, sign Signer, resolve ledger.Resolver, validate Validate, tipCommit, workDir string, vopts []ledger.VerifyOption) (*Result, error) {
	storeDir := filepath.Join(workDir, "ledger")
	if err := c.Materialize(tipCommit, storeDir); err != nil {
		return nil, err
	}
	var openOpts []ledger.Option
	if c.clock != nil {
		openOpts = append(openOpts, ledger.WithClock(c.clock))
	}
	store, err := ledger.Open(storeDir, openOpts...)
	if err != nil {
		return nil, err
	}
	if tipCommit != "" {
		if _, err := store.VerifyFromGenesis(resolve, vopts...); err != nil {
			return nil, fmt.Errorf("fetched stream failed verification: %w", err)
		}
	}
	tip, _, err := store.Tip()
	if err != nil {
		return nil, err
	}
	rec, err := sign(event.Event{
		V: draft.V, TS: draft.TS, Actor: draft.Actor,
		Verb: draft.Verb, Subject: draft.Subject,
		Payload: draft.Payload, Prev: tip,
	})
	if err != nil {
		return nil, err
	}
	if validate != nil {
		if err := validate(store, rec); err != nil {
			return nil, fmt.Errorf("draft failed re-validation against the refreshed tip: %w", err)
		}
	}
	if tipCommit != "" {
		if err := c.persistHead(tipCommit); err != nil {
			return nil, err
		}
	}
	// From seed/1 the keyring projection is the append-time resolver:
	// standing decides who signs at this tip, so an enrolled key appends
	// and a suspended or revoked one refuses, while seed/0 chains keep
	// the caller's root-seam resolver untouched.
	appendResolve := resolve
	if tipCommit != "" {
		var records []*event.Record
		if err := store.Records(func(pos int, r *event.Record) error {
			records = append(records, r)
			return nil
		}); err != nil {
			return nil, err
		}
		ring, ringActive, err := keyring.StateAt(records)
		if err != nil {
			return nil, err
		}
		if keyring.Applies(ringActive) && ring.Seeded() {
			appendResolve = ring.Resolver()
		}
	}
	pos, err := store.Append(rec, appendResolve)
	if err != nil {
		return nil, err
	}
	if c.proposer != nil {
		// The forge-hosted posture: the service, not this client, is
		// the ref's writer. The record proposed is the one just
		// validated, linked to the tip this attempt fetched; a moved
		// tip comes back as the race it is and the loop re-links. The
		// persisted head is left at the fetched tip: the commit the
		// service made is not in this git dir until the next fetch
		// verifies it, and persisting an object we do not hold would
		// turn the monotonic-head rule into a self-inflicted regression.
		res, err := c.proposer.Propose(c.Ref, []*event.Record{rec})
		if err != nil {
			return nil, err
		}
		if res.Position != pos {
			return nil, fmt.Errorf("%w: the service admitted at position %d where this proposer expected %d", ErrNonFastForward, res.Position, pos)
		}
		return res, nil
	}
	commit, err := c.Commit(storeDir, tipCommit, "ledger: "+draft.Verb)
	if err != nil {
		return nil, err
	}
	if err := c.Push(commit); err != nil {
		if staleTreeRefusal(err, pos) {
			if keepErr := c.keepRefused(commit, storeDir, err); keepErr != nil {
				return nil, fmt.Errorf("%w; and the refused tree could not be kept: %v", err, keepErr)
			}
			return nil, fmt.Errorf("%w: %v", ErrStaleTree, err)
		}
		return nil, err
	}
	if err := c.persistHead(commit); err != nil {
		return nil, err
	}
	return &Result{Position: pos, Commit: commit}, nil
}

// keepRefused keeps the evidence under RefusedDir()/<commit>/
// (plans/os-5063e8ba.md D2): commit/ is the rejected commit's own tree,
// materialized from the commit object (the bytes the hook judged),
// worktree/ is the directory the attempt built the commit from, and
// message.txt is the hook's refusal. If the record the hook found came
// from the client's persistent index, commit/ carries it and worktree/
// does not, and the difference between the two is the diagnosis.
func (c *Client) keepRefused(commit, storeDir string, refusal error) error {
	dst := filepath.Join(c.RefusedDir(), commit)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dst, "message.txt"), []byte(refusal.Error()+"\n"), 0o644); err != nil {
		return err
	}
	if err := c.Materialize(commit, filepath.Join(dst, "commit")); err != nil {
		return err
	}
	return copyTree(storeDir, filepath.Join(dst, "worktree"))
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
