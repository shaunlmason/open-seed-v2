package main

// conformance: spec/curation.md "Search": an advisory lexical read
// over the surfacing set, the standing dead ends and the named docs,
// ranked and never delivered; without a repository no lesson is
// indexed and every candidate is reported unresolved.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

func TestKnowledgeSearchIsAnAdvisoryRead(t *testing.T) {
	ld, src, _, _, _, priv, rootKey, keys, _ := offerLedger(t)
	for _, to := range []string{version.Seed2, version.Seed3} {
		if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
			"--verb", "system.protocol.upgraded", "--subject", "system", "--payload", `{"to": "`+to+`"}`); code != 0 {
			t.Fatalf("upgrade to %s: %d %+v", to, code, e)
		}
	}
	v := version.Seed3
	rawAppendAt(t, ld, rootKey, v, "intent.filed", "c-1", `{"intent": "drill", "tier": "trivial", "budget": "small", "routing": "core"}`)
	rawAppendAt(t, ld, rootKey, v, "contract.specified", "c-1", `{"acceptance": {"ref": "accept.md @ 0123456", "executable": false}}`)
	rawAppendAt(t, ld, workerRawKey(22), v, "claim.taken", "c-1", `{}`)
	e, code := runEnv(t, "knowledge", "deadend", "--ledger", ld, "--key", keys["workerA"], "--subject", "c-1",
		"--tried", "retrying the fetch", "--outcome", "the mirror timed out", "--condition", "the mirror was cold", "--environment", "ci-runner/v0")
	if code != 0 {
		t.Fatalf("the holder's dead end: %d %+v", code, e)
	}
	deadEndID := "c-1@" + *e.Position
	if err := os.MkdirAll(filepath.Join(src, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	notes := "# Notes\n\nnothing here\n\n## The mirror\n\nthe mirror is a cache; a cold mirror times out; warm the mirror first\n\n## Lockfiles\n\na lockfile cannot record its own commit\n"
	if err := os.WriteFile(filepath.Join(src, "docs", "notes.md"), []byte(notes), 0o644); err != nil {
		t.Fatal(err)
	}

	// Flags first, the query last: the flag package's shape, every
	// verb's.
	search := func(query string, args ...string) (ledgerEnv, int) {
		t.Helper()
		return runEnv(t, append(append([]string{"knowledge", "search"}, args...), strings.Fields(query)...)...)
	}
	e, code = search("cold mirror", "--ledger", ld, "--repo", src, "--doc", "docs/notes.md", "--now", "2026-10-01T00:00:00Z", "--limit", "5")
	if code != 0 {
		t.Fatalf("search: %d %+v", code, e.Error)
	}
	if e.Result["query"] != "cold mirror" || e.Result["as_of"] != "2026-10-01T00:00:00Z" || e.Result["advisory"] == nil {
		t.Fatalf("the envelope echoes the query and the instant and says it is advisory: %+v", e.Result)
	}
	counts, _ := e.Result["documents"].(map[string]any)
	if counts["lesson"] != 0.0 || counts["deadend"] != 1.0 || counts["doc"] != 3.0 {
		t.Fatalf("the corpus counts by kind: %+v", counts)
	}
	hits, _ := e.Result["hits"].([]any)
	if len(hits) != 2 {
		t.Fatalf("the dead end and the mirror section hit, the lockfile section and the preamble do not: %+v", hits)
	}
	first, _ := hits[0].(map[string]any)
	second, _ := hits[1].(map[string]any)
	if first["kind"] != "doc" || first["id"] != "docs/notes.md#the-mirror" || first["rank"] != 1.0 || second["kind"] != "deadend" || second["id"] != deadEndID || second["rank"] != 2.0 {
		t.Fatalf("the section saying mirror three times outranks the dead end saying it twice: %+v %+v", first, second)
	}
	if s, _ := first["score"].(float64); s <= second["score"].(float64) {
		t.Fatalf("scores descend: %+v %+v", first, second)
	}
	if matched, _ := second["matched"].([]any); len(matched) != 2 || second["snippet"] != "outcome: the mirror timed out" {
		t.Fatalf("a hit names what matched and shows the line it matched on: %+v", second)
	}
	if unresolved, _ := e.Result["lessons_unresolved"].([]any); len(unresolved) != 0 {
		t.Fatalf("no lesson stands, so nothing is unresolved: %+v", unresolved)
	}
	// One hit at limit 1, in the same order.
	e, code = search("cold mirror", "--ledger", ld, "--repo", src, "--doc", "docs/notes.md", "--limit", "1")
	if hits, _ := e.Result["hits"].([]any); code != 0 || len(hits) != 1 || hits[0].(map[string]any)["id"] != "docs/notes.md#the-mirror" {
		t.Fatalf("limit bounds the hits: %d %+v", code, e.Result)
	}
	// Every hit at limit 0; the wall clock when no instant is declared.
	e, code = search("mirror", "--ledger", ld)
	if hits, _ := e.Result["hits"].([]any); code != 0 || len(hits) != 1 || e.Result["as_of"] == "" {
		t.Fatalf("without --repo and --doc the corpus is the fold's dead ends: %d %+v", code, e.Result)
	}
	if counts, _ := e.Result["documents"].(map[string]any); counts["doc"] != 0.0 {
		t.Fatalf("no doc named, none indexed: %+v", counts)
	}
	for name, in := range map[string]struct {
		query string
		args  []string
	}{
		"no query":              {"", []string{"--ledger", ld}},
		"no searchable term":    {"a", []string{"--ledger", ld}},
		"both stores":           {"mirror", []string{"--ledger", ld, "--remote", src}},
		"doc without repo":      {"mirror", []string{"--ledger", ld, "--doc", "docs/notes.md"}},
		"doc climbing out":      {"mirror", []string{"--ledger", ld, "--repo", src, "--doc", "../notes.md"}},
		"doc missing":           {"mirror", []string{"--ledger", ld, "--repo", src, "--doc", "docs/absent.md"}},
		"negative limit":        {"mirror", []string{"--ledger", ld, "--limit", "-1"}},
		"instant not RFC3339":   {"mirror", []string{"--ledger", ld, "--now", "yesterday"}},
		"unknown knowledge sub": {},
	} {
		var got ledgerEnv
		var c int
		if name == "unknown knowledge sub" {
			got, c = runEnv(t, "knowledge", "find", "mirror")
		} else {
			got, c = search(in.query, in.args...)
		}
		if c != 64 || got.Error == nil || got.Error.Code != "usage" {
			t.Errorf("%s refuses at usage: %d %+v", name, c, got.Error)
		}
		if name == "unknown knowledge sub" && !strings.Contains(got.Error.Message, "search") {
			t.Errorf("the subverb list names search: %s", got.Error.Message)
		}
	}
	if got, c := runEnv(t, "knowledge"); c != 64 || got.Error == nil || !strings.Contains(got.Error.Message, "search") {
		t.Fatalf("the bare group names search: %d %+v", c, got.Error)
	}
}
