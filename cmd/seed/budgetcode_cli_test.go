package main

// The budget exit code's narrowness (plans/os-d03bde01.md D1, D3).
//
// This matrix lives in cmd/seed because that is where a BudgetError
// BECOMES an envelope: the conversion happens in the unexported
// remoteFailureEnvelope, and nothing in internal/admit maps errors to
// codes at all. A table over there could assert which refusals set the
// Exhausted flag and NOTHING about which code a caller receives, so
// the mapper-wide regression D3 exists to prevent would sit outside
// every assertion in a drill that read as though it covered it
// (review finding on #206).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/admit"
	"github.com/shaunlmason/open-seed-v2/internal/envelope"
)

// conformance: acceptance criterion 1 — exhaustion at budget.reserve
// comes back as budget_exhausted (exit 27), through the REAL boundary
// rather than by constructing the error.
func TestBudgetExhaustionHasItsOwnCode(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, keys, _ := offerLedger(t)
	offerFile(t, ld, priv, specCommit, "c-1")
	fence, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	_ = fence
	// The small class carries 100 units; asking for more is the
	// capacity refusal a worker actually meets. Driven through the
	// REAL verb: `ledger append` is the raw dev seam and runs no
	// rules, so a refusal drill against it would never reach the
	// budget rule at all.
	e, code := runEnv(t, "budget", "reserve", "--ledger", ld, "--key", keys["workerA"],
		"--subject", "c-1", "--amount", "101")
	if code != envelope.ExitBudgetExhausted {
		t.Fatalf("exhaustion is exit %d, got %d %+v", envelope.ExitBudgetExhausted, code, e.Error)
	}
	if e.Error == nil || e.Error.Code != "budget_exhausted" {
		t.Fatalf("the code a caller branches on: %+v", e.Error)
	}
	if !strings.Contains(e.Error.Message, "exceeds remaining") {
		t.Errorf("the message still carries the whole account: %q", e.Error.Message)
	}
}

