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

// PushDelete removes branch from remote. It is a FOREIGN effect — a
// remote cannot join a local update-ref batch — and so it is the one
// deletion in the design that keeps record -> effect -> outcome:
// publish.DeleteForkIn writes the record.DeleteFork step Requested, this
// runs outside every lock, and publish.ForkGoneIn writes what became of
// it. Deleting a copy that is already gone is an error from git; the
// sequencer observes RemoteHas first rather than treating that error as
// advisory, which is classifying by words.
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

// PushedTo names the remote a copy of branch was pushed to, "" when
// none, from the remote-tracking refs and not from branch.<name>.remote —
// a config line `git switch -c` writes for branches that exist nowhere
// but here. A READING verb of a LOCAL CACHE, kept from the shipped
// package for publish.Standing's question — was a copy ever pushed, and
// where — and for nothing that decides a foreign effect's outcome: the
// tracking ref is written by this machine's own push or fetch and by
// nothing the remote does, so a copy the forge deleted (auto-delete on
// merge, a hand in another checkout) stays listed until `fetch --prune`
// (measured, and the shipped doc said so). publish.DeleteFork observes
// RemoteHas, never this.
//
// The tracking ref is also the ref PushForce leases against, so what
// counts as "the last push" is the same here as there, and
// branch.<name>.merge — absent after a bare `git push origin foo` —
// could not have answered even the question this does answer.
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

// RemoteHas reports whether remote holds a branch of this name, read
// from the remote itself — `git ls-remote --heads <remote> <branch>` —
// and never from refs/remotes/, which is a cache of this machine's own
// last push or fetch. It is the observation a foreign effect's OUTCOME
// rests on (publish.DeleteFork, before and after its push-delete):
// measured, a copy deleted on the remote leaves the tracking ref
// standing and makes `push --delete` fail, so a sequencer that observed
// the cache would retry, forever, a deletion that can never succeed and
// report as owed a copy that does not exist. A non-nil error is "could
// not ask the remote" and never "absent" (rule 7): the sequencer writes
// Uncertain on it and looks again next pass.
//
// The pattern handed to ls-remote is the fully qualified ref rather than
// the bare name, because ls-remote patterns match whole trailing path
// components: `dockhand/jq` would answer for a refs/heads/other/dockhand/jq
// nobody asked about, while refs/heads/dockhand/jq names one ref and only
// that one. The remote is passed after `--` so a name that begins with a
// dash reaches git as a remote and not as an option.
func (r *Repo) RemoteHas(ctx context.Context, remote, branch string) (bool, error) {
	out, err := r.git(ctx, "ls-remote", "--heads", "--", remote, "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	return out != "", nil
}
