package main

// The A2A-shaped cross-organization boundary (plans/os-40ed0ca0.md;
// spec/boundary.md): `boundary card` renders and signs the
// capability card, `boundary check` holds the checked-in card to the
// declaration (CI's gate), `boundary serve` is the read-only surface
// — the card, the task states, artifacts by digest, and nothing else
// — and `boundary tasks` and `boundary fetch` read it as a stranger
// would. The one write across the boundary is the request ingress,
// which is `request file` against the remote the card names.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/boundary"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/posture"
)

// DefaultCardPath is where a deployment checks its card in; CI
// re-renders and diffs it (`boundary check`).
const DefaultCardPath = "boundary/card.json"

// readPublicKey resolves the operator public key a reader was given
// out of band, from a hex string or a file holding one
// (plans/os-f11601e0.md D5). It is the only way a signature is checked,
// because the key never comes from the card: a card that carried the
// key that signed it would prove nothing.
func readPublicKey(hexKey, path string) (ed25519.PublicKey, *envelope.Envelope) {
	if env := refuseTwoKeys(hexKey, path); env != nil {
		return nil, env
	}
	text := hexKey
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --pubkey-file: %v", err))
		}
		text = string(raw)
	}
	// The operator's key is whatever the operator already has. An
	// OpenSSH authorized-keys line is what `ssh-keygen -y` writes and
	// what spec/protocol.md accepts at key load, so it is tried
	// first; a bare hex key is accepted too, because that is the form
	// the card's own `signer` and this build's fingerprints speak.
	if key, err := event.ParsePublicKey([]byte(text)); err == nil {
		return key, nil
	}
	key, err := hex.DecodeString(strings.TrimSpace(text))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, envelope.Fail(envelope.ExitUsage, "usage",
			"the operator key is an ed25519 public key, in OpenSSH authorized-keys form or in hex")
	}
	return ed25519.PublicKey(key), nil
}

// refuseTwoKeys is checked before anything is read, by every verb that
// takes a key. Naming two keys is a usage error rather than a
// precedence rule, and a reader who learned which one wins would have
// learned the wrong thing; refusing it ahead of the card and the
// declaration keeps a malformed invocation from coming back as drift.
func refuseTwoKeys(hexKey, path string) *envelope.Envelope {
	if hexKey != "" && path != "" {
		return envelope.Fail(envelope.ExitUsage, "usage", "give --pubkey or --pubkey-file, not both")
	}
	return nil
}

// runBoundaryVerify is the reader's half of the boundary
// (plans/os-f11601e0.md D5; spec/boundary.md): a stranger holding
// a fetched card and the operator key it was given out of band checks
// one against the other. It takes no declaration and no name, because
// the stranger has neither — `boundary check` is the publisher's verb
// and needs both.
func runBoundaryVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("boundary verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cardPath := fs.String("card", DefaultCardPath, "the card to verify")
	pub := fs.String("pubkey", "", "the operator's public key (hex), given out of band")
	pubFile := fs.String("pubkey-file", "", "a file holding that key")
	if err := fs.Parse(args); err != nil || (*pub == "" && *pubFile == "") || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage",
			"boundary verify requires --pubkey <hex> or --pubkey-file <path> [--card <file>]"), stdout, stderr)
	}
	key, failEnv := readPublicKey(*pub, *pubFile)
	if failEnv != nil {
		return render(failEnv, stdout, stderr)
	}
	raw, err := os.ReadFile(*cardPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", fmt.Sprintf("no card at %s", *cardPath)), stdout, stderr)
	}
	card, err := boundary.Parse(raw)
	if err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	if err := boundary.Verify(card, key); err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	return render(envelope.OK(map[string]any{
		"card": *cardPath, "name": card.Name, "signer": card.Signer,
		"protocol": card.Protocol, "verified": true,
	}), stdout, stderr)
}

func runBoundary(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "boundary requires a subverb: card, check, verify, serve, tasks, fetch"), stdout, stderr)
	}
	switch args[0] {
	case "card":
		return runBoundaryCard(args[1:], stdout, stderr)
	case "check":
		return runBoundaryCheck(args[1:], stdout, stderr)
	case "verify":
		return runBoundaryVerify(args[1:], stdout, stderr)
	case "serve":
		return runBoundaryServe(args[1:], stdout, stderr)
	case "tasks":
		return runBoundaryTasks(args[1:], stdout, stderr)
	case "fetch":
		return runBoundaryFetch(args[1:], stdout, stderr)
	}
	return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("unknown boundary subverb %q — card, check, verify, serve, tasks, fetch", args[0])), stdout, stderr)
}

