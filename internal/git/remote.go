package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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

// RemoteTag is a tag of a repository read by URL and the object it peels
// to: the commit an annotated tag finally names, or a lightweight tag's own
// object. ls-remote does not say what kind of object that is, so a tag that
// names a blob, as git/git's junio-gpg-pub does, reads like any other.
type RemoteTag struct{ Name, Object string }

// ListRemoteTags reads a remote repository's tags by URL with ls-remote,
// with no checkout of its own, or only the named tags when names are given;
// a named tag the remote lacks is absent from the result. It runs outside
// any checkout, so no repository's configuration applies, and never
// prompts for credentials.
func ListRemoteTags(ctx context.Context, executable, url string, names ...string) ([]RemoteTag, error) {
	if !validRemoteURL(url) {
		return nil, fmt.Errorf("git: invalid remote")
	}
	args := []string{"ls-remote", "--tags", "--", url}
	wanted := map[string]bool{}
	for _, name := range names {
		if !ValidRefName("refs/tags/" + name) {
			return nil, fmt.Errorf("git: invalid tag %q", name)
		}
		wanted[name] = true
		// A pattern matches the tag, not its peeled line; ask for both.
		args = append(args, "refs/tags/"+name, "refs/tags/"+name+"^{}")
	}
	out, err := (&Repository{Root: os.TempDir(), Executable: executable}).output(ctx, args...)
	if err != nil {
		return nil, err
	}
	var order []string
	direct, peeled := map[string]string{}, map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		object, ref, ok := strings.Cut(line, "\t")
		name, found := strings.CutPrefix(ref, "refs/tags/")
		if !ok || !found || !ValidObjectID(object) {
			return nil, fmt.Errorf("git: unreadable ls-remote line %q", line)
		}
		if base, ok := strings.CutSuffix(name, "^{}"); ok {
			peeled[base] = object
			continue
		}
		// Patterns match a ref's trailing components; keep only exact names.
		if len(wanted) > 0 && !wanted[name] || !ValidRefName(ref) {
			continue
		}
		if _, seen := direct[name]; seen {
			return nil, fmt.Errorf("git: remote lists tag %s twice", name)
		}
		direct[name] = object
		order = append(order, name)
	}
	tags := make([]RemoteTag, 0, len(order))
	for _, name := range order {
		object := direct[name]
		if commit, ok := peeled[name]; ok {
			object = commit
		}
		tags = append(tags, RemoteTag{Name: name, Object: object})
	}
	return tags, nil
}

// ErrNoRef is a name no ref of a remote resolves, as a fresh clone of it
// would read the name (CloneCheckout).
var ErrNoRef = errors.New("git: no ref of the remote is that name")

// Checkout is what a fresh clone of a remote checks out at a name, as the
// remote's refs say: the commit, or, where no ref is the name and it has
// the form of an abbreviated object name, the abbreviation, which only a
// clone's objects expand, and which the commit checked out begins with.
type Checkout struct {
	Commit       string
	Abbreviation string
}

// abbreviated is gitrevisions(7)'s short object name: a leading substring
// of a commit's hexadecimal name, four digits at least.
var abbreviated = regexp.MustCompile(`^[0-9a-f]{4,63}$`)

// CloneCheckout is the commit a fresh clone of url checks out at name,
// read from the remote's refs with ls-remote rather than a clone: what
// `git clone url && git checkout -q name` leaves HEAD at, as MacPorts'
// Git fetch does. An empty name is the clone's own checkout, the remote's
// HEAD. A whole commit is itself. Otherwise the name resolves among the
// refs a clone has (git-clone(1)): the remote's default branch as a local
// branch, every branch as origin/<branch>, and every tag. git-checkout(1)
// checks out a local branch by that name first; failing that, the first
// ref gitrevisions(7) finds for the name, a tag before a branch; failing
// that, the one remote-tracking branch of the name, which it guesses a
// new branch from. A name none resolves is an abbreviated commit, where it
// has that form, or ErrNoRef. It runs outside any checkout, so no
// repository's configuration applies, and never prompts for credentials.
func CloneCheckout(ctx context.Context, executable, url, name string) (Checkout, error) {
	if !validRemoteURL(url) {
		return Checkout{}, fmt.Errorf("git: invalid remote")
	}
	// Git reads an object name's hexadecimal digits in either case.
	if lower := strings.ToLower(name); ValidObjectID(lower) {
		return Checkout{Commit: lower}, nil
	}
	out, err := (&Repository{Root: os.TempDir(), Executable: executable}).output(ctx, "ls-remote", "--symref", "--", url, "HEAD", "refs/heads/*", "refs/tags/*")
	if err != nil {
		return Checkout{}, err
	}
	// local is what a fresh clone's refs name, by the clone's own names.
	local := map[string]string{}
	var head, defaultBranch string
	peeled := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// --symref names what HEAD points to: the default branch.
		if symref, ok := strings.CutPrefix(line, "ref: "); ok {
			if target, ref, _ := strings.Cut(symref, "\t"); ref == "HEAD" {
				defaultBranch, _ = strings.CutPrefix(target, "refs/heads/")
			}
			continue
		}
		object, ref, ok := strings.Cut(line, "\t")
		if !ok || !ValidObjectID(object) {
			return Checkout{}, fmt.Errorf("git: unreadable ls-remote line %q", line)
		}
		switch {
		case ref == "HEAD":
			head = object
		case strings.HasPrefix(ref, "refs/heads/"):
			local["refs/remotes/origin/"+strings.TrimPrefix(ref, "refs/heads/")] = object
		case strings.HasPrefix(ref, "refs/tags/") && strings.HasSuffix(ref, "^{}"):
			peeled[strings.TrimSuffix(ref, "^{}")] = object
		case strings.HasPrefix(ref, "refs/tags/"):
			local[ref] = object
		}
	}
	// A tag names its commit once peeled, which is what checkout lands on.
	for ref, commit := range peeled {
		local[ref] = commit
	}
	if head != "" {
		local["HEAD"] = head
	}
	if commit, ok := local["refs/remotes/origin/"+defaultBranch]; defaultBranch != "" && ok {
		local["refs/heads/"+defaultBranch] = commit
		local["refs/remotes/origin/HEAD"] = commit
	}
	if name == "" {
		if head == "" {
			return Checkout{}, fmt.Errorf("%w: the remote lists no HEAD", ErrNoRef)
		}
		return Checkout{Commit: head}, nil
	}
	commit := ""
	for _, rule := range []string{name, "refs/" + name, "refs/tags/" + name, "refs/heads/" + name, "refs/remotes/" + name, "refs/remotes/" + name + "/HEAD"} {
		if found, ok := local[rule]; ok {
			commit = found
			break
		}
	}
	if commit == "" {
		commit = local["refs/remotes/origin/"+name]
	}
	if branch, ok := local["refs/heads/"+name]; ok {
		commit = branch
	}
	switch lower := strings.ToLower(name); {
	case commit != "":
		return Checkout{Commit: commit}, nil
	case abbreviated.MatchString(lower):
		return Checkout{Abbreviation: lower}, nil
	}
	return Checkout{}, fmt.Errorf("%w: %s", ErrNoRef, name)
}
