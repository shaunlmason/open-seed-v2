// Package posture holds the deployment's declared admission posture
// (SEED-NEXT.md Part II "Postures"; plans/os-3c72f93f.md): every
// deployment MUST declare which of the three named deployments it runs,
// and cooperative is a declared mode with a named consequence the
// preflight tool states in plain words. The declaration is
// client/deployment state, never ledger content: the guarded ref's tree
// stays HEAD and segments only.
package posture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Posture is one of the charter's three named admission deployments.
type Posture string

const (
	EnforcedSelfHosted  Posture = "enforced-self-hosted"
	EnforcedForgeHosted Posture = "enforced-forge-hosted"
	Cooperative         Posture = "cooperative"
)

// Consequence is the charter's named cooperative consequence, in the
// charter's own words. One copy: the doctor and any future README
// generator quote it, never re-derive it.
const Consequence = "cooperative posture: no server-side enforcement — every writer self-validates; the security invariant (SEED-NEXT.md §I.2) does not hold, and protocol rules are advisory against a hostile credential"

// ForgeHostedGap names the honest state of the forge-hosted posture
// until its admission service lands (docs/build-plan.md Phase 12).
const ForgeHostedGap = "enforced-forge-hosted is declared, but its admission service lands in Phase 12 (docs/build-plan.md); until then this deployment must document its interim enforcement"

// ErrUndeclared is the refusal for a deployment with no declaration.
var ErrUndeclared = errors.New("no admission posture is declared: every deployment MUST declare which posture it runs (SEED-NEXT.md Part II, \"Postures\")")

// ErrUnreadable marks a declaration that exists but cannot be read (a
// directory, denied permissions, an I/O failure): an operational
// failure, never a judgment on the declaration's content.
var ErrUnreadable = errors.New("posture declaration cannot be read")

func validList() string {
	return fmt.Sprintf("%s, %s, %s", EnforcedSelfHosted, EnforcedForgeHosted, Cooperative)
}

// Valid reports whether p is one of the three charter postures.
func (p Posture) Valid() bool {
	switch p {
	case EnforcedSelfHosted, EnforcedForgeHosted, Cooperative:
		return true
	}
	return false
}

// Enforced reports whether the posture carries server-side enforcement.
func (p Posture) Enforced() bool {
	return p == EnforcedSelfHosted || p == EnforcedForgeHosted
}

// DeclarationPath is where the reference deployment keeps the
// declaration, relative to the repository root: the file `seed doctor`
// reads by default, and the one the enforced hook reads from the
// default branch's tip to learn the protected surface
// (plans/os-465e356e.md D1). It is protected by construction: a list
// that could be unprotected by editing the file that carries it would
// protect nothing.
const DeclarationPath = "seed.json"

// DefaultLedgerRef is the ledger ref a forge-hosted deployment rides
// when the admission block does not name one: a branch, because forges
// protect branches and tags and nothing under refs/seed/* (#244).
const DefaultLedgerRef = "refs/heads/seed-ledger"

// Admission is the forge-hosted deployment's admission block. Endpoint
// is where `seed-admit serve` answers proposals; Identity is the forge
// login or app slug the service's git credential belongs to (the
// ledger branch's sole writer); LedgerRef is the branch the ledger
// rides (DefaultLedgerRef when empty); Checks and Reviews are the
// default branch's required status checks and review count; Owners
// are the forge identities that review the protected surface, rendered
// into CODEOWNERS by the reconciler (#244).
type Admission struct {
	Endpoint  string   `json:"endpoint"`
	Identity  string   `json:"identity"`
	LedgerRef string   `json:"ledger_ref"`
	Checks    []string `json:"checks"`
	Reviews   int      `json:"reviews"`
	Owners    []string `json:"owners"`
	// Forge names which forge hosts the repository — "github" (the
	// default when absent, so every existing declaration keeps its
	// meaning) or "forgejo". Api is the forge's API base URL; GitHub's
	// public API is the default when absent, and Forgejo has no public
	// API, so a Forgejo declaration must name its instance.
	Forge string `json:"forge"`
	Api   string `json:"api"`
}

