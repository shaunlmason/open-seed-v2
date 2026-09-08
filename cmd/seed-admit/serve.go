package main

// The service form of the admission validator (SEED-NEXT.md §II.2,
// "enforced, forge-hosted"; plans/os-5c8a312c.md D1, D2): the same
// binary as the pre-receive hook, judging a candidate it builds from a
// proposal with the very admitUpdate the hook runs, and pushing the
// admitted candidate under the identity its git credential carries.
// It signs nothing, retries nothing, and keeps no state but a clone.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/genesis"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
	"github.com/shaunlmason/open-seed-v2/internal/propose"
	"github.com/shaunlmason/open-seed-v2/internal/refusal"
)

// service is one admission endpoint over one ledger ref. The mutex
// serializes proposals through one instance; two instances race at the
// remote like any two writers, and the loser reports the race.
type service struct {
	remote   string
	ref      string
	stateDir string

	mu   sync.Mutex
	last *propose.Health
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remote := fs.String("remote", "", "the ledger repository the service fetches from and pushes to")
	ref := fs.String("ref", posture.DefaultLedgerRef, "the ledger ref (a branch under the forge-hosted posture)")
	state := fs.String("state", "", "the service's state dir: its clone, rebuilt from the remote (default: a temp dir)")
	listen := fs.String("listen", "127.0.0.1:0", "address to listen on")
	announce := fs.String("announce", "", "file to write the bound endpoint to once listening")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *remote == "" {
		fmt.Fprintln(stderr, "usage: seed-admit serve --remote <repo> [--ref <ref>] [--state <dir>] [--listen <addr>] [--announce <file>]")
		return 64
	}
	if *state == "" {
		dir, err := os.MkdirTemp("", "seed-admit-serve-*")
		if err != nil {
			fmt.Fprintf(stderr, "seed-admit serve: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		*state = dir
	}
	svc := &service{remote: *remote, ref: *ref, stateDir: *state}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(stderr, "seed-admit serve: listen %s: %v\n", *listen, err)
		return 1
	}
	endpoint := "http://" + ln.Addr().String()
	if *announce != "" {
		if err := os.WriteFile(*announce, []byte(endpoint+"\n"), 0o644); err != nil {
			fmt.Fprintf(stderr, "seed-admit serve: announce: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "{\"listening\": %q, \"remote\": %q, \"ref\": %q}\n", endpoint, *remote, *ref)
	srv := &http.Server{Handler: svc.handler(), ReadHeaderTimeout: 10 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "seed-admit serve: %v\n", err)
		return 1
	}
	return 0
}

// handler is the two routes: the proposal endpoint speaking the
// envelope, and the health probe.
func (s *service) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/propose", s.handlePropose)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func writeEnvelope(w http.ResponseWriter, status int, env *envelope.Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = env.Render(w)
}

func (s *service) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeEnvelope(w, http.StatusMethodNotAllowed, envelope.Fail(envelope.ExitUsage, "usage", "GET /healthz"))
		return
	}
	s.mu.Lock()
	h := propose.Health{Remote: s.remote, Ref: s.ref}
	if s.last != nil {
		h = *s.last
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h)
}

