# os-37fcf7c6 — `ledger show` stamps the position its chain_invalid was computed at

The review finding on #183 that `plans/os-fa69345e.md` D3 deliberately
declined and pinned with a tripwire: `seed ledger show --position` on
a chain that fails to parse partway refuses `chain_invalid` with no
position, while `seed ledger verify` on the same chain stamps the
failing position. `next/spec/envelope.md` defines the stamp as "the
ledger position the response was computed at" and reserves null for a
refusal raised before any position was read; a scan that read record
0 and failed at record 1 was computed at a position. This card is the
cleanup D3 named, made deliberate.

## What the tree actually shows

- `runLedgerShow` (`next/cmd/seed/ledger.go`) counts inside the
  `store.Records` callback (`count = pos + 1`) and, when the scan
  returns a parse error, renders `chain_invalid` unstamped with the
  comment that a partial count is not a statement about the chain.
- `store.Records` returns the parse error for position `p` before it
  invokes the callback for `p`, so at the failure `count == p`: the
  failing position is already in scope.
- `runLedgerVerify` stamps `fail.Position` and
  `TestLedgerVerifyFailureExitCodes` pins it ("verification refusals
  are position-stamped envelopes"); `TestLedgerShow…` pins the
  opposite for show, in a comment that calls itself a tripwire so the
  change is a decision rather than an accident.

## Design decisions (binding for this task)

**D1. The stamp is where the response was computed, so show stamps
the failing position.** `runLedgerShow`'s scan failure renders
`chain_invalid` stamped with `count` — the position of the record that
did not parse, the same stamp verify gives the same chain. The
message is unchanged. Null stays for a refusal raised before any
position was read: an unreadable ledger directory, a malformed
invocation. Refused: stamping `count-1` (the last good position — the
spec's word is where the response was computed, and it was computed at
the failing record) or the chain's HEAD count (a claim about the chain
the scan did not establish).

**D2. The tripwire inverts, and the spec says "before any position was
read".** The show drill asserts the failing position (`"1"` for the
fixture that corrupts record 1), beside verify's assertion of the same
value on the same chain; `envelope.md`'s sentence on null sharpens from
"before a tip was ever read" to "before any position was read", which
is what both verbs now do.

**D3. Bounds.** No ledger change, no new code, no other verb's stamp
touched. The `--position` not-found path keeps stamping the count
(the tip the scan reached).

## Steps

1. `next/cmd/seed/ledger.go`: the one return.
2. `next/cmd/seed/ledger_test.go`: invert the pin, asserting show's
   and verify's stamps agree on one corrupted chain.
3. `next/spec/envelope.md`: the sentence; `next/docs/progress.md`,
   `next/docs/decisions.md`; receipt; evidence; review.

## File Scope

- `next/cmd/seed/ledger.go`, `next/cmd/seed/ledger_test.go`
- `next/spec/envelope.md`
- `next/docs/progress.md`, `next/docs/decisions.md`
- `receipts/os-37fcf7c6.json`

Nothing else. NOT `next/internal/**`, NOT `.seed/**`, NOT `scripts/**`.

## Acceptance Criteria

**Boundary set (new, shown working):**

1. `seed ledger show --position <n>` on a chain whose record `p` does
   not parse refuses `chain_invalid` (exit 8) stamped `p`, the same
   stamp `seed ledger verify` gives that chain, asserted on one
   fixture by one drill.
2. A refusal raised before any position was read (an unreadable
   directory) stays unstamped, asserted.
3. `make check` green with coverage measured cold, three readings
   above the gate; no model identifiers in any committed artifact.

**Retention set (existing, shown unharmed):**

- Every other show and verify envelope is byte-identical; the
  not-found path still stamps the tip the scan reached; every
  pre-existing chain verifies byte for byte.

## Validation Commands

- Boundary: `cd next && go test ./cmd/seed/ -run 'TestLedgerShow|TestLedgerVerify' -count=1`
- Retention: `cd next && gofmt -l . && go vet ./... && go build ./... && go test ./... -count=1`
- Retention: `make check` (exit checked separately from any pipe; three cold readings)

## Expected diff shape

Modified: one return in `next/cmd/seed/ledger.go`, the inverted pin in
`next/cmd/seed/ledger_test.go`, one sentence in `next/spec/envelope.md`,
the two docs files, the receipt. Nothing new.
