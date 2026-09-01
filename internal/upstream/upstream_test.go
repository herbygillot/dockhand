package upstream

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func TestCoords(t *testing.T) {
	cases := []struct {
		style portstyle.Type
		opts  map[string]string
		want  string
	}{
		{portstyle.GithubSetup,
			map[string]string{"github.author": "openai", "github.project": "tart", "github.tag_prefix": "v"},
			"https://github.com/openai/tart"},
		{portstyle.GitlabSetup,
			map[string]string{"gitlab.author": "a", "gitlab.project": "p", "gitlab.instance": "https://gitlab.com"},
			"https://gitlab.com/a/p"},
		{portstyle.GiteaSetup,
			map[string]string{"gitea.author": "a", "gitea.project": "p", "gitea.domain": "git.example.org"},
			"https://git.example.org/a/p"},
		{portstyle.CodebergSetup,
			map[string]string{"codeberg.author": "dnkl", "codeberg.project": "foot"},
			"https://codeberg.org/dnkl/foot"},
		{portstyle.SourcehutSetup,
			map[string]string{"sourcehut.author": "sircmpwn", "sourcehut.project": "aerc", "sourcehut.instance": "https://git.sr.ht"},
			"https://git.sr.ht/~sircmpwn/aerc"},
	}
	for _, c := range cases {
		r, ok := coords(c.style, c.opts)
		require.True(t, ok, c.want)
		assert.Equal(t, c.want, r.URL)
	}

	// No forge for non-forge carriers; incomplete coordinates refuse.
	_, ok := coords(portstyle.VersionLine, nil)
	assert.False(t, ok)
	_, ok = coords(portstyle.GithubSetup, map[string]string{"github.author": "x"})
	assert.False(t, ok)
	assert.Nil(t, coordOptions(portstyle.PureSetup))
}

func TestStable(t *testing.T) {
	for v, want := range map[string]bool{
		"2.36.0":       true,
		"1.0":          true,
		"3.0-rc1":      false,
		"3.0rc1":       false,
		"2.0-beta.2":   false,
		"1.0alpha":     false,
		"5.0-preview":  false,
		"7.2-nightly":  false,
		"1.2.3-dev":    false,
		"1.99":         true,
		"0.4.0":        true,
		"2024-05-01":   true,
		"1.0-SNAPSHOT": false,
	} {
		assert.Equal(t, want, Stable(v), v)
	}
}