func readOperatorKey(path string) (ed25519.PrivateKey, *envelope.Envelope) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err))
	}
	priv, err := event.ParsePrivateKey(raw)
	if err != nil {
		return nil, envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot parse --key: %v", err))
	}
	return priv, nil
}

// runBoundaryCard renders the card from the declaration, signs it
// with the operator key, and writes it where CI will re-render it.
func runBoundaryCard(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("boundary card", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "", "deployment declaration carrying the boundary block")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the operator that signs the card")
	name := fs.String("name", "", "the deployment's name, as other organizations know it")
	out := fs.String("out", DefaultCardPath, "where the card is written")
	if err := fs.Parse(args); err != nil || *config == "" || *keyPath == "" || *name == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "boundary card requires --config <file> --key <operator key> --name <name> [--out <file>]"), stdout, stderr)
	}
	cfg, err := posture.Load(*config)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read the declaration: %v", err)), stdout, stderr)
	}
	priv, env := readOperatorKey(*keyPath)
	if env != nil {
		return render(env, stdout, stderr)
	}
	card, err := boundary.Render(cfg, *name)
	if err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	if err := boundary.Sign(card, priv); err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	b, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	return render(envelope.OK(map[string]any{"path": *out, "name": card.Name, "signer": card.Signer, "kinds": card.Ingress.Kinds}), stdout, stderr)
}

func boundaryEnvelope(err error) *envelope.Envelope {
	var berr *boundary.Error
	if errors.As(err, &berr) {
		return envelope.Fail(envelope.ExitInvalidTransition, "card_refused", err.Error())
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}

// runBoundaryCheck holds the checked-in card to the declaration: the
// card parses and is signed, its content is what the declaration
// renders, and, given the operator's public key, its signature
// verifies. A stale card is drift (exit 28).
func runBoundaryCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("boundary check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", "", "deployment declaration")
	cardPath := fs.String("card", DefaultCardPath, "the checked-in card")
	name := fs.String("name", "", "the deployment's name the card must carry")
	pub := fs.String("pubkey", "", "the operator's public key (hex), to verify the signature")
	pubFile := fs.String("pubkey-file", "", "a file holding that key")
	if err := fs.Parse(args); err != nil || *config == "" || *name == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "boundary check requires --config <file> --name <name> [--card <file>] [--pubkey <hex> | --pubkey-file <path>]"), stdout, stderr)
	}
	if env := refuseTwoKeys(*pub, *pubFile); env != nil {
		return render(env, stdout, stderr)
	}
	cfg, err := posture.Load(*config)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read the declaration: %v", err)), stdout, stderr)
	}
	raw, err := os.ReadFile(*cardPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitDrift, "card_drift", fmt.Sprintf("no card at %s: render one with boundary card", *cardPath)), stdout, stderr)
	}
	card, err := boundary.Parse(raw)
	if err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	want, err := boundary.Render(cfg, *name)
	if err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	got := *card
	got.Signer, got.Signature = "", ""
	gotCanon, err := got.Canonical()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot canonicalize the card at %s: %v", *cardPath, err)), stdout, stderr)
	}
	wantCanon, err := want.Canonical()
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("cannot canonicalize the rendered card: %v", err)), stdout, stderr)
	}
	if string(gotCanon) != string(wantCanon) {
		return render(envelope.Fail(envelope.ExitDrift, "card_drift", fmt.Sprintf("%s does not say what the declaration renders: re-render it with boundary card", *cardPath)), stdout, stderr)
	}
	// The gate this verb backs is a CONTENT gate unless a key is given
	// (plans/os-f11601e0.md D1): with no key it proves the card says
	// what the declaration renders, and nothing about who signed it.
	// The repository's own `make check` runs it that way deliberately,
	// because the tree holds no operator key to check against; a reader
	// supplies one out of band, through `boundary verify`.
	verified := *pub != "" || *pubFile != ""
	if verified {
		key, failEnv := readPublicKey(*pub, *pubFile)
		if failEnv != nil {
			return render(failEnv, stdout, stderr)
		}
		if err := boundary.Verify(card, key); err != nil {
			return render(boundaryEnvelope(err), stdout, stderr)
		}
	}
	result := map[string]any{"card": *cardPath, "name": card.Name, "signer": card.Signer, "verified": verified}
	if !verified {
		result["note"] = "content only: the card says what the declaration renders. No key was given, so nothing here checks who signed it; a reader supplies the operator key out of band and runs `seed boundary verify`."
	}
	return render(envelope.OK(result), stdout, stderr)
}