// conformance: acceptance criterion 2 — every other BudgetError
// refusal still comes back as chain_invalid. ALL THIRTEEN of them:
// the rule has fourteen refusal sites, exhaustion is one, and every
// one of the other thirteen appears below at least once.
//
// This is the assertion the whole design turns on. Mapping the rule's
// refusals together would rebuild, one level narrower, exactly the
// conflation this card exists to remove: a caller branching on the code
// to retry with a smaller amount would retry against a malformed
// payload forever.
//
// Five of the thirteen need a chain shaped a particular way rather
// than just a bad payload — a subject whose class the table does not
// know, a reservation that never passed the authoring boundary, one
// already closed, one closed by a stranger, and a spend with nothing
// reserved. Each gets its OWN subject on the shared ledger, so a row
// that appends facts cannot change what a later row is testing. An
// earlier draft skipped these five and read as though it covered the
// rule; the count in its own name is what caught that.
func TestOnlyExhaustionGetsTheBudgetCode(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, _, fps := offerLedger(t)

	// held stands up one subject and claims it, returning its fence.
	// The class rides intent.filed, so an unknown one is filed here
	// rather than injected into the capacity table.
	held := func(t *testing.T, subject, class string, seed byte) string {
		t.Helper()
		for _, step := range [][2]string{
			{"intent.filed", `{"intent": "drill", "tier": "trivial", "budget": "` + class + `", "routing": "core"}`},
			{"contract.specified", `{"acceptance": {"ref": "accept.md @ ` + specCommit + `", "executable": false}}`},
		} {
			if e, code := runEnv(t, "ledger", "append", "--ledger", ld, "--key", priv,
				"--verb", step[0], "--subject", subject, "--payload", step[1]); code != 0 {
				t.Fatalf("%s %s: %d %+v", subject, step[0], code, e)
			}
		}
		pos, err := admitAppend(t, ld, workerRawKey(seed), "claim.taken", subject, `{}`)
		if err != nil {
			t.Fatalf("claim %s: %v", subject, err)
		}
		return strconv.Itoa(pos)
	}
	reserve := func(t *testing.T, subject, fence string, seed byte) string {
		t.Helper()
		pos, err := admitAppend(t, ld, workerRawKey(seed), "budget.reserve", subject,
			`{"amount": "10", "fence": "`+fence+`"}`)
		if err != nil {
			t.Fatalf("reserve on %s: %v", subject, err)
		}
		return strconv.Itoa(pos)
	}

	f := held(t, "c-1", "small", 22)
	for _, tc := range []struct {
		name, verb, payload string
		subject             string
		seed                byte
		// want is the refusal this row must actually reach. Without
		// it a row can land on a DIFFERENT site and still pass, which
		// is how a matrix ends up agreeing with a convenient shape
		// rather than the shipped one: the first draft's "non-numeric
		// actuals" row cited reservation 0, so it was refused at the
		// no-such-reservation site and never read an actuals field at
		// all.
		want string
	}{
		// The reserve path.
		{"an unknown reserve field", "budget.reserve", `{"amount": "10", "fence": "` + f + `", "x": 1}`, "c-1", 22,
			"the reserve payload is the strict object"},
		{"a non-numeric amount", "budget.reserve", `{"amount": "lots", "fence": "` + f + `"}`, "c-1", 22,
			"is not a positive integer of class units"},
		{"a zero amount", "budget.reserve", `{"amount": "0", "fence": "` + f + `"}`, "c-1", 22,
			"is not a positive integer of class units"},
		{"a negative amount", "budget.reserve", `{"amount": "-1", "fence": "` + f + `"}`, "c-1", 22,
			"is not a positive integer of class units"},
		{"a reserve from a non-holder", "budget.reserve", `{"amount": "10", "fence": "` + f + `"}`, "c-1", 23,
			"only the active claim holder or the operator lane reserves"},
		// The close paths.
		{"a malformed settle payload", "budget.settle",
			`{"reservation": "0", "actuals": "1", "fence": "` + f + `", "x": 1}`, "c-1", 22,
			"the close payload is the strict object"},
		{"a settle citing no reservation", "budget.settle", `{"reservation": "0", "actuals": "1", "fence": "` + f + `"}`, "c-1", 22,
			"is no reservation on this subject"},
		{"a settle citing a non-position", "budget.settle", `{"reservation": "tip", "actuals": "1", "fence": "` + f + `"}`, "c-1", 22,
			"is not a chain position"},
		{"a release carrying actuals", "budget.release", `{"reservation": "0", "actuals": "1", "fence": "` + f + `"}`, "c-1", 22,
			"release frees a reservation with zero actuals"},
		{"a release citing no reservation", "budget.release", `{"reservation": "0", "fence": "` + f + `"}`, "c-1", 22,
			"is no reservation on this subject"},
		{"a malformed release payload", "budget.release",
			`{"reservation": "0", "fence": "` + f + `", "x": 1}`, "c-1", 22,
			"the close payload is the strict object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusalKeepsChainInvalid(t, ld, tc.seed, tc.verb, tc.subject, tc.payload, tc.want)
		})
	}

	// The five that need a shaped chain, each on its own subject.
	t.Run("a class the capacity table does not know", func(t *testing.T) {
		fu := held(t, "c-unknown", "colossal", 22)
		refusalKeepsChainInvalid(t, ld, 22, "budget.reserve", "c-unknown",
			`{"amount": "10", "fence": "`+fu+`"}`, "has no capacity in the class table")
	})
	t.Run("a settle laundering an unauthored reservation", func(t *testing.T) {
		// Staged RAW: `ledger append` runs no rules, so this reserve
		// folds into the subject's reservations while never having
		// passed admission. Closing it is what would launder it into
		// spend history.
		fl := held(t, "c-launder", "small", 22)
		pos := rawAppend(t, ld, workerRawKey(23), "budget.reserve", "c-launder",
			`{"amount": "10", "fence": "`+fl+`"}`)
		refusalKeepsChainInvalid(t, ld, 23, "budget.settle", "c-launder",
			`{"reservation": "`+strconv.Itoa(pos)+`", "actuals": "1", "fence": "`+fl+`"}`,
			"never passed the authoring boundary")
	})
	t.Run("a second close of an already-closed reservation", func(t *testing.T) {
		fd := held(t, "c-double", "small", 22)
		r := reserve(t, "c-double", fd, 22)
		if _, err := admitAppend(t, ld, workerRawKey(22), "budget.settle", "c-double",
			`{"reservation": "`+r+`", "actuals": "1", "fence": "`+fd+`"}`); err != nil {
			t.Fatalf("the first settle must be admitted: %v", err)
		}
		refusalKeepsChainInvalid(t, ld, 22, "budget.settle", "c-double",
			`{"reservation": "`+r+`", "actuals": "1", "fence": "`+fd+`"}`,
			"is already effectively closed")
	})
	t.Run("a close by someone other than the reserving signer", func(t *testing.T) {
		fs := held(t, "c-stranger", "small", 22)
		r := reserve(t, "c-stranger", fs, 22)
		refusalKeepsChainInvalid(t, ld, 23, "budget.release", "c-stranger",
			`{"reservation": "`+r+`", "fence": "`+fs+`"}`,
			"only the reservation's own reserving signer or the operator lane closes it")
	})
	t.Run("actuals that are not a count", func(t *testing.T) {
		// Needs a real reservation to cite: the no-such-reservation
		// site sits ahead of the actuals check, so a row citing
		// position 0 never reaches this one.
		fa := held(t, "c-actuals", "small", 22)
		r := reserve(t, "c-actuals", fa, 22)
		refusalKeepsChainInvalid(t, ld, 22, "budget.settle", "c-actuals",
			`{"reservation": "`+r+`", "actuals": "some", "fence": "`+fa+`"}`,
			"is not a non-negative integer of class units")
	})
	t.Run("a spend with nothing reserved", func(t *testing.T) {
		// run.started is the spending table's only entry and the
		// SUPERVISOR's act, which is why no key a worker loop signs
		// with can trip this gate (spec/loop-verbs.md).
		fr := held(t, "c-spend", "small", 22)
		refusalKeepsChainInvalid(t, ld, 21, "run.started", "c-spend",
			`{"fence": "`+fr+`", "runner": "local", "attempt": "`+fps["workerA"]+`"}`,
			"spends, and no open valid reservation stands")
	})
}