// Forge kinds a declaration may name.
const (
	ForgeGitHub  = "github"
	ForgeForgejo = "forgejo"
)

// ForgeKind returns the declared forge, defaulting to GitHub when absent.
func (a *Admission) ForgeKind() string {
	if a == nil || a.Forge == "" {
		return ForgeGitHub
	}
	return a.Forge
}

// Trust choices a fresh reader may declare for checkpoints
// (spec/checkpoints.md; plans/os-7508ab9e.md D1).
const (
	TrustReplay  = "replay"  // every fresh reader verifies from genesis once
	TrustSigners = "signers" // a fresh reader may start from a capable signer's checkpoint
)

// Checkpoints is the declaration's checkpoint-trust block: which of a
// fresh clone's two verification obligations this deployment chose.
// An absent block is undeclared — never a default — because the
// charter says the choice is declared, not defaulted.
type Checkpoints struct {
	Trust string `json:"trust"`
}

// Governance names the governance root and how the protected surface
// changes (charter §II.14; plans/os-0d4f2af3.md D1, D5). Root is the
// root key's fingerprint; Owners are the forge identities that review
// the protected surface; ChangeProcess is the one process the charter
// names.
type Governance struct {
	Root          string   `json:"root"`
	Owners        []string `json:"owners,omitempty"`
	ChangeProcess string   `json:"change_process"`
}

// ChangeProcessPROwnerReview is the only change process the charter
// names for the protected surface.
const ChangeProcessPROwnerReview = "pr+owner-review"

// SquadGuardrail is a squad's tier posture: the tier work files at by
// default and the highest tier an agent-kind key may claim; and, when
// the squad opts in, its racing block (plans/os-56bee171.md D1).
type SquadGuardrail struct {
	Default  string  `json:"default"`
	MaxAgent string  `json:"max_agent"`
	Racing   *Racing `json:"racing,omitempty"`
}

// Racing is a squad's explicit opt-in to racing mode (charter §II.6,
// III.F row 7): the most claims the squad tolerates on one contract at
// once, and the compute-for-latency trade stated in the operator's own
// words. An absent block is no racing; a block says both or refuses.
type Racing struct {
	Racers int    `json:"racers"`
	Cost   string `json:"cost"`
}

// PathFloor is the minimum tier a contract touching a prefix files at.
type PathFloor struct {
	Prefix string `json:"prefix"`
	Min    string `json:"min"`
}

// ApprovalRule is one per-verb require-approval entry (charter §II.14;
// plans/os-5781a026.md D1): an act of Verb by a key whose roster kind
// is in Kinds (absent means every non-human kind; human may be named,
// since a deployment may want a human's act approved too) on a
// contract whose tier is at or above MinTier (absent means every
// tier; a system subject has no tier and is governed whenever the
// verb is) requires an open approval.granted naming the verb and the
// actor. The vocabularies (catalog verbs, roster kinds, tiers) are
// checked where the tables are, at `seed preseed check`.
type ApprovalRule struct {
	Verb    string   `json:"verb"`
	Actors  []string `json:"actors,omitempty"`
	Kinds   []string `json:"kinds,omitempty"`
	MinTier string   `json:"min_tier,omitempty"`
}

