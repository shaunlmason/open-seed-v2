package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/boundary"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"golang.org/x/crypto/ssh"
)

func pubHexOf(t *testing.T, privPath string) string {
	t.Helper()
	raw, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := event.ParsePrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(priv.Public().(ed25519.PublicKey))
}

// conformance: plans/os-40ed0ca0.md AC1–AC5 — two organizations with
// distinct roots and no shared key: acme publishes its card, beta
// verifies it against acme's operator key, files a cross-repo request
// through acme's ingress with a key that holds nothing, watches the
// task move requested → accepted → working → done through acme's
// boundary reads as acme's lanes drive the contract, fetches the
// receipt by digest verified on arrival, and cites it in its own
// chain. Then the refusals, and opacity pinned route by route.
func TestTwoOrganizationsAcrossTheBoundary(t *testing.T) {
	ldA, rootA, dispatcherA, ingressB, claimerA := requestLedger(t)
	verifierA, vpub, vfp := writeWorkerKey(t, 34)
	appendA := func(key, verb, subject, payload string) string {
		t.Helper()
		e, code := runEnv(t, "ledger", "append", "--ledger", ldA, "--key", key, "--verb", verb, "--subject", subject, "--payload", payload)
		if code != 0 || e.Position == nil {
			t.Fatalf("%s: %d %+v %+v", verb, code, e, e.Error)
		}
		return *e.Position
	}
	appendA(rootA, "actor.enrolled", vfp, fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "verifier"}`, vpub))
	appendA(rootA, "actor.granted", vfp, `{"capability": "verdict"}`)
	dir := t.TempDir()
	cfgA := writeDeclaration(t, `{"posture": "cooperative", `+federationBase+`, "boundary": {"accepts": ["cross-repo"], "ingress": `+jsonString(ldA)+`}}`)
	cardPath := filepath.Join(dir, "card.json")
	// acme publishes its card.
	e, code := runEnv(t, "boundary", "card", "--config", cfgA, "--key", rootA, "--name", "acme", "--out", cardPath)
	if code != 0 || !e.OK || e.Result["signer"] == nil {
		t.Fatalf("the card renders and signs: %d %+v", code, e)
	}
	cardBytes, _ := os.ReadFile(cardPath)
	// beta verifies it against the operator key it was given out of band.
	if e, code := runEnv(t, "boundary", "check", "--config", cfgA, "--name", "acme", "--card", cardPath, "--pubkey", pubHexOf(t, rootA)); code != 0 || !e.OK || e.Result["verified"] != true {
		t.Fatalf("the card verifies against acme's operator key: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "check", "--config", cfgA, "--name", "acme", "--card", cardPath, "--pubkey", pubHexOf(t, dispatcherA)); code != 3 || e.Error == nil || e.Error.Code != "card_refused" {
		t.Fatalf("another key does not verify it: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "check", "--config", cfgA, "--name", "acme-2", "--card", cardPath); code != 28 || e.Error == nil || e.Error.Code != "card_drift" {
		t.Fatalf("a card that does not say what the declaration renders is drift: %d %+v", code, e)
	}
	// acme's artifact store holds the receipt its lanes will publish.
	artA := filepath.Join(dir, "art-a")
	receipt := []byte("receipt: the checks passed\n")
	digest, err := artifact.Open(artA).Put(receipt)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&boundaryService{ledgerDir: ldA, artifacts: artifact.Open(artA), card: cardBytes}).handler())
	defer srv.Close()
	// beta files the proposal through acme's ingress; a kind the card
	// does not accept refuses at the ingress too.
	if e, code := runEnv(t, "request", "file", "--ledger", ldA, "--key", ingressB, "--config", cfgA, "--subject", "system", "--origin", "beta", "--kind", "mirror-edit", "--reference", "beta/c-9 @ 0123456", "--summary", "s"); code != 3 || e.Error == nil || e.Error.Code != "request_refused" || !strings.Contains(e.Error.Message, "card accepts") {
		t.Fatalf("a kind the card refuses: %d %+v", code, e)
	}
	e, code = runEnv(t, "request", "file", "--ledger", ldA, "--key", ingressB, "--config", cfgA, "--subject", "system", "--origin", "beta", "--kind", "cross-repo", "--reference", "beta/c-9 @ "+strings.Repeat("b", 40), "--summary", "beta proposes shared work")
	if code != 0 || !e.OK {
		t.Fatalf("the cross-repo request enters: %d %+v", code, e)
	}
	reqPos := e.Result["request"].(string)
	tasks := func() []map[string]any {
		t.Helper()
		e, code := runEnv(t, "boundary", "tasks", "--remote", srv.URL)
		if code != 0 || !e.OK {
			t.Fatalf("boundary tasks: %d %+v", code, e)
		}
		rows, _ := e.Result["tasks"].([]any)
		out := []map[string]any{}
		for _, r := range rows {
			out = append(out, r.(map[string]any))
		}
		return out
	}
	// Opacity is checked at EVERY state, not only at the end: a claim
	// holder rides the view only while the claim stands, so a sweep
	// taken after settlement alone would miss it (a mutation that
	// appended the claimant to the artifacts survived exactly that).
	// The taboo list is recomputed from the chain as it grows.
	get := func(path string) ([]byte, int) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		b := make([]byte, 1<<16)
		for {
			n, err := resp.Body.Read(b)
			buf.Write(b[:n])
			if err != nil {
				break
			}
		}
		return []byte(buf.String()), resp.StatusCode
	}
	opaque := func() {
		t.Helper()
		st, env := loadVerdictState(ldA)
		if env != nil {
			t.Fatal(env)
		}
		taboo := []string{ldA}
		for _, r := range st.records {
			taboo = append(taboo, r.Event.Actor)
			if len(r.Event.Payload) > 4 {
				taboo = append(taboo, string(r.Event.Payload))
			}
		}
		for _, path := range []string{"/tasks", "/tasks/" + reqPos} {
			body, status := get(path)
			if status != 200 {
				t.Fatalf("%s: %d %s", path, status, body)
			}
			var objects []json.RawMessage
			if path == "/tasks" {
				if err := json.Unmarshal(body, &objects); err != nil {
					t.Fatalf("%s is a list: %v", path, err)
				}
			} else {
				objects = []json.RawMessage{body}
			}
			for _, o := range objects {
				fields, err := boundary.FieldsOf(o)
				if err != nil || strings.Join(fields, ",") != strings.Join(boundary.TaskFields, ",") {
					t.Fatalf("%s carries the pinned fields only: %v %v", path, fields, err)
				}
			}
			if err := boundary.Sweep(body, taboo); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
		}
	}
	state := func(want string) {
		t.Helper()
		rows := tasks()
		if len(rows) != 1 || rows[0]["state"] != want {
			t.Fatalf("the task is %s: %+v", want, rows)
		}
		opaque()
	}
	state("requested")
	// acme's lanes drive the contract; beta watches the state alone.
	intent := appendA(dispatcherA, "intent.filed", "c-2", `{"intent": "shared work from beta", "tier": "trivial", "budget": "small", "routing": "core"}`)
	if e, code := runEnv(t, "request", "answer", "--ledger", ldA, "--key", dispatcherA, "--subject", "system", "--request", reqPos, "--outcome", "filed", "--intent", intent); code != 0 {
		t.Fatalf("the answer: %d %+v", code, e)
	}
	state("accepted")
	appendA(rootA, "contract.specified", "c-2", `{"acceptance": {"ref": "specs/c2.md @ abc1234", "executable": false}}`)
	// Claiming is online-only at the CLI; acme's worker claims through
	// the library against its own ledger, the offer drills' way.
	fencePos, err := admitAppend(t, ldA, workerRawKey(33), "claim.taken", "c-2", `{}`)
	if err != nil {
		t.Fatalf("acme's worker claims: %v", err)
	}
	fence := fmt.Sprintf("%d", fencePos)
	state("working")
	packet := `{"acceptance": ["resume"], "decisions": [], "base": "1234567..1234567", "refs": [], "findings": []}`
	sub := appendA(claimerA, "submission.made", "c-2", `{"fence": "`+fence+`", "packet": `+packet+`}`)
	verdict := appendA(verifierA, "verdict.rendered", "c-2", `{"verdict": "pass", "receipt": "`+digest+`", "submission": "`+sub+`", "independence": "L1"}`)
	appendA(claimerA, "merge.requested", "c-2", `{"verdict": "`+verdict+`"}`)
	appendA(rootA, "merge.observed", "c-2", `{"merged": "`+strings.Repeat("0", 40)+`", "pr": "pr/1"}`)
	state("done")
	rows := tasks()
	arts, _ := rows[0]["artifacts"].([]any)
	if len(arts) != 1 || arts[0] != digest {
		t.Fatalf("the receipt is published by digest: %+v", rows)
	}
	if e, code := runEnv(t, "boundary", "tasks", "--remote", srv.URL, "--request", reqPos); code != 0 || len(e.Result["tasks"].([]any)) != 1 {
		t.Fatalf("one task by its request: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "tasks", "--remote", srv.URL, "--request", "999"); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("no task at a request nobody filed: %d %+v", code, e)
	}
	// beta fetches the receipt by digest, verified on arrival, and
	// cites it in its own chain.
	artB := filepath.Join(dir, "art-b")
	if e, code := runEnv(t, "boundary", "fetch", "--remote", srv.URL, "--digest", digest, "--artifacts", artB); code != 0 || !e.OK {
		t.Fatalf("the fetch: %d %+v", code, e)
	}
	if got, err := artifact.Open(artB).Get(digest); err != nil || string(got) != string(receipt) {
		t.Fatalf("stored under the same digest: %v", err)
	}
	if e, code := runEnv(t, "boundary", "fetch", "--remote", srv.URL, "--digest", "receipts/os-1.json", "--artifacts", artB); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("by path refuses: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "fetch", "--remote", srv.URL, "--digest", strings.Repeat("c", 64), "--artifacts", artB); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("a digest the store lacks: %d %+v", code, e)
	}
	ldB, _, _, ingressA, _ := requestLedger(t)
	if e, code := runEnv(t, "request", "file", "--ledger", ldB, "--key", ingressA, "--subject", "system", "--origin", "acme", "--kind", "cross-repo", "--reference", digest, "--summary", "acme's receipt for the shared work"); code != 0 || !e.OK {
		t.Fatalf("beta cites the receipt by digest in its own chain: %d %+v", code, e)
	}
	// The card and the rest of the surface, route by route: the pinned
	// fields and nothing from the ledger — no fingerprint, no payload,
	// no packet, no path.
	st, env := loadVerdictState(ldA)
	if env != nil {
		t.Fatal(env)
	}
	taboo := []string{ldA, packet}
	for _, r := range st.records {
		taboo = append(taboo, r.Event.Actor)
		if len(r.Event.Payload) > 4 {
			taboo = append(taboo, string(r.Event.Payload))
		}
	}
	for _, route := range []struct {
		path   string
		fields []string
	}{
		{"/card", boundary.CardFields},
		{"/tasks/" + reqPos, boundary.TaskFields},
	} {
		body, status := get(route.path)
		if status != 200 {
			t.Fatalf("%s: %d %s", route.path, status, body)
		}
		fields, err := boundary.FieldsOf(body)
		if err != nil || strings.Join(fields, ",") != strings.Join(route.fields, ",") {
			t.Fatalf("%s carries the pinned fields only: %v %v", route.path, fields, err)
		}
		// The card's signer is the operator's fingerprint by design;
		// nothing else on the chain rides any response.
		sweep := taboo
		if route.path == "/card" {
			sweep = []string{}
			for _, s := range taboo {
				if !strings.Contains(cardBytes2(cardBytes), s) {
					sweep = append(sweep, s)
				}
			}
		}
		if err := boundary.Sweep(body, sweep); err != nil {
			t.Fatalf("%s: %v", route.path, err)
		}
	}
	if body, status := get("/tasks"); status != 200 || boundary.Sweep(body, taboo) != nil {
		t.Fatalf("/tasks: %d %s", status, body)
	}
	if body, status := get("/artifacts/" + digest); status != 200 || string(body) != string(receipt) {
		t.Fatalf("/artifacts: %d %s", status, body)
	}
	for _, path := range []string{"/ledger", "/segments/000001.jsonl", "/seed.json", "/lanes/dispatcher.json", "/tasks/abc", "/artifacts/receipts/os-1.json", "/"} {
		if body, status := get(path); status != 404 || !strings.Contains(string(body), "not_found") {
			t.Fatalf("%s is not a route: %d %s", path, status, body)
		}
	}
	if resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader("{}")); err != nil || resp.StatusCode != 405 {
		t.Fatalf("the boundary is read-only: %v", err)
	}
	// The keyring vocabulary gained nothing cross-org.
	for _, verb := range []string{"request.filed", "request.answered", "intent.filed", "claim.taken"} {
		for _, c := range keyring.AcceptedCapabilities(verb) {
			if strings.Contains(c, "cross") || strings.Contains(c, "org") {
				t.Fatalf("%s accepts a cross-org capability: %s", verb, c)
			}
		}
	}
}

func cardBytes2(b []byte) string { return string(b) }

// conformance: plans/os-40ed0ca0.md AC4, AC6 — a reader refuses a task
// view carrying a field the pin does not list, the surface refuses to
// start on an unsigned card or one naming a manifest, artifacts behind
// a bearer need it, and the serve and the reads refuse their usage.
func TestBoundaryRefusals(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/tasks" {
			_, _ = w.Write([]byte(`[{"request": 1, "answer": null, "state": "requested", "artifacts": [], "actor": "0123"}]`))
			return
		}
		if r.URL.Path == "/tasks/2" {
			_, _ = w.Write([]byte(`{"request": 2, "answer": null, "state": "requested"}`))
			return
		}
		_, _ = w.Write([]byte(`{"request": 1, "answer": null, "state": "requested", "artifacts": [], "fence": "3"}`))
	}))
	defer fake.Close()
	if e, code := runEnv(t, "boundary", "tasks", "--remote", fake.URL); code != 3 || e.Error == nil || e.Error.Code != "boundary_unpinned" || !strings.Contains(e.Error.Message, "actor") {
		t.Fatalf("a field the pin does not list: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "tasks", "--remote", fake.URL, "--request", "1"); code != 3 || e.Error == nil || e.Error.Code != "boundary_unpinned" {
		t.Fatalf("one task with an unpinned field: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "tasks", "--remote", fake.URL, "--request", "2"); code != 3 || e.Error == nil || e.Error.Code != "boundary_unpinned" {
		t.Fatalf("a task missing a pinned field: %d %+v", code, e)
	}
	ld, root, _, _, _ := requestLedger(t)
	dir := t.TempDir()
	cfg := writeDeclaration(t, `{"posture": "cooperative", `+federationBase+`, "boundary": {"accepts": ["cross-repo"], "ingress": `+jsonString(ld)+`}}`)
	card := filepath.Join(dir, "card.json")
	if _, code := runEnv(t, "boundary", "card", "--config", cfg, "--key", root, "--name", "acme", "--out", card); code != 0 {
		t.Fatal("the card")
	}
	raw, _ := os.ReadFile(card)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	write := func(name string, mutate func(map[string]any)) string {
		c := map[string]any{}
		for k, v := range doc {
			c[k] = v
		}
		mutate(c)
		b, _ := json.Marshal(c)
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, b, 0o644)
		return p
	}
	unsigned := write("unsigned.json", func(c map[string]any) { c["signature"] = "" })
	manifest := write("manifest.json", func(c map[string]any) { c["name"] = "acme (lane manifest: lanes/dispatcher.json)" })
	for name, path := range map[string]string{"unsigned": unsigned, "naming a manifest": manifest} {
		if e, code := runEnv(t, "boundary", "serve", "--ledger", ld, "--artifacts", dir, "--card", path); code != 3 || e.Error == nil || e.Error.Code != "card_refused" {
			t.Fatalf("the surface refuses a card %s: %d %+v", name, code, e)
		}
		if e, code := runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", path); code != 3 || e.Error == nil || e.Error.Code != "card_refused" {
			t.Fatalf("check refuses a card %s: %d %+v", name, code, e)
		}
	}
	if e, code := runEnv(t, "boundary", "serve", "--ledger", ld); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("serve usage: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "card", "--config", cfg); code != 64 {
		t.Fatalf("card usage: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "wander"); code != 64 {
		t.Fatalf("an unknown subverb: %d %+v", code, e)
	}
	// Artifacts behind a bearer.
	art := filepath.Join(dir, "art")
	digest, _ := artifact.Open(art).Put([]byte("plan\n"))
	srv := httptest.NewServer((&boundaryService{ledgerDir: ld, artifacts: artifact.Open(art), card: raw, bearer: "s3cret"}).handler())
	defer srv.Close()
	out := filepath.Join(dir, "out")
	if e, code := runEnv(t, "boundary", "fetch", "--remote", srv.URL, "--digest", digest, "--artifacts", out); code != 5 || e.Error == nil || !strings.Contains(e.Error.Message, "401") {
		t.Fatalf("without the bearer: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "fetch", "--remote", srv.URL, "--digest", digest, "--artifacts", out, "--bearer", "s3cret"); code != 0 || !e.OK {
		t.Fatalf("with the bearer: %d %+v", code, e)
	}
	// The card and the tasks need no bearer.
	if resp, err := http.Get(srv.URL + "/card"); err != nil || resp.StatusCode != 200 {
		t.Fatal("the card is credential-free")
	}
	if resp, err := http.Get(srv.URL + "/tasks"); err != nil || resp.StatusCode != 200 {
		t.Fatal("the task states are credential-free")
	}
}

// conformance: plans/os-f11601e0.md AC3, AC4 — the reader's half of the
// boundary. A stranger holds a card it fetched and, out of band, the
// operator key that is supposed to have signed it; `boundary verify`
// checks one against the other with no declaration and no name,
// because a stranger has neither. Every way the pair can fail to match
// is a boundary refusal, which is what makes it distinct from
// `card_drift`: drift is the publisher's finding that its own card is
// stale, and a reader is not in a position to make it.
func TestBoundaryVerifyReadsACardWithNoDeclaration(t *testing.T) {
	ld, root, dispatcher, _, _ := requestLedger(t)
	dir := t.TempDir()
	cfg := writeDeclaration(t, `{"posture": "cooperative", `+federationBase+`, "boundary": {"accepts": ["cross-repo"], "ingress": `+jsonString(ld)+`}}`)
	card := filepath.Join(dir, "card.json")
	rendered, code := runEnv(t, "boundary", "card", "--config", cfg, "--key", root, "--name", "acme", "--out", card)
	if code != 0 || !rendered.OK {
		t.Fatalf("the card renders: %d %+v", code, rendered)
	}
	signer, _ := rendered.Result["signer"].(string)
	operator := pubHexOf(t, root)

	// AC3: the key alone reads the card, and the verdict names who signed it.
	e, code := runEnv(t, "boundary", "verify", "--card", card, "--pubkey", operator)
	if code != 0 || !e.OK || e.Result["verified"] != true {
		t.Fatalf("the operator key verifies the card: %d %+v", code, e)
	}
	if e.Result["signer"] != signer || e.Result["name"] != "acme" {
		t.Fatalf("the verdict reports the signer and the name: %+v", e.Result)
	}
	// The same key out of a file, which is how one arrives out of band.
	keyFile := filepath.Join(dir, "acme.pub")
	if err := os.WriteFile(keyFile, []byte(operator+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "boundary", "verify", "--card", card, "--pubkey-file", keyFile); code != 0 || !e.OK || e.Result["verified"] != true {
		t.Fatalf("the key from a file verifies the card: %d %+v", code, e)
	}
	// The key arrives in whatever form the operator holds it: the
	// OpenSSH authorized-keys line ssh-keygen -y writes is accepted
	// wherever the hex is (spec/protocol.md "Algorithms").
	sshFile := filepath.Join(dir, "acme.pub")
	rawKey, err := hex.DecodeString(operator)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := ssh.NewPublicKey(ed25519.PublicKey(rawKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sshFile, ssh.MarshalAuthorizedKey(sshKey), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, code := runEnv(t, "boundary", "verify", "--card", card, "--pubkey-file", sshFile); code != 0 || !e.OK || e.Result["verified"] != true {
		t.Fatalf("an OpenSSH public key file verifies the card: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", card, "--pubkey-file", sshFile); code != 0 || e.Result["verified"] != true {
		t.Fatalf("check reads the same key file: %d %+v", code, e)
	}
	// Two ways to name one key is a usage error, not a precedence rule,
	// and it is refused before anything is read: a bad invocation that
	// also names a missing declaration comes back as usage, never as
	// drift.
	if e, code := runEnv(t, "boundary", "verify", "--card", card, "--pubkey", operator, "--pubkey-file", keyFile); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("--pubkey and --pubkey-file together: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", card, "--pubkey", operator, "--pubkey-file", keyFile); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("check refuses two keys: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", filepath.Join(dir, "gone.json"), "--pubkey", operator, "--pubkey-file", keyFile); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("two keys outrank a card that is not there: %d %+v", code, e)
	}
	// No key at all is a usage error: verify without one would check nothing.
	if e, code := runEnv(t, "boundary", "verify", "--card", card); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("verify without a key: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "verify", "--card", card, "--pubkey", "not-a-key"); code != 64 || e.Error == nil || e.Error.Code != "usage" {
		t.Fatalf("a key that is not an ed25519 public key in hex: %d %+v", code, e)
	}
	if e, code := runEnv(t, "boundary", "verify", "--card", filepath.Join(dir, "absent.json"), "--pubkey", operator); code != 4 || e.Error == nil || e.Error.Code != "not_found" {
		t.Fatalf("no card at that path: %d %+v", code, e)
	}

	// AC4: the four ways the signature can fail, each a boundary refusal.
	raw, err := os.ReadFile(card)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	write := func(name string, mutate func(map[string]any)) string {
		c := map[string]any{}
		for k, v := range doc {
			c[k] = v
		}
		mutate(c)
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sig, _ := doc["signature"].(string)
	truncated := write("truncated.json", func(c map[string]any) { c["signature"] = sig[:len(sig)-2] })
	absent := write("absent-signature.json", func(c map[string]any) { c["signature"] = "" })
	edited := write("edited.json", func(c map[string]any) { c["name"] = "acme-evil" })
	for name, args := range map[string][]string{
		"a key that signed nothing here": {"--card", card, "--pubkey", pubHexOf(t, dispatcher)},
		"a truncated signature":          {"--card", truncated, "--pubkey", operator},
		"no signature at all":            {"--card", absent, "--pubkey", operator},
		"a field edited after signing":   {"--card", edited, "--pubkey", operator},
	} {
		e, code := runEnv(t, append([]string{"boundary", "verify"}, args...)...)
		if code != 3 || e.Error == nil || e.Error.Code != "card_refused" {
			t.Fatalf("%s refuses at the boundary: %d %+v", name, code, e)
		}
	}
	// The publisher's verb reads the edited card as drift, because it
	// has the declaration to compare against. The reader's verb cannot,
	// and says so with the refusal instead: the two are not the same
	// finding wearing different exit codes.
	if e, code := runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", edited); code != 28 || e.Error == nil || e.Error.Code != "card_drift" {
		t.Fatalf("the publisher reads the edited card as drift: %d %+v", code, e)
	}
	// AC5: the content gate says what it is. Passing with no key is not
	// a signature check, and the envelope does not let a reader mistake
	// it for one.
	e, code = runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", card)
	if code != 0 || !e.OK || e.Result["verified"] != false {
		t.Fatalf("the keyless gate passes on content: %d %+v", code, e)
	}
	note, _ := e.Result["note"].(string)
	if !strings.Contains(note, "content only") || !strings.Contains(note, "boundary verify") {
		t.Fatalf("the keyless gate names itself a content gate: %q", note)
	}
	if e, code := runEnv(t, "boundary", "check", "--config", cfg, "--name", "acme", "--card", card, "--pubkey", operator); code != 0 || e.Result["verified"] != true || e.Result["note"] != nil {
		t.Fatalf("with a key it is a signature gate and drops the note: %d %+v", code, e)
	}
}