// refusalKeepsChainInvalid drives one refusal through the boundary and
// asserts the envelope a caller receives. Both are the shipped code
// paths: nothing here constructs a BudgetError, and the assertion is
// about the CODE rather than the flag the error carries.
func refusalKeepsChainInvalid(t *testing.T, ld string, seed byte, verb, subject, payload, want string) {
	t.Helper()
	sawBudgetRefusal(want)
	_, err := admitAppendErr(ld, workerRawKey(seed), verb, subject, payload)
	if err == nil {
		t.Fatal("must refuse at the boundary")
	}
	// A SKIP here would be a silent hole, so this is fatal: a row that
	// never reaches the budget rule tests nothing about the budget
	// rule's mapping. (One candidate row was dropped for exactly that:
	// a reserve payload carrying no fence is refused by the FENCE rule
	// first, since the holder must cite the window, so it can never
	// testify here.)
	var be *admit.BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("this row must reach the BUDGET rule to say anything about its mapping, got %v", err)
	}
	if !strings.Contains(be.Error(), want) {
		t.Fatalf("this row must reach the site it names (%q), and reached %q instead", want, be.Error())
	}
	env := remoteFailureEnvelope(err)
	if env.Exit == envelope.ExitBudgetExhausted {
		t.Fatalf("this is NOT exhaustion and must not carry its code — a caller answering "+
			"budget_exhausted by asking for less would be answering a bug: %+v", env.Error)
	}
	if env.Error == nil || env.Error.Code != "chain_invalid" {
		t.Fatalf("every non-exhaustion budget refusal keeps chain_invalid: %+v", env.Error)
	}
}

