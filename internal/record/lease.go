package record

import "time"

// Lease is the claimable part of a job, attempt, or resource: the current
// claim, the generation it was issued under, and the retry bookkeeping that
// outlives a cleared claim. Drivers take a lease inside a write transaction,
// perform the external call outside it, and release the lease when they
// record the result.
type Lease struct {
	Claim *Claim
	// ClaimGeneration retains the last issued generation when Claim is cleared,
	// so a later claim by the same owner cannot adopt an older result.
	ClaimGeneration uint64
	// ConsecutiveFailures drives retry backoff; expected waiting resets it.
	ConsecutiveFailures uint32 `json:",omitempty"`
	// ConsecutiveWaits counts expected waits since the last change, so a wait
	// that never resolves lengthens its interval and stays visible; a failure
	// or a settled result resets it.
	ConsecutiveWaits uint32 `json:",omitempty"`
	// WaitKind names what the consecutive waits were for; a wait of another
	// kind starts the count over.
	WaitKind string `json:",omitempty"`
	// RetryAt is the earliest time the next action may run; nil imposes no delay.
	RetryAt *time.Time
}

// Eligible reports whether no live claim exists and any scheduled retry is due.
func (l *Lease) Eligible(now time.Time) bool {
	return !l.Claim.Live(now) && (l.RetryAt == nil || !l.RetryAt.After(now))
}

// Release clears the claim and any scheduled retry once a result is recorded.
func (l *Lease) Release() { l.Claim, l.RetryAt = nil, nil }
