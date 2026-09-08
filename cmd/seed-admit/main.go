// seed-admit is the enforced posture's pre-receive hook
// (docs/build-plan.md Phase 2 item 3; plans/os-d3591e09.md): with it
// installed, the validator is the ledger ref's sole writer. It is
// stateless by construction — every decision is rebuilt from the
// repository it guards — and it imports the same admission rule set the
// cooperative client runs (internal/admit), so postures differ in where
// the rules run, never in which rules run.
//
// Division of labor per pushed range: one full VerifyFromGenesis over
// the pushed stream proves what verification owns for every record
// (parse, linkage, signatures, actor resolution, version discipline,
// upgrade schemas), and the records beyond the previously admitted tip
// then pass the admission-only rules that verification deliberately
// tolerates in history: the halt gate, halt verb shapes, and payload
// classification. A record-level prefix check pins append-only-ness:
// commit-graph fast-forward alone would still allow a descendant commit
// whose tree rewrites admitted records.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/transition"
)

const (
	defaultRef = "refs/seed/ledger"
	zeroID     = "0000000000000000000000000000000000000000"
)

func main() {
	// The service form (plans/os-5c8a312c.md D1): the same judgment,
	// answering proposals instead of reading the hook protocol.
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		os.Exit(runServe(os.Args[2:], os.Stdout, os.Stderr))
	}
	guarded := os.Getenv("SEED_ADMIT_REF")
	if guarded == "" {
		guarded = defaultRef
	}
	os.Exit(run(os.Stdin, os.Stderr, ".", guarded, os.Getenv(pusherEnv)))
}

// run processes the pre-receive update lines. Any refusal fails the
// whole push atomically (git applies no ref updates when the hook exits
// non-zero), which is exactly the boundary the charter wants. The
// guarded ref takes the ledger half (admitUpdate); every other ref
// takes the code-ref half (authorizeRef, plans/os-465e356e.md D1),
// judged for the pusher the transport asserted.
func run(stdin io.Reader, stderr io.Writer, gitDir, guarded, pusher string) int {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	var code *codeRefContext
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			fmt.Fprintf(stderr, "seed-admit: malformed update line %q\n", scanner.Text())
			return 1
		}
		oldID, newID, ref := fields[0], fields[1], fields[2]
		if ref != guarded {
			if pusher == "" {
				// Refused before any repository access: with no
				// identity there is nothing to authorize against.
				fmt.Fprintf(stderr, "seed-admit: rule ref: %s: no pusher identity asserted (%s) — code refs are refused without one\n", ref, pusherEnv)
				return 1
			}
			if code == nil {
				// The code-ref context is the ledger as it stands
				// BEFORE this push (the guarded ref's current tip),
				// loaded once per push: a push that carries both a
				// ledger update and a code update is judged on the
				// code side against admitted standing, never against
				// what the same push proposes.
				ctx, err := loadCodeRefContext(gitDir, guarded, pusher)
				if err != nil {
					fmt.Fprintf(stderr, "seed-admit: %v\n", err)
					return 1
				}
				code = ctx
			}
			if err := code.authorize(gitDir, oldID, newID, ref); err != nil {
				fmt.Fprintf(stderr, "seed-admit: %v\n", err)
				return 1
			}
			continue
		}
		if err := admitUpdate(gitDir, oldID, newID); err != nil {
			fmt.Fprintf(stderr, "seed-admit: %v\n", err)
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "seed-admit: reading updates: %v\n", err)
		return 1
	}
	return 0
}

