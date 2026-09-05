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
//
// It is where a branch's upstream lives, and only that. It is not
// whether the branch was ever pushed — PushedTo answers that — because
// `git switch -c foo origin/master`, the ordinary way to start a
// branch, sets branch.foo.remote to origin for a branch that exists
// nowhere but here.
func (r *Repo) TrackedRemote(ctx context.Context, branch string) string {
	out, _ := r.git(ctx, "config", "--get", "branch."+branch+".remote")
	return out
}

// PushedTo names the remote a branch has been pushed to — the one
// holding refs/remotes/<remote>/<branch> — and "" when none does.
//
// The remote-tracking ref rather than the tracking configuration,
// because the two answer different questions. branch.<name>.remote
// says where a branch's upstream lives, and a branch cut from a
// remote-tracking base has one before it has ever left the machine;
// branch.<name>.merge is absent after a bare `git push origin foo`.
// The ref is written by every successful push, -u or bare, and by
// nothing else. It is the same ref PushForce leases against, so what
// counts as "the last push" is the same here as there.
//
// It can be stale: a copy deleted on the remote leaves the ref behind
// until `fetch --prune`. For the callers this has that is the right
// answer — the copy did exist, and so may a pull request opened from
// it.
//
// When more than one remote holds a copy, the first in ref order is
// named. That is not a shape dockhand produces — Push sends a branch
// to one remote — and a second copy would need a question this does
// not ask.
func (r *Repo) PushedTo(ctx context.Context, branch string) (string, error) {
	copies, err := r.remoteCopies(ctx, branch)
	if err != nil {
		return "", err
	}
	return copies[branch], nil
}

// Pushed is PushedTo for every branch under a slash-terminated prefix,
// resolved in one ref listing: each branch some remote holds a copy
// of, keyed by branch name, mapped to that remote. A pass over the
// whole namespace asks this once where it would otherwise ask once
// per branch.
//
// The keys are the copies' names, whether or not a local branch of
// that name still stands: a copy left on the remote after a local
// deletion is listed too, and a caller with a branch list in hand
// reads only the keys it asked about.
func (r *Repo) Pushed(ctx context.Context, prefix string) (map[string]string, error) {
	return r.remoteCopies(ctx, prefix)
}

// remoteCopies lists the remote-tracking refs whose branch half is
// want, or lies under it when want is a slash-terminated namespace,
// keyed by branch and mapped to the remote — the first in ref order
// when several hold one.
//
// One listing of all of refs/remotes/, split here, rather than a
// for-each-ref pattern per remote: a remote's name may itself hold a
// slash, so neither half of <remote>/<branch> has a known width, and
// the branch half is the one the caller names. The split is at the
// first place the branch could begin, which puts a slashed remote name
// wholly on the remote side.
func (r *Repo) remoteCopies(ctx context.Context, want string) (map[string]string, error) {
	out, err := r.git(ctx, "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if err != nil {
		return nil, err
	}
	namespace := strings.HasSuffix(want, "/")
	copies := map[string]string{}
	for line := range strings.Lines(out) {
		ref := strings.TrimPrefix(strings.TrimSpace(line), "refs/remotes/")
		i := strings.Index(ref, "/"+want)
		if i < 0 {
			continue
		}
		remote, branch := ref[:i], ref[i+1:]
		// A branch name is matched whole: dockhand/jq is not a copy of
		// dockhand/jq-1.8, however the ref listing sorts them.
		if !namespace && branch != want {
			continue
		}
		if _, seen := copies[branch]; !seen {
			copies[branch] = remote
		}
	}
	return copies, nil
}
