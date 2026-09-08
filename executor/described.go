package executor

// Described is the optional half of the adapter contract
// (plans/os-083112ac.md D2): an adapter states whether its substrate can
// stop spend synchronously. It is optional so the public Adapter
// interface is unchanged — an external adapter that does not implement it
// is treated as a risk limit, the honest default. The supervisor and the
// report read it to say, per adapter, whether the budget is a guarantee
// or a limit.

// Budget postures an adapter may declare.
const (
	// BudgetEnforced: the supervisor can kill the substrate, so the
	// reservation is a guarantee (local worktree, container).
	BudgetEnforced = "enforced"
	// BudgetRiskLimit: a provider or a remote process may spend past the
	// reservation before the interrupt lands, so the budget is honestly
	// a risk limit, never a guarantee (cloud session, remote worker).
	BudgetRiskLimit = "risk-limit"
)

// Description is an adapter's static self-report.
type Description struct {
	Name    string `json:"name"`
	Harness string `json:"harness"`
	Budget  string `json:"budget"`
	Reason  string `json:"reason"`
}

// Described is implemented by an adapter that states its budget posture.
type Described interface {
	Describe() Description
}

// DescribeOf returns an adapter's description, defaulting an adapter that
// does not implement Described to a risk limit — the safe assumption
// when the substrate has not proven it can be stopped synchronously.
func DescribeOf(name string, a Adapter) Description {
	if d, ok := a.(Described); ok {
		return d.Describe()
	}
	return Description{Name: name, Harness: a.Tuple().Harness, Budget: BudgetRiskLimit,
		Reason: "the adapter does not state its budget posture, so spend is treated as a risk limit"}
}

// Describe reports the local worktree adapter's posture: the supervisor
// kills the worktree's processes, so the reservation is enforced.
func (LocalWorktree) Describe() Description {
	return Description{Name: "local-worktree", Harness: LocalHarness, Budget: BudgetEnforced,
		Reason: "the supervisor kills the worktree's own processes, so the reservation is a guarantee"}
}