// boundaryService is the read-only surface: a clone (the ledger dir,
// re-read on every request so a refreshed clone is reflected), an
// artifact store, and the checked-in card. Nothing here writes.
type boundaryService struct {
	ledgerDir string
	artifacts *artifact.Store
	card      []byte
	bearer    string
}

var (
	taskRoute     = regexp.MustCompile(`^/tasks/([0-9]+)$`)
	artifactRoute = regexp.MustCompile(`^/artifacts/([0-9a-f]{64})$`)
)

func (s *boundaryService) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.route)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *boundaryService) refuse(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

// route is the pinned surface (boundary.Routes): three reads and no
// write, and a path outside them is not_found whatever it names —
// the ledger, the declaration, a lane — so the surface is no oracle
// for what exists.
func (s *boundaryService) route(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.refuse(w, http.StatusMethodNotAllowed, "usage", "the boundary is read-only: GET only")
		return
	}
	switch {
	case r.URL.Path == "/card":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.card)
	case r.URL.Path == "/tasks":
		tasks, env := s.tasks()
		if env != nil {
			s.refuse(w, http.StatusServiceUnavailable, env.Error.Code, env.Error.Message)
			return
		}
		writeJSON(w, http.StatusOK, tasks)
	case taskRoute.MatchString(r.URL.Path):
		n, _ := strconv.Atoi(taskRoute.FindStringSubmatch(r.URL.Path)[1])
		tasks, env := s.tasks()
		if env != nil {
			s.refuse(w, http.StatusServiceUnavailable, env.Error.Code, env.Error.Message)
			return
		}
		for _, t := range tasks {
			if t.Request == n {
				writeJSON(w, http.StatusOK, t)
				return
			}
		}
		s.refuse(w, http.StatusNotFound, "not_found", "no task at that request")
	case artifactRoute.MatchString(r.URL.Path):
		if s.bearer != "" && r.Header.Get("Authorization") != "Bearer "+s.bearer {
			s.refuse(w, http.StatusUnauthorized, "unauthorized", "artifacts need the bearer the card's operator gave out of band")
			return
		}
		digest := artifactRoute.FindStringSubmatch(r.URL.Path)[1]
		b, err := s.artifacts.Get(digest)
		if err != nil {
			s.refuse(w, http.StatusNotFound, "not_found", "no artifact under that digest")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(b)
	default:
		s.refuse(w, http.StatusNotFound, "not_found", "the boundary serves the card, the tasks and artifacts by digest, nothing else")
	}
}

func (s *boundaryService) tasks() ([]boundary.Task, *envelope.Envelope) {
	st, env := loadVerdictState(s.ledgerDir)
	if env != nil {
		return nil, env
	}
	return boundary.Tasks(st.records, st.fold), nil
}

func runBoundaryServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("boundary serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ledgerDir := fs.String("ledger", "", "the ledger clone the task states derive from (re-read per request)")
	artifacts := fs.String("artifacts", "", "the artifact store served by digest")
	cardPath := fs.String("card", DefaultCardPath, "the checked-in card served at /card")
	listen := fs.String("listen", "127.0.0.1:0", "address to listen on")
	announce := fs.String("announce", "", "file to write the bound endpoint to once listening")
	bearer := fs.String("bearer", "", "optional bearer token artifact reads must carry")
	if err := fs.Parse(args); err != nil || *ledgerDir == "" || *artifacts == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "boundary serve requires --ledger <dir> --artifacts <dir> [--card <file>] [--listen <addr>] [--announce <file>] [--bearer <token>]"), stdout, stderr)
	}
	raw, err := os.ReadFile(*cardPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read the card: %v", err)), stdout, stderr)
	}
	if _, err := boundary.Parse(raw); err != nil {
		return render(boundaryEnvelope(err), stdout, stderr)
	}
	if _, env := loadVerdictState(*ledgerDir); env != nil {
		return render(env, stdout, stderr)
	}
	svc := &boundaryService{ledgerDir: *ledgerDir, artifacts: artifact.Open(*artifacts), card: raw, bearer: *bearer}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("listen %s: %v", *listen, err)), stdout, stderr)
	}
	endpoint := "http://" + ln.Addr().String()
	if *announce != "" {
		if err := os.WriteFile(*announce, []byte(endpoint+"\n"), 0o644); err != nil {
			return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
		}
	}
	announced, _ := json.Marshal(map[string]any{"listening": endpoint, "routes": boundary.Routes})
	fmt.Fprintln(stdout, string(announced))
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
		fmt.Fprintf(stderr, "boundary serve: %v\n", err)
		return 1
	}
	return 0
}

