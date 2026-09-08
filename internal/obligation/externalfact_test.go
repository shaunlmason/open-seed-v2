package obligation

import "testing"

// conformance: plans/os-b45c308d.md D5 — an external fact discharges
// nothing: check.observed and the other catalogued observations
// appear in no fact-shaped discharger set, and merge.observed appears
// only as the observer's close of the merge debt behind a citing
// request.
func TestExternalFactDischargesNothing(t *testing.T) {
	for kind, verbs := range factDischargers {
		for _, v := range verbs {
			switch v {
			case "check.observed", "merge.observed", "plan.approved", "workflow.merged", "curation.lesson.promoted", "curation.lesson.retired":
				t.Errorf("%s is discharged by the observation %s", kind, v)
			}
		}
	}
}
