package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Remote struct{ Name, FetchURL, PushURL string }

func (r *Repository) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := r.output(ctx, "remote")
	if err != nil {
		return nil, err
	}
	var result []Remote
	for _, name := range strings.Fields(string(out)) {
		if !ValidBranchName(name) {
			return nil, fmt.Errorf("git: unsupported remote name %q", name)
		}
		fetch, err := r.output(ctx, "remote", "get-url", "--all", name)
		if err != nil {
			return nil, err
		}
		push, err := r.output(ctx, "remote", "get-url", "--push", "--all", name)
		if err != nil {
			return nil, err
		}
		fetches, pushes := strings.Split(strings.TrimSpace(string(fetch)), "\n"), strings.Split(strings.TrimSpace(string(push)), "\n")
		if len(fetches) != 1 || len(pushes) != 1 || fetches[0] == "" || pushes[0] == "" {
			return nil, fmt.Errorf("git: remote %s needs one fetch and one push URL", name)
		}
		result = append(result, Remote{Name: name, FetchURL: fetches[0], PushURL: pushes[0]})
	}
	return result, nil
}

func validRemoteURL(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00\r\n")
}

func (r *Repository) RemoteHead(ctx context.Context, remote, branch string) (RefValue, error) {
	if !validRemoteURL(remote) || !ValidBranchName(branch) {
		return RefValue{}, fmt.Errorf("git: invalid remote or branch")
	}
	ref := "refs/heads/" + branch
	out, err := r.output(ctx, "ls-remote", "--refs", "--", remote, ref)
	if err != nil {
		return RefValue{}, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return RefValue{}, nil
	}
	rows := strings.Split(strings.TrimSpace(string(out)), "\n")
	fields := strings.Fields(rows[0])
	if len(rows) != 1 || len(fields) != 2 || fields[1] != ref || !ValidObjectID(fields[0]) {
		return RefValue{}, fmt.Errorf("git: ambiguous remote head")
	}
	return RefValue{Exists: true, Object: fields[0]}, nil
}

// Push uses a literal destination ref and an explicit expected value, independent
// of remote-tracking refs. Repeating a confirmed desired head is a no-op.
func (r *Repository) Push(ctx context.Context, request Push) error {
	if !validRemoteURL(request.Remote) || !ValidBranchName(request.Branch) || !ValidObjectID(request.Commit) || (request.ExpectedRemote.Exists && !ValidObjectID(request.ExpectedRemote.Object)) || (!request.ExpectedRemote.Exists && request.ExpectedRemote.Object != "") {
		return fmt.Errorf("git: invalid push request")
	}
	actual, err := r.RemoteHead(ctx, request.Remote, request.Branch)
	if err != nil {
		return err
	}
	if actual == (RefValue{Exists: true, Object: request.Commit}) {
		return nil
	}
	if actual != request.ExpectedRemote {
		return &RefConflict{Name: request.Branch, Expected: request.ExpectedRemote, Actual: actual}
	}
	ref := "refs/heads/" + request.Branch
	_, err = r.output(ctx, "-c", "push.followTags=false", "push", "--porcelain", "--no-verify", "--no-follow-tags", "--recurse-submodules=no", "--force-with-lease="+ref+":"+request.ExpectedRemote.Object, "--", request.Remote, request.Commit+":"+ref)
	return err
}

// CheckContributionBase obtains the selected base without writing FETCH_HEAD
// or remote-tracking refs, then checks the contribution's ancestry.
func (r *Repository) CheckContributionBase(ctx context.Context, remote, branch, base, commit string) error {
	if !validRemoteURL(remote) || !ValidBranchName(branch) || !ValidObjectID(base) || !ValidObjectID(commit) {
		return fmt.Errorf("git: invalid contribution base")
	}
	head, err := r.RemoteHead(ctx, remote, branch)
	if err != nil {
		return err
	}
	if !head.Exists {
		return fmt.Errorf("%w: upstream base branch is missing", ErrRefConflict)
	}
	if kind, err := r.ObjectType(ctx, head.Object); err != nil || kind != "commit" {
		if _, err := r.output(ctx, "fetch", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--", remote, "refs/heads/"+branch); err != nil {
			return err
		}
	}
	ancestor := r.IsAncestor
	included, err := ancestor(ctx, base, head.Object)
	if err != nil {
		return err
	}
	if !included {
		return fmt.Errorf("%w: recorded base is not in the selected upstream branch", ErrRefConflict)
	}
	merged, err := ancestor(ctx, commit, head.Object)
	if err != nil {
		return err
	}
	if merged {
		return fmt.Errorf("%w: contribution is already in the upstream branch", ErrRefConflict)
	}
	return nil
}

// DeleteRemoteBranch removes a remote branch only while it still holds the
// expected commit, using the same lease the push path relies on. A branch that
// is already gone is not an error; a moved branch is a RefConflict.
func (r *Repository) DeleteRemoteBranch(ctx context.Context, remote, branch string, expected RefValue) error {
	if !validRemoteURL(remote) || !ValidBranchName(branch) || !expected.Exists || !ValidObjectID(expected.Object) {
		return fmt.Errorf("git: invalid remote branch deletion")
	}
	actual, err := r.RemoteHead(ctx, remote, branch)
	if err != nil {
		return err
	}
	if !actual.Exists {
		return nil
	}
	if actual != expected {
		return &RefConflict{Name: branch, Expected: expected, Actual: actual}
	}
	ref := "refs/heads/" + branch
	_, err = r.output(ctx, "-c", "push.followTags=false", "push", "--porcelain", "--no-verify", "--no-follow-tags", "--recurse-submodules=no", "--force-with-lease="+ref+":"+expected.Object, "--", remote, ":"+ref)
	return err
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (r *Repository) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	if !ValidObjectID(ancestor) || !ValidObjectID(descendant) {
		return false, fmt.Errorf("git: literal commit objects are required")
	}
	_, err := r.output(ctx, "merge-base", "--is-ancestor", ancestor, descendant)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && ctx.Err() == nil {
		return false, nil
	}
	return err == nil, err
}

// CountCommits counts the commits reachable from head that base does not reach.
func (r *Repository) CountCommits(ctx context.Context, base, head string) (int, error) {
	if !ValidObjectID(base) || !ValidObjectID(head) {
		return 0, fmt.Errorf("git: literal commit objects are required")
	}
	out, err := r.output(ctx, "rev-list", "--count", base+".."+head, "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// MergeBase is the best common ancestor of two commits.
func (r *Repository) MergeBase(ctx context.Context, a, b string) (string, error) {
	if !ValidObjectID(a) || !ValidObjectID(b) {
		return "", fmt.Errorf("git: literal commit objects are required")
	}
	out, err := r.output(ctx, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(string(out))
	if !ValidObjectID(base) {
		return "", fmt.Errorf("git: no common ancestor")
	}
	return base, nil
}

// FirstCommitAbove is the oldest commit reachable from head that base does not reach.
func (r *Repository) FirstCommitAbove(ctx context.Context, base, head string) (string, error) {
	if !ValidObjectID(base) || !ValidObjectID(head) {
		return "", fmt.Errorf("git: literal commit objects are required")
	}
	out, err := r.output(ctx, "rev-list", "--reverse", base+".."+head, "--")
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if !ValidObjectID(first) {
		return "", fmt.Errorf("git: nothing above the base")
	}
	return first, nil
}
