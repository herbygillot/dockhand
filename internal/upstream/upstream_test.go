package upstream

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream/courtesy"
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

// TestJudgeCensus enumerates every input shape Judge answers and pins
// the answer, verdict and Latest together. The arms were nested
// conditions until two falsehoods were found inside them, and the point
// of a flat census beside a flat rule list is that closing one arm
// cannot move another quietly.
//
// FIVE rows moved deliberately in the restructure and every other row is
// what the judge answered before it, asserted unchanged after. Two are
// the holes the step was for, marked HOLE. The other three are marked
// EMPTY-FORGE and belong to one widening: rules 1 and 2 asked
// `ForgeVersions == nil` and now ask `len(...) == 0`, so a forge that
// was asked and named nothing is judged as the absence it is rather than
// falling through to the arms below. HEAD answered those three
// LivecheckRot with a truncated detail, Agreement over a forge that
// named nothing, and ForgeOnly at an EMPTY version — three falsehoods of
// exactly the kind the holes were, reached by a different road. The
// third of them moves a band: ForgeOnly is judged (10) and NoSignal is
// not (53).
//
// None of the three is reachable through upstream.Check today. Tags
// builds with `var versions []string` and Releases returns nil when its
// filter empties, so both forge witnesses answer nil rather than an
// empty slice; the widening is about what Judge does with an observation
// it can be handed, not about a verdict any port has been given.
func TestJudgeCensus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		obs     Observation
		verdict Verdict
		latest  string
	}{
		// Rules 1 and 2: no forge testimony, nil or empty alike.
		{"neither witness produced anything",
			Observation{}, NoSignal, ""},
		{"livecheck disabled and no forge",
			Observation{LivecheckDisabled: true}, NoSignal, ""},
		{"EMPTY-FORGE: the forge was asked and named nothing (HEAD: LivecheckRot, detail \"forge has \")",
			Observation{ForgeVersions: []string{}}, NoSignal, ""},
		{"no forge: livecheck stands alone",
			Observation{Livecheck: "2.0"}, LivecheckOnly, "2.0"},
		{"EMPTY-FORGE: an empty forge answer is the same absence (HEAD: Agreement over a forge that named nothing)",
			Observation{Livecheck: "2.0", ForgeVersions: []string{}}, LivecheckOnly, "2.0"},
		{"EMPTY-FORGE: livecheck disabled, forge named nothing (HEAD: ForgeOnly at an empty version; the band moves 10 -> 53)",
			Observation{LivecheckDisabled: true, ForgeVersions: []string{}}, NoSignal, ""},

		// Rule 3: the forge alone.
		{"livecheck disabled, forge answers with a stable",
			Observation{LivecheckDisabled: true, ForgeVersions: []string{"1.0", "2.0"}}, ForgeOnly, "2.0"},
		{"HOLE 1: livecheck disabled and every tag is prerelease-style",
			Observation{LivecheckDisabled: true, ForgeVersions: []string{"1.9.0-rc1", "2.0.0-beta"}},
			PrereleaseNewest, ""},
		{"livecheck disabled, every tag prerelease, and the port already rides one",
			Observation{LivecheckDisabled: true, Current: "0.5.0-alpha",
				ForgeVersions: []string{"0.5.0-alpha", "0.6.0-alpha"}},
			PrereleaseLateral, "0.6.0-alpha"},
		{"livecheck disabled, every tag prerelease, and the port rides a stable",
			Observation{LivecheckDisabled: true, Current: "1.0",
				ForgeVersions: []string{"1.9.0-rc1", "2.0.0-beta"}},
			PrereleaseNewest, ""},

		// Rule 4: rot.
		{"livecheck matched nothing while the forge has versions",
			Observation{ForgeVersions: []string{"1.0", "2.0"}}, LivecheckRot, ""},

		// Rule 5: no stable tag to compare against.
		{"the port rides prereleases and follows them upward",
			Observation{Livecheck: "0.6.0-alpha", Current: "0.3.1-alpha", Authoritative: true,
				ForgeVersions: []string{"0.4.0-alpha", "0.5.0-alpha", "0.6.0-alpha"}},
			PrereleaseLateral, "0.6.0-alpha"},
		{"a stable-styled port offered only prereleases regresses",
			Observation{Livecheck: "2.3.2-beta", Current: "0.5.4", Authoritative: true,
				ForgeVersions: []string{"2.3.0-beta", "2.3.2-beta"}},
			PrereleaseNewest, ""},
		{"the lateral rule moves upward only",
			Observation{Livecheck: "0.3.0-alpha", Current: "0.3.1-alpha", Authoritative: true,
				ForgeVersions: []string{"0.3.0-alpha"}},
			PrereleaseNewest, ""},
		{"an unknown current version proves no lateral move",
			Observation{Livecheck: "0.6.0-alpha", Authoritative: true,
				ForgeVersions: []string{"0.6.0-alpha"}},
			PrereleaseNewest, ""},
		{"authoritative beta-named releases still decline",
			Observation{Livecheck: "2.3.2-beta", Authoritative: true,
				ForgeVersions: []string{"2.3.0-beta", "2.3.2-beta"}},
			PrereleaseNewest, ""},
		{"HOLE 2: a stable livecheck no prerelease tag is equal to",
			Observation{Livecheck: "1.2.0", ForgeVersions: []string{"2.0.0-beta"}},
			LivecheckUncorroborated, ""},
		{"a prerelease-styled tag that IS livecheck's answer corroborates it",
			Observation{Livecheck: "1.0-prealpha", ForgeVersions: []string{"1.0-pre"}},
			Agreement, "1.0-prealpha"},

		// Rules 6a-6c: livecheck against the newest stable tag.
		{"behind: the forge has a newer stable",
			Observation{Livecheck: "1.0", ForgeVersions: []string{"1.0", "2.0"}}, LivecheckBehind, ""},
		{"ahead of every tag",
			Observation{Livecheck: "3.0", ForgeVersions: []string{"1.0", "2.0"}}, LivecheckAhead, ""},
		{"ahead of an authoritative releases feed",
			Observation{Livecheck: "1.17.0", Authoritative: true,
				ForgeVersions: []string{"1.16.0", "1.16.1"}},
			LivecheckAhead, ""},
		{"a prerelease whose release the forge has is superseded",
			Observation{Livecheck: "1.17.0-rc.3", Authoritative: true,
				ForgeVersions: []string{"1.16.1", "1.17.0"}},
			PrereleaseSuperseded, "1.17.0"},
		{"superseded on the tag path too",
			Observation{Livecheck: "1.17.0-rc.3",
				ForgeVersions: []string{"1.16.1", "1.17.0", "1.17.0-rc.3"}},
			PrereleaseSuperseded, "1.17.0"},
		{"a prerelease with no release behind it declines",
			Observation{Livecheck: "2.0.0-rc.1", ForgeVersions: []string{"1.0.0", "2.0.0-rc.1"}},
			PrereleaseNewest, ""},
		{"livecheck is the raw newest and ahead only of the stable subset",
			Observation{Livecheck: "2.3.2-beta",
				ForgeVersions: []string{"1.0.0", "2.3.0-beta", "2.3.2-beta"}},
			PrereleaseNewest, ""},
		{"agreement on the newest stable",
			Observation{Livecheck: "2.0", ForgeVersions: []string{"1.0", "2.0"}}, Agreement, "2.0"},
		{"only prereleases are newer: livecheck's conservatism is policy",
			Observation{Livecheck: "1.0", ForgeVersions: []string{"1.0", "2.0.rc.1"}}, Agreement, "1.0"},
		{"per-PR CI tags never outrank the release livecheck names",
			Observation{Livecheck: "0.4.96",
				ForgeVersions: []string{"0.4.94", "0.4.95", "0.4.96", "2026.9.1-pr5150.5", "2026.9.1-pr5150.4"}},
			Agreement, "0.4.96"},
		{"authoritative releases outrank tag heuristics",
			Observation{Livecheck: "0.4.96", Authoritative: true,
				ForgeVersions: []string{"0.4.94", "0.4.95", "0.4.96"}},
			Agreement, "0.4.96"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Judge(tc.obs)
			assert.Equal(t, tc.verdict, r.Verdict)
			assert.Equal(t, tc.latest, r.Latest)
		})
	}
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

	versions, err := Tags(context.Background(), tool.NewFinder(nil), Repo{URL: dir, TagPrefix: "v"})
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

	versions, err := Tags(context.Background(), tool.NewFinder(nil),
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

// A stable-looking livecheck answer against only-prerelease tags used
// to be Agreement — and Agreement means both witnesses named the same
// version, which the forge had not done. It named betas, none of them
// this. The label was published on one witness's word while telling the
// reader two had corroborated it, and a port whose stable releases live
// off-forge resolved on that basis.
//
// What was right about the old reading survives: livecheck matching
// none of the forge's prerelease tags is the maintainer's policy and
// never rot. Policy is testimony. It is just not two testimonies, so
// the outcome is named for what it is and the version is not published.
func TestJudgeStableLivecheckNoPrereleaseTagCorroborates(t *testing.T) {
	r := Judge(Observation{
		Livecheck:     "1.2.0",
		ForgeVersions: []string{"2.0.0-beta"},
	})
	assert.Equal(t, LivecheckUncorroborated, r.Verdict)
	assert.Empty(t, r.Latest, "one witness never publishes a version")
	assert.Contains(t, r.Detail, "livecheck 1.2.0 stands alone")
	assert.Contains(t, r.Detail, "newest 2.0.0-beta")
	assert.False(t, Judged(r.Verdict),
		"the forge answered and corroborated nothing: upstream's band, not a decline of dockhand's")
}

// The arm survives the membership test rather than becoming dead code:
// a version can be stable by the name heuristic and vercmp-equal to a
// tag the same heuristic calls a prerelease, through the comparator's
// own documented quirk (a trailing alpha segment against an exhausted
// string compares equal). When that happens the forge really did name
// livecheck's answer, and two witnesses really do agree.
func TestJudgeAPrereleaseStyledTagCanStillCorroborate(t *testing.T) {
	require.True(t, Stable("1.0-prealpha"), "the pre token is inside a word")
	require.False(t, Stable("1.0-pre"))
	require.Zero(t, macports.VerCmp("1.0-prealpha", "1.0-pre"))

	r := Judge(Observation{
		Livecheck:     "1.0-prealpha",
		ForgeVersions: []string{"1.0-pre"},
	})
	assert.Equal(t, Agreement, r.Verdict)
	assert.Equal(t, "1.0-prealpha", r.Latest, "the maintainer's spelling wins")
}

// Livecheck disabled is an absent witness by the maintainer's own
// declaration, so the forge is the only one the port offers — and it
// answered completely. When everything it has is prerelease-style the
// old arm resolved to the raw newest, which put a -beta in a Portfile
// as the version: exactly what every other arm of the judge refuses.
//
// The refusal is dockhand's own opinion of sound testimony, so it takes
// the PrereleaseNewest shape and stays with the plan declines rather
// than sliding into upstream's band. The decline that reaches the user
// is plan.LatestUnresolved, whose remedy names `--to`.
func TestJudgeForgeOnlyWithNothingButPrereleasesDeclines(t *testing.T) {
	r := Judge(Observation{
		LivecheckDisabled: true,
		ForgeVersions:     []string{"1.9.0-rc1", "2.0.0-beta"},
	})
	assert.Equal(t, PrereleaseNewest, r.Verdict)
	assert.Empty(t, r.Latest, "a -beta must never land in a Portfile as the version")
	assert.Contains(t, r.Detail, "livecheck is disabled")
	assert.Contains(t, r.Detail, "2.0.0-beta")
	assert.True(t, Judged(r.Verdict),
		"the one witness this port has ran and answered soundly; the refusal is dockhand's")

	// A stable tag among them and the forge stands alone as before.
	r = Judge(Observation{
		LivecheckDisabled: true,
		ForgeVersions:     []string{"1.9.0", "2.0.0-beta"},
	})
	assert.Equal(t, ForgeOnly, r.Verdict)
	assert.Equal(t, "1.9.0", r.Latest)
}

// The lateral escape does not depend on livecheck having run. Whether
// following a prerelease gives up stability is a question about the
// port's own version and the forge's tags; livecheck is not a term in
// it, and which witness is absent decides how many spoke rather than
// what a regression is.
//
// Refusing here would have re-closed the update path PrereleaseLateral
// was entered to keep open — the verdict's own field case is a port of
// exactly this shape — so the disabled arm applies the same two rules
// in the same order as the arm where livecheck answered.
func TestJudgeForgeAloneKeepsTheLateralEscape(t *testing.T) {
	lateral := Judge(Observation{
		LivecheckDisabled: true,
		Current:           "0.5.0-alpha",
		ForgeVersions:     []string{"0.5.0-alpha", "0.6.0-alpha"},
	})
	assert.Equal(t, PrereleaseLateral, lateral.Verdict)
	assert.Equal(t, "0.6.0-alpha", lateral.Latest, "alpha to alpha gives up no stability")

	// The safety property is untouched: a port on a stable version is
	// never offered a prerelease, however the prereleases were found.
	regression := Judge(Observation{
		LivecheckDisabled: true,
		Current:           "1.0",
		ForgeVersions:     []string{"1.9.0-rc1", "2.0.0-beta"},
	})
	assert.Equal(t, PrereleaseNewest, regression.Verdict)
	assert.Empty(t, regression.Latest)

	// And a lateral move must still be upward: the newest prerelease
	// being the one the port already rides resolves nothing.
	standing := Judge(Observation{
		LivecheckDisabled: true,
		Current:           "0.6.0-alpha",
		ForgeVersions:     []string{"0.5.0-alpha", "0.6.0-alpha"},
	})
	assert.Equal(t, PrereleaseNewest, standing.Verdict)
	assert.Empty(t, standing.Latest)
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
	vs, _, err := Manners{}.releases(context.Background(), gh,
		Repo{URL: "https://github.com/superfly/flyctl.git", TagPrefix: "v"}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"0.4.96", "0.4.95"}, vs,
		"prereleases, drafts, and nonconforming tags all excluded")
}

