package git

import (
	"context"
	"fmt"
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

// RemoteTip is the object a remote's branch currently holds, "" when it
// holds none. It is asked of the REMOTE and never of a tracking ref: a
// tracking ref is written by this machine's own push or fetch and by
// nothing the remote does, so it answers "what did I last see" where
// this answers "what is there".
//
// It is what an expected-value push or delete is built over, and it is
// separate from RemoteHas because existence and identity are different
// questions — a caller that only needs to know whether a copy stands
// should not have to carry an object id it will not use.
func (r *Repo) RemoteTip(ctx context.Context, remote, branch string) (string, error) {
	out, err := r.git(ctx, "ls-remote", "--heads", "--", remote, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return "", nil
	}
	oid, _, ok := strings.Cut(line, "\t")
	if !ok {
		return "", fmt.Errorf("git: %s answered a listing this build cannot read: %q", remote, line)
	}
	return strings.TrimSpace(oid), nil
}

// PushExact sends ONE OBJECT to ONE REF, asserting what that ref holds
// now. It is the only push a publication makes.
//
// THE SOURCE IS AN OBJECT ID AND NOT A BRANCH NAME, and that is the
// whole reason this exists. Push sent the branch, so whatever the branch
// held at the moment git ran is what left the machine — and a
// publication authorizes BYTES: it revalidates a tip, spends a forge
// round trip on the pull request's state, and only then pushes. A commit
// arriving in that window (an `accept` in another terminal, a person's
// own `git commit`, a `bump --replace`) was published under a permit
// that described the earlier one, with the evidence, the body and the
// publication row all naming a commit nobody sent. A probe changed the
// branch inside the forge callback and watched the later commit land.
//
// THE DESTINATION IS SPELLED IN FULL, so a remote whose HEAD or push
// configuration would have routed a bare name elsewhere cannot.
//
// expect is what the remote ref must hold for this to be allowed: an
// object id to replace, or "" to require that it does not exist yet.
// git spells both as --force-with-lease=<ref>:<expect>, which refuses
// with "stale info" rather than overwriting — so a fork copy somebody
// else moved between the gather and here stops the publication instead
// of being trampled.
func (r *Repo) PushExact(ctx context.Context, remote, oid, branch, expect string) error {
	ref := "refs/heads/" + branch
	_, err := r.git(ctx, "push", "--force-with-lease="+ref+":"+expect, "--", remote, oid+":"+ref)
	return err
}

// PushDeleteExact removes branch from remote, asserting the object it
// holds. It is PushDelete with the guard PushDelete does not have: a
// remote branch reused or advanced after publication is a conflict to
// report, never a newer piece of work to delete on an old record's say-so.
func (r *Repo) PushDeleteExact(ctx context.Context, remote, branch, expect string) error {
	ref := "refs/heads/" + branch
	_, err := r.git(ctx, "push", "--force-with-lease="+ref+":"+expect, "--", remote, ":"+ref)
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
// none, from the remote-tracking refs.
//
// IT IS A REPORTING VERB AND NO EFFECT IS AIMED BY IT ANY MORE.
// publish.DeleteFork used to choose its target this way, and this
// function's own doc conceded the hazard — "when more than one remote
// holds a copy, the first in ref order is named" — which is a sort
// order deciding which remote loses a branch. The publication row now
// records the exact remote, branch and object it pushed
// (record.Fork), and the deletion addresses that. What is left here is
// the question the tracking refs can honestly answer: did a copy of
// this branch ever leave this machine, and roughly where to.
//
// The tracking ref is written by this machine's own push or fetch and
// by nothing the remote does, so a copy the forge deleted stays listed
// until `fetch --prune`. Nothing that decides a foreign effect reads it.
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
