package command_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A command that prints only a document prints the same bytes at any
// width: a usage page, fenced blocks and all, and a completion script
// (#94).
func TestADocumentIsTheSameAtAnyWidth(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"usage", "configuration"}, {"completion", "fish"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			cmdtest.NewWorld(t)
			cmdtest.SameAtAnyWidth(t, args...)
		})
	}
}

// review --markdown prints the Markdown body it would post, and nothing
// else: the same bytes at any width, its long lines among them (#94).
func TestAReviewsMarkdownIsTheSameAtAnyWidth(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "Update jq docs")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905, URL: "https://github.com/macports/macports-ports/pull/34905"}, Title: "Update jq docs", State: forge.PullRequestOpen}}
	cmdtest.SameAtAnyWidth(t, "review", "34905", "--markdown")
	out, _, err := cmdtest.Dockhand(t, "review", "34905", "--markdown")
	require.NoError(t, err)
	requireWiderThan20(t, out)
}

// riftProject stands in for GitHub, for a new port's plan.
type riftProject struct{}

func (riftProject) Project(context.Context, string) (engine.Project, error) {
	return engine.Project{Owner: "rift-dev", Name: "rift", Description: "Fast structural diff for config files", License: "MIT", Tag: "v0.4.2",
		Files: map[string][]byte{"Cargo.toml": []byte("[package]\nname = \"rift\"\n")}}, nil
}

// create --plan's Portfile is whole at any width, its 131-column
// modeline among it, where its sentences wrap (#93, #94).
func TestACreatePlansPortfileIsWholeAtAnyWidth(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	cmdtest.Stands(t).ProjectReader = riftProject{}
	payload := "\n" + portfile.Modeline + "\n\nPortSystem          1.0\nPortGroup           github 1.0\n"
	requireWiderThan20(t, payload)
	cmdtest.PayloadAtAnyWidth(t, payload, "create", "https://github.com/rift-dev/rift", "--plan", "--category", "textproc")
}

// longLine is a Portfile comment of 130 columns and more, which a
// terminal's width would break.
var longLine = "# " + strings.Repeat("a comment that runs on past any terminal ", 3)

// longPortfile is a world whose jq Portfile has longLine, and the bumper.
func longPortfile(t *testing.T) cmdtest.World {
	t.Helper()
	w := cmdtest.NewWorld(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"+longLine+"\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.7.1")
	cmdtest.WithBumper(t)
	return w
}

// A plan's diff is whole at any width, a Portfile's 130-column line
// among it, where its sentences wrap: update --plan wrote its diff
// through the wrapper (#93, #94).
func TestAPlansDiffIsWholeAtAnyWidth(t *testing.T) {
	t.Parallel()
	longPortfile(t)
	cmdtest.PayloadAtAnyWidth(t, "\n "+longLine+"\n", "update", "jq", "--new", "--plan")
}

// revbump --plan's diff is whole at any width, as update's is (#94).
func TestARevbumpPlansDiffIsWholeAtAnyWidth(t *testing.T) {
	t.Parallel()
	longPortfile(t)
	cmdtest.PayloadAtAnyWidth(t, "\n "+longLine+"\n+revision 1\n", "revbump", "jq", "--plan", "--subject", "rebuild")
}

// onABranch is longPortfile's world with jq updated on a branch, the
// checkout the command lines run in.
func onABranch(t *testing.T) cmdtest.World {
	t.Helper()
	w := longPortfile(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	return w
}

// diff's patch is whole at any width, where the summary line it writes
// before it wraps (#94).
func TestADiffsPatchIsWholeAtAnyWidth(t *testing.T) {
	t.Parallel()
	onABranch(t)
	cmdtest.PayloadAtAnyWidth(t, "\n-version 1.7.1\n+version 1.8.1\n "+longLine+"\n", "diff")
}

// submit --plan's description is whole at any width, each line as the
// pull request would have it, indented, where what's said of it wraps
// (#93, #94).
func TestASubmitPlansDescriptionIsWholeAtAnyWidth(t *testing.T) {
	t.Parallel()
	w := onABranch(t)
	cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "tidy", "-y")
	require.NoError(t, err)
	payload := "\n    Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**\n\n    #### Description\n\n    Updates from 1.7.1.\n"
	requireWiderThan20(t, payload)
	cmdtest.PayloadAtAnyWidth(t, payload, "submit", "--no-check", "--plan")
}

// requireWiderThan20 holds that a payload has a line 20 columns would
// wrap, which a test at two widths needs to mean anything.
func requireWiderThan20(t *testing.T, payload string) {
	t.Helper()
	for line := range strings.SplitSeq(payload, "\n") {
		if len([]rune(line)) > 20 {
			return
		}
	}
	t.Fatalf("no line of the payload is wider than 20 columns:\n%s", payload)
}
