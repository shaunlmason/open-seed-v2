package importer

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
)

// ManifestSchema names the mapping manifest's shape.
const ManifestSchema = "seed-import-manifest/0"

// Placed is one event an export record became.
type Placed struct {
	Position int    `json:"position"`
	Verb     string `json:"verb"`
	Subject  string `json:"subject"`
	Signer   string `json:"signer"`
}

// Disposition is what one export record became: events, an artifact,
// or a drop row, exactly one kind per record. Bridges (events the
// transform inserted so a v1 move fits Seed's table) ride on the
// record that needed them, noted.
type Disposition struct {
	Record   string   `json:"record"`
	Verb     string   `json:"verb,omitempty"`
	Actor    string   `json:"actor,omitempty"`
	Events   []Placed `json:"events,omitempty"`
	Artifact string   `json:"artifact,omitempty"`
	Drop     string   `json:"drop,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// Kind is the disposition's kind: event, artifact, drop, or none (a
// record that produced nothing, noted).
func (d Disposition) Kind() string {
	switch {
	case len(d.Events) > 0:
		return "event"
	case d.Artifact != "":
		return "artifact"
	case d.Drop != "":
		return "drop"
	}
	return "none"
}

// IdentityRow is one import-generated identity as the manifest lists it.
type IdentityRow struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Kind        string   `json:"kind"`
	Known       bool     `json:"known"`
	Fingerprint string   `json:"fingerprint"`
	Grants      []string `json:"grants"`
	Enrolled    int      `json:"enrolled"`
	Suspended   int      `json:"suspended"`
}

// Counts is the losslessness arithmetic.
type Counts struct {
	Records      int `json:"records"`
	Dispositions int `json:"dispositions"`
	Events       int `json:"events"`
	Artifacts    int `json:"artifacts"`
	Drops        int `json:"drops"`
	None         int `json:"none"`
}

// Manifest is the mapping manifest system.imported cites: every export
// record's disposition, every identity, the counts, and what could not
// be resolved.
type Manifest struct {
	Schema     string        `json:"schema"`
	Source     string        `json:"source"`
	ExportHead string        `json:"export_head"`
	Anchor     Anchor        `json:"anchor"`
	Table      string        `json:"table"`
	Keys       string        `json:"keys"`
	Identities []IdentityRow `json:"identities"`
	Records    []Disposition `json:"records"`
	Counts     Counts        `json:"counts"`
	Unresolved []string      `json:"unresolved"`
}

// KeysNote is the manifest's statement about provenance.
const KeysNote = "every identity below was enrolled with a key the importer generated and held in memory for this import only; attribution is not trust (spec/actors.md), and each key is suspended at the position listed"

func (m *Manifest) finish(records int) {
	m.Counts = Counts{Records: records, Dispositions: len(m.Records)}
	for _, d := range m.Records {
		switch d.Kind() {
		case "event":
			m.Counts.Events += len(d.Events)
		case "artifact":
			m.Counts.Artifacts++
		case "drop":
			m.Counts.Drops++
		default:
			m.Counts.None++
		}
	}
	if m.Unresolved == nil {
		m.Unresolved = []string{}
	}
	sort.Strings(m.Unresolved)
}

// Bytes renders the manifest as indented JSON.
func (m *Manifest) Bytes() ([]byte, error) { return json.MarshalIndent(m, "", "  ") }

// Check is the losslessness drill: every export record has exactly one
// disposition, and the counts agree.
func Check(m *Manifest, src *Source) error {
	want := map[string]bool{}
	for _, e := range src.Entries {
		want[fmt.Sprintf("run-log#%d", e.Index)] = true
	}
	for p := range src.Export.Files {
		if p != "run-log.jsonl" {
			want[p] = true
		}
	}
	seen := map[string]bool{}
	for _, d := range m.Records {
		if seen[d.Record] {
			return fmt.Errorf("%s has two dispositions", d.Record)
		}
		seen[d.Record] = true
		if !want[d.Record] {
			return fmt.Errorf("%s is a disposition for a record the export does not hold", d.Record)
		}
	}
	for r := range want {
		if !seen[r] {
			return fmt.Errorf("%s has no disposition", r)
		}
	}
	if m.Counts.Records != len(want) || m.Counts.Dispositions != len(m.Records) || m.Counts.Records != m.Counts.Dispositions {
		return fmt.Errorf("counts disagree: %d records, %d dispositions", m.Counts.Records, m.Counts.Dispositions)
	}
	// Every figure is recomputed from the dispositions, not trusted.
	again := &Manifest{Records: m.Records}
	again.finish(len(want))
	if again.Counts != m.Counts {
		return fmt.Errorf("counts disagree with the dispositions: stated %+v, recomputed %+v", m.Counts, again.Counts)
	}
	return nil
}

// ParseManifest reads a manifest from the artifact store by digest.
func ParseManifest(store *artifact.Store, digest string) (*Manifest, error) {
	b, err := store.Get(digest)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("the manifest does not parse: %v", err)
	}
	if m.Schema != ManifestSchema {
		return nil, fmt.Errorf("the manifest is %s, not %s", m.Schema, ManifestSchema)
	}
	return &m, nil
}
