package main

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	maxProviderCooldown       = 5 * time.Minute
	maxProviderCooldownJitter = 500 * time.Millisecond
)

type credentialScope uint8

const (
	credentialRouter credentialScope = 1 << iota
	credentialGeocoder
)

// sharedCooldown is adapter-wide: a 429 from either official endpoint slows
// all subsequent outbound attempts. It is deliberately independent from the
// per-endpoint circuit breakers because rate limiting is not an outage.
type sharedCooldown struct {
	mu    sync.Mutex
	until time.Time
}

// slidingWindowGate is a process-wide guard for subscription quotas expressed
// as successful/request objects per rolling minute. It is intentionally checked
// before egress, so rejected calls do not consume provider request budget.
// Platform Manager limits remain the cross-replica hard boundary.
type slidingWindowGate struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts []time.Time
}

func newSlidingWindowGate(limit int, window time.Duration) *slidingWindowGate {
	return &slidingWindowGate{limit: limit, window: window, attempts: make([]time.Time, 0, min(limit, 64))}
}

func (g *slidingWindowGate) Try(now time.Time) (bool, time.Duration) {
	if g == nil || g.limit <= 0 || g.window <= 0 {
		return true, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	cutoff := now.Add(-g.window)
	firstActive := 0
	for firstActive < len(g.attempts) && !g.attempts[firstActive].After(cutoff) {
		firstActive++
	}
	if firstActive > 0 {
		copy(g.attempts, g.attempts[firstActive:])
		g.attempts = g.attempts[:len(g.attempts)-firstActive]
	}
	if len(g.attempts) >= g.limit {
		retryAfter := g.attempts[0].Add(g.window).Sub(now)
		if retryAfter < time.Millisecond {
			retryAfter = time.Millisecond
		}
		return false, retryAfter
	}
	g.attempts = append(g.attempts, now)
	return true, 0
}

func (c *sharedCooldown) Extend(now time.Time, delay time.Duration) {
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	if delay > maxProviderCooldown {
		delay = maxProviderCooldown
	}
	candidate := now.Add(delay)
	c.mu.Lock()
	if candidate.After(c.until) {
		c.until = candidate
	}
	c.mu.Unlock()
}

func (c *sharedCooldown) WaitDuration(now time.Time, jitter func(time.Duration) time.Duration) time.Duration {
	c.mu.Lock()
	remaining := c.until.Sub(now)
	c.mu.Unlock()
	if remaining <= 0 {
		return 0
	}
	if remaining > maxProviderCooldown {
		remaining = maxProviderCooldown
	}
	spread := maxProviderCooldownJitter
	added := time.Duration(0)
	if jitter != nil && spread > 0 {
		added = jitter(spread)
		if added < 0 {
			added = 0
		}
		if added > spread {
			added = spread
		}
	}
	return remaining + added
}

func defaultCooldownJitter(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(maximum) + 1))
}

func providerCooldownDelay(attempt int, base, maximum, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxProviderCooldown {
			return maxProviderCooldown
		}
		return retryAfter
	}
	if base < time.Millisecond {
		base = time.Millisecond
	}
	if maximum < base {
		maximum = base
	}
	delay := base
	for i := 0; i < attempt && delay < maximum; i++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	if delay > maxProviderCooldown {
		delay = maxProviderCooldown
	}
	return delay
}

type credentialFaultLatch struct {
	mu     sync.Mutex
	faults credentialScope
}

func (l *credentialFaultLatch) Fail(scope credentialScope) {
	l.mu.Lock()
	l.faults |= scope
	l.mu.Unlock()
}

func (l *credentialFaultLatch) Success(scope credentialScope) {
	l.mu.Lock()
	l.faults &^= scope
	l.mu.Unlock()
}

func (l *credentialFaultLatch) Failed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.faults != 0
}

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

type circuitBreaker struct {
	mu           sync.Mutex
	threshold    int
	openDuration time.Duration
	now          func() time.Time
	failures     int
	openedAt     time.Time
	state        breakerState
}

func newCircuitBreaker(threshold int, openDuration time.Duration) *circuitBreaker {
	return &circuitBreaker{threshold: threshold, openDuration: openDuration, now: time.Now}
}

func (b *circuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == breakerOpen {
		if b.now().Sub(b.openedAt) < b.openDuration {
			return false
		}
		b.state = breakerHalfOpen
		return true
	}
	return b.state == breakerClosed
}

func (b *circuitBreaker) Success() {
	b.mu.Lock()
	b.failures = 0
	b.state = breakerClosed
	b.openedAt = time.Time{}
	b.mu.Unlock()
}

func (b *circuitBreaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == breakerHalfOpen {
		b.open()
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.open()
	}
}

func (b *circuitBreaker) State() breakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerOpen && b.now().Sub(b.openedAt) >= b.openDuration {
		return breakerHalfOpen
	}
	return b.state
}

func (b *circuitBreaker) open() {
	b.state = breakerOpen
	b.openedAt = b.now()
}

type bulkhead struct {
	semaphore chan struct{}
	wait      time.Duration
}

func newBulkhead(maxConcurrency int, wait time.Duration) *bulkhead {
	return &bulkhead{semaphore: make(chan struct{}, maxConcurrency), wait: wait}
}

func (b *bulkhead) Acquire(ctx context.Context) error {
	if b.wait == 0 {
		select {
		case b.semaphore <- struct{}{}:
			return nil
		default:
			return errBulkheadFull
		}
	}
	timer := time.NewTimer(b.wait)
	defer timer.Stop()
	select {
	case b.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errBulkheadFull
	}
}

func (b *bulkhead) Release() { <-b.semaphore }

func retryDelay(attempt int, base, maximum, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	delay := base
	for i := 0; i < attempt && delay < maximum/2; i++ {
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	// Full jitter prevents synchronized retry waves. A lower bound keeps tests and
	// operations from accidentally turning a configured delay into a busy loop.
	if delay <= time.Millisecond {
		return delay
	}
	return time.Duration(rand.Int64N(int64(delay-time.Millisecond))) + time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