func TestJudge(t *testing.T) {
	// Agreement.
	r := Judge(Observation{Livecheck: "2.0", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "2.0", r.Latest)

	// Rot: livecheck matched nothing, forge has versions.
	r = Judge(Observation{Livecheck: "", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, LivecheckRot, r.Verdict)
	assert.Empty(t, r.Latest)

	// Behind: forge has a newer stable.
	r = Judge(Observation{Livecheck: "1.0", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, LivecheckBehind, r.Verdict)
	assert.Empty(t, r.Latest)

	// Only prereleases newer: livecheck's conservatism is policy.
	r = Judge(Observation{Livecheck: "1.0", ForgeVersions: []string{"1.0", "2.0.rc.1"}})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "1.0", r.Latest)

	// Ahead: livecheck newer than every tag.
	r = Judge(Observation{Livecheck: "3.0", ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, LivecheckAhead, r.Verdict)

	// Livecheck disabled, forge answers.
	r = Judge(Observation{LivecheckDisabled: true, ForgeVersions: []string{"1.0", "2.0"}})
	assert.Equal(t, ForgeOnly, r.Verdict)
	assert.Equal(t, "2.0", r.Latest)

	// No forge: livecheck stands alone.
	r = Judge(Observation{Livecheck: "2.0"})
	assert.Equal(t, LivecheckOnly, r.Verdict)
	assert.Equal(t, "2.0", r.Latest)

	// Nothing at all.
	r = Judge(Observation{LivecheckDisabled: true})
	assert.Equal(t, NoSignal, r.Verdict)
}

func TestTags(t *testing.T) {
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", args, out)
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "x")
	run("tag", "v1.0.0")
	run("tag", "v1.1.0")
	run("tag", "-a", "v2.0.0-rc1", "-m", "rc") // annotated: exercises peeled dedup
	run("tag", "other-3.0")                    // wrong scheme: excluded

	versions, err := Tags(context.Background(), Repo{URL: dir, TagPrefix: "v"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1.0.0", "1.1.0", "2.0.0-rc1"}, versions)
}

// go.setup delegates tag naming to the forge family its domain selects;
// the scheme is found in that family's options.
func TestCoordsGoDelegatedTagScheme(t *testing.T) {
	r, ok := coords(portstyle.GoSetup, map[string]string{
		"go.domain": "github.com", "go.author": "robpike", "go.project": "ivy",
		"github.tag_prefix": "v", "github.tag_suffix": "",
	})
	require.True(t, ok)
	assert.Equal(t, "https://github.com/robpike/ivy", r.URL)
	assert.Equal(t, "v", r.TagPrefix)
}

func TestTagsStripsSuffix(t *testing.T) {
	testenv.Tool(t, "git")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", args, out)
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "x")
	run("tag", "v1.0.0-release")
	run("tag", "v1.1.0-release")
	run("tag", "v2.0.0") // suffix missing: not this port's scheme

	versions, err := Tags(context.Background(),
		Repo{URL: dir, TagPrefix: "v", TagSuffix: "-release"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1.0.0", "1.1.0"}, versions)
}

func TestCoordsCarriesSuffix(t *testing.T) {
	r, ok := coords(portstyle.GithubSetup, map[string]string{
		"github.author": "a", "github.project": "p",
		"github.tag_prefix": "REL_", "github.tag_suffix": "_final",
	})
	require.True(t, ok)
	assert.Equal(t, "REL_", r.TagPrefix)
	assert.Equal(t, "_final", r.TagSuffix)
}

func TestCoordsNewFamilies(t *testing.T) {
	// notabug: a fixed-host forge of its own.
	r, ok := coords(portstyle.NotabugSetup, map[string]string{
		"notabug.author": "a", "notabug.project": "p", "notabug.tag_prefix": "v",
	})
	require.True(t, ok)
	assert.Equal(t, "https://notabug.org/a/p", r.URL)
	assert.Equal(t, "v", r.TagPrefix)

	// cgit: no author, project hangs off the instance, clone URL has .git.
	r, ok = coords(portstyle.CgitSetup, map[string]string{
		"cgit.url": "git.zx2c4.com", "cgit.project": "wireguard-tools",
	})
	require.True(t, ok)
	assert.Equal(t, "https://git.zx2c4.com/wireguard-tools.git", r.URL)

	// octave delegates to github; the coordinates land there.
	r, ok = coords(portstyle.OctaveSetup, map[string]string{
		"github.author": "gnu-octave", "github.project": "statistics", "github.tag_prefix": "release-",
	})
	require.True(t, ok)
	assert.Equal(t, "https://github.com/gnu-octave/statistics", r.URL)
	assert.Equal(t, "release-", r.TagPrefix)

	// R delegates to gitlab for gitlab-domain packages.
	r, ok = coords(portstyle.RSetup, map[string]string{
		"gitlab.author": "r-pkg", "gitlab.project": "thing", "gitlab.instance": "https://gitlab.com",
	})
	require.True(t, ok)
	assert.Equal(t, "https://gitlab.com/r-pkg/thing", r.URL)

	// An R port on CRAN sets no forge family options: no forge, honestly.
	_, ok = coords(portstyle.RSetup, map[string]string{})
	assert.False(t, ok)
}

// mergestat's field case: every release is a prerelease-style tag, an
// ancient stable tag exists, and livecheck tracks the raw newest. The
// old verdict charged livecheck with being "ahead of the forge" while
// printing two identical versions — an explanation that contradicted
// itself and sent the field run chasing a livecheck bug.
func TestJudgePrereleaseNewestIsNotALivecheckFault(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "2.3.2-beta",
		ForgeVersions: []string{"1.0.0", "2.3.0-beta", "2.3.2-beta"},
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest, "prerelease-only newest declines, never guesses")
	assert.Contains(t, r.Detail, "prerelease-style")
	assert.Contains(t, r.Detail, "newest stable is 1.0.0")
}

// gopass's field case: livecheck matches rc tags, the forge's releases
// API has the release itself. Semver puts 1.17.0-rc.3 strictly BEFORE
// 1.17.0, but VerCmp orders it above — so the old verdict declined
// "livecheck ahead" while printing the right answer (stable 1.17.0) in
// the same breath. The release supersedes its prerelease, and
// resolution proceeds with it.
func TestJudgePrereleaseSupersededByItsRelease(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "1.17.0-rc.3",
		ForgeVersions: []string{"1.16.1", "1.17.0"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseSuperseded, r.Verdict)
	assert.Equal(t, "1.17.0", r.Latest, "the release stands")
	assert.Contains(t, r.Detail, "1.17.0-rc.3")
	assert.Contains(t, r.Detail, "the release 1.17.0 stands")
}

// The same rule on the tag path, where the rc is also the forge's raw
// newest: previously PrereleaseNewest declined here, but an rc whose
// release exists is superseded, not news.
func TestJudgePrereleaseSupersededOnTheTagPath(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "1.17.0-rc.3",
		ForgeVersions: []string{"1.16.1", "1.17.0", "1.17.0-rc.3"},
	})
	assert.Equal(t, PrereleaseSuperseded, r.Verdict)
	assert.Equal(t, "1.17.0", r.Latest)
}

// A prerelease with NO release behind it is still a decline: the base
// outranks every stable tag, so nothing supersedes it (mergestat's
// shape keeps its verdict).
func TestJudgePrereleaseWithoutItsReleaseStillDeclines(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "2.0.0-rc.1",
		ForgeVersions: []string{"1.0.0", "2.0.0-rc.1"},
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest)
}

// mergestat's field regression: upstream publishes beta-NAMED
// releases with prerelease=false, so the authoritative feed carried
// them and the old shortcut (authoritative means stable) planned a
// bump to 2.3.2-beta. The witnesses compose: the flag filters what
// upstream disclaims, the name heuristic judges what remains.
func TestJudgeAuthoritativeBetaNamedReleasesStillDecline(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "2.3.2-beta",
		ForgeVersions: []string{"2.3.0-beta", "2.3.2-beta"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest, "a -beta must never land in a Portfile as the version")
	assert.Contains(t, r.Detail, "no stable version exists")

	// With an old stable in the release list, the tags-path shape
	// holds on the authoritative path too.
	r = Judge(Observation{
		Livecheck:     "2.3.2-beta",
		ForgeVersions: []string{"0.1.0", "2.3.0-beta", "2.3.2-beta"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest)
	assert.Contains(t, r.Detail, "newest stable is 0.1.0")
}

// amber-lang's field case, the contrast to mergestat: the port
// itself rides an alpha because upstream has never cut anything else,
// so alpha to alpha gives up no stability — declining closes the
// port's only possible update path. mergestat differs in exactly one
// respect: its port version is stable-styled, so a -beta would be a
// stability regression, and it still declines.
func TestJudgePrereleaseLateralWhenThePortRidesPrereleases(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "0.6.0-alpha",
		Current:       "0.3.1-alpha",
		ForgeVersions: []string{"0.4.0-alpha", "0.5.0-alpha", "0.6.0-alpha"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseLateral, r.Verdict)
	assert.Equal(t, "0.6.0-alpha", r.Latest)
	assert.Contains(t, r.Detail, "rides prereleases (0.3.1-alpha)")

	// A stable-styled port offered only prereleases still declines:
	// that move IS a stability regression (mergestat's shape).
	r = Judge(Observation{
		Livecheck:     "2.3.2-beta",
		Current:       "0.5.4",
		ForgeVersions: []string{"2.3.0-beta", "2.3.2-beta"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest)

	// The lateral rule moves upward only; anything else declines.
	r = Judge(Observation{
		Livecheck:     "0.3.0-alpha",
		Current:       "0.3.1-alpha",
		ForgeVersions: []string{"0.3.0-alpha"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest)

	// An unknown current version proves nothing and stays a decline.
	r = Judge(Observation{
		Livecheck:     "0.6.0-alpha",
		ForgeVersions: []string{"0.6.0-alpha"},
		Authoritative: true,
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest)
}

// A stable-looking livecheck answer against only-prerelease tags is
// still trusted: livecheck matching none of them is policy, not rot.
func TestJudgeStableLivecheckAgainstOnlyPrereleaseTags(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "1.2.0",
		ForgeVersions: []string{"2.0.0-beta"},
	})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "1.2.0", r.Latest)
}

// The gopass-satellite field case: upstream tags v1.17.0 but cuts no
// GitHub release, so the authoritative feed tops out at 1.16.1 and
// livecheck looks ahead. The tag list is the second witness: a tag
// matching livecheck's answer resolves the report.
func TestCorroborateResolvesATagOnlyVersion(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "1.17.0",
		ForgeVersions: []string{"1.16.0", "1.16.1"},
		Authoritative: true,
	})
	require.Equal(t, LivecheckAhead, r.Verdict)
	assert.Contains(t, r.Detail, "forge release", "authoritative comparisons must not claim to have read tags")
	assert.NotContains(t, r.Detail, "forge tag")

	got := corroborate(r, []string{"1.16.1", "1.17.0"})
	assert.Equal(t, TagWithoutRelease, got.Verdict)
	assert.Equal(t, "1.17.0", got.Latest)
	assert.Contains(t, got.Detail, "upstream cut no release for it")
}

func TestCorroborateHardensTheDeclineWhenNoTagMatches(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "9.0.0",
		ForgeVersions: []string{"1.16.1"},
		Authoritative: true,
	})
	got := corroborate(r, []string{"1.16.1", "1.17.0"})
	assert.Equal(t, LivecheckAhead, got.Verdict)
	assert.Empty(t, got.Latest, "an uncorroborated ahead still declines")
	assert.Contains(t, got.Detail, "no forge tag matches either")
}

func TestReleaseBase(t *testing.T) {
	for version, want := range map[string]string{
		"1.17.0-rc.3":       "1.17.0",
		"2.3.2-beta":        "2.3.2",
		"1.2.3.rc1":         "1.2.3",
		"3.0.0-pre":         "3.0.0",
		"2026.9.1-pr5150.5": "2026.9.1",
	} {
		base, ok := releaseBase(version)
		require.True(t, ok, version)
		assert.Equal(t, want, base, version)
	}
	_, ok := releaseBase("1.17.0")
	assert.False(t, ok, "a stable version has no prerelease base")
	_, ok = releaseBase("rc1")
	assert.False(t, ok, "all token, no base")
}

func TestJudgeLivecheckAheadNamesBothComparisons(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "9.0.0",
		ForgeVersions: []string{"1.0.0", "2.0.0"},
	})
	assert.Equal(t, LivecheckAhead, r.Verdict)
	assert.Contains(t, r.Detail, "newer than any forge tag")
}

