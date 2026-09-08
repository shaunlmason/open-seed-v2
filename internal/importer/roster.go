package importer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/keyring"
)

// VerifierName is the dedicated import identity that renders the pass
// verdict when the v1 closer of a card was also one of its claimants:
// Seed's independence rule refuses a claimant's verdict, and the
// import does not manufacture independence a name never had.
const VerifierName = "import-verifier"

// actor is one import-generated identity: a v1 actor name, the key
// the importer generated for it, and the grants it needs.
type actor struct {
	name  string
	kind  string
	known bool
	s     *signer
	// verbs is every verb the identity signed in the pass, for the
	// manifest and the drills.
	verbs     map[string]bool
	grants    []string
	enrolled  int
	suspended int
	// operator marks the importing operator's own key, which signs
	// what no generated identity may (contract.cancelled, the card
	// reconciliations) and is neither enrolled nor suspended here.
	operator bool
	// needed is whether the rehearsal had the identity sign anything;
	// the verifier is enrolled only when needed.
	needed bool
}

// capFor is the one capability the import grants for a verb it
// synthesizes: never operator, which no generated key ever holds.
func capFor(verb string) string {
	switch verb {
	case "intent.filed", "contract.specified", "contract.blocked", "contract.unblocked", "contract.returned", "claim.reaped":
		return keyring.CapDispatch
	case "claim.taken", "claim.released", "claim.parked", "submission.made", "merge.requested":
		return keyring.CapClaim
	case "merge.observed":
		return keyring.CapObserver
	case "verdict.rendered":
		return keyring.CapVerdict
	}
	return ""
}

// roster is the identity plan: one actor per distinct v1 name, keys
// held in memory for the import only.
type roster struct {
	byName map[string]*actor
	order  []string
	// ver is the dedicated verifier, held apart from the names so a
	// predecessor actor called import-verifier is its own identity.
	ver *actor
}

func newRoster(t *Table, names []string) (*roster, error) {
	r := &roster{byName: map[string]*actor{}}
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	sorted := make([]string, 0, len(set))
	for n := range set {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		id, known := t.IdentityFor(n)
		a, err := newActor(id, known)
		if err != nil {
			return nil, err
		}
		r.byName[n] = a
		r.order = append(r.order, n)
	}
	ver, err := newActor(Identity{Kind: "service", Name: VerifierName}, true)
	if err != nil {
		return nil, err
	}
	r.ver = ver
	return r, nil
}

func newActor(id Identity, known bool) (*actor, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	s, err := newSigner(priv)
	if err != nil {
		return nil, err
	}
	return &actor{name: id.Name, kind: id.Kind, known: known, s: s, verbs: map[string]bool{}}, nil
}

// all lists the named identities and then the verifier.
func (r *roster) all() []*actor {
	out := make([]*actor, 0, len(r.order)+1)
	for _, n := range r.order {
		out = append(out, r.byName[n])
	}
	return append(out, r.ver)
}

// deriveGrants turns the verbs the rehearsal had each identity sign
// into its grants (plans/os-cf13fb51.md D3: sufficient for the verbs
// the name performs, bridges included, minimal, listed): the one
// capability each verb consumes, never operator.
func (r *roster) deriveGrants() {
	for _, a := range r.all() {
		set := map[string]bool{}
		for v := range a.verbs {
			if c := capFor(v); c != "" {
				set[c] = true
			}
		}
		a.needed = len(a.verbs) > 0
		a.grants = make([]string, 0, len(set))
		for c := range set {
			a.grants = append(a.grants, c)
		}
		sort.Strings(a.grants)
	}
}

// get resolves a v1 actor name; a name the plan never saw (a card
// author absent from the run-log, say) is enrolled on the fly as the
// table resolves it, and listed.
func (r *roster) get(name string) *actor {
	if a, ok := r.byName[name]; ok {
		return a
	}
	return nil
}

func (r *roster) verifier() *actor { return r.ver }

// reset clears what a pass recorded, keeping the keys and grants.
func (r *roster) reset() {
	for _, a := range r.all() {
		a.verbs = map[string]bool{}
		a.enrolled, a.suspended = 0, 0
	}
}

func (a *actor) pubHex() string { return hex.EncodeToString(a.s.pub) }
