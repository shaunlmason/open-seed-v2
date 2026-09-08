package main

// The promotion packet's proposed declaration is held to the tree
// (plans/os-4fde2bdf.md D4): the JSON block under "The deployment" is
// what the operator copies to seed.json at the cutover, so it is
// linted here the way the fixture deployment's declaration is linted
// under make check-next, and a stale block is found in CI rather than
// at the flip.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// packetRootPlaceholder is the one value in the packet's block the
// operator substitutes: the fingerprint of the root key. No fingerprint
// equals it, so the block as printed cannot initialize a ledger, which
// is what the drills below hold it to.
const packetRootPlaceholder = "<the root key's fingerprint>"

// packetDeclaration extracts the proposed declaration from the packet.
func packetDeclaration(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "promotion.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "**The deployment.**")
	if i < 0 {
		t.Fatal("the packet has no deployment paragraph")
	}
	rest := doc[i:]
	start := strings.Index(rest, "```json\n")
	if start < 0 {
		t.Fatal("the deployment paragraph carries no json block")
	}
	rest = rest[start+len("```json\n"):]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatal("the json block is unterminated")
	}
	return rest[:end]
}

// conformance: build plan section 5 criterion 5 (the cutover written
// down) — the declaration the packet proposes passes seed preseed
// check against the shipped lanes: tiers in the vocabulary, teams
// naming shipped manifests, the protected surface complete, the
// protocol in the register; and the drill fails on a planted tier
// outside the vocabulary, so it is a check and not a parse.
func TestPacketDeclarationLints(t *testing.T) {
	block := packetDeclaration(t)
	write := func(content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "seed.json")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	e, code := runEnv(t, "preseed", "check", "--config", write(block), "--lanes", filepath.Join("..", "..", "lanes"))
	if code != 0 || !e.OK {
		t.Fatalf("the packet's proposed declaration lints: %d %+v", code, e)
	}
	if e.Result["governance"] != true || e.Result["guardrails"] != true || e.Result["teams"] != true {
		t.Fatalf("the declaration declares governance, guardrails and teams: %+v", e.Result)
	}
	planted := strings.Replace(block, `"max_agent": "standard"`, `"max_agent": "sky"`, 1)
	if planted == block {
		t.Fatal("the mutation did not apply")
	}
	if e, code := runEnv(t, "preseed", "check", "--config", write(planted), "--lanes", filepath.Join("..", "..", "lanes")); code != 13 || e.Error == nil || e.Error.Code != "preseed_incomplete" {
		t.Fatalf("a tier outside the vocabulary is refused by name: %d %+v", code, e)
	}
}

// conformance: build plan section 5 criterion 5, the review findings on
// the task PR: the block as printed initializes nothing, since `seed init
// --preseed` refuses it `preseed_drift` on governance.root before
// genesis with the ledger untouched; with the root key's fingerprint
// substituted it writes genesis and every activation through the
// declared protocol, appends nothing on a second run, and `seed
// preseed check --ledger` agrees with nothing pending, so the
// comparison the lint alone never reaches is exercised on the block
// the operator copies.
func TestPacketDeclarationInitializesUnderTheRootKey(t *testing.T) {
	block := packetDeclaration(t)
	if !strings.Contains(block, `"root": "`+packetRootPlaceholder+`"`) {
		t.Fatalf("the packet's block marks governance.root as the substitution %q", packetRootPlaceholder)
	}
	_, priv, _ := writeKeys(t)
	fp := signerFingerprint(t, priv)
	lanes := filepath.Join("..", "..", "lanes")

	empty := filepath.Join(t.TempDir(), "ledger")
	e, code := runEnv(t, "init", "--ledger", empty, "--key", priv, "--preseed", writeDeclaration(t, block), "--lanes", lanes)
	if code != 28 || e.Error == nil || e.Error.Code != "preseed_drift" || !strings.Contains(e.Error.Message, "governance.root") || !strings.Contains(e.Error.Message, packetRootPlaceholder) || !strings.Contains(e.Error.Message, fp) {
		t.Fatalf("the unsubstituted block refuses preseed_drift naming the placeholder and the key: %d %+v", code, e)
	}
	if _, err := os.Stat(filepath.Join(empty, "HEAD")); err == nil {
		t.Fatal("nothing is written under a refused root")
	}

	cfg := writeDeclaration(t, strings.Replace(block, packetRootPlaceholder, fp, 1))
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	e, code = runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", lanes)
	if code != 0 || !e.OK || e.Result["unchanged"] != false {
		t.Fatalf("the substituted block initializes: %d %+v", code, e)
	}
	appended, _ := e.Result["appended"].([]any)
	declared, _ := e.Result["protocol"].(string)
	if len(appended) < 2 || appended[0] != "system.genesis" || !strings.HasSuffix(appended[len(appended)-1].(string), " "+declared) || *e.Position != strconv.Itoa(len(appended)-1) {
		t.Fatalf("genesis, then every activation through the declared protocol %s: %v at %s", declared, appended, *e.Position)
	}
	if e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", lanes); code != 0 || e.Result["unchanged"] != true {
		t.Fatalf("the second run appends nothing: %d %+v", code, e)
	}
	e, code = runEnv(t, "preseed", "check", "--config", cfg, "--ledger", ledgerDir, "--lanes", lanes)
	if pending, _ := e.Result["pending"].([]any); code != 0 || !e.OK || len(pending) != 0 || *e.Position != strconv.Itoa(len(appended)-1) {
		t.Fatalf("the check agrees with the chain with nothing pending: %d %+v", code, e)
	}
}