func admitUpdate(gitDir, oldID, newID string) error {
	if newID == zeroID {
		return fmt.Errorf("rule ref: deletion of the ledger ref is refused")
	}
	// The ledger half reads the declaration the way the code half does:
	// at the default branch's tip as it stands BEFORE this push
	// (postures.md). A push that both changes the declaration and rides
	// it is judged against the declaration that stood before it — the
	// code half's own rule for mixed pushes. No file, or an unborn
	// branch, is no declaration: the hook as before. A file that does not
	// parse fails closed, the code half's posture (D2), and so does a
	// default branch that cannot even be resolved (a detached or
	// unreadable HEAD): each of those would otherwise judge the push with
	// every declaration-driven rule disabled while the code half refuses
	// the same state, one broken declaration splitting the boundary into
	// two.
	var decl *posture.Config
	if branch, err := defaultBranch(gitDir); err != nil {
		return fmt.Errorf("rule ref: %v — the ledger half refuses until an operator repairs the repository's HEAD", err)
	} else {
		var perr error
		decl, perr = readDeclarationAt(gitDir, branch)
		if perr != nil {
			return fmt.Errorf("rule ref: the deployment declaration at %s does not parse (%v) — the ledger half refuses until an operator repairs it", posture.DeclarationPath, perr)
		}
	}
	if oldID != zeroID {
		if err := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "--git-dir", gitDir, "merge-base", "--is-ancestor", oldID, newID).Run(); err != nil {
			return fmt.Errorf("rule ref: non-fast-forward update is refused (admitted history is append-only)")
		}
	}
	if err := validateTreeShape(gitDir, newID); err != nil {
		return err
	}

	newStore, cleanupNew, err := materialize(gitDir, newID)
	if err != nil {
		return err
	}
	defer cleanupNew()

	// One verified replay proves the whole pushed stream and hands the
	// records to the admission pass.
	resolve, _, err := genesis.Bootstrap(newStore)
	if err != nil {
		return fmt.Errorf("rule verify: %w", err)
	}
	var records []*event.Record
	if _, err := newStore.VerifyFromGenesis(resolve, ledger.WithObserver(func(pos int, rec *event.Record) {
		records = append(records, rec)
	})); err != nil {
		return fmt.Errorf("rule verify: %w", err)
	}

	// The previously admitted range is a strict record prefix: same
	// count boundary, same tip hash (hash-chain equality makes the whole
	// prefix identical).
	oldCount := 0
	if oldID != zeroID {
		oldStore, cleanupOld, err := materialize(gitDir, oldID)
		if err != nil {
			return err
		}
		defer cleanupOld()
		oldTip, n, err := oldStore.Tip()
		if err != nil {
			return fmt.Errorf("rule ref: previously admitted stream unreadable: %v", err)
		}
		oldCount = n
		if oldCount > len(records) {
			return fmt.Errorf("rule ref: pushed stream drops admitted records (%d < %d)", len(records), oldCount)
		}
		if oldCount > 0 {
			h, err := records[oldCount-1].Event.Hash()
			if err != nil {
				return fmt.Errorf("rule ref: %v", err)
			}
			if h != oldTip {
				return fmt.Errorf("rule ref: pushed stream rewrites admitted history at or before position %d", oldCount-1)
			}
		}
	}

	// Admission-only rules for the new records: what a full verification
	// deliberately tolerates in history is exactly what the boundary
	// refuses in new events.
	rules := admissionRules(admit.Default())
	table, err := transition.Default()
	if err != nil {
		return fmt.Errorf("rule ref: %v", err)
	}
	prev := event.EmptyHash
	if oldCount > 0 {
		h, err := records[oldCount-1].Event.Hash()
		if err != nil {
			return fmt.Errorf("rule ref: %v", err)
		}
		prev = h
	}
	for i := oldCount; i < len(records); i++ {
		// The context for record i is the CLI's own over the verified
		// prefix (admit.ContextOver): the resolver from seed/1, the
		// keyring, the halt state, the fold, the supported set and the
		// record list the budget rule's validity replays read. A hook
		// that built a narrower context by hand refused a run.started
		// the cooperative client admitted — found by the Phase 12
		// item 3 storm — so the two now share one constructor. The
		// genesis record has no prefix and keeps the empty context.
		var ctx *admit.Context
		if i == 0 {
			ctx = &admit.Context{Count: 0, Tip: prev, Resolve: resolve, Keyring: keyring.New(), Table: table, Lifecycle: table.FoldRecords(nil)}
		} else {
			opts := []admit.Option{}
			if decl != nil {
				opts = append(opts, admit.WithDeclaration(decl))
			}
			ctx, err = admit.ContextOver(records[:i], opts...)
			if err != nil {
				return fmt.Errorf("rule ref: %v", err)
			}
		}
		if err := admit.Run(ctx, records[i], rules); err != nil {
			return fmt.Errorf("position %d: %w", i, err)
		}
		h, err := records[i].Event.Hash()
		if err != nil {
			return fmt.Errorf("rule ref: %v", err)
		}
		prev = h
	}
	return nil
}

// admissionRules selects the boundary's share of the one admission set
// by exclusion, not inclusion: only the checks the completed replay has
// demonstrably proved for every pushed record (actor resolution and
// signature; version discipline) are dropped, so rules future phases
// append to admit.Default() flow through to the server boundary
// automatically instead of being silently bypassed (#94 review).
func admissionRules(all []admit.Rule) []admit.Rule {
	replayOwned := map[string]bool{"actor": true, "version": true}
	var rules []admit.Rule
	for _, r := range all {
		if !replayOwned[r.Name] {
			rules = append(rules, r)
		}
	}
	return rules
}

// validateTreeShape refuses any path on the guarded ref outside the
// ledger layout (HEAD and top-level segments/*.jsonl): the charter's
// references-not-content boundary applies to the tree itself, or a
// fast-forward push could ride arbitrary content on the authoritative
// ref beside an unchanged record stream (#94 review).
func validateTreeShape(gitDir, commit string) error {
	out, err := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "--git-dir", gitDir, "ls-tree", "-r", "--name-only", commit).Output()
	if err != nil {
		return fmt.Errorf("rule ref: cannot list pushed tree %.12s: %v", commit, err)
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name == "" || name == "HEAD" {
			continue
		}
		rest, ok := strings.CutPrefix(name, "segments/")
		if ok && strings.HasSuffix(rest, ".jsonl") && !strings.Contains(rest, "/") {
			continue
		}
		return fmt.Errorf("rule tree: %q is outside the ledger layout (only HEAD and segments/*.jsonl ride the guarded ref)", name)
	}
	return nil
}

// materialize extracts a commit's tree into a temp dir and opens it as a
// read-only ledger store.
func materialize(gitDir, commit string) (*ledger.Store, func(), error) {
	dir, err := os.MkdirTemp("", "seed-admit-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	archive := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "--git-dir", gitDir, "archive", commit)
	untar := exec.Command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	untar.Stdin = pipe
	if err := untar.Start(); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := archive.Run(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("rule ref: cannot read pushed tree %.12s: %v", commit, err)
	}
	if err := untar.Wait(); err != nil {
		cleanup()
		return nil, nil, err
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("rule ref: pushed tree %.12s holds no ledger: %v", commit, err)
	}
	return store, cleanup, nil
}
