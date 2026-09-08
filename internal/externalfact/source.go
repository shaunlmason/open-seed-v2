package externalfact

// The read-only sources (plans/os-b45c308d.md D6; spec/external-facts.md
// "Sources"): how an observer reads an external authority before
// recording what it said. A source answers a pull request's merge
// state and the forge's word on its head, forge-neutral, and has no
// other method: no apply, merge, rerun, cancel, label or protection.
// The forge sources issue HTTP GET alone, with one named variance:
// GitHub exposes review-thread resolution only through GraphQL, so
// the GitHub source posts one read-only query to /graphql, and the
// helper refuses any operation that is not a query. The observation
// component holds the least credential that reads; the mutating
// protections adapters live in another package, which this one does
// not import (the observation-control lint pins both).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// requestTimeout bounds every forge read: a forge that accepts the
// connection and never answers must not hang an observer or an
// unattended maintenance pass.
const requestTimeout = 60 * time.Second

func newClient() *http.Client { return &http.Client{Timeout: requestTimeout} }

// Source is the read-only forge interface every observer reads
// through. It never writes.
type Source interface {
	Merged(pr string) (sha string, merged bool, err error)
	Checks(pr string) (Observation, error)
}

// Observation is what the forge says about a pull request's head:
// forge-neutral, and exactly what check.observed carries
// (spec/observations-forge.md).
type Observation struct {
	// Head is the pull request's current head commit, a full sha.
	Head string
	// Checks is green, red or pending: every check run completed
	// passing; any completed otherwise; else something still running.
	// A pull request the forge lists no checks for is green: nothing
	// failed and nothing is running, the v1 engine's gate posture.
	Checks string
	// UnresolvedThreads is the count of review threads not marked
	// resolved, nil where the forge cannot say.
	UnresolvedThreads *int
	// Review is approved, changes_requested or none: the latest
	// review state per reviewer, reduced.
	Review string
}

// PRNumber extracts the numeric id from a pr reference ("pr/12" or
// "12"): the merge.observed ref grammar, shared with submission.made's
// pr field and check.observed.
func PRNumber(pr string) (string, error) {
	n := strings.TrimPrefix(pr, "pr/")
	if n == "" {
		return "", fmt.Errorf("pr %q names no pull request", pr)
	}
	if _, err := strconv.Atoi(n); err != nil {
		return "", fmt.Errorf("pr %q must be a number or pr/<n>", pr)
	}
	return n, nil
}

// prState is the merge-relevant subset both forges' pull-request
// objects carry.
type prState struct {
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
}

// prHead is the head-bearing subset both forges' pull-request objects
// carry beside the merge state.
type prHead struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// reader is the GET-only HTTP seam the two forge sources share.
type reader struct {
	base   string
	token  string
	scheme string
	accept string
	http   *http.Client
}

func (c *reader) client() *http.Client {
	if c.http != nil {
		return c.http
	}
	return http.DefaultClient
}

// get issues one GET and decodes the JSON answer. It is the only
// method on the seam that takes a path, and it takes no method: a
// source cannot write through it.
func (c *reader) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	return c.send(req, out)
}

func (c *reader) send(req *http.Request, out any) error {
	req.Header.Set("Accept", c.accept)
	if c.scheme == "Bearer" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	if c.token != "" {
		req.Header.Set("Authorization", c.scheme+" "+c.token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d: %.300s", req.Method, req.URL.Path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s %s: %v", req.Method, req.URL.Path, err)
		}
	}
	return nil
}

// GitHub reads one repository's pull requests. An empty base means
// api.github.com.
type GitHub struct {
	c    *reader
	repo string
}

// NewGitHub returns a GitHub source.
func NewGitHub(base, owner, repo, token string) *GitHub {
	if base == "" {
		base = "https://api.github.com"
	}
	return &GitHub{c: &reader{base: strings.TrimRight(base, "/"), token: token, scheme: "Bearer", accept: "application/vnd.github+json", http: newClient()}, repo: "/repos/" + owner + "/" + repo}
}

// Merged reads a GitHub pull request's merge state.
func (g *GitHub) Merged(pr string) (string, bool, error) {
	n, err := PRNumber(pr)
	if err != nil {
		return "", false, err
	}
	var st prState
	if err := g.c.get(g.repo+"/pulls/"+n, &st); err != nil {
		return "", false, err
	}
	return st.MergeCommitSHA, st.Merged, nil
}

