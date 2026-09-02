package upstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
)

// ErrNoGit reports that the forge resolver needs git and the machine
// has none.
var ErrNoGit = errors.New("upstream: git not found on PATH")

// Tags lists the versions of a forge's tags: one git ls-remote round
// trip, unauthenticated and unmetered, the same call for every git
// forge. Peeled duplicates are dropped, and the port's declared tag
// prefix and suffix are stripped — a tag not matching the scheme is
// not a version of this port and is excluded. git is resolved through
// the run's finder; GIT_TERMINAL_PROMPT=0 keeps a private repository
// from hanging on a credential prompt.
func Tags(ctx context.Context, tools *tool.Finder, r Repo) ([]string, error) {
	git, err := tools.Find(tool.Git)
	if err != nil {
		return nil, ErrNoGit
	}
	out, _, err := tool.Output(ctx, git, tool.Opts{
		Args: []string{"ls-remote", "--tags", r.URL},
		Env:  append(os.Environ(), "GIT_TERMINAL_PROMPT=0"),
	})
	if err != nil {
		return nil, fmt.Errorf("upstream: ls-remote %s: %s", r.URL, err) //nolint:errorlint // not wrapped: the exec error beneath carries the child's exit status, which ExitCode would take for a band
	}

	var versions []string
	for line := range strings.Lines(string(out)) {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		tag, ok := strings.CutPrefix(ref, "refs/tags/")
		if !ok || strings.HasSuffix(tag, "^{}") {
			continue
		}
		v, ok := strings.CutPrefix(tag, r.TagPrefix)
		if !ok {
			continue
		}
		if v, ok = strings.CutSuffix(v, r.TagSuffix); !ok {
			continue
		}
		if v != "" {
			versions = append(versions, v)
		}
	}
	return versions, nil
}
