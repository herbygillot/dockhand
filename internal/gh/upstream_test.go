package gh

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// repoWithRemotes is a real repository carrying the remotes named, so
// what is under test is the resolution and not a fake's idea of one.
func repoWithRemotes(t *testing.T, remotes map[string]string) *git.Repo {
	t.Helper()
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "--quiet")
	run("config", "user.name", "t")
	run("config", "user.email", "t@t")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o644))
	run("add", ".")
	run("commit", "--quiet", "-m", "initial")
	for name, url := range remotes {
		run("remote", "add", name, url)
	}
	repo, err := git.Open(context.Background(), tool.NewFinder(nil), dir)
	require.NoError(t, err)
	return repo
}

// forge answers `api repos/<full>` from a table and records what it was
// asked, so a test can prove the lookup is ONE call.
func forge(t *testing.T, bodies map[string]string, asked *[]string) Runner {
	t.Helper()
	return func(_ context.Context, args ...string) (string, error) {
		require.Equal(t, "api", args[0])
		full := strings.TrimPrefix(args[1], "repos/")
		if asked != nil {
			*asked = append(*asked, full)
		}
		body, ok := bodies[full]
		if !ok {
			return "", errors.New("HTTP 404: Not Found")
		}
		return body, nil
	}
}

// "ORIGIN" IS A CONVENTION AND NOT A FACT. `git clone <your fork>` makes
// origin the FORK and sets the primary branch to track it, so both
// halves of the old rule — the tracked remote, else origin — named the
// person's own copy. The mint would have cut branches from it and
// publish would have opened the pull request against it, where nobody
// who maintains the project would ever see it.
func TestUpstreamFollowsAForkToItsSource(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{
		"origin":   "git@github.com:herby/macports-ports.git",
		"upstream": "git@github.com:macports/macports-ports.git",
	})
	var asked []string
	run := forge(t, map[string]string{
		"herby/macports-ports": `{"fork":true,"parent":{"full_name":"macports/macports-ports"},"source":{"full_name":"macports/macports-ports"}}`,
	}, &asked)

	remote, full, err := Upstream(context.Background(), run, repo)
	require.NoError(t, err)
	assert.Equal(t, "upstream", remote, "the remote pointing at the project, whatever it is called")
	assert.Equal(t, "macports/macports-ports", full)
	assert.Len(t, asked, 1, "one call answers it either way: a fork names its own source")
}

// AND A CHECKOUT CLONED FROM THE PROJECT NEEDS NO SECOND LOOK. The
// candidate is not a fork, so it is the upstream.
func TestUpstreamTakesARepositoryThatIsNotAForkAsItself(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{
		"origin": "git@github.com:macports/macports-ports.git",
		"herby":  "git@github.com:herby/macports-ports.git",
	})
	run := forge(t, map[string]string{
		"macports/macports-ports": `{"fork":false,"parent":null,"source":null}`,
	}, nil)

	remote, full, err := Upstream(context.Background(), run, repo)
	require.NoError(t, err)
	assert.Equal(t, "origin", remote)
	assert.Equal(t, "macports/macports-ports", full)
}

// SOURCE BEFORE PARENT: a fork of a fork's parent is another fork, and
// the project is the root of the network.
func TestUpstreamPrefersTheSourceOverTheImmediateParent(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{
		"origin":  "git@github.com:herby/macports-ports.git",
		"project": "git@github.com:macports/macports-ports.git",
		"middle":  "git@github.com:someone/macports-ports.git",
	})
	run := forge(t, map[string]string{
		"herby/macports-ports": `{"fork":true,"parent":{"full_name":"someone/macports-ports"},"source":{"full_name":"macports/macports-ports"}}`,
	}, nil)

	remote, full, err := Upstream(context.Background(), run, repo)
	require.NoError(t, err)
	assert.Equal(t, "project", remote)
	assert.Equal(t, "macports/macports-ports", full)
}

// A PROJECT THIS CHECKOUT CANNOT REACH IS ITS OWN SENTENCE. Knowing
// which repository is upstream and having a remote that points at it are
// two facts, and a person whose remotes are all forks is told what to
// add rather than left with a base nobody can explain.
func TestUpstreamRefusesWhenNoRemotePointsAtTheProject(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{
		"origin": "git@github.com:herby/macports-ports.git",
	})
	run := forge(t, map[string]string{
		"herby/macports-ports": `{"fork":true,"parent":{"full_name":"macports/macports-ports"},"source":{"full_name":"macports/macports-ports"}}`,
	}, nil)

	_, full, err := Upstream(context.Background(), run, repo)
	require.ErrorIs(t, err, ErrNoUpstream)
	assert.Equal(t, "macports/macports-ports", full, "what it looked for is still worth reporting")
	assert.Contains(t, err.Error(), "git remote add upstream")
}

// A FORGE THAT WILL NOT ANSWER IS A REFUSAL AND NEVER A GUESS. Both
// roads that read this — the mint's base and `gh pr create --repo` —
// take an irreversible or misdirected step on a wrong answer.
func TestUpstreamRefusesRatherThanGuessingWhenTheForgeIsSilent(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{
		"origin": "git@github.com:herby/macports-ports.git",
	})
	_, _, err := Upstream(context.Background(), forge(t, nil, nil), repo)
	require.ErrorIs(t, err, ErrNoUpstream)

	_, _, err = Upstream(context.Background(), nil, repo)
	require.ErrorIs(t, err, ErrNoUpstream, "no forge wired is the same refusal")
}

// THE PERSON'S OWN ANSWER WINS AND COSTS NOTHING. It is git config
// because which remote is upstream is a property of the checkout, and
// it is the escape hatch for every case the lookup cannot reach: a
// mirror, a private tree, a host that is not GitHub at all.
func TestUpstreamHonoursTheConfiguredRemoteWithoutAskingTheForge(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{
		"origin":  "git@github.com:herby/macports-ports.git",
		"mirror":  "git@git.example.org:ports/tree.git",
		"nothere": "git@github.com:someone/macports-ports.git",
	})
	require.NoError(t, repo.SetConfig(context.Background(), UpstreamConfigKey, "mirror"))
	var asked []string

	remote, full, err := Upstream(context.Background(), forge(t, nil, &asked), repo)
	require.NoError(t, err)
	assert.Equal(t, "mirror", remote)
	assert.Equal(t, "ports/tree", full)
	assert.Empty(t, asked, "the person said which; nothing needs asking")
}

// AND A CONFIGURED REMOTE THAT IS NOT THERE IS NAMED, never silently
// replaced by a guess.
func TestUpstreamRefusesAConfiguredRemoteThatDoesNotExist(t *testing.T) {
	repo := repoWithRemotes(t, map[string]string{"origin": "git@github.com:macports/macports-ports.git"})
	require.NoError(t, repo.SetConfig(context.Background(), UpstreamConfigKey, "typo"))

	_, _, err := Upstream(context.Background(), forge(t, nil, nil), repo)
	require.ErrorIs(t, err, ErrNoUpstream)
	assert.Contains(t, err.Error(), "typo")
}
