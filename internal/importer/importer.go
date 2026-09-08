package importer

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/artifact"
	"github.com/shaunlmason/open-seed-v2/internal/ledger"
)

// Options is one import.
type Options struct {
	Export       *Export
	Source       string // the clone whose history anchors the export
	Repo         string // where cited receipts and plans are read (default: Source)
	AnchorTag    string // empty: the newest seed-anchor tag
	LedgerDir    string
	ArtifactsDir string
	Operator     ed25519.PrivateKey
	Now          time.Time
	Table        *Table // nil: the embedded table
	// Trace, when set, is told how long each phase took.
	Trace      func(phase string, d time.Duration)
	TableBytes []byte // the table's bytes, for the manifest's digest
}

// Result is what an import wrote.
type Result struct {
	Anchor         *Anchor
	Manifest       *Manifest
	ManifestDigest string
	Imported       int // the position of system.imported
	Records        int
	Artifacts      int
}

// Run performs the import: anchors first, the table's completeness,
// the empty-ledger check, then three passes of the transform (grants
// discovered, positions fixed, the manifest cited), and only then the
// write, followed by verification from genesis.
func Run(o Options) (*Result, error) {
	if o.Export == nil {
		return nil, errors.New("no export")
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.Repo == "" {
		o.Repo = o.Source
	}
	phase := time.Now()
	trace := func(name string) {
		if o.Trace != nil {
			o.Trace(name, time.Since(phase))
		}
		phase = time.Now()
	}
	table := o.Table
	tableBytes := o.TableBytes
	if table == nil {
		var err error
		if table, err = DefaultTable(); err != nil {
			return nil, err
		}
		tableBytes = TableJSON()
	}
	anchor, err := VerifyAnchor(o.Export, o.Source, o.AnchorTag)
	if err != nil {
		return nil, err
	}
	trace("anchor")
	src, err := Read(o.Export)
	if err != nil {
		return nil, err
	}
	trace("read")
	if um := table.Unmapped(src.Verbs()); len(um) > 0 {
		return nil, &Refusal{Kind: "import_unmapped", Detail: fmt.Sprintf("run-log verbs with no row in the transform table: %s — a drop is a row, and an entry is never skipped silently", strings.Join(um, ", "))}
	}
	for _, id := range src.CardIDs {
		if _, ok := table.States[src.Cards[id].State]; !ok {
			return nil, &Refusal{Kind: "import_unmapped", Detail: fmt.Sprintf("card %s declares state %q, which has no row in the transform table", id, src.Cards[id].State)}
		}
	}
	if count, err := ledgerRecords(o.LedgerDir); err != nil {
		return nil, err
	} else if count > 0 {
		return nil, &Refusal{Kind: "ledger_not_empty", Detail: fmt.Sprintf("%s holds %d records: the import is a genesis transform and writes into empty ledgers only", o.LedgerDir, count)}
	}
	op, err := newSigner(o.Operator)
	if err != nil {
		return nil, err
	}
	ros, err := newRoster(table, src.Names())
	if err != nil {
		return nil, err
	}
	r := newReplay(src, table, ros, op, o.Repo, anchor, o.Now)
	// The rehearsal: the whole transform over a dry chain that folds
	// the lifecycle without admission, so the verbs each identity
	// signs — the bridges included — are known before any grant is
	// derived (D3: grants derived from the run-log before replay).
	if err := r.run(placeholderDigest, true); err != nil {
		return nil, err
	}
	ros.deriveGrants()
	trace("rehearsal")
	// The first admitted pass cites a placeholder manifest digest; its
	// positions are what the manifest records, since only the payload
	// of system.imported changes when the real digest is cited.
	if err := r.run(placeholderDigest, false); err != nil {
		return nil, err
	}
	trace("pass-1")
	manifest := r.manifest(tableDigest(tableBytes))
	if err := Check(manifest, src); err != nil {
		return nil, fmt.Errorf("the manifest is not lossless: %v", err)
	}
	mb, err := manifest.Bytes()
	if err != nil {
		return nil, err
	}
	digest := artifact.Digest(mb)
	fixed := r.manifest(tableDigest(tableBytes))
	// The second pass cites the real digest; every record after
	// system.imported is re-signed over the new prev and re-admitted,
	// and the check is that every position reproduced.
	if err := r.run(digest, false); err != nil {
		return nil, err
	}
	trace("pass-2")
	final := r.manifest(tableDigest(tableBytes))
	if err := samePlacement(fixed, final); err != nil {
		return nil, fmt.Errorf("the manifest's positions did not reproduce: %v", err)
	}
	// Nothing above touched the target; the store is created only now,
	// for a chain already admitted in full, and the emptiness read
	// above is repeated against the store it opens.
	store, err := ledger.Open(o.LedgerDir)
	if err != nil {
		return nil, fmt.Errorf("cannot open ledger dir: %v", err)
	}
	if _, count, err := store.Tip(); err != nil {
		return nil, err
	} else if count > 0 {
		return nil, &Refusal{Kind: "ledger_not_empty", Detail: fmt.Sprintf("%s gained %d records while the import ran", o.LedgerDir, count)}
	}
	art := artifact.Open(o.ArtifactsDir)
	for _, b := range r.artifacts {
		if _, err := art.Put(b); err != nil {
			return nil, fmt.Errorf("artifact store: %v", err)
		}
	}
	if _, err := art.Put(mb); err != nil {
		return nil, fmt.Errorf("artifact store: %v", err)
	}
	trace("artifacts")
	if _, err := store.AppendAll(r.ch.records, r.ch.resolve); err != nil {
		return nil, fmt.Errorf("ledger append: %v", err)
	}
	trace("write")
	if _, err := store.VerifyFromGenesis(r.ch.resolve); err != nil {
		return nil, fmt.Errorf("the written ledger does not verify from genesis: %v", err)
	}
	trace("verify")
	return &Result{Anchor: anchor, Manifest: manifest, ManifestDigest: digest, Imported: r.importedPos, Records: len(r.ch.records), Artifacts: len(r.artifacts) + 1}, nil
}

func samePlacement(a, b *Manifest) error {
	if len(a.Records) != len(b.Records) {
		return fmt.Errorf("%d dispositions, then %d", len(a.Records), len(b.Records))
	}
	for i := range a.Records {
		x, y := a.Records[i], b.Records[i]
		if x.Record != y.Record || len(x.Events) != len(y.Events) || x.Artifact != y.Artifact || x.Drop != y.Drop {
			return fmt.Errorf("%s differs between passes", x.Record)
		}
		for j := range x.Events {
			if x.Events[j].Position != y.Events[j].Position || x.Events[j].Verb != y.Events[j].Verb {
				return fmt.Errorf("%s event %d differs between passes", x.Record, j)
			}
		}
	}
	if len(a.Identities) != len(b.Identities) {
		return fmt.Errorf("%d identities, then %d", len(a.Identities), len(b.Identities))
	}
	for i := range a.Identities {
		if a.Identities[i].Enrolled != b.Identities[i].Enrolled || a.Identities[i].Suspended != b.Identities[i].Suspended {
			return fmt.Errorf("identity %s differs between passes", a.Identities[i].Name)
		}
	}
	return nil
}

// ledgerRecords counts a ledger directory's records without creating
// or healing anything: an absent directory is an empty ledger, and one
// that exists is opened read-only.
func ledgerRecords(dir string) (int, error) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	store, err := ledger.OpenReadOnly(dir)
	if err != nil {
		return 0, fmt.Errorf("cannot read ledger dir: %v", err)
	}
	_, count, err := store.Tip()
	return count, err
}
