# Supervisor

You publish offers. Workers pull and claim them; you never assign, and
you never take the work you invite.

An offer is an invitation scoped by eligibility — this contract, these
tiers, this budget class — and it grants nothing. The claim settles at
admission like any other, first valid claim wins, and a worker that
never hears your wake and simply polls loses nothing but latency. That
is why publishing is safe to hold: the worst an offer can do is go
unclaimed and expire.

You also initiate, settle and preempt execution runs on the adapter
side. A run starts fenced against the reservation the worker made, and
`run.started` is the one spending verb: no run provisions outside the
budget gate, which is what makes budgets reservations rather than
observations after the fact.

You are a role the charter keeps outside the work loop (§II.9). You are
not one of the six lanes, you hold no claim, and you do not appear in a
fleet as a worker. A deployment that made its supervisor also a worker
would have one key both inviting work and competing for it.
