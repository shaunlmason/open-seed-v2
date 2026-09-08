package executor

// CloudSession is the cloud-agent-session adapter (plans/os-083112ac.md
// D1, D5): it opens a session at the declared endpoint, meters by polling
// the session's usage, and disposes by closing it. Its budget is a
// RISK LIMIT, never a guarantee — a provider may bill past the
// reservation before the close lands (D2). The bearer credential is the
// NAME of an environment variable, read at provision time; the
// declaration never holds a secret.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/obs"
)

// CloudHarness is the versioned name the cloud adapter resolves.
const CloudHarness = "cloud-session/v0"

// CloudSession opens sessions at Endpoint, authenticated by the token in
// $Credential (an env-var name), over HTTP.
type CloudSession struct {
	Endpoint   string
	Credential string
	Token      string // resolved from $Credential by the CLI; set directly in drills
	HTTP       *http.Client
}

func (c CloudSession) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Tuple reports the one field the adapter controls statically.
// Tuple reports the harness and, when the provider answers the runtime
// pre-flight (GET /runtime), the runtime a session will report, so the
// static report carries the environment a provision resolves to. A
// provider that does not answer leaves the field empty, and run start
// refuses by naming it rather than inventing a value.
func (c CloudSession) Tuple() Tuple {
	t := Tuple{Harness: CloudHarness}
	if c.Endpoint == "" {
		return t
	}
	var out struct {
		Runtime string `json:"runtime"`
	}
	if err := c.do(http.MethodGet, "/runtime", nil, &out); err == nil {
		t.Environment = out.Runtime
	}
	return t
}

// Wake is the documented no-op.
func (CloudSession) Wake(string) error { return nil }

// Describe reports the risk-limit posture.
func (CloudSession) Describe() Description {
	return Description{Name: "cloud-session", Harness: CloudHarness, Budget: BudgetRiskLimit,
		Reason: "a provider may bill past the reservation before the close lands, so the budget is a risk limit"}
}

type cloudSession struct {
	ID      string `json:"id"`
	Runtime string `json:"runtime"`
}

type cloudUsage struct {
	Units int `json:"units"`
}

func (c CloudSession) do(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cloud session %s %s: encoding the request: %w", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.Endpoint, "/")+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloud session %s %s: %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Provision verifies the admitted start, opens a session over the packet
// digest and base, and holds the resolved runtime to the admitted tuple.
func (c CloudSession) Provision(spec ProvisionSpec) (Run, error) {
	started, err := verifyStarted(spec)
	if err != nil {
		return nil, err
	}
	if c.Endpoint == "" {
		return nil, fmt.Errorf("the cloud-session adapter needs an endpoint")
	}
	digest := sha256.Sum256(spec.Packet)
	var sess cloudSession
	if err := c.do(http.MethodPost, "/sessions", map[string]string{
		"packet_digest": hex.EncodeToString(digest[:]),
		"base":          spec.Base,
	}, &sess); err != nil {
		return nil, fmt.Errorf("opening the cloud session: %w", err)
	}
	if sess.ID == "" || sess.Runtime == "" {
		return nil, fmt.Errorf("the cloud provider returned no session id or runtime")
	}
	resolve := func(declared Tuple) Tuple {
		out := declared
		out.Harness = CloudHarness
		out.Environment = sess.Runtime
		return out
	}
	var resolved Tuple
	if started.Tuple != nil {
		resolved = resolve(*started.Tuple)
		if field, have, want, differs := resolved.Diff(*started.Tuple); differs {
			_ = c.do(http.MethodDelete, "/sessions/"+sess.ID, nil, nil)
			return nil, fmt.Errorf("%w: %s resolved to %q, the admitted start declared %q", ErrTupleMismatch, field, have, want)
		}
	} else {
		resolved = resolve(Tuple{})
	}
	return &cloudRun{adapter: c, spec: spec, session: sess.ID, tuple: resolved}, nil
}

type cloudRun struct {
	adapter CloudSession
	spec    ProvisionSpec
	session string
	tuple   Tuple
}

// Workspace is the session reference; the work runs in the cloud, not on
// a local checkout.
func (r *cloudRun) Workspace() string { return "cloud-session:" + r.session }
func (r *cloudRun) Tuple() Tuple      { return r.tuple }

// Meter records the run's metered usage: the GREATER of the loop's
// estimate and the provider's reported usage. It never adds the two (a
// double count) and never takes only the estimate — the provider's poll
// is what it will bill, and a risk-limit budget must never under-report
// it. The figure is what the budget is settled against.
func (r *cloudRun) Meter(units int, step string) error {
	var u cloudUsage
	if err := r.adapter.do(http.MethodGet, "/sessions/"+r.session+"/usage", nil, &u); err != nil {
		return err
	}
	total := units
	if u.Units > total {
		total = u.Units
	}
	return obs.Append(r.spec.ObsDir, r.spec.Actor, obs.FormatFence(r.spec.Fence), obs.Line{
		TS: time.Now().UTC().Format(time.RFC3339), Subject: r.spec.Subject, Step: step, Units: total,
	})
}

// Dispose closes the session. A provider that cannot stop synchronously
// is why the budget is a risk limit, not a guarantee.
func (r *cloudRun) Dispose() error {
	return r.adapter.do(http.MethodDelete, "/sessions/"+r.session, nil, nil)
}
