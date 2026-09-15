package forge

import "time"

// RateLimitError is a confirmed refusal to perform the requested operation.
// A write may be attempted again after RetryAt; transport failures do not qualify.
type RateLimitError struct {
	RetryAt time.Time
	Err     error
}

func (e *RateLimitError) Error() string { return e.Err.Error() }
func (e *RateLimitError) Unwrap() error { return e.Err }