// handlePropose decodes strictly and judges. The status is transport
// (200 admitted, 409 the ref moved, 422 refused, 503 the service cannot
// reach or write the remote); the envelope is the answer, and it is the
// boundary's own.
func (s *service) handlePropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeEnvelope(w, http.StatusMethodNotAllowed, envelope.Fail(envelope.ExitUsage, "usage", "POST /propose with {ref, records}"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, propose.MaxBody+1))
	if err != nil || len(body) > propose.MaxBody {
		writeEnvelope(w, propose.StatusRefused, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", fmt.Sprintf("a proposal is at most %d bytes", propose.MaxBody)))
		return
	}
	var raw struct {
		Ref     string            `json:"ref"`
		Records []json.RawMessage `json:"records"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil || dec.More() {
		writeEnvelope(w, propose.StatusRefused, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", "the proposal does not parse as {ref, records}"))
		return
	}
	if raw.Ref != s.ref {
		writeEnvelope(w, propose.StatusRefused, envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("this service admits proposals for %s only (asked for %q)", s.ref, raw.Ref)))
		return
	}
	if len(raw.Records) == 0 {
		writeEnvelope(w, propose.StatusRefused, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", "a proposal carries at least one record"))
		return
	}
	recs := make([]*event.Record, 0, len(raw.Records))
	for i, line := range raw.Records {
		rec, err := event.ParseRecord(line)
		if err != nil {
			writeEnvelope(w, propose.StatusRefused, envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", fmt.Sprintf("proposed record %d does not parse: %v", i, err)))
			return
		}
		recs = append(recs, rec)
	}
	env, status := s.judge(recs)
	writeEnvelope(w, status, env)
}

// judge fetches the tip, appends the proposal onto a materialized copy,
// commits the candidate in the service's own git dir, judges it with
// the hook's admitUpdate, and pushes only what was admitted.
func (s *service) judge(recs []*event.Record) (*envelope.Envelope, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := gitref.NewClient(s.stateDir, s.remote, s.ref)
	if err != nil {
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot prepare the service's clone: %v", err)), propose.StatusUnavailable
	}
	tip, err := client.Fetch()
	if err != nil {
		return refusal.Envelope(err), propose.StatusUnavailable
	}
	work, err := os.MkdirTemp("", "seed-admit-judge-*")
	if err != nil {
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), propose.StatusUnavailable
	}
	defer os.RemoveAll(work)
	ledgerDir := filepath.Join(work, "ledger")
	if err := client.Materialize(tip, ledgerDir); err != nil {
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot materialize the tip: %v", err)), propose.StatusUnavailable
	}
	store, err := ledger.Open(ledgerDir)
	if err != nil {
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), propose.StatusUnavailable
	}

	// The mirror carries the guarded ref alone, so its default branch is
	// unborn and the hook's declaration read (at the default branch's tip,
	// the code half's own) would find nothing. The default branch is
	// resolved THROUGH THE TRANSPORT (ls-remote --symref), because the
	// deployment's remote may be an SSH/HTTPS URL, and `git --git-dir <url>`
	// is not a repository for one; then it is fetched INTO the mirror's
	// own branch (a plain `fetch remote HEAD` lands in FETCH_HEAD, not the
	// branch the read follows) and the mirror's HEAD pointed at it, so
	// readDeclarationAt reads the same tip the hook on the remote reads
	// (the read is over the pre-push tip, postures.md). An unborn HEAD is
	// an empty deployment — the hook as before; a detached or unreadable
	// one fails closed as unavailable, not a silent no-declaration.
	branch, berr := defaultBranchRemote(s.remote)
	switch {
	case berr != nil:
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot resolve the deployment's default branch: %v", berr)), propose.StatusUnavailable
	case branch != "":
		if out, ferr := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "--git-dir", client.GitDir(), "fetch", "-q", s.remote, branch).CombinedOutput(); ferr != nil {
			return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot fetch the deployment's default branch: %v: %s", ferr, out)), propose.StatusUnavailable
		}
		if out, ferr := exec.Command("git", "-c", "core.autocrlf=false", "-c", "core.eol=lf", "--git-dir", client.GitDir(), "update-ref", "-m", "judge", branch, "FETCH_HEAD").CombinedOutput(); ferr != nil {
			return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot land the default branch in the mirror: %v: %s", ferr, out)), propose.StatusUnavailable
		}
		if out, ferr := exec.Command("git", "--git-dir", client.GitDir(), "symbolic-ref", "HEAD", branch).CombinedOutput(); ferr != nil {
			return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot point the mirror's HEAD at the default branch: %v: %s", ferr, out)), propose.StatusUnavailable
		}
	}
	// branch == "": the deployment's HEAD is unborn — nothing to fetch, no
	// declaration to read, the hook as before.
	storeTip, count, err := store.Tip()
	if err != nil {
		return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), propose.StatusRefused
	}

	// The race check comes before anything is appended: a proposal
	// linked to a tip that is no longer the tip is the one answer the
	// proposer's loop handles by itself.
	if recs[0].Event.Prev != storeTip {
		env := envelope.Fail(envelope.ExitContention, "non_fast_forward", fmt.Sprintf("the ledger ref moved: the proposal links to %.12s but the tip is %.12s; re-link and propose again", recs[0].Event.Prev, storeTip))
		return refusal.StampTip(env, count), propose.StatusMoved
	}
	for i := 1; i < len(recs); i++ {
		prev, err := recs[i-1].Event.Hash()
		if err != nil {
			return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), propose.StatusRefused
		}
		if recs[i].Event.Prev != prev {
			return refusal.StampTip(envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", fmt.Sprintf("proposed record %d does not link to record %d", i, i-1)), count), propose.StatusRefused
		}
	}

	resolve, err := s.resolver(store, count, recs[0])
	if err != nil {
		return refusal.StampTip(refusal.Envelope(err), count), propose.StatusRefused
	}
	var appended []string
	for _, rec := range recs {
		if _, err := store.Append(rec, resolve); err != nil {
			return refusal.StampTip(refusal.Envelope(err), count), propose.StatusRefused
		}
		h, err := rec.Event.Hash()
		if err != nil {
			return envelope.Fail(envelope.ExitChainInvalid, "chain_invalid", err.Error()), propose.StatusRefused
		}
		appended = append(appended, h)
	}
	verbs := make([]string, 0, len(recs))
	for _, rec := range recs {
		verbs = append(verbs, rec.Event.Verb)
	}
	candidate, err := client.Commit(ledgerDir, tip, "ledger: "+strings.Join(verbs, ", "))
	if err != nil {
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot build the candidate: %v", err)), propose.StatusUnavailable
	}
	oldID := tip
	if oldID == "" {
		oldID = zeroID
	}
	// One derivation: the hook's judgment over old→candidate, with the
	// boundary's typed refusal surviving the wrap so the envelope
	// carries the boundary's own code.
	if err := admitUpdate(client.GitDir(), oldID, candidate); err != nil {
		env := refusal.StampTip(refusal.Envelope(err), count)
		if env.Exit == envelope.ExitUnavailable {
			return env, propose.StatusUnavailable
		}
		return env, propose.StatusRefused
	}
	if err := client.Push(candidate); err != nil {
		switch {
		case errors.Is(err, gitref.ErrNonFastForward):
			return refusal.StampTip(envelope.Fail(envelope.ExitContention, "non_fast_forward", "the ledger ref moved while the proposal was judged; re-link and propose again"), count), propose.StatusMoved
		default:
			// The service's own push refused or the remote unreachable:
			// a deployment problem (the identity is not the ref's
			// writer, or the remote is down), never the proposer's.
			return refusal.Envelope(err), propose.StatusUnavailable
		}
	}
	if err := client.RecordVerifiedHead(candidate); err != nil {
		return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), propose.StatusUnavailable
	}
	newCount := count + len(recs)
	pos := newCount - 1
	s.last = &propose.Health{Remote: s.remote, Ref: s.ref, Position: &pos, Tip: candidate}
	env := envelope.OK(map[string]any{
		"position": pos,
		"commit":   candidate,
		"appended": appended,
		"verbs":    verbs,
	})
	return refusal.StampTip(env, newCount), propose.StatusAdmitted
}

// resolver is the append-time signature resolver, chosen as
// gitref.AppendLoop chooses it: the genesis root seam for a seed/0
// chain, the keyring projection from seed/1, and for an empty ledger
// the proposed genesis's own payload.
func (s *service) resolver(store *ledger.Store, count int, first *event.Record) (ledger.Resolver, error) {
	if count == 0 {
		payload, err := genesis.Parse(first)
		if err != nil {
			return nil, fmt.Errorf("an empty ledger admits a genesis proposal only: %w", err)
		}
		return payload.Resolver(first.Event.Actor)
	}
	resolve, _, err := genesis.Bootstrap(store)
	if err != nil {
		return nil, err
	}
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
		return ring.Resolver(), nil
	}
	return resolve, nil
}