// Checks reads a GitHub pull request's head, check runs, review
// threads and reviews.
func (g *GitHub) Checks(pr string) (Observation, error) {
	n, err := PRNumber(pr)
	if err != nil {
		return Observation{}, err
	}
	var head prHead
	if err := g.c.get(g.repo+"/pulls/"+n, &head); err != nil {
		return Observation{}, err
	}
	if head.Head.SHA == "" {
		return Observation{}, fmt.Errorf("pull request %s names no head", pr)
	}
	var runs struct {
		CheckRuns []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := g.c.get(g.repo+"/commits/"+head.Head.SHA+"/check-runs?per_page=100", &runs); err != nil {
		return Observation{}, err
	}
	checks := "green"
	for _, c := range runs.CheckRuns {
		if c.Status != "completed" {
			if checks == "green" {
				checks = "pending"
			}
			continue
		}
		switch c.Conclusion {
		case "success", "neutral", "skipped":
		default:
			checks = "red"
		}
	}
	threads, err := g.unresolvedThreads(n)
	if err != nil {
		return Observation{}, err
	}
	var reviews []struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		State string `json:"state"`
	}
	if err := g.c.get(g.repo+"/pulls/"+n+"/reviews?per_page=100", &reviews); err != nil {
		return Observation{}, err
	}
	latest := map[string]string{}
	for _, r := range reviews {
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED":
			latest[r.User.Login] = r.State
		}
	}
	return Observation{Head: head.Head.SHA, Checks: checks, UnresolvedThreads: &threads, Review: reduceReviews(latest, "CHANGES_REQUESTED", "APPROVED")}, nil
}

// threadsQuery is the one GraphQL operation the source sends: a query,
// paged, over a pull request's review threads' resolution.
const threadsQuery = `query($o:String!,$n:String!,$pr:Int!,$after:String){repository(owner:$o,name:$n){pullRequest(number:$pr){reviewThreads(first:100,after:$after){pageInfo{hasNextPage endCursor}nodes{isResolved}}}}}`

