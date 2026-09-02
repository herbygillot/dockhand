package git

import (
	"context"
	"strings"
)

// Remotes returns each remote's fetch URL.
func (r *Repo) Remotes(ctx context.Context) (map[string]string, error) {
	out, err := r.git(ctx, "remote", "-v")
	if err != nil {
		return nil, err
	}
	remotes := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == "(fetch)" {
			remotes[fields[0]] = fields[1]
		}
	}
	return remotes, nil
}

// Push pushes a branch to a remote under its own name, recording the
// upstream tracking configuration — the push config a later PR lookup
// derives the head ref from (D21), and what makes a bare `git push`
// work for the human who takes the branch over.
func (r *Repo) Push(ctx context.Context, remote, branch string) error {
	_, err := r.git(ctx, "push", "-u", remote, branch)
	return err
}

// PushForce replaces a remote branch with the local one — promote
// --force's republish after a branch was re-minted. --force-with-lease
// rather than --force: the lease is the remote-tracking ref the last
// push recorded, so a copy moved from another machine is refused
// instead of trampled.
func (r *Repo) PushForce(ctx context.Context, remote, branch string) error {
	_, err := r.git(ctx, "push", "--force-with-lease", "-u", remote, branch)
	return err
}

// PushDelete removes a branch from a remote — the undo of Push, for
// cleanup that follows a merged PR. Deleting a ref that is already
// gone is an error from git, and callers who want idempotence should
// treat it as advisory.
func (r *Repo) PushDelete(ctx context.Context, remote, branch string) error {
	_, err := r.git(ctx, "push", remote, "--delete", branch)
	return err
}

// TrackedRemote names the remote a branch tracks, "" when none.
func (r *Repo) TrackedRemote(ctx context.Context, branch string) string {
	out, _ := r.git(ctx, "config", "--get", "branch."+branch+".remote")
	return out
}
