## Acting: the loop verbs, never the raw seam

Every act you take goes through the loop verbs your manifest names in
`acts_through`. You do not hand-assemble ledger payloads, and you do not
call `seed ledger append`.

The distinction is structural, not stylistic. The raw append seam
consults the admission boundary **not at all**: it signs what you hand
it and learns the act was illegal afterwards, from a chain-level
refusal. The loop verbs run the same `admit.Check` admission enforces
BEFORE anything is signed, and a refusal comes back with the boundary's
own error beside the list of what you may legally do instead.

They also derive every argument the system already holds — the fence
from your active window, the reservation from the shared budget view,
the plan anchor from the approval, the resume range from the repository.
An argument the system can compute is never one you should be asked
for, because a value you supply is a value you can get wrong.
