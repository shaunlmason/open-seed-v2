# The one fast backpressure command. Agents merge whatever passes, so
# keep it fast and deterministic: it is the term that multiplies at
# scale. Tool output is captured and shown only on failure, because
# check's output must be byte-stable run to run.

.PHONY: check

check:
	@badfmt="$$(gofmt -l .)" && { test -z "$$badfmt" || { echo "check: gofmt failures:"; echo "$$badfmt"; exit 1; }; }
	@out="$$(go vet ./... 2>&1)" || { echo "check: go vet failed:"; echo "$$out"; exit 1; }
	@out="$$(go build ./... 2>&1)" || { echo "check: go build failed:"; echo "$$out"; exit 1; }
	@# The suite and the gate run through cmd/covergate, because coverage
	@# collection is lossy at a low rate and the rule that saves you (re-collect
	@# COLD once, then treat a second failure as real) is one an unattended
	@# agent must otherwise apply against its own instinct.
	@go run ./cmd/covergate -gate 90 -dir .
	@# The four metrics against the representative history, each held to the
	@# ceiling in perf/budgets.json, a miss re-measured cold once before it fails.
	@go run ./cmd/perfgate -budgets perf/budgets.json -dir .
	@# The fixture deployment's declaration is CI-verified: tiers in the
	@# vocabulary, teams naming shipped manifests, the protected surface complete.
	@go run ./cmd/seed preseed check --config fixtures/deployment/seed.json --lanes lanes >/dev/null
	@go run ./cmd/seed boundary check --config fixtures/deployment/seed.json --name open-seed-v2 --card boundary/card.json >/dev/null
	@# The governed docs are drift-checked: the lifecycle, capability,
	@# exit-code and per-lane documents must match what `seed docs generate`
	@# renders from the tables they came from.
	@go run ./cmd/seed docs check --root . >/dev/null
