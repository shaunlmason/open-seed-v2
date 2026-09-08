package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// requestTimeout bounds every forge call: a forge that accepts the
// connection and never answers must not hang an apply.
const requestTimeout = 60 * time.Second

func newClient() *http.Client { return &http.Client{Timeout: requestTimeout} }

// forgeClient is the HTTP seam the two forge exporters share: a base
// URL, the token read once from the named environment variable, and
// the header scheme the forge expects. The credential lives only here,
// in memory, and is never echoed into an error, a plan or an issue.
type forgeClient struct {
	base   string
	token  string
	scheme string // "Bearer" for GitHub, "token" for Forgejo
	accept string
	http   *http.Client
}

func tokenFrom(env string) (string, error) {
	tok := os.Getenv(env)
	if tok == "" {
		return "", fmt.Errorf("no credential: %s is unset", env)
	}
	return tok, nil
}

func (c *forgeClient) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", c.accept)
	if c.scheme == "Bearer" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	if c.token != "" {
		req.Header.Set("Authorization", c.scheme+" "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d: %.300s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode, nil
}
