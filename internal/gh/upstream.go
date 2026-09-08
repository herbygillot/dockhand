package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
)

// UpstreamConfigKey is the git config a person sets when they want to
// say which remote is upstream rather than have it worked out:
// `git config dockhand.upstream upstream`.
//
// It is CONFIG AND NOT A FLAG because which remote is upstream is a
// property of the checkout, not of an invocation — a person who had to
// type it would type it on every bump forever — and because it is the
// escape hatch for every case the lookup below cannot reach: a
// repository the forge has no fork data for, a mirror, a private tree,
// a host that is not GitHub at all.
const UpstreamConfigKey = "dockhand.upstream"

// ErrNoUpstream is the official upstream not being establishable from
// this checkout. It is a distinguishable error and not a best guess,
// because the two roads that ask cannot both survive a guess: one bases
// a change on it and the other OPENS A PULL REQUEST AGAINST IT.
var ErrNoUpstream = errors.New("gh: the official upstream could not be established")

// repoView is the part of a repository the forge tells us that a URL
// cannot: whether it is somebody's copy, and of what.
type repoView struct {
	Fork   bool `json:"fork"`
	Parent *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
	Source *struct {
		FullName string `json:"full_name"`
	} `json:"source"`
}

// Upstream names the remote that points at the OFFICIAL upstream, and
// the owner/repo it is.
//
// "ORIGIN" IS A CONVENTION AND NOT A FACT, which is the whole reason
// this function exists. The rule it replaces — the primary branch's
// tracked remote, else origin — is right for a checkout cloned from the
// project and wrong for the commoner arrangement: `git clone <your
// fork>` makes origin THE FORK and sets the primary branch to track it,
// so both halves of that rule point at the person's own copy. Two roads
// read this answer and neither survives it being wrong. The mint bases a
// change on it, so a wrong answer cuts branches from a fork that may be
// months behind; publish passes it to `gh pr create --repo`, so a wrong
// answer opens the pull request against the person's own fork, where
// nobody who maintains the project will ever see it.
//
// THE FORGE IS ASKED BECAUSE ONLY THE FORGE KNOWS. Which repository is
// the project and which is somebody's copy of it is not in a URL, not in
// tracking config, and not in a name; it is a fact about the fork
// network, and GitHub keeps it. One call answers either way: a
// repository that is not a fork IS the upstream, and one that is names
// its own source.
//
// SOURCE BEFORE PARENT. `parent` is the immediate ancestor, which for a
// fork of a fork is another fork; `source` is the root of the network,
// which is the project. Both are read so a forge that fills only one
// still answers.
//
// It refuses rather than guessing. A caller that can proceed on
// something weaker is the caller that knows so, and it says so in its
// own words — see the mint, which declines the fetch and bases the
// change on the local branch after telling the person why.
func Upstream(ctx context.Context, run Runner, repo *git.Repo) (remote, ownerRepo string, err error) {
	remotes, err := repo.Remotes(ctx)
	if err != nil {
		return "", "", err
	}
	// The person's own answer, first and without asking anything.
	if name, set, cerr := repo.Config(ctx, UpstreamConfigKey); cerr == nil && set && name != "" {
		url, ok := remotes[name]
		if !ok {
			return "", "", fmt.Errorf("%w: %s names %q, and this checkout has no such remote",
				ErrNoUpstream, UpstreamConfigKey, name)
		}
		owner, repoName, ok := OwnerRepoFromURL(url)
		if !ok {
			return "", "", fmt.Errorf("%w: %s names %q, whose url says no owner/repo (%s)",
				ErrNoUpstream, UpstreamConfigKey, name, url)
		}
		return name, owner + "/" + repoName, nil
	}

	// The candidate to ask ABOUT. It is the old rule, kept as a starting
	// point rather than as an answer: whichever it names, the forge's
	// reply resolves the question, because a fork names its source.
	candidate, err := repo.PrimaryRemote(ctx)
	if err != nil {
		return "", "", err
	}
	url, ok := remotes[candidate]
	if !ok {
		return "", "", fmt.Errorf("%w: this checkout has no remote %q to start from; set %s",
			ErrNoUpstream, candidate, UpstreamConfigKey)
	}
	owner, name, ok := OwnerRepoFromURL(url)
	if !ok {
		return "", "", fmt.Errorf("%w: cannot read owner/repo from remote %q (%s)",
			ErrNoUpstream, candidate, url)
	}
	full := owner + "/" + name

	view, err := viewRepo(ctx, run, full)
	if err != nil {
		return "", "", fmt.Errorf("%w: asking the forge about %s: %w", ErrNoUpstream, full, err)
	}
	if !view.Fork {
		return candidate, full, nil
	}
	root := fullName(view.Source)
	if root == "" {
		root = fullName(view.Parent)
	}
	if root == "" {
		return "", "", fmt.Errorf("%w: %s is a fork and the forge named no source for it",
			ErrNoUpstream, full)
	}
	// The project is known; whether this checkout can REACH it is a
	// second question, and a different sentence. A person whose remotes
	// are all forks is told what to add rather than left with a base
	// nobody can explain.
	for rname, rurl := range remotes {
		if o, n, ok := OwnerRepoFromURL(rurl); ok && o+"/"+n == root {
			return rname, root, nil
		}
	}
	return "", root, fmt.Errorf("%w: %s is the upstream of %s, and no remote here points at it — `git remote add upstream` one, or set %s",
		ErrNoUpstream, root, full, UpstreamConfigKey)
}

// viewRepo asks the forge what it knows about one repository.
func viewRepo(ctx context.Context, run Runner, ownerRepo string) (repoView, error) {
	if run == nil {
		return repoView{}, errors.New("no forge is wired")
	}
	out, err := run(ctx, "api", "repos/"+ownerRepo)
	if err != nil {
		return repoView{}, err
	}
	var v repoView
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &v); err != nil {
		return repoView{}, fmt.Errorf("reading the forge's answer for %s: %w", ownerRepo, err)
	}
	return v, nil
}

func fullName(r *struct {
	FullName string `json:"full_name"`
}) string {
	if r == nil {
		return ""
	}
	return r.FullName
}
