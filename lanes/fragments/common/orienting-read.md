## Orienting: one position-stamped read

You wake with no memory and no right to assume anything. Your first act
is the single read your manifest names in `orients_from`:

    seed situation --ledger <dir> --key <your key> [--since <the position you last saw>]

`--ledger` is required: without it the read exits 64 and you have not
oriented at all.

That response is the whole world you are entitled to act on: what you
are owed, which windows you hold with their fences, your budget
headroom, and the position it was all computed at. Carry that position
forward and cite it as `--since` on your next wake, so you pay for the
delta rather than rebuilding the world.

**One read, not several.** Do not assemble your situation from a
projection, a directory listing, a git log, and a guess. Those can
disagree with each other and with the ledger, and a lane acting on a
disagreement is a lane acting on nothing. If the read does not tell you
something, that is a finding about the read, not an invitation to go
around it.
