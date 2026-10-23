package github

import (
	"context"
	"fmt"
	"time"
)

// Runtime is GitHub as one process reaches it: one client, its login, and
// the rate limits GitHub's answers report, which the forge, upstream
// discovery, the source archiver, project inspection, Actions, and setup's
// login line all share, where each built a client of its own (the
// architecture review of 5897a82, CX-11). It's built once, where the
// process is composed.
type Runtime struct {
	Client *Client
}

// NewRuntime is GitHub as the person's login reaches it (SystemClient).
func NewRuntime(signin Signin) *Runtime {
	return &Runtime{Client: SystemClient(signin)}
}

// AccountChanged lets go of the login the client holds, and the API client
// carrying it, so its next use reads the login as it is now: serve's, when
// the login changed under it, in one place for everything that shares the
// client.
func (r *Runtime) AccountChanged() {
	if r == nil || r.Client == nil {
		return
	}
	r.Client.forgetLogin()
}

// Identity watches who the runtime's login acts as, from the client's own
// credentials, so a renewal the store didn't take is the watcher's too.
func (r *Runtime) Identity() *IdentityWatcher {
	if r == nil || r.Client == nil {
		return &IdentityWatcher{}
	}
	return &IdentityWatcher{Credentials: r.Client.Credentials}
}

// Allowance is what's left of the hour's requests to GitHub's API for the
// runtime's login, or for requests without one, as GitHub says it.
type Allowance struct {
	Remaining, Limit int
	// Reset is when the hour's count starts again.
	Reset time.Time
}

// Allowance asks GitHub what's left of the hour's requests, which its
// rate_limit endpoint answers without counting against it, for a look to
// weigh its estimate against what other sessions sharing the login have
// spent.
func (r *Runtime) Allowance(ctx context.Context) (Allowance, error) {
	if r == nil || r.Client == nil {
		return Allowance{}, fmt.Errorf("github: no client to ask")
	}
	return r.Client.Allowance(ctx)
}

// Allowance asks GitHub what's left of the hour's requests for the
// client's login, or for requests without one (Runtime.Allowance).
func (c *Client) Allowance(ctx context.Context) (Allowance, error) {
	api, err := c.API(ctx)
	if err != nil {
		return Allowance{}, err
	}
	limits, _, err := api.RateLimit.Get(ctx)
	if err != nil {
		return Allowance{}, RateLimitError(err)
	}
	core := limits.GetCore()
	if core == nil {
		return Allowance{}, fmt.Errorf("github: GitHub said nothing of the hour's requests")
	}
	return Allowance{Remaining: core.Remaining, Limit: core.Limit, Reset: core.Reset.Time}, nil
}