// Governs reports whether the entry reaches a key: the fingerprints
// Actors names and the roster kinds Kinds names, and every non-human
// kind when it names neither. The policy varies by actor (the
// charter's individual keypair: one agent allowed while another of the
// same kind needs an approval) and by risk class (the kind, and the
// tier the caller compares separately). A key with no asserted kind (a
// governance root, enrolled by genesis rather than by an operator's
// assertion) is reached by no kind selector, only by its fingerprint:
// the root is the operator every approval is answered by.
func (r ApprovalRule) Governs(actor, kind string) bool {
	for _, a := range r.Actors {
		if a == actor {
			return true
		}
	}
	if kind == "" {
		return false
	}
	if len(r.Kinds) == 0 && len(r.Actors) == 0 {
		return kind != "human"
	}
	for _, k := range r.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Guardrails is the declaration's guardrails block: tiers per squad and
// per path (charter III.L row 1), the agent ceiling reading the
// roster's kind (III.E row 9), and the verbs that require an approval
// (III.L row 4's third mode).
type Guardrails struct {
	Squads    map[string]SquadGuardrail `json:"squads,omitempty"`
	Paths     []PathFloor               `json:"paths,omitempty"`
	Approvals []ApprovalRule            `json:"approvals,omitempty"`
}

// Squad is one declared squad and the lane manifests it runs.
type Squad struct {
	Name  string   `json:"name"`
	Lanes []string `json:"lanes"`
}

// Teams is the declaration's teams block: the squads `routing` names.
type Teams struct {
	Squads []Squad `json:"squads"`
}

// Config is the deployment declaration.
// FederationRemote is one read remote (plans/os-48df10a2.md D4): a
// name, the git remote whose ledger ref is read, and that ref. Read
// only: nothing here names a key or a write.
type FederationRemote struct {
	Name   string `json:"name"`
	Remote string `json:"remote"`
	Ref    string `json:"ref"`
}

// Boundary is the declaration's boundary block (plans/os-40ed0ca0.md
// D1): what the capability card publishes — the request kinds this
// deployment accepts through its ingress and the remote or endpoint a
// request is filed through. Optional; absent means the card names no
// ingress and every request kind admits as before.
type Boundary struct {
	Accepts []string `json:"accepts"`
	Ingress string   `json:"ingress"`
}

// Federation is the declaration's federation block: the other
// deployments this one reads, uniformly, into its org view. Absent is
// no federation.
type Federation struct {
	Remotes []FederationRemote `json:"remotes"`
}

type Config struct {
	Posture    Posture     `json:"posture"`
	Admission  *Admission  `json:"admission,omitempty"`
	Federation *Federation `json:"federation,omitempty"`
	Boundary   *Boundary   `json:"boundary,omitempty"`
	// Protected enumerates the protected surface (SEED-NEXT.md §II.14)
	// as repository-relative path prefixes: a path equal to an entry,
	// or under an entry as a directory, is write-denied to every agent
	// credential at the enforced hook. Optional; the declaration's own
	// path is always protected whether or not it is listed.
	Protected   []string     `json:"protected,omitempty"`
	Checkpoints *Checkpoints `json:"checkpoints,omitempty"`
	// The preseed's blocks (plans/os-0d4f2af3.md D1): the protocol
	// version the deployment activates through, the governance root
	// and change process, the guardrails and the teams. Each is
	// undeclared when absent, never defaulted.
	Protocol   string      `json:"protocol,omitempty"`
	Governance *Governance `json:"governance,omitempty"`
	Guardrails *Guardrails `json:"guardrails,omitempty"`
	Teams      *Teams      `json:"teams,omitempty"`
	// The executor substrates a deployment provisions through
	// (plans/os-083112ac.md D3): the container runtime and image, the
	// cloud endpoint and credential env-var name, and the enrolled
	// remote workers. A credential is the NAME of an environment
	// variable, never a secret; the lint refuses a token-shaped value.
	Executors *Executors `json:"executors,omitempty"`
}

// Executors declares the non-local substrate configuration.
type Executors struct {
	Container *ContainerConfig `json:"container,omitempty"`
	Cloud     *CloudConfig     `json:"cloud,omitempty"`
	Remote    *RemoteConfig    `json:"remote,omitempty"`
}

// ContainerConfig names the OCI runtime and image.
type ContainerConfig struct {
	Runtime string `json:"runtime"` // docker | podman | fake
	Image   string `json:"image"`
}

// CloudConfig names the session endpoint and the env var holding the
// bearer credential.
type CloudConfig struct {
	Endpoint   string `json:"endpoint"`
	Credential string `json:"credential"` // an environment-variable NAME
}

// RemoteConfig names the enrolled workers.
type RemoteConfig struct {
	Workers []WorkerConfig `json:"workers"`
}

// WorkerConfig is one enrolled remote worker.
type WorkerConfig struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
}

