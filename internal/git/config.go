package git

import (
	"context"
	"strings"
)

// exitUnset is `git config --get`'s exit code for a key nothing sets.
// It shares the number with every other "the thing you asked about is
// not there" in git's plumbing — rev-parse --verify's missing ref, notes
// show's unannotated commit — and it is the whole reason the read below
// classifies by code: git prints nothing either way, so an unset key and
// a repository that could not be opened at all are the same empty
// stdout.
const exitUnset = 1

// Config reads one config value the way git itself resolves it —
// system, global, then the repository — and reports whether it is set
// at all.
//
// The bool is rule 7 and not a convenience: `core.logAllRefUpdates` may
// legitimately hold the empty string, and a caller that read "" as
// "nobody set this" would rewrite a value a person chose. A non-nil
// error is "the repository could not be asked", which is neither of the
// two answers and must never be reported as either.
func (r *Repo) Config(ctx context.Context, key string) (string, bool, error) {
	out, code, err := execGit(ctx, r.tools, r.Root, nil, "config", "--get", key)
	if err != nil {
		if code == exitUnset {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimRight(string(out), "\n"), true, nil
}

// SetConfig writes one value into the REPOSITORY's own config —
// .git/config, never the user's global one — because the settings
// dockhand ensures are properties of this checkout's refs and not of
// the person. statestore.LogRef is the one caller: refs/dockhand gets
// no reflog under any other value of core.logAllRefUpdates, and the ref
// that is the authority for live leases would then be the one ref in
// the repository with no recovery path.
//
// It is a plumbing verb and holds no policy: which key, which value and
// whether the existing one should be left alone are the caller's, so
// that this package still decides nothing.
func (r *Repo) SetConfig(ctx context.Context, key, value string) error {
	_, err := r.git(ctx, "config", key, value)
	return err
}
