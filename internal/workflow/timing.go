package workflow

import (
	"fmt"
	"time"
)

// Timeouts bounds one external call, not the lifetime of a verification build.
type Timeouts struct {
	Resolve, Prepare, Provision, Observe, Publish, Cleanup time.Duration
}

func (t Timeouts) defaults() (Timeouts, error) {
	for _, item := range []struct {
		value    *time.Duration
		fallback time.Duration
	}{
		{&t.Resolve, 5 * time.Minute}, {&t.Prepare, 10 * time.Minute},
		// Submission includes source staging/index generation before VM startup.
		{&t.Provision, 15 * time.Minute}, {&t.Observe, 30 * time.Second},
		{&t.Publish, 2 * time.Minute}, {&t.Cleanup, time.Minute},
	} {
		if *item.value < 0 {
			return Timeouts{}, fmt.Errorf("workflow: operation timeouts must be positive")
		}
		if *item.value == 0 {
			*item.value = item.fallback
		}
	}
	return t, nil
}

func (c *cycle) attemptTimeout(action attemptAction) time.Duration {
	switch action {
	case submitAttempt, reconcileAttempt:
		return c.timeouts.Provision
	case cancelAttempt:
		return c.timeouts.Cleanup
	default:
		return c.timeouts.Observe
	}
}
