# The injection corpus

Hostile text for the dispatcher's conformance suite
(`plans/os-b779b4c7.md`; charter III.J). Each file is a payload
fragment a persuaded dispatcher might be talked into filing, named for
what it attempts.

These are **not** parser inputs. Nothing here is tested for being
"detected": there is no model under `next/`, so "never obeyed" is not
directly testable, and a suite that fed injection strings to code with
no instructions to ignore would test the corpus rather than the system.
The corpus exists so that every assertion about the boundary is made
with adversarial text in the payload rather than with `{}`, and so that
the two acts whose consequence a persuaded lane can actually inflict
are exercised with the text that would do the persuading.

The invariant under test is the charter's own: *capability bounds, not
fencing, carry the invariant.* Believing this text changes nothing,
except where `residuals.json` says it does.
