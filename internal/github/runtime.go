package github

import "github.com/herbygillot/dockhand/internal/credential"

// Runtime is GitHub as one process reaches it: one client, its login, and
// the rate limits GitHub's answers report, which the forge, upstream
// discovery, the source archiver, project inspection, Actions, and setup's
// login line all share, where each built a client of its own (the
// architecture review of 5897a82, CX-11). It's built once, where the
// process is composed.
type Runtime struct {
	Client *Client
}

// NewRuntime is GitHub as the person's login in store reaches it, GH_TOKEN
// first (SystemClient).
func NewRuntime(store credential.Store) *Runtime {
	return &Runtime{Client: SystemClient(store)}
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
