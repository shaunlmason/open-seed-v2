package propose

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
)

func record(t *testing.T) *event.Record {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	fp, _ := event.Fingerprint(priv.Public().(ed25519.PublicKey))
	rec, err := event.Sign(event.Event{V: "seed/0", TS: "2026-09-01T00:00:00Z", Actor: fp, Verb: "message.sent", Subject: "c-1", Payload: json.RawMessage(`{"n": 1}`), Prev: event.EmptyHash}, priv)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// answer serves one canned answer and records the proposal it received.
func answer(t *testing.T, status int, body string) (*Client, *Proposal) {
	t.Helper()
	got := &Proposal{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(got); err != nil {
			t.Errorf("the proposal must be {ref, records}: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL + "/"), got
}

func fail(exit int, code, msg string, pos string) string {
	env := envelope.Fail(exit, code, msg)
	if pos != "" {
		env.Position = &pos
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// The status is transport and the envelope is the answer: 200 lands
// with position and commit, 409 is the race the loop retries, 422 is
// the boundary's own refusal verbatim, and everything else — including
// an answer that is not an envelope — is the service being unavailable.
func TestProposeMapsEveryAnswer(t *testing.T) {
	rec := record(t)
	ok := envelope.OK(map[string]any{"position": 3, "commit": "abc"})
	okBody, _ := json.Marshal(ok)
	c, got := answer(t, StatusAdmitted, string(okBody))
	res, err := c.Propose("refs/heads/seed-ledger", []*event.Record{rec})
	if err != nil || res.Position != 3 || res.Commit != "abc" {
		t.Fatalf("200 lands: %+v %v", res, err)
	}
	if got.Ref != "refs/heads/seed-ledger" || len(got.Records) != 1 || got.Records[0].Sig != rec.Sig {
		t.Fatalf("the proposal carries the ref and the records verbatim: %+v", got)
	}

	c, _ = answer(t, StatusMoved, fail(envelope.ExitContention, "non_fast_forward", "the ledger ref moved", "4"))
	_, err = c.Propose("r", []*event.Record{rec})
	if !errors.Is(err, gitref.ErrNonFastForward) || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("409 is the race: %v", err)
	}
	if _, isRefusal := IsRefusal(err); isRefusal {
		t.Fatal("a race is not a refusal")
	}

	c, _ = answer(t, StatusRefused, fail(envelope.ExitClassificationRef, "classification_refused", "too much prose", "7"))
	_, err = c.Propose("r", []*event.Record{rec})
	ref, isRefusal := IsRefusal(err)
	if !isRefusal || ref.Exit != 9 || ref.Code != "classification_refused" || ref.Message != "too much prose" || ref.Position == nil || *ref.Position != "7" || !errors.Is(err, gitref.ErrRemoteRejected) {
		t.Fatalf("422 is the boundary's envelope verbatim, unwrapping to the remote's rejection: %v", err)
	}

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"503":             {StatusUnavailable, fail(envelope.ExitUnavailable, "unavailable", "remote down", "")},
		"not an envelope": {StatusAdmitted, `<html>`},
		"wrong version":   {StatusAdmitted, `{"v": "other/1", "ok": true}`},
		"admitted no pos": {StatusAdmitted, `{"v": "` + envelope.V + `", "ok": true, "result": {}, "affordances": [], "exit": 0}`},
		"refused no code": {StatusRefused, `{"v": "` + envelope.V + `", "ok": false, "affordances": [], "exit": 8}`},
		"teapot":          {418, fail(envelope.ExitUsage, "usage", "no", "")},
	} {
		c, _ := answer(t, tc.status, tc.body)
		_, err := c.Propose("r", []*event.Record{rec})
		if !errors.Is(err, gitref.ErrUnavailable) {
			t.Errorf("%s must be unavailable, got %v", name, err)
		}
		if _, isRefusal := IsRefusal(err); isRefusal {
			t.Errorf("%s is not a refusal", name)
		}
	}

	dead := New("http://127.0.0.1:1")
	if _, err := dead.Propose("r", []*event.Record{rec}); !errors.Is(err, gitref.ErrUnavailable) || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("a dead endpoint is unavailable by name: %v", err)
	}
	if _, err := dead.Probe(); !errors.Is(err, gitref.ErrUnavailable) {
		t.Fatalf("a dead probe is unavailable: %v", err)
	}
}

// The probe decodes the health document and refuses anything else.
func TestProbe(t *testing.T) {
	pos := 5
	h, _ := json.Marshal(Health{Remote: "r", Ref: "refs/heads/seed-ledger", Position: &pos, Tip: "t"})
	c, _ := answer(t, http.StatusOK, string(h))
	got, err := c.Probe()
	if err != nil || got.Remote != "r" || got.Position == nil || *got.Position != 5 || got.Tip != "t" {
		t.Fatalf("the probe reads the health document: %+v %v", got, err)
	}
	c, _ = answer(t, http.StatusOK, `nope`)
	if _, err := c.Probe(); !errors.Is(err, gitref.ErrUnavailable) {
		t.Fatalf("a non-document probe answer is unavailable: %v", err)
	}
	c, _ = answer(t, http.StatusServiceUnavailable, `{}`)
	if _, err := c.Probe(); !errors.Is(err, gitref.ErrUnavailable) {
		t.Fatalf("a failing probe is unavailable: %v", err)
	}
}

func TestIntField(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want int
		ok   bool
	}{{float64(3), 3, true}, {"4", 4, true}, {json.Number("5"), 5, true}, {"x", 0, false}, {nil, 0, false}, {true, 0, false}} {
		got, ok := intField(map[string]any{"k": tc.v}, "k")
		if got != tc.want || ok != tc.ok {
			t.Errorf("%v: got %d %v", tc.v, got, ok)
		}
	}
}
