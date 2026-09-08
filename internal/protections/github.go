package protections

// The GitHub adapter over the REST rulesets API (plans/os-5c8a312c.md
// D6): net/http and a token from the environment, nothing else. Bypass
// identities are the canonical actor forms the API needs and nothing
// the core has to resolve: `app:<id>` (an installed GitHub App),
// `team:<id>`, `deploy-key`, `org-admin`.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GitHub reconciles one repository's rulesets.
type GitHub struct {
	Base  string // https://api.github.com, or a test server
	Owner string
	Repo  string
	Token string
	HTTP  *http.Client

	ids map[string]int64 // ruleset name -> id, from the last Read
}

// NewGitHub returns an adapter; an empty base means api.github.com.
func NewGitHub(base, owner, repo, token string) *GitHub {
	if base == "" {
		base = "https://api.github.com"
	}
	return &GitHub{Base: strings.TrimRight(base, "/"), Owner: owner, Repo: repo, Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// ErrIdentityForm names the accepted bypass identity forms.
var ErrIdentityForm = errors.New("a GitHub bypass identity is app:<id>, team:<id>, deploy-key or org-admin")

type ghActor struct {
	ActorID    *int64 `json:"actor_id,omitempty"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode"`
}

func actorFor(identity string) (ghActor, error) {
	switch {
	case identity == "deploy-key":
		return ghActor{ActorType: "DeployKey", BypassMode: "always"}, nil
	case identity == "org-admin":
		id := int64(1)
		return ghActor{ActorID: &id, ActorType: "OrganizationAdmin", BypassMode: "always"}, nil
	case strings.HasPrefix(identity, "app:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(identity, "app:"), 10, 64)
		if err != nil {
			return ghActor{}, fmt.Errorf("%w: got %q", ErrIdentityForm, identity)
		}
		return ghActor{ActorID: &id, ActorType: "Integration", BypassMode: "always"}, nil
	case strings.HasPrefix(identity, "team:"):
		id, err := strconv.ParseInt(strings.TrimPrefix(identity, "team:"), 10, 64)
		if err != nil {
			return ghActor{}, fmt.Errorf("%w: got %q", ErrIdentityForm, identity)
		}
		return ghActor{ActorID: &id, ActorType: "Team", BypassMode: "always"}, nil
	}
	return ghActor{}, fmt.Errorf("%w: got %q", ErrIdentityForm, identity)
}

func identityFor(a ghActor) string {
	switch a.ActorType {
	case "DeployKey":
		return "deploy-key"
	case "OrganizationAdmin":
		return "org-admin"
	case "Integration":
		if a.ActorID != nil {
			return "app:" + strconv.FormatInt(*a.ActorID, 10)
		}
	case "Team":
		if a.ActorID != nil {
			return "team:" + strconv.FormatInt(*a.ActorID, 10)
		}
	}
	return a.ActorType
}

type ghRule struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

type ghRuleset struct {
	ID           int64     `json:"id,omitempty"`
	Name         string    `json:"name"`
	Target       string    `json:"target"`
	Enforcement  string    `json:"enforcement"`
	BypassActors []ghActor `json:"bypass_actors"`
	Conditions   struct {
		RefName struct {
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules []ghRule `json:"rules"`
}

func (g *GitHub) do(method, path string, body any, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, g.Base+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("github %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("github %s %s: %d: %.300s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("github %s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

func (g *GitHub) repoPath() string { return "/repos/" + g.Owner + "/" + g.Repo }

// Read lists the repository's default branch and its Seed-named
// rulesets (foreign rulesets are left alone by the diff, so they are
// not read).
func (g *GitHub) Read() (*State, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.do(http.MethodGet, g.repoPath(), nil, &repo); err != nil {
		return nil, err
	}
	var listing []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if _, err := g.do(http.MethodGet, g.repoPath()+"/rulesets?per_page=100", nil, &listing); err != nil {
		return nil, err
	}
	st := &State{DefaultBranch: repo.DefaultBranch, Rulesets: map[string]Ruleset{}}
	g.ids = map[string]int64{}
	for _, l := range listing {
		if !strings.HasPrefix(l.Name, "seed-") {
			continue
		}
		var full ghRuleset
		if _, err := g.do(http.MethodGet, g.repoPath()+"/rulesets/"+strconv.FormatInt(l.ID, 10), nil, &full); err != nil {
			return nil, err
		}
		g.ids[l.Name] = l.ID
		st.Rulesets[l.Name] = fromGitHub(full)
	}
	return st, nil
}

func fromGitHub(r ghRuleset) Ruleset {
	out := Ruleset{Name: r.Name, Target: r.Target, Refs: append([]string{}, r.Conditions.RefName.Include...)}
	for _, a := range r.BypassActors {
		out.Bypass = append(out.Bypass, identityFor(a))
	}
	for _, rule := range r.Rules {
		switch rule.Type {
		case RuleDeletion, RuleNonFastForward, RuleUpdate:
			out.Rules = append(out.Rules, Rule{Type: rule.Type})
		case RulePullRequest:
			var p struct {
				Count      int  `json:"required_approving_review_count"`
				Threads    bool `json:"required_review_thread_resolution"`
				CodeOwners bool `json:"require_code_owner_review"`
			}
			_ = json.Unmarshal(rule.Parameters, &p)
			out.Rules = append(out.Rules, Rule{Type: RulePullRequest, Params: map[string]any{
				"required_approving_review_count":   p.Count,
				"required_review_thread_resolution": p.Threads,
				"require_code_owner_review":         p.CodeOwners,
			}})
		case RuleStatusChecks:
			var p struct {
				Checks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			}
			_ = json.Unmarshal(rule.Parameters, &p)
			contexts := make([]string, 0, len(p.Checks))
			for _, c := range p.Checks {
				contexts = append(contexts, c.Context)
			}
			sort.Strings(contexts)
			out.Rules = append(out.Rules, Rule{Type: RuleStatusChecks, Params: map[string]any{"contexts": contexts}})
		default:
			out.Rules = append(out.Rules, Rule{Type: rule.Type})
		}
	}
	return out
}

func toGitHub(r Ruleset) (ghRuleset, error) {
	out := ghRuleset{Name: r.Name, Target: r.Target, Enforcement: "active", BypassActors: []ghActor{}, Rules: []ghRule{}}
	out.Conditions.RefName.Include = append([]string{}, r.Refs...)
	out.Conditions.RefName.Exclude = []string{}
	for _, id := range r.Bypass {
		a, err := actorFor(id)
		if err != nil {
			return out, err
		}
		out.BypassActors = append(out.BypassActors, a)
	}
	for _, rule := range r.Rules {
		switch rule.Type {
		case RuleDeletion, RuleNonFastForward:
			out.Rules = append(out.Rules, ghRule{Type: rule.Type})
		case RuleUpdate:
			p, _ := json.Marshal(map[string]any{"update_allows_fetch_and_merge": false})
			out.Rules = append(out.Rules, ghRule{Type: rule.Type, Parameters: p})
		case RulePullRequest:
			count, _ := rule.Params["required_approving_review_count"].(int)
			if f, ok := rule.Params["required_approving_review_count"].(float64); ok {
				count = int(f)
			}
			threads, _ := rule.Params["required_review_thread_resolution"].(bool)
			owners, _ := rule.Params["require_code_owner_review"].(bool)
			p, _ := json.Marshal(map[string]any{
				"required_approving_review_count":   count,
				"dismiss_stale_reviews_on_push":     false,
				"require_code_owner_review":         owners,
				"require_last_push_approval":        false,
				"required_review_thread_resolution": threads,
			})
			out.Rules = append(out.Rules, ghRule{Type: rule.Type, Parameters: p})
		case RuleStatusChecks:
			var contexts []string
			switch v := rule.Params["contexts"].(type) {
			case []string:
				contexts = v
			case []any:
				for _, c := range v {
					if s, ok := c.(string); ok {
						contexts = append(contexts, s)
					}
				}
			}
			checks := make([]map[string]any, 0, len(contexts))
			for _, c := range contexts {
				checks = append(checks, map[string]any{"context": c})
			}
			p, _ := json.Marshal(map[string]any{"strict_required_status_checks_policy": false, "required_status_checks": checks})
			out.Rules = append(out.Rules, ghRule{Type: rule.Type, Parameters: p})
		default:
			return out, fmt.Errorf("rule %s has no GitHub translation", rule.Type)
		}
	}
	return out, nil
}

// Apply performs the changes: create by POST, update by PUT on the id
// the last Read found, delete by DELETE; manual changes are the
// operator's and are skipped by name.
func (g *GitHub) Apply(changes []Change, desired *State) error {
	if g.ids == nil {
		if _, err := g.Read(); err != nil {
			return err
		}
	}
	for _, c := range changes {
		switch c.Kind {
		case ChangeCreate:
			body, err := toGitHub(desired.Rulesets[c.Ruleset])
			if err != nil {
				return err
			}
			var created ghRuleset
			if _, err := g.do(http.MethodPost, g.repoPath()+"/rulesets", body, &created); err != nil {
				return err
			}
			g.ids[c.Ruleset] = created.ID
		case ChangeUpdate:
			id, ok := g.ids[c.Ruleset]
			if !ok {
				return fmt.Errorf("ruleset %s has no id from the last read", c.Ruleset)
			}
			body, err := toGitHub(desired.Rulesets[c.Ruleset])
			if err != nil {
				return err
			}
			if _, err := g.do(http.MethodPut, g.repoPath()+"/rulesets/"+strconv.FormatInt(id, 10), body, nil); err != nil {
				return err
			}
		case ChangeDelete:
			id, ok := g.ids[c.Ruleset]
			if !ok {
				return fmt.Errorf("ruleset %s has no id from the last read", c.Ruleset)
			}
			if _, err := g.do(http.MethodDelete, g.repoPath()+"/rulesets/"+strconv.FormatInt(id, 10), nil, nil); err != nil {
				return err
			}
			delete(g.ids, c.Ruleset)
		case ChangeManual:
		}
	}
	return nil
}
