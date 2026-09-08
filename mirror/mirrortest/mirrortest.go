// Package mirrortest holds the in-process fake forges the mirror
// conformance suite and the cross-component drills run against: a
// GitHub and a Forgejo issues API that record every request they
// serve, check the credential on each, and can be told to refuse the
// next write. No test contacts a real forge and no fixture carries a
// credential; the token the fakes accept is the literal "tok".
package mirrortest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Token is the credential every fake requires.
const Token = "tok"

// Issue is the fakes' own issue record, in forge-neutral form.
type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
	Closed bool
}

// Forge is the state and request log a fake keeps.
type Forge struct {
	mu     sync.Mutex
	next   int
	issues map[int]*Issue
	labels map[string]int64 // name to id (forgejo), or defined (github: id unused)
	// Calls is every request served, "METHOD path", in order.
	Calls []string
	// FailWrites makes every mutating request answer 500 while set.
	FailWrites bool
	// PullRequests is the number of pull requests the GitHub fake lists
	// among its issues (GitHub does), which an exporter must skip.
	PullRequests int
}

func newForge() *Forge {
	return &Forge{next: 1, issues: map[int]*Issue{}, labels: map[string]int64{}}
}

// Issues returns the fake's issues sorted by number.
func (f *Forge) Issues() []Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Issue, 0, len(f.issues))
	for _, i := range f.issues {
		c := *i
		c.Labels = append([]string{}, i.Labels...)
		out = append(out, c)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number < out[b].Number })
	return out
}

// Plant adds an issue directly, as a person at the forge would, and
// returns its number.
func (f *Forge) Plant(title, body string, labels []string, closed bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.next
	f.next++
	f.issues[n] = &Issue{Number: n, Title: title, Body: body, Labels: append([]string{}, labels...), Closed: closed}
	for _, l := range labels {
		f.define(l)
	}
	return n
}

// define registers a label as the repository would hold it, as a
// person's edit at the forge does.
func (f *Forge) define(name string) {
	if _, ok := f.labels[name]; !ok {
		f.labels[name] = int64(len(f.labels) + 1)
	}
}

// Defined lists the repository's labels, sorted.
func (f *Forge) Defined() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.labels))
	for n := range f.labels {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Edit changes an issue in place, as a person at the forge would.
func (f *Forge) Edit(n int, fn func(*Issue)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.issues[n]; ok {
		fn(i)
		for _, l := range i.Labels {
			f.define(l)
		}
	}
}

// Writes counts the mutating requests served.
func (f *Forge) Writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if !strings.HasPrefix(c, "GET ") {
			n++
		}
	}
	return n
}

func (f *Forge) record(r *http.Request) {
	f.Calls = append(f.Calls, r.Method+" "+r.URL.Path)
}

func page(r *http.Request, sizeKey string, def int) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get(sizeKey))
	if size < 1 {
		size = def
	}
	return p, size
}

func (f *Forge) sorted() []*Issue {
	out := make([]*Issue, 0, len(f.issues))
	for _, i := range f.issues {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number < out[b].Number })
	return out
}

// hexColor is the label color both forges accept: exactly six hex
// digits, no leading hash.
func hexColor(c string) bool {
	if len(c) != 6 {
		return false
	}
	for _, ch := range c {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return false
		}
	}
	return true
}

