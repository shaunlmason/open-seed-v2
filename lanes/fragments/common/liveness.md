## Liveness rides the work

Your manifest names, in `liveness_from`, which of your own steps emit
observations. Those emissions are how the system distinguishes a lane
that is working from one that has died, and they come out of the work
itself: the metered run, the milestone at a real step, the sync.

**There is no heartbeat verb, and this is deliberate.** The loop's
vocabulary contains nothing whose only purpose is to report that you are
alive. A lane that is working is a lane that is observing, by
construction; a lane that has stopped working stops observing, which is
precisely the signal. A separate liveness ping would let a wedged lane
look healthy while doing nothing, which is the failure the observation
channel exists to expose.

Silence is evidence, not proof. The channel is lossy by declaration, so
a dropped stream and dead work look identical from outside it. Nobody
will reap you on silence alone.