// conformance: build plan section 5 criterion 5, the review findings on
// the task PR: the order "The deployment" gives reaches the flip when
// followed literally: the import writes the deployment's ledger from
// empty, `seed init --preseed` over the imported chain appends exactly
// the activations the declaration names beyond the import's and
// nothing on a second run, `seed preseed check --ledger` reads green
// with nothing pending, and a lane key enrolls under the root; and the
// reverse order, the declaration initialized first, refuses the import
// `ledger_not_empty` by name with the initialized chain untouched, so
// the order is a check and not prose.
func TestPacketProcedureReachesTheFlip(t *testing.T) {
	_, priv, _ := writeKeys(t)
	fp := signerFingerprint(t, priv)
	cfg := writeDeclaration(t, strings.Replace(packetDeclaration(t), packetRootPlaceholder, fp, 1))
	lanes := filepath.Join("..", "..", "lanes")
	src, commit := v1Repo(t, "")
	export := v1Export(t, src, commit, nil)

	// The packet's order: the import first, into an empty ledger.
	ledgerDir := filepath.Join(t.TempDir(), "ledger")
	e, code := runEnv(t, "import", "--from-open-seed", export, "--source", src, "--ledger", ledgerDir, "--artifacts", filepath.Join(t.TempDir(), "artifacts"), "--key", priv)
	if code != 0 || !e.OK {
		t.Fatalf("the import writes the deployment's ledger from empty: %d %+v", code, e)
	}
	records := int(e.Result["records"].(float64))

	// The declaration over the imported chain: exactly the activations
	// beyond the import's seed/5, up to the declared protocol.
	e, code = runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", lanes)
	if code != 0 || !e.OK || e.Result["unchanged"] != false {
		t.Fatalf("init --preseed applies the declaration over the imported chain: %d %+v", code, e)
	}
	declared, _ := e.Result["protocol"].(string)
	var want []string
	past := false
	for _, v := range version.Supported() {
		if past {
			want = append(want, "system.protocol.upgraded "+v)
		}
		if v == version.Seed5 {
			past = true
		}
		if v == declared {
			break
		}
	}
	appended, _ := e.Result["appended"].([]any)
	if len(appended) != len(want) {
		t.Fatalf("the activations beyond the import's: want %v, got %v", want, appended)
	}
	for i := range want {
		if appended[i] != want[i] {
			t.Fatalf("activation %d: want %q, got %v", i, want[i], appended[i])
		}
	}
	if *e.Position != strconv.Itoa(records-1+len(appended)) {
		t.Fatalf("the position after the activations: %s, want %d", *e.Position, records-1+len(appended))
	}
	if e, code := runEnv(t, "init", "--ledger", ledgerDir, "--key", priv, "--preseed", cfg, "--lanes", lanes); code != 0 || e.Result["unchanged"] != true {
		t.Fatalf("the second run appends nothing: %d %+v", code, e)
	}
	e, code = runEnv(t, "preseed", "check", "--config", cfg, "--ledger", ledgerDir, "--lanes", lanes)
	if pending, _ := e.Result["pending"].([]any); code != 0 || !e.OK || len(pending) != 0 {
		t.Fatalf("the check reads green with nothing pending: %d %+v", code, e)
	}

	// A lane key enrolls under the root, and the chain verifies.
	_, lanePub, laneFP := writeWorkerKey(t, 43)
	if e, code := runEnv(t, "ledger", "append", "--ledger", ledgerDir, "--key", priv, "--verb", "actor.enrolled", "--subject", laneFP, "--payload", fmt.Sprintf(`{"key": %q, "kind": "agent", "name": "implementer"}`, lanePub)); code != 0 || !e.OK {
		t.Fatalf("a lane key enrolls under the root: %d %+v", code, e)
	}
	if e, code := runEnv(t, "ledger", "verify", "--ledger", ledgerDir); code != 0 || !e.OK || e.Result["count"] != float64(records+len(appended)+1) {
		t.Fatalf("the chain verifies from genesis with every write counted: %d %+v", code, e)
	}

	// The reverse order: the declaration initialized first, then the
	// import, which refuses by name and touches nothing.
	reversed := filepath.Join(t.TempDir(), "ledger")
	if e, code := runEnv(t, "init", "--ledger", reversed, "--key", priv, "--preseed", cfg, "--lanes", lanes); code != 0 || !e.OK {
		t.Fatalf("init --preseed on an empty ledger: %d %+v", code, e)
	}
	before, _ := runEnv(t, "ledger", "verify", "--ledger", reversed)
	e, code = runEnv(t, "import", "--from-open-seed", export, "--source", src, "--ledger", reversed, "--artifacts", filepath.Join(t.TempDir(), "artifacts"), "--key", priv)
	if code != 3 || e.Error == nil || e.Error.Code != "ledger_not_empty" {
		t.Fatalf("an initialized ledger refuses the import ledger_not_empty: %d %+v", code, e)
	}
	if after, _ := runEnv(t, "ledger", "verify", "--ledger", reversed); !after.OK || after.Result["count"] != before.Result["count"] || after.Result["tip"] != before.Result["tip"] {
		t.Fatalf("the initialized chain is untouched by the refused import: %+v then %+v", before.Result, after.Result)
	}
}
