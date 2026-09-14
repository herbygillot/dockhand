package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
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

func (r *Repository) Contribution(ctx context.Context, base, commit string) (string, []string, error) {
	if !ValidObjectID(base) || !ValidObjectID(commit) {
		return "", nil, fmt.Errorf("git: contribution requires base and commit objects")
	}
	parent, err := r.SingleParent(ctx, commit)
	if err != nil {
		return "", nil, err
	}
	if parent != base {
		return "", nil, fmt.Errorf("git: publication requires one commit above the recorded base; squash or rebind the contribution")
	}
	message, err := r.output(ctx, "show", "-s", "--format=%B", commit, "--")
	if err != nil {
		return "", nil, err
	}
	diff, err := r.output(ctx, "diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-z", base, commit, "--")
	if err != nil {
		return "", nil, err
	}
	var paths []string
	for _, name := range bytes.Split(diff, []byte{0}) {
		if len(name) > 0 {
			paths = append(paths, string(name))
		}
	}
	return strings.TrimSpace(string(message)), paths, nil
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
	ancestor := func(a, b string) (bool, error) {
		_, err := r.output(ctx, "merge-base", "--is-ancestor", a, b)
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && ctx.Err() == nil {
			return false, nil
		}
		return err == nil, err
	}
	included, err := ancestor(base, head.Object)
	if err != nil {
		return err
	}
	if !included {
		return fmt.Errorf("%w: recorded base is not in the selected upstream branch", ErrRefConflict)
	}
	merged, err := ancestor(commit, head.Object)
	if err != nil {
		return err
	}
	if merged {
		return fmt.Errorf("%w: contribution is already in the upstream branch", ErrRefConflict)
	}
	return nil
}
