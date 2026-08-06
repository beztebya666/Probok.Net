package main

import (
	"sync"
	"testing"
)

func TestRequestBudgetCannotOversubscribe(t *testing.T) {
	budget := newRequestBudget(5)
	var wg sync.WaitGroup
	var acquired int
	var mutex sync.Mutex
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, ok := budget.lease(2)
			if !ok {
				return
			}
			mutex.Lock()
			acquired += lease.allowance
			mutex.Unlock()
			lease.finish(lease.allowance)
		}()
	}
	wg.Wait()
	used, _, exhausted := budget.snapshot()
	if acquired > 5 || used > 5 || !exhausted {
		t.Fatalf("budget oversubscribed: acquired=%d used=%d exhausted=%v", acquired, used, exhausted)
	}
}