// flyctl's field shape: clean semver releases beside per-PR CI tags
// that never become releases. The -pr<digits> spelling must classify
// prerelease-style, or livecheck at the true newest reads as behind a
// tag that was never a release at all.
func TestJudgePRBuildTagsAreNotStable(t *testing.T) {
	assert.False(t, Stable("2026.9.1-pr5150.5"))
	assert.False(t, Stable("v2026.9.1-pr5150.4"))
	assert.True(t, Stable("0.4.96"))
	assert.True(t, Stable("1.0-print"), "letters after pr are not a PR build")

	r := Judge(Observation{
		Livecheck:     "0.4.96",
		ForgeVersions: []string{"0.4.94", "0.4.95", "0.4.96", "2026.9.1-pr5150.5", "2026.9.1-pr5150.4"},
	})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "0.4.96", r.Latest)
}

func TestReleasesParsesAndFiltersGitHubsAnswer(t *testing.T) {
	gh := func(_ context.Context, args ...string) (string, error) {
		assert.Contains(t, args[1], "repos/superfly/flyctl/releases")
		return `[{"tag_name":"v0.4.96","prerelease":false,"draft":false},
			{"tag_name":"v0.4.97","prerelease":true,"draft":false},
			{"tag_name":"v0.4.98","prerelease":false,"draft":true},
			{"tag_name":"v0.4.95","prerelease":false,"draft":false},
			{"tag_name":"nonconforming","prerelease":false,"draft":false}]`, nil
	}
	vs, ok := Releases(context.Background(), gh, Repo{URL: "https://github.com/superfly/flyctl.git", TagPrefix: "v"})
	require.True(t, ok)
	assert.Equal(t, []string{"0.4.96", "0.4.95"}, vs,
		"prereleases, drafts, and nonconforming tags all excluded")
}

func TestReleasesFallsBackHonestly(t *testing.T) {
	_, ok := Releases(context.Background(), nil, Repo{URL: "https://github.com/x/y"})
	assert.False(t, ok, "no gh, no releases")
	gh := func(context.Context, ...string) (string, error) { return "[]", nil }
	_, ok = Releases(context.Background(), gh, Repo{URL: "https://github.com/x/y"})
	assert.False(t, ok, "a tag-only repo answers with tags, not an empty forge")
	_, ok = Releases(context.Background(), gh, Repo{URL: "https://gitlab.com/x/y"})
	assert.False(t, ok, "the releases API is GitHub's alone")
}

func TestJudgeAuthoritativeReleasesOutrankTagHeuristics(t *testing.T) {
	// flyctl's full shape: with releases authoritative, the calver CI
	// tags never enter the comparison at all.
	r := Judge(Observation{
		Livecheck:     "0.4.96",
		ForgeVersions: []string{"0.4.94", "0.4.95", "0.4.96"},
		Authoritative: true,
	})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "0.4.96", r.Latest)
}
