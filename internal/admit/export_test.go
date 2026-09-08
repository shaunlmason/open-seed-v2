package admit

// The random walk (plans/os-21bf939f.md) lives in the external test
// package, admit_test, because it consumes internal/simulate's audit
// and that package imports this one. These exports hand it the shared
// walk scenario and the sweep's independent re-draft, so the walk is a
// third consumer of one scenario and one re-draft rather than a copy
// of either.

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/event"
)

// WalkScenario replays the shared walk script into a fresh store and
// returns the admitted records in position order with the signing
// keys by lane name (root plus the five lanes the script enrolls).
func WalkScenario(t *testing.T) ([]*event.Record, map[string]ed25519.PrivateKey) {
	t.Helper()
	store, resolve, signer := seededStore(t)
	lanes := walkLanes(t)
	keys := map[string]ed25519.PrivateKey{"root": signer}
	for name, key := range lanes {
		keys[name] = key
	}
	loose := walkResolver(t, resolve, lanes)
	for _, s := range walkScript(t, lanes) {
		runWalkStep(t, store, loose, keys, s)
	}
	ctx, err := ContextAt(store)
	if err != nil {
		t.Fatal(err)
	}
	return ctx.Records, keys
}

// Redraft drafts verb for the key on subject at the context's position
// exactly as the regression-class sweep re-drafts a listed verb: the
// sweep's independent view derivation at the given clock, the catalog's
// synthesizer, the derived probe subject where the catalog derives one,
// signed by the key. ok is false for a verb the catalog does not draft.
func Redraft(t *testing.T, ctx *Context, key ed25519.PrivateKey, subject, verb string, now time.Time) (rec *event.Record, ok bool) {
	t.Helper()
	var synth func(*probeView) string
	for _, p := range affordanceCatalog {
		if p.verb == verb {
			synth = p.synth
			break
		}
	}
	if synth == nil {
		return nil, false
	}
	v := probeViewAt(ctx, subject, now)
	v.actor = fpOf(t, key)
	on := subject
	if derive, has := probeSubjects[verb]; has {
		if derived := derive(v); derived != "" {
			on = derived
		}
	}
	rec, err := event.Sign(event.Event{
		V: ctx.Active, TS: v.now, Actor: v.actor, Verb: verb,
		Subject: on, Payload: []byte(synth(v)), Prev: ctx.Tip,
	}, key)
	if err != nil {
		t.Fatalf("drafting %s on %s: %v", verb, on, err)
	}
	return rec, true
}

// FixtureKey, FingerprintOf and EnrollBody expose the package's test
// fixtures to the external test package.
func FixtureKey(t testing.TB, first byte) ed25519.PrivateKey { return fixtureKey(t, first) }

func FingerprintOf(t *testing.T, key ed25519.PrivateKey) string { return fpOf(t, key) }

func EnrollBody(t *testing.T, key ed25519.PrivateKey, kind, name string) string {
	return enrollBody(t, key, kind, name)
}