func boundaryGet(remote, path, bearer string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(remote, "/")+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return b, resp.StatusCode, err
}

// pinned refuses a task object whose fields are not exactly the pin: a
// field the other side added is a deliberate change to a pinned list,
// and a stranger's reader does not silently learn a new word — nor
// does it read a view that dropped one.
func pinned(body []byte) *envelope.Envelope {
	fields, err := boundary.FieldsOf(body)
	if err != nil {
		return envelope.Fail(envelope.ExitInvalidTransition, "boundary_unpinned", "the task is not an object")
	}
	if strings.Join(fields, ",") != strings.Join(boundary.TaskFields, ",") {
		return envelope.Fail(envelope.ExitInvalidTransition, "boundary_unpinned", fmt.Sprintf("the task carries the fields %s; the pin is exactly %s", strings.Join(fields, ", "), strings.Join(boundary.TaskFields, ", ")))
	}
	return nil
}

// runBoundaryTasks reads the target's task states as a stranger:
// the pinned fields, refused if the other side says more.
func runBoundaryTasks(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("boundary tasks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remote := fs.String("remote", "", "the target's boundary endpoint")
	reqPos := fs.String("request", "", "one request's position (default: every task)")
	if err := fs.Parse(args); err != nil || *remote == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "boundary tasks requires --remote <endpoint> [--request <position>]"), stdout, stderr)
	}
	path := "/tasks"
	if *reqPos != "" {
		if _, err := strconv.Atoi(*reqPos); err != nil {
			return render(envelope.Fail(envelope.ExitUsage, "usage", "--request is a chain position"), stdout, stderr)
		}
		path += "/" + *reqPos
	}
	body, status, err := boundaryGet(*remote, path, "")
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if status == http.StatusNotFound {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", "no task at that request"), stdout, stderr)
	}
	if status != http.StatusOK {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("the boundary answered %d: %s", status, strings.TrimSpace(string(body)))), stdout, stderr)
	}
	var objects []json.RawMessage
	if *reqPos != "" {
		objects = []json.RawMessage{body}
	} else if err := json.Unmarshal(body, &objects); err != nil {
		return render(envelope.Fail(envelope.ExitInvalidTransition, "boundary_unpinned", "the tasks are not a list"), stdout, stderr)
	}
	tasks := []any{}
	for _, o := range objects {
		if env := pinned(o); env != nil {
			return render(env, stdout, stderr)
		}
		var t any
		_ = json.Unmarshal(o, &t)
		tasks = append(tasks, t)
	}
	return render(envelope.OK(map[string]any{"remote": *remote, "tasks": tasks}), stdout, stderr)
}

// runBoundaryFetch fetches one artifact by digest and stores it under
// that digest, verified on arrival.
func runBoundaryFetch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("boundary fetch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	remote := fs.String("remote", "", "the target's boundary endpoint")
	digest := fs.String("digest", "", "the artifact's sha256 digest")
	artifacts := fs.String("artifacts", "", "the local artifact store")
	bearer := fs.String("bearer", "", "the bearer the target's operator gave, when artifacts need one")
	if err := fs.Parse(args); err != nil || *remote == "" || *digest == "" || *artifacts == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "boundary fetch requires --remote <endpoint> --digest <sha256> --artifacts <dir> [--bearer <token>]"), stdout, stderr)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*digest) {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "artifacts cross by digest only: --digest is a lowercase-hex sha256, never a path or a name"), stdout, stderr)
	}
	body, status, err := boundaryGet(*remote, "/artifacts/"+*digest, *bearer)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error()), stdout, stderr)
	}
	if status == http.StatusNotFound {
		return render(envelope.Fail(envelope.ExitNotFound, "not_found", "the target holds no artifact under that digest"), stdout, stderr)
	}
	if status != http.StatusOK {
		return render(envelope.Fail(envelope.ExitUnavailable, "unavailable", fmt.Sprintf("the boundary answered %d: %s", status, strings.TrimSpace(string(body)))), stdout, stderr)
	}
	if err := artifact.Open(*artifacts).PutVerified(*digest, body); err != nil {
		return render(envelope.Fail(envelope.ExitInvalidTransition, "artifact_mismatch", err.Error()), stdout, stderr)
	}
	return render(envelope.OK(map[string]any{"digest": *digest, "bytes": fmt.Sprintf("%d", len(body)), "artifacts": *artifacts}), stdout, stderr)
}
