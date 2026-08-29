package upstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoGit reports that the forge resolver needs git and the machine
// has none.
var ErrNoGit = errors.New("upstream: git not found on PATH")

// Tags lists the versions of a forge's tags: one git ls-remote round
// trip, unauthenticated and unmetered, the same call for every git
// forge. Peeled duplicates are dropped, and the port's declared tag
// prefix and suffix are stripped — a tag not matching the scheme is
// not a version of this port and is excluded.
func Tags(ctx context.Context, r Repo) ([]string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrNoGit
	}
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, git, "ls-remote", "--tags", r.URL)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("upstream: ls-remote %s: %s", r.URL, msg)
	}

	var versions []string
	for line := range strings.Lines(out.String()) {
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
