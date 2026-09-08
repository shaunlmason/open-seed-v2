// The bridge from the loop's verb seam to this CLI's dispatch
// (plans/os-abb206c8.md D1). internal/loop is a library that performs
// every act through a seam rather than reimplementing one; this is the
// implementation of that seam, and it lives here because run() is the
// dispatch and package main is not importable.
//
// It exists so the loop can be driven IN PROCESS: item 4's small-team
// and fleet fixtures run with no model and no wake channel, and a
// library plus this adapter is what they can drive. A `seed loop run`
// verb is deliberately absent (D1): it would invite treating the CLI as
// the agent, when the work step is the caller's and always was.
package main

import (
	"bytes"
	"encoding/json"

	"github.com/shaunlmason/open-seed-v2/internal/loop"
)

// loopVerbs runs seed verbs through run(), parsing the envelope the
// loop reasons about. Nothing here interprets a refusal: the code and
// message travel to the loop exactly as rendered, because the packet
// the loop writes on an exit must carry the boundary's own account.
type loopVerbs struct{}

// Run implements loop.Verbs.
func (loopVerbs) Run(args ...string) loop.Result {
	var out, errOut bytes.Buffer
	exit := run(args, &out, &errOut)
	res := loop.Result{Exit: exit}
	var env struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Position *string `json:"position"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		// An unparseable envelope is a refusal the loop can still act
		// on: it exits deliberately rather than treating silence as
		// success, which is the failure mode that leaves a claim
		// standing.
		res.Code = "unavailable"
		res.Message = "the verb produced no envelope this build can read: " + err.Error()
		return res
	}
	res.OK = env.OK
	res.Result = env.Result
	if env.Error != nil {
		res.Code = env.Error.Code
		res.Message = env.Error.Message
	}
	if env.Position != nil {
		res.Position = *env.Position
	}
	return res
}