// envVarName is the shape a credential (an env-var name) must have: an
// identifier, never a token. A token-shaped value fails it, so a secret
// pasted where a name belongs is refused rather than stored.
var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SquadNames lists the declared squads, sorted; nil when teams are
// undeclared.
func (c *Config) SquadNames() []string {
	if c == nil || c.Teams == nil {
		return nil
	}
	out := make([]string, 0, len(c.Teams.Squads))
	for _, s := range c.Teams.Squads {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// AgentCeiling is the highest tier an agent-kind key may claim in the
// squad; ok is false when the guardrails or the squad are undeclared.
func (c *Config) AgentCeiling(squad string) (string, bool) {
	if c == nil || c.Guardrails == nil {
		return "", false
	}
	g, ok := c.Guardrails.Squads[squad]
	if !ok || g.MaxAgent == "" {
		return "", false
	}
	return g.MaxAgent, true
}

// ApprovalRule reads the require-approval entry for a verb
// (plans/os-5781a026.md D1); ok is false when the guardrails are
// undeclared or name no entry for it. Policy, not chain validity: no
// declaration, no rule.
func (c *Config) ApprovalRule(verb string) (ApprovalRule, bool) {
	if c == nil || c.Guardrails == nil {
		return ApprovalRule{}, false
	}
	for _, r := range c.Guardrails.Approvals {
		if r.Verb == verb {
			return r, true
		}
	}
	return ApprovalRule{}, false
}

// RacingFor reads a squad's racing opt-in (plans/os-56bee171.md D1):
// the declared cap and the cost in the operator's words; ok is false
// when the guardrails, the squad or the block are undeclared.
func (c *Config) RacingFor(squad string) (racers int, cost string, ok bool) {
	if c == nil || c.Guardrails == nil {
		return 0, "", false
	}
	g, found := c.Guardrails.Squads[squad]
	if !found || g.Racing == nil {
		return 0, "", false
	}
	return g.Racing.Racers, g.Racing.Cost, true
}

// Floor is the minimum tier a contract touching path files at: the
// strictest floor among the declared prefixes the path is under; ok is
// false when none applies.
func (c *Config) Floor(path string, above func(a, b string) bool) (string, bool) {
	if c == nil || c.Guardrails == nil {
		return "", false
	}
	path = strings.TrimSuffix(path, "/")
	floor, found := "", false
	for _, f := range c.Guardrails.Paths {
		prefix := strings.TrimSuffix(f.Prefix, "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			if !found || above(f.Min, floor) {
				floor, found = f.Min, true
			}
		}
	}
	return floor, found
}

// CheckpointTrust returns the declared trust choice, or "" when the
// block is absent (undeclared).
func (c *Config) CheckpointTrust() string {
	if c == nil || c.Checkpoints == nil {
		return ""
	}
	return c.Checkpoints.Trust
}

// LedgerRef is the ref the ledger rides under this declaration: the
// admission block's branch under enforced-forge-hosted, or "" for the
// other postures, whose callers keep their own default (#244).
func (c *Config) LedgerRef() string {
	if c.Posture != EnforcedForgeHosted || c.Admission == nil {
		return ""
	}
	if c.Admission.LedgerRef == "" {
		return DefaultLedgerRef
	}
	return c.Admission.LedgerRef
}

// Load reads a strict declaration {"posture": "<name>", "protected"?}:
// unknown fields, trailing data, unknown postures and malformed
// protected entries refuse; a missing file is ErrUndeclared.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrUndeclared
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	return Parse(b)
}

// Parse decodes the declaration's bytes with Load's strictness, for a
// reader that already holds them (the hook reads the file out of a
// commit rather than off a filesystem).
func Parse(b []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("posture declaration does not parse: %v (valid postures: %s)", err, validList())
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("posture declaration carries trailing data (valid postures: %s)", validList())
	}
	if !c.Posture.Valid() {
		return nil, fmt.Errorf("%q is not a Seed posture (valid postures: %s)", c.Posture, validList())
	}
	if err := c.validateAdmission(); err != nil {
		return nil, fmt.Errorf("%v (valid postures: %s)", err, validList())
	}
	if c.Checkpoints != nil && c.Checkpoints.Trust != TrustReplay && c.Checkpoints.Trust != TrustSigners {
		return nil, fmt.Errorf("checkpoints.trust is %q or %q (a fresh reader's verification obligation is declared, not defaulted), got %q", TrustReplay, TrustSigners, c.Checkpoints.Trust)
	}
	for i, p := range c.Protected {
		if err := validProtectedEntry(p); err != nil {
			return nil, fmt.Errorf("protected[%d] %q: %v (valid postures: %s)", i, p, err, validList())
		}
	}
	if err := c.validatePreseed(); err != nil {
		return nil, err
	}
	return &c, nil
}

