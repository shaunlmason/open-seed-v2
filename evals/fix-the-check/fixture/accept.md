# fix-the-check: acceptance

The greeting `greet.sh` prints must be exactly `hello, world`. The
fixture ships with a misspelling, so this check is red until the
one-line fix lands; the reference solution under `solution/` makes it
green.

## Validation commands

- `sh evals/fix-the-check/fixture/check.sh`