// The three answers, told apart. A repository that publishes no
// releases and a call that could not be made mean opposite things to a
// sweep — the first says the tags are the whole truth, the second says
// the authoritative witness was lost and every remaining port will be
// judged on the heuristic it exists to correct — and they used to be
// the same false.
func TestReleasesFallsBackHonestly(t *testing.T) {
	ctx := context.Background()
	vs, _, err := Manners{}.releases(ctx, nil, Repo{URL: "https://github.com/x/y"}, "")
	assert.Empty(t, vs, "no gh, no releases")
	require.NoError(t, err, "having no gh is not a failed call")

	empty := func(context.Context, ...string) (string, error) { return "[]", nil }
	vs, _, err = Manners{}.releases(ctx, empty, Repo{URL: "https://github.com/x/y"}, "")
	assert.Empty(t, vs, "a tag-only repo answers with tags, not an empty forge")
	require.NoError(t, err, "a repository that publishes no releases is not a failure")

	vs, _, err = Manners{}.releases(ctx, empty, Repo{URL: "https://gitlab.com/x/y"}, "")
	assert.Empty(t, vs, "the releases API is GitHub's alone")
	require.NoError(t, err)

	refuse := func(context.Context, ...string) (string, error) {
		return "", errors.New("gh api: HTTP 403: API rate limit exceeded")
	}
	vs, _, err = Manners{}.releases(ctx, refuse, Repo{URL: "https://github.com/x/y"}, "")
	assert.Empty(t, vs)
	require.Error(t, err, "a refused call must not read as a repository with no releases")
	assert.Contains(t, err.Error(), "rate limit")
}