// graphql posts one read-only GraphQL query: the named variance from
// GET-only, because GitHub exposes thread resolution nowhere else. It
// refuses an operation that is not a query, so the seam cannot carry
// a mutation whatever a caller passes.
func (g *GitHub) graphql(query string, variables map[string]any, out any) error {
	if !strings.HasPrefix(strings.TrimSpace(query), "query") {
		return fmt.Errorf("the observation source sends queries only: %.20q is not one", query)
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, g.c.base+"/graphql", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return g.c.send(req, out)
}

// unresolvedThreads counts the pull request's review threads not
// marked resolved, paging past the first hundred.
func (g *GitHub) unresolvedThreads(n string) (int, error) {
	owner, repo, _ := strings.Cut(strings.TrimPrefix(g.repo, "/repos/"), "/")
	number, err := strconv.Atoi(n)
	if err != nil {
		return 0, fmt.Errorf("pr %q is not a number", n)
	}
	unresolved := 0
	var after *string
	for {
		var resp struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						ReviewThreads struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								IsResolved bool `json:"isResolved"`
							} `json:"nodes"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := g.graphql(threadsQuery, map[string]any{"o": owner, "n": repo, "pr": number, "after": after}, &resp); err != nil {
			return 0, err
		}
		if len(resp.Errors) > 0 {
			return 0, fmt.Errorf("github graphql: %s", resp.Errors[0].Message)
		}
		page := resp.Data.Repository.PullRequest.ReviewThreads
		for _, node := range page.Nodes {
			if !node.IsResolved {
				unresolved++
			}
		}
		if !page.PageInfo.HasNextPage {
			return unresolved, nil
		}
		cursor := page.PageInfo.EndCursor
		after = &cursor
	}
}

// Forgejo reads one repository's pull requests on a Forgejo instance.
type Forgejo struct {
	c    *reader
	repo string
}

// NewForgejo returns a Forgejo source; base is the instance URL.
func NewForgejo(base, owner, repo, token string) *Forgejo {
	return &Forgejo{c: &reader{base: strings.TrimRight(base, "/"), token: token, scheme: "token", accept: "application/json", http: newClient()}, repo: "/api/v1/repos/" + owner + "/" + repo}
}

// Merged reads a Forgejo pull request's merge state.
func (f *Forgejo) Merged(pr string) (string, bool, error) {
	n, err := PRNumber(pr)
	if err != nil {
		return "", false, err
	}
	var st prState
	if err := f.c.get(f.repo+"/pulls/"+n, &st); err != nil {
		return "", false, err
	}
	return st.MergeCommitSHA, st.Merged, nil
}

// Checks reads a Forgejo pull request's head, combined commit status
// and reviews. Forgejo has no thread resolution, so the thread count
// is nil: what the forge cannot say is named, never dropped.
func (f *Forgejo) Checks(pr string) (Observation, error) {
	n, err := PRNumber(pr)
	if err != nil {
		return Observation{}, err
	}
	var head prHead
	if err := f.c.get(f.repo+"/pulls/"+n, &head); err != nil {
		return Observation{}, err
	}
	if head.Head.SHA == "" {
		return Observation{}, fmt.Errorf("pull request %s names no head", pr)
	}
	var status struct {
		State string `json:"state"`
	}
	if err := f.c.get(f.repo+"/commits/"+head.Head.SHA+"/status", &status); err != nil {
		return Observation{}, err
	}
	checks := "green"
	switch strings.ToLower(status.State) {
	case "failure", "error":
		checks = "red"
	case "pending":
		checks = "pending"
	}
	var reviews []struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		State string `json:"state"`
	}
	if err := f.c.get(f.repo+"/pulls/"+n+"/reviews", &reviews); err != nil {
		return Observation{}, err
	}
	latest := map[string]string{}
	for _, r := range reviews {
		switch r.State {
		case "APPROVED", "REQUEST_CHANGES":
			latest[r.User.Login] = r.State
		}
	}
	return Observation{Head: head.Head.SHA, Checks: checks, Review: reduceReviews(latest, "REQUEST_CHANGES", "APPROVED")}, nil
}

// reduceReviews folds the latest review state per reviewer into the
// observation's literal: any reviewer still requesting changes wins,
// else any approval, else none.
func reduceReviews(latest map[string]string, changes, approved string) string {
	review := "none"
	for _, state := range latest {
		if state == changes {
			return "changes_requested"
		}
		if state == approved {
			review = "approved"
		}
	}
	return review
}

// Snapshot answers from a JSON file, the credential-free arm the
// drills use: {"pulls": {"pr/1": {"merged": true, "merge_commit_sha":
// "<sha>", "head": "<sha>", "checks": "red", "unresolved_threads": 1,
// "review": "none"}}}.
type Snapshot struct{ Path string }

func (s Snapshot) load(out any) error {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return fmt.Errorf("reading the pull-request snapshot: %w", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("the pull-request snapshot does not parse: %w", err)
	}
	return nil
}

// Merged reads the pull request's state from the snapshot.
func (s Snapshot) Merged(pr string) (string, bool, error) {
	var doc struct {
		Pulls map[string]prState `json:"pulls"`
	}
	if err := s.load(&doc); err != nil {
		return "", false, err
	}
	st, ok := doc.Pulls[pr]
	if !ok {
		return "", false, fmt.Errorf("the snapshot names no pull request %q", pr)
	}
	return st.MergeCommitSHA, st.Merged, nil
}

// Checks reads the pull request's observation from the snapshot.
func (s Snapshot) Checks(pr string) (Observation, error) {
	var doc struct {
		Pulls map[string]struct {
			Head    string `json:"head"`
			Checks  string `json:"checks"`
			Threads *int   `json:"unresolved_threads"`
			Review  string `json:"review"`
		} `json:"pulls"`
	}
	if err := s.load(&doc); err != nil {
		return Observation{}, err
	}
	st, ok := doc.Pulls[pr]
	if !ok {
		return Observation{}, fmt.Errorf("the snapshot names no pull request %q", pr)
	}
	if st.Head == "" || st.Checks == "" {
		return Observation{}, fmt.Errorf("the snapshot's pull request %q carries no head or checks", pr)
	}
	review := st.Review
	if review == "" {
		review = "none"
	}
	return Observation{Head: st.Head, Checks: st.Checks, UnresolvedThreads: st.Threads, Review: review}, nil
}

// Open constructs the source a `--forge` flag names, the token read
// from the environment variable tokenEnv (never from a flag or a
// file): snapshot from a file, github and forgejo over HTTP.
func Open(kind, repo, api, tokenEnv, snapshot string) (Source, error) {
	switch kind {
	case "snapshot":
		if snapshot == "" {
			return nil, fmt.Errorf("the snapshot forge needs --snapshot <file>")
		}
		return Snapshot{Path: snapshot}, nil
	case "github", "forgejo":
		owner, name, ok := strings.Cut(repo, "/")
		if !ok || owner == "" || name == "" {
			return nil, fmt.Errorf("the %s forge needs --github <owner/name>", kind)
		}
		token := os.Getenv(tokenEnv)
		if token == "" {
			return nil, &NoCredential{Kind: kind, Env: tokenEnv}
		}
		if kind == "forgejo" {
			if api == "" {
				return nil, fmt.Errorf("the forgejo forge needs its instance URL in --api")
			}
			return NewForgejo(api, owner, name, token), nil
		}
		return NewGitHub(api, owner, name, token), nil
	}
	return nil, fmt.Errorf("unknown forge %q (snapshot | github | forgejo)", kind)
}

// NoCredential is a forge named with no token in its environment
// variable: unavailable rather than a usage error, and the message
// names the variable, never a value.
type NoCredential struct{ Kind, Env string }

func (e *NoCredential) Error() string {
	return fmt.Sprintf("the %s forge needs a token in $%s", e.Kind, e.Env)
}