// And the flag is set at exactly ONE site, asserted against the error
// itself: the matrix above proves the mapping, this proves the source.
func TestExhaustedFlagMarksOnlyCapacity(t *testing.T) {
	ld, _, _, specCommit, _, priv, _, keys, _ := offerLedger(t)
	_ = keys
	offerFile(t, ld, priv, specCommit, "c-1")
	fence, err := admitAppend(t, ld, workerRawKey(22), "claim.taken", "c-1", `{}`)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	f := strconv.Itoa(fence)
	exhausted := func(payload string) bool {
		_, err := admitAppendErr(ld, workerRawKey(22), "budget.reserve", "c-1", payload)
		var be *admit.BudgetError
		if !errors.As(err, &be) {
			t.Fatalf("expected a budget refusal, got %v", err)
		}
		return be.Exhausted
	}
	if !exhausted(`{"amount": "101", "fence": "` + f + `"}`) {
		t.Error("capacity exhaustion must set the flag")
	}
	if exhausted(`{"amount": "lots", "fence": "` + f + `"}`) {
		t.Error("a malformed amount is not exhaustion")
	}
}

// The matrix's own parity, which is what makes "all thirteen" a fact
// rather than a number someone typed. sawBudgetRefusal records the
// site each row claims to reach; TestEveryBudgetRefusalHasARow reads
// the rule's refusal sites out of the source and fails when one has no
// row — so a FOURTEENTH refusal added later cannot quietly ship with
// no assertion about the code it comes back as.
//
// This is D2's lesson applied to the matrix instead of the table: a
// drill that must be updated by hand to catch a regression cannot
// catch that regression.
var budgetSitesSeen = map[string]bool{}

func sawBudgetRefusal(want string) { budgetSitesSeen[want] = true }

func TestEveryBudgetRefusalHasARow(t *testing.T) {
	// The matrix populates the set, so it must have run first. Go runs
	// a package's tests in source order within one binary; this file
	// declares the matrix above, and the check is cheap enough to make
	// the dependency explicit rather than assumed.
	if len(budgetSitesSeen) == 0 {
		t.Run("matrix", TestOnlyExhaustionGetsTheBudgetCode)
	}
	sites, err := budgetRefusalSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 14 {
		t.Fatalf("the budget rule's refusal sites moved (%d found): every one of them needs a "+
			"row in the narrowness matrix, and the count is stated so a new one cannot pass "+
			"unnoticed:\n%s", len(sites), strings.Join(sites, "\n"))
	}
	for _, site := range sites {
		if strings.Contains(site, "exceeds remaining") {
			continue // exhaustion: the one site that does NOT keep chain_invalid
		}
		covered := false
		for want := range budgetSitesSeen {
			if strings.Contains(site, want) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("no row reaches this refusal, so nothing asserts the code it comes back as:\n  %s", site)
		}
	}
}

// budgetRefusalSites reads the reason strings of the budget rule's
// refusals out of internal/admit/admit.go. Parsing the source keeps
// the matrix honest against a rule that grows; a hand-listed copy
// would drift the moment the rule did.
func budgetRefusalSites() ([]string, error) {
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "admit", "admit.go"))
	if err != nil {
		return nil, err
	}
	src := string(b)
	start := strings.Index(src, `{Name: "budget", Check:`)
	if start < 0 {
		return nil, errors.New("the budget rule is no longer declared as a named rule literal")
	}
	end := strings.Index(src[start:], "\n\t\t}},")
	if end < 0 {
		return nil, errors.New("cannot find the end of the budget rule")
	}
	var sites []string
	for _, line := range strings.Split(src[start:start+end], "\n") {
		i := strings.Index(line, "&BudgetError{")
		if i < 0 {
			continue
		}
		r := strings.Index(line[i:], `Reason: `)
		if r < 0 {
			return nil, fmt.Errorf("a BudgetError with no inline reason: %s", strings.TrimSpace(line))
		}
		rest := line[i+r:]
		q := strings.Index(rest, `"`)
		e := strings.Index(rest[q+1:], `"`)
		if q < 0 || e < 0 {
			return nil, fmt.Errorf("cannot read the reason string: %s", strings.TrimSpace(line))
		}
		sites = append(sites, rest[q+1:q+1+e])
	}
	return sites, nil
}