// validatePreseed holds the preseed blocks to their shapes: names
// non-empty and unique, tiers non-empty (the vocabulary is the tier
// table's and is checked where the table is, at admission and at
// `seed preseed check`), prefixes clean, the change process the one
// the charter names.
// validateFederation holds the federation block to its shape: names
// non-empty and unique, remotes non-empty, refs full ref names or
// absent (the default ledger ref).
func (c *Config) validateFederation() error {
	if c.Federation == nil {
		return nil
	}
	seen := map[string]bool{}
	for i, r := range c.Federation.Remotes {
		if strings.TrimSpace(r.Name) == "" || strings.ContainsAny(r.Name, " \t\r\n/\\") {
			return fmt.Errorf("federation.remotes[%d].name is a non-empty token without spaces or slashes, got %q", i, r.Name)
		}
		if seen[r.Name] {
			return fmt.Errorf("federation.remotes names %q twice", r.Name)
		}
		seen[r.Name] = true
		if strings.TrimSpace(r.Remote) == "" {
			return fmt.Errorf("federation.remotes.%s names no git remote", r.Name)
		}
		if r.Ref != "" && !strings.HasPrefix(r.Ref, "refs/") {
			return fmt.Errorf("federation.remotes.%s.ref is a full ref name (refs/…) or absent for the default ledger ref, got %q", r.Name, r.Ref)
		}
	}
	return nil
}

// validateBoundary holds the boundary block to its shape: the accepted
// kinds a non-empty list of distinct tokens, the ingress non-empty.
func (c *Config) validateBoundary() error {
	if c.Boundary == nil {
		return nil
	}
	if len(c.Boundary.Accepts) == 0 {
		return fmt.Errorf("boundary.accepts names the request kinds the deployment takes, at least one")
	}
	seen := map[string]bool{}
	for _, k := range c.Boundary.Accepts {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, " \t") || seen[k] {
			return fmt.Errorf("boundary.accepts entries are distinct non-empty tokens, got %q", k)
		}
		seen[k] = true
	}
	if strings.TrimSpace(c.Boundary.Ingress) == "" {
		return fmt.Errorf("boundary.ingress names the remote or endpoint a request is filed through")
	}
	return nil
}

