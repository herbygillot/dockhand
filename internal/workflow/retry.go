package workflow

import (
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
)

// failureDeadline advances durable consecutive failures. The first retry uses
// the configured delay; subsequent retries spread jobs without shortening it.
func (c *cycle) failureDeadline(key string, failures *uint32, err error) time.Time {
	if *failures < ^uint32(0) {
		*failures += 1
	}
	delay := c.retry
	ceiling := max(5*time.Minute, delay)
	for i := uint32(1); i < *failures && delay < ceiling; i++ {
		delay = min(delay, ceiling/2) * 2
		delay = min(delay, ceiling)
	}
	if *failures > 1 && delay < ceiling {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(key))
		jitter := time.Duration((uint64(hash.Sum32()) + uint64(*failures)*2654435761) % 1000)
		delay = min(ceiling, delay+(delay/4)*jitter/1000)
	}
	deadline := c.engine.now().Add(delay)
	var limited *forge.RateLimitError
	if errors.As(err, &limited) && limited.RetryAt.After(deadline) {
		deadline = limited.RetryAt
	}
	return deadline
}

// waitingDeadline schedules the next look at expected progress of one kind.
// Failures are forgotten and the wait is counted. Unbounded kinds poll at
// their own interval; budgeted kinds stretch the interval up to a ceiling and
// report exhaustion once the budget of consecutive waits is spent, so a wait
// that never resolves neither spins nor hides.
func (c *cycle) waitingDeadline(kind waitKind, failures, waits *uint32) (time.Time, bool) {
	*failures = 0
	if *waits < ^uint32(0) {
		*waits += 1
	}
	policy := waitPolicies[kind]
	delay := c.wait
	switch kind {
	case waitBuild:
		delay = c.observe
	case waitCancellation:
		delay = c.retry
	}
	if policy.backoff {
		ceiling := max(5*time.Minute, delay)
		for i := uint32(1); i < *waits && delay < ceiling; i++ {
			delay = min(delay*2, ceiling)
		}
	}
	return c.engine.now().Add(delay), policy.budget > 0 && *waits > policy.budget
}

// exhausted describes a budgeted wait that never resolved.
func exhausted(kind waitKind, waits uint32, detail string) string {
	return fmt.Sprintf("no progress after %d %s waits: %s", waits, kind, detail)
}
