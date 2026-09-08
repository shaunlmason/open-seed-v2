package mirror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ForgejoTokenEnv is the environment variable the Forgejo exporter
// reads its token from when the config names none.
const ForgejoTokenEnv = "SEED_MIRROR_FORGEJO_TOKEN"

// forgejo mirrors into a Forgejo repository's issues. Forgejo names
// labels by id, so the exporter resolves (and on first use creates)
// the repository label for each name it sets: a variance in transport,
// not in the mirror contract the suite proves.
type forgejo struct {
	c      *forgeClient
	repo   string
	labels map[string]int64
}

func newForgejo(cfg Config) (Adapter, error) {
	if cfg.Owner == "" || cfg.Repo == "" {
		return nil, errors.New("the forgejo exporter needs --owner and --repo")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("the forgejo exporter needs --base-url")
	}
	env := cfg.TokenEnv
	if env == "" {
		env = ForgejoTokenEnv
	}
	tok, err := tokenFrom(env)
	if err != nil {
		return nil, err
	}
	return &forgejo{
		c:    &forgeClient{base: strings.TrimRight(cfg.BaseURL, "/"), token: tok, scheme: "token", accept: "application/json", http: newClient()},
		repo: "/api/v1/repos/" + cfg.Owner + "/" + cfg.Repo,
	}, nil
}

func (f *forgejo) Name() string { return "forgejo" }

type fjLabel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type fjIssue struct {
	Number int64     `json:"number"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	State  string    `json:"state"`
	Labels []fjLabel `json:"labels"`
}

func (i fjIssue) issue() Issue {
	labels := []string{}
	for _, l := range i.Labels {
		labels = append(labels, l.Name)
	}
	return Issue{ID: strconv.FormatInt(i.Number, 10), Title: i.Title, Body: i.Body, Labels: labels, Closed: i.State == "closed"}
}

func (f *forgejo) List(ctx context.Context) ([]Issue, error) {
	out := []Issue{}
	for page := 1; ; page++ {
		var got []fjIssue
		if _, err := f.c.do(ctx, http.MethodGet, fmt.Sprintf("%s/issues?state=all&type=issues&limit=50&page=%d", f.repo, page), nil, &got); err != nil {
			return nil, err
		}
		for _, i := range got {
			out = append(out, i.issue())
		}
		if len(got) < 50 {
			return out, nil
		}
	}
}

// loadLabels reads the repository's labels into the cache.
func (f *forgejo) loadLabels(ctx context.Context) error {
	f.labels = map[string]int64{}
	for page := 1; ; page++ {
		var got []fjLabel
		if _, err := f.c.do(ctx, http.MethodGet, fmt.Sprintf("%s/labels?limit=50&page=%d", f.repo, page), nil, &got); err != nil {
			return err
		}
		for _, l := range got {
			f.labels[l.Name] = l.ID
		}
		if len(got) < 50 {
			return nil
		}
	}
}

// labelIDs resolves label names to the repository's label ids,
// creating a label the repository lacks. A miss re-reads the
// repository first: a person may have defined the label since the
// cache was filled, and Forgejo refuses a duplicate.
func (f *forgejo) labelIDs(ctx context.Context, names []string) ([]int64, error) {
	if f.labels == nil {
		if err := f.loadLabels(ctx); err != nil {
			return nil, err
		}
	}
	ids := make([]int64, 0, len(names))
	for _, n := range names {
		id, ok := f.labels[n]
		if !ok {
			if err := f.loadLabels(ctx); err != nil {
				return nil, err
			}
			id, ok = f.labels[n]
		}
		if !ok {
			var made fjLabel
			if _, err := f.c.do(ctx, http.MethodPost, f.repo+"/labels", map[string]any{"name": n, "color": labelColor}, &made); err != nil {
				return nil, err
			}
			f.labels[n] = made.ID
			id = made.ID
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *forgejo) Create(ctx context.Context, d Desired) (Issue, error) {
	ids, err := f.labelIDs(ctx, []string{d.Label})
	if err != nil {
		return Issue{}, err
	}
	var got fjIssue
	body := map[string]any{"title": d.Title, "body": d.Body, "labels": ids, "closed": d.Closed}
	if _, err := f.c.do(ctx, http.MethodPost, f.repo+"/issues", body, &got); err != nil {
		return Issue{}, err
	}
	return got.issue(), nil
}

func (f *forgejo) Update(ctx context.Context, id string, d Desired, foreign []string) error {
	ids, err := f.labelIDs(ctx, append(append([]string{}, foreign...), d.Label))
	if err != nil {
		return err
	}
	state := "open"
	if d.Closed {
		state = "closed"
	}
	if _, err := f.c.do(ctx, http.MethodPatch, f.repo+"/issues/"+id, map[string]any{"title": d.Title, "body": d.Body, "state": state}, nil); err != nil {
		return err
	}
	// Forgejo replaces an issue's labels through its own endpoint.
	_, err = f.c.do(ctx, http.MethodPut, f.repo+"/issues/"+id+"/labels", map[string]any{"labels": ids}, nil)
	return err
}
