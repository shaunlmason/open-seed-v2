package admit

import (
	"errors"
	"strings"
	"testing"

	"github.com/shaunlmason/open-seed-v2/internal/imported"
	"github.com/shaunlmason/open-seed-v2/internal/keyring"
	"github.com/shaunlmason/open-seed-v2/internal/version"
)

// conformance: plans/os-cf13fb51.md D2, spec/import.md — the
// once-per-ledger provenance record at the boundary itself: undefined
// before seed/5, strict in shape, operator-only, admitted once and
// refused the second time, whatever the importer's own drills do.
func TestSystemImportedAdmitsOncePerLedger(t *testing.T) {
	ctx, signer, worker, _, step := grantFixture(t)
	head := strings.Repeat("a", 40)
	manifest := strings.Repeat("b", 64)
	payload, err := imported.Render(head, "seed-anchor/20260101T000000Z", manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Undefined before seed/5.
	if err := Check(ctx, draftV(t, signer, version.Seed1, imported.Verb, imported.Subject, string(payload), ctx.Tip)); err == nil {
		t.Fatal("system.imported is not defined at seed/1")
	}
	for _, v := range []string{version.Seed2, version.Seed3, version.Seed4, version.Seed5} {
		ctx = step(signer, ctx.Active, "system.protocol.upgraded", "system", `{"to": "`+v+`"}`)
	}
	if ctx.Active != version.Seed5 {
		t.Fatalf("the chain is at %s, not seed/5", ctx.Active)
	}
	// Operator-only: a claim-granted worker key is refused.
	ctx = step(signer, version.Seed5, keyring.VerbGranted, fpOf(t, worker), `{"capability": "`+keyring.CapClaim+`"}`)
	var grant *OutOfGrantError
	if err := Check(ctx, draftV(t, worker, version.Seed5, imported.Verb, imported.Subject, string(payload), ctx.Tip)); !errors.As(err, &grant) {
		t.Fatalf("a worker key cannot import, got %v", err)
	}
	// Strict shape: a malformed payload refuses.
	if err := Check(ctx, draftV(t, signer, version.Seed5, imported.Verb, imported.Subject, `{"source": "open-seed", "export_head": "short", "anchor": "x", "manifest": "`+manifest+`"}`, ctx.Tip)); err == nil {
		t.Fatal("a short export_head refuses")
	}
	if err := Check(ctx, draftV(t, signer, version.Seed5, imported.Verb, "not-system", string(payload), ctx.Tip)); err == nil {
		t.Fatal("the subject is system")
	}
	// Once: the operator's record admits, and a second is refused
	// naming the first.
	if err := Check(ctx, draftV(t, signer, version.Seed5, imported.Verb, imported.Subject, string(payload), ctx.Tip)); err != nil {
		t.Fatalf("the operator's import record admits: %v", err)
	}
	ctx = step(signer, version.Seed5, imported.Verb, imported.Subject, string(payload))
	err = Check(ctx, draftV(t, signer, version.Seed5, imported.Verb, imported.Subject, string(payload), ctx.Tip))
	if err == nil || !strings.Contains(err.Error(), "already") && !strings.Contains(err.Error(), "once") {
		t.Fatalf("a second import on one ledger refuses naming the first, got %v", err)
	}
}
