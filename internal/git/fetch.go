package git

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
)

// FetchBranch freezes a remote branch without updating local branches,
// remote-tracking refs, or FETCH_HEAD. Concurrent fetches use separate refs.
func (r *Repository) FetchBranch(ctx context.Context, remote, branch string) (commit, tree string, err error) {
	if !validRemoteURL(remote) || !ValidBranchName(branch) {
		return "", "", fmt.Errorf("git: invalid fetch source")
	}
	ref := "refs/dockhand/fetch/" + rand.Text()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		value, readErr := r.ReadRef(cleanup, ref)
		if readErr != nil {
			err = errors.Join(err, readErr)
			return
		}
		if value.Exists {
			err = errors.Join(err, r.UpdateRefs(cleanup, []RefChange{{Name: ref, Expected: value}}))
		}
	}()
	_, err = r.output(ctx, "fetch", "--no-tags", "--no-write-fetch-head", "--no-auto-maintenance", "--recurse-submodules=no", "--refmap=", "--", remote, "refs/heads/"+branch+":"+ref)
	if err != nil {
		return "", "", err
	}
	value, err := r.ReadRef(ctx, ref)
	if err != nil {
		return "", "", err
	}
	if !value.Exists {
		return "", "", fmt.Errorf("git: fetched branch %s is missing", branch)
	}
	trees, err := r.CommitTrees(ctx, []string{value.Object})
	if err != nil {
		return "", "", err
	}
	return value.Object, trees[value.Object], nil
}

// FetchPullRequest fetches a GitHub pull request's head from remote, the
// repository (owner/name) it was opened in, into
// refs/dockhand/reviews/<owner>/<name>/<number>, where it stays for the
// next review, and returns its commit. The ref names the repository, so a
// sandbox's #145 and MacPorts' never share one (the rc6 full stage's G22).
// The ref of the number alone, refs/dockhand/reviews/<number>, which
// builds before kept, is never read, and is left as it is.
func (r *Repository) FetchPullRequest(ctx context.Context, remote, repository string, number int) (string, error) {
	owner, name, found := strings.Cut(strings.ToLower(repository), "/")
	if !validRemoteURL(remote) || number <= 0 || !found || strings.Contains(name, "/") || !ValidBranchName(owner) || !ValidBranchName(name) {
		return "", fmt.Errorf("git: invalid fetch source")
	}
	ref := fmt.Sprintf("refs/dockhand/reviews/%s/%s/%d", owner, name, number)
	if _, err := r.output(ctx, "fetch", "--no-tags", "--no-write-fetch-head", "--no-auto-maintenance", "--recurse-submodules=no", "--refmap=", "--", remote, fmt.Sprintf("+refs/pull/%d/head:%s", number, ref)); err != nil {
		return "", err
	}
	value, err := r.ReadRef(ctx, ref)
	if err != nil {
		return "", err
	}
	if !value.Exists {
		return "", fmt.Errorf("git: fetching pull request %d left no head", number)
	}
	return value.Object, nil
}
