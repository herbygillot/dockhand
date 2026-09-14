package git

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
