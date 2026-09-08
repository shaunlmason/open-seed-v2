package executor

// The static report every adapter hands run start: harness always, and
// the environment a provision will resolve to whenever the substrate
// can name it ahead of the start (spec/qualification.md, the
// review finding on the item 2 PR). A substrate that cannot leaves the
// field empty so the verb refuses by name rather than inventing one.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shaunlmason/open-seed-v2/executor/fakeoci"
)

func TestContainerStaticTupleNamesTheFakeDigest(t *testing.T) {
	c := Container{Runtime: fakeoci.New(), Image: "example/runner:1", Fake: true}
	if got := c.Tuple(); got.Harness != ContainerHarness || got.Environment != "fake-oci:"+fakeoci.Digest("example/runner:1") {
		t.Fatalf("the fake runtime names the digest ahead of the start: %+v", got)
	}
	if got := (Container{Runtime: fakeoci.New()}).Tuple(); got.Environment != "" {
		t.Fatalf("no image, no environment: %+v", got)
	}
}

func TestRemoteWorkerStaticTupleCarriesTheDeclaredEnvironment(t *testing.T) {
	if got := (RemoteWorker{WorkerName: "w1", Environment: "ubuntu-24.04/x86_64"}).Tuple(); got.Harness != RemoteHarness || got.Environment != "ubuntu-24.04/x86_64" {
		t.Fatalf("the worker's declared environment is the static report: %+v", got)
	}
}

func TestCloudSessionStaticTupleAsksTheProviderForItsRuntime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/runtime" {
			json.NewEncoder(w).Encode(map[string]string{"runtime": "provider-runtime/2"})
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	defer srv.Close()
	if got := (CloudSession{Endpoint: srv.URL, Token: "tok"}).Tuple(); got.Harness != CloudHarness || got.Environment != "provider-runtime/2" {
		t.Fatalf("the provider's runtime pre-flight fills the environment: %+v", got)
	}
	// A provider without the pre-flight, or no endpoint at all, leaves
	// the field empty rather than inventing a runtime.
	silent := httptest.NewServer(http.NotFoundHandler())
	defer silent.Close()
	if got := (CloudSession{Endpoint: silent.URL}).Tuple(); got.Environment != "" {
		t.Fatalf("a silent provider leaves the environment empty: %+v", got)
	}
	if got := (CloudSession{}).Tuple(); got.Environment != "" || got.Harness != CloudHarness {
		t.Fatalf("no endpoint, no pre-flight: %+v", got)
	}
}

func TestCloudSessionDoSurfacesAnUnencodableBody(t *testing.T) {
	c := CloudSession{Endpoint: "http://127.0.0.1:1"}
	if err := c.do(http.MethodPost, "/sessions", map[string]any{"bad": func() {}}, nil); err == nil {
		t.Fatal("a body that cannot be encoded is an error, never an empty request")
	}
}
