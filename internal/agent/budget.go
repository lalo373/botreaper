package agent

// Budget mirrors agent/iteration_budget.py: caps API calls per turn with a
// one-call grace window so a turn never dies mid-tool-round.
type Budget struct {
	max       int
	used      int
	graceUsed bool
}

// NewBudget creates a turn budget (max<=0 means the 500 default).
func NewBudget(max int) *Budget {
	if max <= 0 {
		max = 500
	}
	return &Budget{max: max}
}

// Admit reports whether another API call may start.
func (b *Budget) Admit() bool {
	if b.used < b.max {
		return true
	}
	if !b.graceUsed {
		b.graceUsed = true
		return true
	}
	return false
}

// Spend records one API call.
func (b *Budget) Spend() { b.used++ }

// Used returns consumed calls.
func (b *Budget) Used() int { return b.used }
