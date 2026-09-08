package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shaunlmason/open-seed-v2/internal/envelope"
	"github.com/shaunlmason/open-seed-v2/internal/event"
	"github.com/shaunlmason/open-seed-v2/internal/importer"
	"github.com/shaunlmason/open-seed-v2/internal/refusal"
)

// runImport is the second of the two commands (plans/os-cf13fb51.md
// D1; spec/import.md): `scripts/seed state export` on the
// predecessor, then `seed import --from-open-seed <export> --source
// <clone> --ledger <dir> --artifacts <dir> --key <key> [--anchor <tag>]
// [--repo <dir>]`. Anchors are verified before any transform, the
// transform is the table, and every event meets the boundary.
func runImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	export := fs.String("from-open-seed", "", "the predecessor's export (scripts/seed state export)")
	source := fs.String("source", "", "clone whose seed-state history and seed-anchor tags anchor the export")
	repo := fs.String("repo", "", "checkout the cited receipts and plans are read from (default: --source)")
	anchor := fs.String("anchor", "", "the seed-anchor tag to verify against (default: the newest in --source)")
	ledgerDir := fs.String("ledger", "", "empty ledger directory to write")
	artifacts := fs.String("artifacts", "", "artifact store to write the cited bodies, receipts, plans and the manifest to")
	keyPath := fs.String("key", "", "OpenSSH ed25519 private key of the importing operator")
	if err := fs.Parse(args); err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", err.Error()), stdout, stderr)
	}
	if *export == "" || *source == "" || *ledgerDir == "" || *artifacts == "" || *keyPath == "" || fs.NArg() != 0 {
		return render(envelope.Fail(envelope.ExitUsage, "usage", "import requires --from-open-seed <export> --source <clone> --ledger <dir> --artifacts <dir> --key <openssh-ed25519-private> [--anchor <tag>] [--repo <dir>]"), stdout, stderr)
	}
	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --key: %v", err)), stdout, stderr)
	}
	signer, err := event.ParsePrivateKey(keyBytes)
	if err != nil {
		return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("--key: %v", err)), stdout, stderr)
	}
	exp, err := importer.ReadExport(*export)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return render(envelope.Fail(envelope.ExitUsage, "usage", fmt.Sprintf("cannot read --from-open-seed: %v", err)), stdout, stderr)
		}
		return render(envelope.Fail(envelope.ExitUnreadable, "unreadable", err.Error()), stdout, stderr)
	}
	res, err := importer.Run(importer.Options{
		Export: exp, Source: *source, Repo: *repo, AnchorTag: *anchor,
		LedgerDir: *ledgerDir, ArtifactsDir: *artifacts, Operator: signer, Now: time.Now(),
	})
	if err != nil {
		return render(importEnvelope(err), stdout, stderr)
	}
	m := res.Manifest
	identities := make([]map[string]any, 0, len(m.Identities))
	for _, id := range m.Identities {
		identities = append(identities, map[string]any{"name": id.Name, "kind": id.Kind, "fingerprint": id.Fingerprint, "grants": id.Grants, "enrolled": id.Enrolled, "suspended": id.Suspended, "known": id.Known})
	}
	return render(envelope.OK(map[string]any{
		"ledger":      *ledgerDir,
		"artifacts":   *artifacts,
		"anchor":      map[string]string{"tag": res.Anchor.Tag, "commit": res.Anchor.Commit},
		"export_head": exp.Head,
		"manifest":    res.ManifestDigest,
		"imported":    res.Imported,
		"records":     res.Records,
		"counts":      m.Counts,
		"identities":  identities,
		"unresolved":  m.Unresolved,
	}), stdout, stderr)
}

// importEnvelope maps the importer's refusals: the anchor and table
// refusals under exit 29 (import_refused), the non-empty ledger under
// the genesis refusal, and a record the boundary refused as the
// boundary's own envelope naming the export record.
func importEnvelope(err error) *envelope.Envelope {
	var ref *importer.Refusal
	if errors.As(err, &ref) {
		switch ref.Kind {
		case "ledger_not_empty":
			return envelope.Fail(envelope.ExitInvalidTransition, "ledger_not_empty", ref.Detail)
		default:
			return envelope.Fail(envelope.ExitImportRefused, ref.Kind, ref.Detail)
		}
	}
	var refused *importer.RefusedError
	if errors.As(err, &refused) {
		env := refusal.Envelope(refused.Err)
		if env.Error != nil {
			env.Error.Message = refused.Error()
		}
		pos := fmt.Sprintf("%d", refused.Position)
		env.Position = &pos
		return env
	}
	return envelope.Fail(envelope.ExitUnavailable, "unavailable", err.Error())
}