func decode(r *http.Request, into any) error {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// GitHub serves a GitHub-shaped issues API for owner/repo "o/r".
func GitHub() (*httptest.Server, *Forge) {
	f := newForge()
	mux := http.NewServeMux()
	ghIssue := func(i *Issue) map[string]any {
		labels := []map[string]any{}
		for _, l := range i.Labels {
			labels = append(labels, map[string]any{"name": l})
		}
		state := "open"
		if i.Closed {
			state = "closed"
		}
		return map[string]any{"number": i.Number, "title": i.Title, "body": i.Body, "state": state, "labels": labels}
	}
	// Labels are repository objects on GitHub: an issue may name only
	// a label that exists, and nothing but the label API defines one.
	mux.HandleFunc("/repos/o/r/labels", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if r.Header.Get("Authorization") != "Bearer "+Token {
			write(w, 401, map[string]any{"message": "bad credentials"})
			return
		}
		if r.Method != http.MethodPost {
			write(w, 405, nil)
			return
		}
		if f.FailWrites {
			write(w, 500, map[string]any{"message": "refused"})
			return
		}
		var in struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := decode(r, &in); err != nil || in.Name == "" || !hexColor(in.Color) {
			write(w, 422, map[string]any{"message": "a label needs a name and a six-digit hex color"})
			return
		}
		if _, dup := f.labels[in.Name]; dup {
			write(w, 422, map[string]any{"message": "label exists"})
			return
		}
		f.define(in.Name)
		write(w, 201, map[string]any{"name": in.Name, "color": in.Color})
	})
	mux.HandleFunc("/repos/o/r/labels/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if r.Header.Get("Authorization") != "Bearer "+Token {
			write(w, 401, map[string]any{"message": "bad credentials"})
			return
		}
		name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/repos/o/r/labels/"))
		if r.Method != http.MethodGet || err != nil {
			write(w, 405, nil)
			return
		}
		if _, ok := f.labels[name]; !ok {
			write(w, 404, map[string]any{"message": "not found"})
			return
		}
		write(w, 200, map[string]any{"name": name})
	})
	known := func(labels []string) bool {
		for _, l := range labels {
			if _, ok := f.labels[l]; !ok {
				return false
			}
		}
		return true
	}
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if r.Header.Get("Authorization") != "Bearer "+Token {
			write(w, 401, map[string]any{"message": "bad credentials"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			all := []map[string]any{}
			for k := 0; k < f.PullRequests; k++ {
				all = append(all, map[string]any{"number": 100000 + k, "title": "a pull request", "body": "", "state": "open", "labels": []any{}, "pull_request": map[string]any{"url": "x"}})
			}
			for _, i := range f.sorted() {
				all = append(all, ghIssue(i))
			}
			p, size := page(r, "per_page", 30)
			lo, hi := (p-1)*size, p*size
			if lo > len(all) {
				lo = len(all)
			}
			if hi > len(all) {
				hi = len(all)
			}
			write(w, 200, all[lo:hi])
		case http.MethodPost:
			if f.FailWrites {
				write(w, 500, map[string]any{"message": "refused"})
				return
			}
			var in struct {
				Title  string   `json:"title"`
				Body   string   `json:"body"`
				Labels []string `json:"labels"`
			}
			if err := decode(r, &in); err != nil {
				write(w, 422, map[string]any{"message": err.Error()})
				return
			}
			if !known(in.Labels) {
				write(w, 422, map[string]any{"message": "an issue may name only a label the repository defines"})
				return
			}
			n := f.next
			f.next++
			i := &Issue{Number: n, Title: in.Title, Body: in.Body, Labels: append([]string{}, in.Labels...)}
			f.issues[n] = i
			write(w, 201, ghIssue(i))
		default:
			write(w, 405, nil)
		}
	})
	mux.HandleFunc("/repos/o/r/issues/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if r.Header.Get("Authorization") != "Bearer "+Token {
			write(w, 401, map[string]any{"message": "bad credentials"})
			return
		}
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/"))
		i, ok := f.issues[n]
		if err != nil || !ok {
			write(w, 404, map[string]any{"message": "not found"})
			return
		}
		if r.Method != http.MethodPatch {
			write(w, 405, nil)
			return
		}
		if f.FailWrites {
			write(w, 500, map[string]any{"message": "refused"})
			return
		}
		var in map[string]json.RawMessage
		if err := decode(r, &in); err != nil {
			write(w, 422, map[string]any{"message": err.Error()})
			return
		}
		if v, ok := in["title"]; ok {
			_ = json.Unmarshal(v, &i.Title)
		}
		if v, ok := in["body"]; ok {
			_ = json.Unmarshal(v, &i.Body)
		}
		if v, ok := in["state"]; ok {
			var s string
			_ = json.Unmarshal(v, &s)
			i.Closed = s == "closed"
		}
		if v, ok := in["labels"]; ok {
			var ls []string
			_ = json.Unmarshal(v, &ls)
			if !known(ls) {
				write(w, 422, map[string]any{"message": "an issue may name only a label the repository defines"})
				return
			}
			i.Labels = ls
		}
		write(w, 200, ghIssue(i))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		write(w, 404, map[string]any{"message": fmt.Sprintf("no route %s %s", r.Method, r.URL.Path)})
	})
	return httptest.NewServer(mux), f
}

