// Package propose is the client half of the forge-hosted admission
// protocol (plans/os-5c8a312c.md D2, D3): a proposal is signed records,
// already linked to the tip the proposer fetched, posted to the
// admission service, which answers with the very envelope the CLI would
// have printed. The client maps the answer onto gitref's typed outcomes
// so the append loop retries a race, surfaces a refusal with the
// boundary's own code, and treats everything else as the remote being
// unavailable.
package propose

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/gitref"
)

// Proposal is the wire body of POST /propose: the ref the records are
// for and the records in protocol.md's record encoding, in chain order.
type Proposal struct {
	Ref     string          `json:"ref"`
	Records []*event.Record `json:"records"`
}

// Health is the body of GET /healthz: what the service serves and the
// last tip it verified, for the doctor's probe.
type Health struct {
	Remote   string `json:"remote"`
	Ref      string `json:"ref"`
	Position *int   `json:"position"`
	Tip      string `json:"tip"`
}

// The HTTP status each envelope rides (spec/postures.md): the
// status is transport, the envelope is the answer, and a proposer that
// reads only the status still learns the one thing it must act on.
const (
	StatusAdmitted    = http.StatusOK
	StatusMoved       = http.StatusConflict
	StatusRefused     = http.StatusUnprocessableEntity
	StatusUnavailable = http.StatusServiceUnavailable
)

// MaxBody bounds a proposal and an answer: records are small by the
// classification lint's own bounds, and a body past this is not one.
const MaxBody = 1 << 20

// Client proposes to one service endpoint.
type Client struct {
	Endpoint string
	HTTP     *http.Client
}

// New returns a client for the endpoint (scheme and host, no path).
func New(endpoint string) *Client {
	return &Client{Endpoint: strings.TrimRight(endpoint, "/"), HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Propose posts the records and maps the answer: 200 admitted (the
// result carries the position and commit), 409 the ref moved
// (ErrNonFastForward, so AppendLoop re-links and retries), 422 refused
// (*gitref.Refusal with the service's envelope), anything else
// unavailable. Propose implements gitref.Proposer.
func (c *Client) Propose(ref string, recs []*event.Record) (*gitref.Result, error) {
	body, err := json.Marshal(Proposal{Ref: ref, Records: recs})
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Post(c.Endpoint+"/propose", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: proposing to %s: %v", gitref.ErrUnavailable, c.Endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the service's answer: %v", gitref.ErrUnavailable, err)
	}
	var env envelope.Envelope
	if jerr := json.Unmarshal(raw, &env); jerr != nil || env.V != envelope.V {
		return nil, fmt.Errorf("%w: the service answered %d with no envelope: %.200s", gitref.ErrUnavailable, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	message := ""
	code := ""
	if env.Error != nil {
		message, code = env.Error.Message, env.Error.Code
	}
	switch resp.StatusCode {
	case StatusAdmitted:
		pos, ok := intField(env.Result, "position")
		commit, _ := env.Result["commit"].(string)
		if !env.OK || !ok || commit == "" {
			return nil, fmt.Errorf("%w: the service admitted without naming a position and commit: %.200s", gitref.ErrUnavailable, strings.TrimSpace(string(raw)))
		}
		return &gitref.Result{Position: pos, Commit: commit}, nil
	case StatusMoved:
		return nil, fmt.Errorf("%w: %s", gitref.ErrNonFastForward, message)
	case StatusRefused:
		if code == "" {
			return nil, fmt.Errorf("%w: the service refused without a code: %.200s", gitref.ErrUnavailable, strings.TrimSpace(string(raw)))
		}
		return nil, &gitref.Refusal{Exit: env.Exit, Code: code, Message: message, Position: env.Position}
	default:
		return nil, fmt.Errorf("%w: the service answered %d %s: %s", gitref.ErrUnavailable, resp.StatusCode, code, message)
	}
}

// Probe fetches the service's health for the doctor.
func (c *Client) Probe() (*Health, error) {
	resp, err := c.HTTP.Get(c.Endpoint + "/healthz")
	if err != nil {
		return nil, fmt.Errorf("%w: probing %s: %v", gitref.ErrUnavailable, c.Endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", gitref.ErrUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: the service answered %d to the probe", gitref.ErrUnavailable, resp.StatusCode)
	}
	var h Health
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("%w: the probe answered with no health document: %v", gitref.ErrUnavailable, err)
	}
	return &h, nil
}

// intField reads a JSON number or numeric string out of a result map.
func intField(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	}
	return 0, false
}

// IsRefusal reports whether err is a service refusal and returns it.
func IsRefusal(err error) (*gitref.Refusal, bool) {
	var r *gitref.Refusal
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}
