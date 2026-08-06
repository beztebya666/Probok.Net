package main

import "sync"

type requestBudget struct {
	mu       sync.Mutex
	limit    int
	used     int
	reserved int
	billable int
}

type budgetLease struct {
	budget    *requestBudget
	allowance int
	done      bool
}

func newRequestBudget(limit int) *requestBudget { return &requestBudget{limit: limit} }

func (b *requestBudget) lease(maxAllowance int) (*budgetLease, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	available := b.limit - b.used - b.reserved
	if available <= 0 {
		return nil, false
	}
	if maxAllowance < 1 {
		maxAllowance = 1
	}
	if available > maxAllowance {
		available = maxAllowance
	}
	b.reserved += available
	return &budgetLease{budget: b, allowance: available}, true
}

func (l *budgetLease) finish(actual int) {
	if l == nil || l.done {
		return
	}
	if actual < 0 {
		actual = 0
	}
	if actual > l.allowance {
		actual = l.allowance
	}
	l.budget.mu.Lock()
	l.budget.reserved -= l.allowance
	l.budget.used += actual
	l.budget.mu.Unlock()
	l.done = true
}

func (b *requestBudget) snapshot() (used, remaining int, exhausted bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used, b.limit - b.used - b.reserved, b.used+b.reserved >= b.limit
}

func (b *requestBudget) addBillable(units int) {
	if units <= 0 {
		return
	}
	b.mu.Lock()
	b.billable += units
	b.mu.Unlock()
}

func (b *requestBudget) billableUnits() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.billable
}
