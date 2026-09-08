package main

// The three adapters' full bracket, credential-free (plans/os-083112ac.md
// D4, D5, D6): each provisions against an admitted run.started through an
// in-process fake substrate (fakeoci, a fake cloud provider, an artifact
// store the fake worker reads), resolves its harness, and disposes. The
// local adapter's bracket is TestDisposabilityDrill; these prove the
// three share the admitted-start gate with no runtime and no credentials.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/executor"
	"github.com/shaunlmason/open-seed-v2/executor/fakeoci"
)

// admittedRunSpec builds a chain to an admitted run.started and returns
// the provision spec (its Tuple nil, so any adapter provisions it).
func admittedRunSpec(t *testing.T) executor.ProvisionSpec {
	t.Helper()
	ld, src, base, specCommit, _, priv, _, keys, fps := offerLedger(t)
	offerFile(t, ld, priv, specCommit, "c-1")
	fencePos, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	fence := strconv.Itoa(fencePos)
	e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["workerA"],
		"--verb", "budget.reserve", "--subject", "c-1", "--payload", `{"amount": "10", "fence": "`+fence+`"}`)
	if code != 0 {
		t.Fatalf("reserve: %d %+v", code, e)
	}
	reservation := *e.Position
	e, code = runEnv(t, "ledger", "append", "--ledger", ld, "--key", keys["supervisor"],
		"--verb", "run.started", "--subject", "c-1", "--payload", `{"fence": "`+fence+`", "reservation": "`+reservation+`"}`)
	if code != 0 {
		t.Fatalf("run.started: %d %+v", code, e)
	}
	started, _ := strconv.Atoi(*e.Position)
	return executor.ProvisionSpec{
		Ledger: ld, Repo: src, Base: base, Subject: "c-1", Actor: fps["workerA"],
		Fence: fencePos, Started: started, Packet: []byte(`{"drill": "packet"}`),
		ObsDir: filepath.Join(t.TempDir(), "obs"),
	}
}

func TestContainerAdapterFullBracketCredentialFree(t *testing.T) {
	spec := admittedRunSpec(t)
	rt := fakeoci.New()
	run, err := executor.Container{Runtime: rt, Image: "example/runner:1", Fake: true}.Provision(spec)
	if err != nil {
		t.Fatalf("container Provision: %v", err)
	}
	if got := run.Tuple(); got.Harness != executor.ContainerHarness || got.Environment != "fake-oci:"+fakeoci.Digest("example/runner:1") {
		t.Fatalf("the container resolves its harness and the fake digest: %+v", got)
	}
	if b, err := os.ReadFile(filepath.Join(run.Workspace(), ".seed-run", "packet.json")); err != nil || string(b) != `{"drill": "packet"}` {
		t.Fatalf("the packet lands in the workspace: %v %q", err, b)
	}
	if err := run.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if len(rt.Started) != 1 || len(rt.Stopped) != 1 || len(rt.Removed) != 1 {
		t.Fatalf("dispose stops and removes: start %v stop %v remove %v", rt.Started, rt.Stopped, rt.Removed)
	}
}

func TestCloudAdapterFullBracket(t *testing.T) {
	closed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/runtime":
			json.NewEncoder(w).Encode(map[string]string{"runtime": "provider-runtime/2"})
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			json.NewEncoder(w).Encode(map[string]string{"id": "s1", "runtime": "provider-runtime/2"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/usage"):
			json.NewEncoder(w).Encode(map[string]int{"units": 7})
		case r.Method == http.MethodDelete:
			closed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	spec := admittedRunSpec(t)
	adapter := executor.CloudSession{Endpoint: srv.URL, Credential: "X", Token: "tok"}
	if got := adapter.Tuple(); got.Environment != "provider-runtime/2" {
		t.Fatalf("the static report carries the provider's runtime before a session opens: %+v", got)
	}
	run, err := adapter.Provision(spec)
	if err != nil {
		t.Fatalf("cloud Provision: %v", err)
	}
	if got := run.Tuple(); got.Harness != executor.CloudHarness || got.Environment != "provider-runtime/2" {
		t.Fatalf("the cloud adapter resolves the provider runtime: %+v", got)
	}
	if err := run.Meter(3, "work"); err != nil {
		t.Fatalf("meter polls usage: %v", err)
	}
	// The recorded figure is the provider's poll (7), never the estimate
	// plus the poll (10): a risk-limit meter reports what the provider
	// will bill, and never double-counts.
	stream := filepath.Join(spec.ObsDir, spec.Actor, strconv.Itoa(spec.Fence)+".jsonl")
	if b, err := os.ReadFile(stream); err != nil || !strings.Contains(string(b), `"units":7`) || strings.Contains(string(b), `"units":10`) {
		t.Fatalf("the cloud meter records the provider's poll, not the sum: %v %q", err, b)
	}
	if err := run.Dispose(); err != nil || !closed {
		t.Fatalf("dispose closes the session: %v closed=%v", err, closed)
	}
}

func TestRemoteWorkerFullBracket(t *testing.T) {
	spec := admittedRunSpec(t)
	art := t.TempDir()
	run, err := executor.RemoteWorker{ArtifactDir: art, WorkerName: "worker-1", Environment: "enrolled/v0"}.Provision(spec)
	if err != nil {
		t.Fatalf("remote Provision: %v", err)
	}
	if got := run.Tuple(); got.Harness != executor.RemoteHarness || got.Environment != "enrolled/v0" {
		t.Fatalf("the remote adapter resolves the enrolled environment: %+v", got)
	}
	// The packet is in the store for the worker to pull; a pickup line is
	// on the stream. No connection was opened.
	stream := filepath.Join(spec.ObsDir, spec.Actor, strconv.Itoa(spec.Fence)+".jsonl")
	b, err := os.ReadFile(stream)
	if err != nil || !strings.Contains(string(b), "pickup packet=") {
		t.Fatalf("the pickup line must be on the stream: %v %q", err, b)
	}
	if err := run.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	b, _ = os.ReadFile(stream)
	if !strings.Contains(string(b), "disposed worker=worker-1") {
		t.Fatalf("dispose records the disposed line: %q", b)
	}
}