func (c *Config) validatePreseed() error {
	if err := c.validateFederation(); err != nil {
		return err
	}
	if err := c.validateBoundary(); err != nil {
		return err
	}
	if err := c.validateExecutors(); err != nil {
		return err
	}
	if c.Governance != nil {
		if strings.TrimSpace(c.Governance.Root) == "" {
			return errors.New("governance.root names the governance root's key fingerprint")
		}
		if c.Governance.ChangeProcess != ChangeProcessPROwnerReview {
			return fmt.Errorf("governance.change_process is %q, the one process the charter names for the protected surface, got %q", ChangeProcessPROwnerReview, c.Governance.ChangeProcess)
		}
		for _, o := range c.Governance.Owners {
			if strings.TrimSpace(o) == "" {
				return errors.New("governance.owners must not carry an empty identity")
			}
		}
	}
	if c.Guardrails != nil {
		for name, g := range c.Guardrails.Squads {
			if strings.TrimSpace(name) == "" {
				return errors.New("guardrails.squads must not carry an empty squad name")
			}
			if strings.TrimSpace(g.Default) == "" || strings.TrimSpace(g.MaxAgent) == "" {
				return fmt.Errorf("guardrails.squads.%s declares both default and max_agent tiers", name)
			}
			if g.Racing != nil {
				if g.Racing.Racers < 2 {
					return fmt.Errorf("guardrails.squads.%s.racing.racers is %d: a race is two or more claims at once, and one is exclusivity (spec/lifecycle.md, Racing)", name, g.Racing.Racers)
				}
				if strings.TrimSpace(g.Racing.Cost) == "" {
					return fmt.Errorf("guardrails.squads.%s.racing.cost is empty: the compute-for-latency trade is stated where the opt-in is, in the operator's words", name)
				}
			}
		}
		for _, f := range c.Guardrails.Paths {
			if validProtectedEntry(f.Prefix) != nil || strings.TrimSpace(f.Min) == "" {
				return fmt.Errorf("guardrails.paths entries are clean repository-relative prefixes with a min tier, got %+v", f)
			}
		}
		// The approvals block's shape (plans/os-5781a026.md D1): one
		// entry per verb, the verb a token, the kinds distinct tokens,
		// the floor a token or absent. The vocabularies are the lint's.
		seenVerb := map[string]bool{}
		for i, a := range c.Guardrails.Approvals {
			if strings.TrimSpace(a.Verb) == "" || strings.ContainsAny(a.Verb, " \t\r\n") {
				return fmt.Errorf("guardrails.approvals[%d].verb names the governed verb, one token, got %q", i, a.Verb)
			}
			if seenVerb[a.Verb] {
				return fmt.Errorf("guardrails.approvals names %q twice: one entry per verb", a.Verb)
			}
			seenVerb[a.Verb] = true
			seenActor := map[string]bool{}
			for _, f := range a.Actors {
				if strings.TrimSpace(f) == "" || strings.ContainsAny(f, " \t\r\n") || seenActor[f] {
					return fmt.Errorf("guardrails.approvals.%s.actors entries are distinct key fingerprints, got %q", a.Verb, f)
				}
				seenActor[f] = true
			}
			seenKind := map[string]bool{}
			for _, k := range a.Kinds {
				if strings.TrimSpace(k) == "" || strings.ContainsAny(k, " \t\r\n") || seenKind[k] {
					return fmt.Errorf("guardrails.approvals.%s.kinds entries are distinct roster kinds, got %q", a.Verb, k)
				}
				seenKind[k] = true
			}
			if a.MinTier != "" && (strings.TrimSpace(a.MinTier) == "" || strings.ContainsAny(a.MinTier, " \t\r\n")) {
				return fmt.Errorf("guardrails.approvals.%s.min_tier is a tier or absent, got %q", a.Verb, a.MinTier)
			}
		}
	}
	if c.Teams != nil {
		seen := map[string]bool{}
		for _, s := range c.Teams.Squads {
			if strings.TrimSpace(s.Name) == "" {
				return errors.New("teams.squads must not carry an empty squad name")
			}
			if seen[s.Name] {
				return fmt.Errorf("teams.squads names %q twice", s.Name)
			}
			seen[s.Name] = true
			if len(s.Lanes) == 0 {
				return fmt.Errorf("teams.squads.%s runs at least one lane manifest", s.Name)
			}
			for _, l := range s.Lanes {
				if strings.TrimSpace(l) == "" {
					return fmt.Errorf("teams.squads.%s names an empty lane", s.Name)
				}
			}
		}
	}
	return nil
}

// validProtectedEntry admits a clean repository-relative path prefix:
// non-empty, not absolute, not escaping the root, nothing a git
// pathname would not spell the same way.
func validProtectedEntry(p string) error {
	trimmed := strings.TrimSuffix(p, "/")
	switch {
	case strings.TrimSpace(trimmed) == "":
		return errors.New("an empty entry protects nothing")
	case strings.HasPrefix(p, "/"):
		return errors.New("entries are repository-relative, never absolute")
	case strings.Contains(p, "\\"):
		return errors.New("entries use forward slashes")
	case path.Clean(trimmed) != trimmed || trimmed == "." || trimmed == ".." || strings.HasPrefix(trimmed, "../"):
		return errors.New("entries are clean relative paths (no '.', '..' or doubled separators)")
	}
	return nil
}

