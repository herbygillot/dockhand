package workflow

import (
	"errors"
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

// waitingDeadline schedules the next look at expected progress. Failures are
// forgotten, and consecutive waits stretch the interval up to a ceiling so a
// wait that never resolves neither spins nor disappears: the count stays on
// the record for status to show.
func (c *cycle) waitingDeadline(failures, waits *uint32) time.Time {
	*failures = 0
	if *waits < ^uint32(0) {
		*waits += 1
	}
	delay := c.wait
	ceiling := max(5*time.Minute, delay)
	for i := uint32(1); i < *waits && delay < ceiling; i++ {
		delay = min(delay*2, ceiling)
	}
	return c.engine.now().Add(delay)
}