// Forgejo serves a Forgejo-shaped issues API for owner/repo "o/r",
// labels by id.
func Forgejo() (*httptest.Server, *Forge) {
	f := newForge()
	mux := http.NewServeMux()
	fjLabel := func(name string) map[string]any {
		return map[string]any{"id": f.labels[name], "name": name}
	}
	fjIssue := func(i *Issue) map[string]any {
		labels := []map[string]any{}
		for _, l := range i.Labels {
			labels = append(labels, fjLabel(l))
		}
		state := "open"
		if i.Closed {
			state = "closed"
		}
		return map[string]any{"number": i.Number, "title": i.Title, "body": i.Body, "state": state, "labels": labels}
	}
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "token "+Token {
			write(w, 401, map[string]any{"message": "bad credentials"})
			return false
		}
		return true
	}
	byID := func(id int64) (string, bool) {
		for name, lid := range f.labels {
			if lid == id {
				return name, true
			}
		}
		return "", false
	}
	mux.HandleFunc("/api/v1/repos/o/r/labels", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if !auth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			names := make([]string, 0, len(f.labels))
			for n := range f.labels {
				names = append(names, n)
			}
			sort.Strings(names)
			out := []map[string]any{}
			for _, n := range names {
				out = append(out, fjLabel(n))
			}
			p, size := page(r, "limit", 30)
			lo, hi := (p-1)*size, p*size
			if lo > len(out) {
				lo = len(out)
			}
			if hi > len(out) {
				hi = len(out)
			}
			write(w, 200, out[lo:hi])
		case http.MethodPost:
			if f.FailWrites {
				write(w, 500, map[string]any{"message": "refused"})
				return
			}
			var in struct {
				Name  string `json:"name"`
				Color string `json:"color"`
			}
			if err := decode(r, &in); err != nil || in.Name == "" || !hexColor(in.Color) {
				write(w, 422, map[string]any{"message": "a label needs a name and a six-digit hex color"})
				return
			}
			if _, dup := f.labels[in.Name]; dup {
				write(w, 422, map[string]any{"message": "label exists"})
				return
			}
			f.define(in.Name)
			write(w, 201, fjLabel(in.Name))
		default:
			write(w, 405, nil)
		}
	})
	mux.HandleFunc("/api/v1/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if !auth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			all := []map[string]any{}
			for _, i := range f.sorted() {
				all = append(all, fjIssue(i))
			}
			p, size := page(r, "limit", 30)
			lo, hi := (p-1)*size, p*size
			if lo > len(all) {
				lo = len(all)
			}
			if hi > len(all) {
				hi = len(all)
			}
			write(w, 200, all[lo:hi])
		case http.MethodPost:
			if f.FailWrites {
				write(w, 500, map[string]any{"message": "refused"})
				return
			}
			var in struct {
				Title  string  `json:"title"`
				Body   string  `json:"body"`
				Labels []int64 `json:"labels"`
				Closed bool    `json:"closed"`
			}
			if err := decode(r, &in); err != nil {
				write(w, 422, map[string]any{"message": err.Error()})
				return
			}
			labels := []string{}
			for _, id := range in.Labels {
				name, ok := byID(id)
				if !ok {
					write(w, 422, map[string]any{"message": fmt.Sprintf("no label %d", id)})
					return
				}
				labels = append(labels, name)
			}
			n := f.next
			f.next++
			i := &Issue{Number: n, Title: in.Title, Body: in.Body, Labels: labels, Closed: in.Closed}
			f.issues[n] = i
			write(w, 201, fjIssue(i))
		default:
			write(w, 405, nil)
		}
	})
	mux.HandleFunc("/api/v1/repos/o/r/issues/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		if !auth(w, r) {
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/o/r/issues/")
		idStr, sub, _ := strings.Cut(rest, "/")
		n, err := strconv.Atoi(idStr)
		i, ok := f.issues[n]
		if err != nil || !ok {
			write(w, 404, map[string]any{"message": "not found"})
			return
		}
		if f.FailWrites {
			write(w, 500, map[string]any{"message": "refused"})
			return
		}
		switch {
		case sub == "" && r.Method == http.MethodPatch:
			var in map[string]json.RawMessage
			if err := decode(r, &in); err != nil {
				write(w, 422, map[string]any{"message": err.Error()})
				return
			}
			if v, ok := in["title"]; ok {
				_ = json.Unmarshal(v, &i.Title)
			}
			if v, ok := in["body"]; ok {
				_ = json.Unmarshal(v, &i.Body)
			}
			if v, ok := in["state"]; ok {
				var s string
				_ = json.Unmarshal(v, &s)
				i.Closed = s == "closed"
			}
			write(w, 201, fjIssue(i))
		case sub == "labels" && r.Method == http.MethodPut:
			var in struct {
				Labels []int64 `json:"labels"`
			}
			if err := decode(r, &in); err != nil {
				write(w, 422, map[string]any{"message": err.Error()})
				return
			}
			labels := []string{}
			for _, id := range in.Labels {
				name, ok := byID(id)
				if !ok {
					write(w, 422, map[string]any{"message": fmt.Sprintf("no label %d", id)})
					return
				}
				labels = append(labels, name)
			}
			i.Labels = labels
			write(w, 200, []any{})
		default:
			write(w, 405, nil)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.record(r)
		write(w, 404, map[string]any{"message": fmt.Sprintf("no route %s %s", r.Method, r.URL.Path)})
	})
	return httptest.NewServer(mux), f
}