// validateExecutors holds the executors block to its shapes and refuses
// a secret where a name belongs (plans/os-083112ac.md D3).
func (c *Config) validateExecutors() error {
	e := c.Executors
	if e == nil {
		return nil
	}
	if e.Container != nil {
		switch e.Container.Runtime {
		case "docker", "podman", "fake":
		default:
			return fmt.Errorf("executors.container.runtime must be docker, podman or fake, got %q", e.Container.Runtime)
		}
		if strings.TrimSpace(e.Container.Image) == "" {
			return errors.New("executors.container.image must name an image reference")
		}
	}
	if e.Cloud != nil {
		u, err := url.Parse(e.Cloud.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("executors.cloud.endpoint must be an http(s) URL, got %q", e.Cloud.Endpoint)
		}
		if !envVarName.MatchString(e.Cloud.Credential) {
			return fmt.Errorf("executors.cloud.credential must be an environment-variable NAME, never a secret, got %q — the token lives in the environment, not the tree", e.Cloud.Credential)
		}
	}
	if e.Remote != nil {
		for _, w := range e.Remote.Workers {
			if strings.TrimSpace(w.Name) == "" || strings.TrimSpace(w.Environment) == "" {
				return errors.New("executors.remote.workers each need a name and an environment")
			}
		}
	}
	return nil
}

// validateAdmission holds the admission block to its posture: required
// and well-formed under enforced-forge-hosted (#244), refused elsewhere.
func (c *Config) validateAdmission() error {
	if c.Posture != EnforcedForgeHosted {
		if c.Admission != nil {
			return fmt.Errorf("the admission block is valid under %s only (this declaration says %q)", EnforcedForgeHosted, c.Posture)
		}
		return nil
	}
	a := c.Admission
	if a == nil {
		return fmt.Errorf("%s requires the admission block {endpoint, identity, ledger_ref, checks, reviews, owners}: the service actors propose to and the identity the forge lets write the ledger branch", EnforcedForgeHosted)
	}
	u, err := url.Parse(a.Endpoint)
	if a.Endpoint == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("admission.endpoint must be an http(s) URL the admission service answers on, got %q", a.Endpoint)
	}
	if strings.TrimSpace(a.Identity) == "" {
		return errors.New("admission.identity must name the forge identity the admission service pushes as (the ledger branch's sole writer)")
	}
	if a.LedgerRef != "" && !strings.HasPrefix(a.LedgerRef, "refs/heads/") {
		return fmt.Errorf("admission.ledger_ref must be a branch (refs/heads/...), because forges protect branches and tags and nothing under refs/seed/*: got %q", a.LedgerRef)
	}
	if a.Reviews < 0 {
		return fmt.Errorf("admission.reviews must be zero or more, got %d", a.Reviews)
	}
	for _, chk := range a.Checks {
		if strings.TrimSpace(chk) == "" {
			return errors.New("admission.checks must not carry an empty check name")
		}
	}
	for _, o := range a.Owners {
		if strings.TrimSpace(o) == "" {
			return errors.New("admission.owners must not carry an empty identity")
		}
	}
	switch a.Forge {
	case "", ForgeGitHub, ForgeForgejo:
	default:
		return fmt.Errorf("admission.forge must be %q or %q (absent means %q), got %q", ForgeGitHub, ForgeForgejo, ForgeGitHub, a.Forge)
	}
	if a.Api != "" {
		u, err := url.Parse(a.Api)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("admission.api must be an http(s) URL naming the forge's API, got %q", a.Api)
		}
	}
	if a.Forge == ForgeForgejo && a.Api == "" {
		return errors.New("admission.api is required under forgejo: Forgejo has no public API, so the declaration must name its instance")
	}
	return nil
}

// ProtectedSurface is the declared list plus the declaration's own
// path, deduplicated and sorted: the enumeration the hook and the
// doctor both report.
func (c *Config) ProtectedSurface() []string {
	seen := map[string]bool{DeclarationPath: true}
	out := []string{DeclarationPath}
	for _, p := range c.Protected {
		e := strings.TrimSuffix(p, "/")
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}

// Protects reports whether a repository path falls on the protected
// surface: equal to an entry, or under it as a directory.
func (c *Config) Protects(p string) bool {
	for _, e := range c.ProtectedSurface() {
		if p == e || strings.HasPrefix(p, e+"/") {
			return true
		}
	}
	return false
}