// A refused releases call walls the host, exactly as a refused
// ls-remote does. Before, the refusal was absorbed: nothing walled,
// and every remaining port silently lost its authoritative witness.
func TestARefusedReleasesCallWallsTheAPI(t *testing.T) {
	m := Manners{Pacer: courtesy.NewPacer(courtesy.Policy{Ceiling: 2, Backoff: time.Hour}, nil)}
	refuse := func(context.Context, ...string) (string, error) {
		return "", errors.New("gh api: HTTP 403: You have exceeded a secondary rate limit")
	}
	_, _, err := m.releases(context.Background(), refuse, Repo{URL: "https://github.com/x/y"}, "")
	require.Error(t, err)

	left, up := m.Pacer.Walled("api.github.com")
	assert.True(t, up, "a forge that refused dockhand is still being asked")
	assert.Positive(t, left)

	// The second repository on the same API is refused without a call.
	_, _, err = m.releases(context.Background(), func(context.Context, ...string) (string, error) {
		t.Error("the request the wall exists to prevent was made anyway")
		return "", nil
	}, Repo{URL: "https://github.com/other/repo"}, "")
	require.ErrorIs(t, err, courtesy.ErrWalled)
}

// gh answers a 304 with a non-zero exit, and tool.Output discards a
// failed command's stdout — so the whole payoff of asking
// conditionally arrives as an error with the status in its text. Read
// it, or every unchanged releases feed loses its authoritative witness
// the moment the TTL expires, which is the opposite of what
// revalidation is for.
func TestAConditionalReleasesCallReadsA304OutOfGhsFailure(t *testing.T) {
	dir := t.TempDir()
	cache := courtesy.NewCache(dir, time.Hour, nil)
	repo := Repo{URL: "https://github.com/x/y", TagPrefix: "v"}
	m := Manners{Cache: cache}

	first := func(_ context.Context, args ...string) (string, error) {
		assert.NotContains(t, strings.Join(args, " "), "If-None-Match",
			"the first call has nothing to revalidate against")
		return "HTTP/2.0 200 OK\nETag: W/\"abc\"\n\n[{\"tag_name\":\"v1.2.0\"}]", nil
	}
	vs, _, err := m.releases(context.Background(), first, repo, "d1")
	require.NoError(t, err)
	assert.Equal(t, []string{"1.2.0"}, vs)

	// The TTL expires and the feed has not changed. gh prints the 304
	// head and exits non-zero; what reaches us is the error alone.
	stale := courtesy.NewCache(dir, time.Nanosecond, nil)
	asked := 0
	notMod := func(_ context.Context, args ...string) (string, error) {
		asked++
		assert.Contains(t, strings.Join(args, " "), `If-None-Match: W/"abc"`)
		return "", errors.New(`gh api: HTTP 304: Not Modified (https://api.github.com/repos/x/y/releases)`)
	}
	vs, src, err := Manners{Cache: stale}.releases(context.Background(), notMod, repo, "d1")
	require.NoError(t, err, "a 304 is the answer, not a failure")
	assert.Equal(t, 1, asked)
	assert.Equal(t, courtesy.Revalidated, src, "the census must show the cheap answer it was")
	assert.Equal(t, []string{"1.2.0"}, vs, "the stored feed stands")
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
