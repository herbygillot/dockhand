package portstyle

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// All tests are hermetic: values are handcrafted or known fixture facts,
// so no interpreter is needed. The eval side of corroboration is proven by
// the gated fidelity tests; here the correspondence logic itself is under
// test.

func locate(t *testing.T, src string, vals info.Values) (Located, error) {
	t.Helper()
	b := []byte(src)
	tree, errs := syntax.Parse(b)
	require.Empty(t, errs)
	return Locate(b, tree, vals, info.FieldVersion)
}

func mustLocate(t *testing.T, src string, vals info.Values) Located {
	t.Helper()
	loc, err := locate(t, src, vals)
	require.NoError(t, err)
	return loc
}

func mustDecline(t *testing.T, src string, vals info.Values, want DeclineType) *Decline {
	t.Helper()
	_, err := locate(t, src, vals)
	var d *Decline
	require.ErrorAs(t, err, &d)
	require.Equal(t, want, d.Type)
	return d
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile("../testdata/portfiles/" + name)
	require.NoError(t, err)
	return string(src)
}

func TestLocateVersionLine(t *testing.T) {
	src := "PortSystem 1.0\nname foo\nversion             1.2.3\n"
	loc := mustLocate(t, src, info.Values{Name: "foo", Version: "1.2.3"})
	require.Equal(t, VersionLine, loc.Style)
	require.Equal(t, "1.2.3", loc.Span.Text([]byte(src)))
}

func TestLocateEveryStyleShape(t *testing.T) {
	cases := []struct {
		src   string
		style Type
	}{
		{"go.setup            github.com/robpike/ivy 0.4.0 v\n", GoSetup},
		{"github.setup        lampepfl dotty 0.4.0\n", GithubSetup},
		{"gitlab.setup        shackra goimapnotify 0.4.0\n", GitlabSetup},
		{"bitbucket.setup     foo bar 0.4.0\n", BitbucketSetup},
		{"perl5.setup         Unicode-UTF8 0.4.0\n", Perl5Setup},
		{"ruby.setup          minitar 0.4.0 gem {} rubygems\n", RubySetup},
		{"R.setup             cran cran PLordprob 0.4.0\n", RSetup},
		{"cgit.setup          https://git.zx2c4.com wireguard-tools 0.4.0\n", CgitSetup},
		{"codeberg.setup      dnkl foot 0.4.0\n", CodebergSetup},
		{"crossbinutils.setup spu 0.4.0\n", CrossBinutilsSetup},
		{"crossgcc.setup      arm-none-eabi 0.4.0\n", CrossGccSetup},
		{"crossgdb.setup      avr 0.4.0\n", CrossGdbSetup},
		{"elpa.setup          magit 0.4.0\n", ElpaSetup},
		{"gitea.setup         gitea tea 0.4.0\n", GiteaSetup},
		{"go_toolchain.setup  0.4.0\n", GoToolchainSetup},
		{"hunspelldict.setup  de_DE 0.4.0 German\n", HunspellDictSetup},
		{"luarocks.setup      luafilesystem 0.4.0\n", LuarocksSetup},
		{"notabug.setup       author proj 0.4.0\n", NotabugSetup},
		{"octave.setup        github gnu-octave statistics 0.4.0\n", OctaveSetup},
		{"sourcehut.setup     ~sircmpwn aerc 0.4.0\n", SourcehutSetup},
		{"x11font.setup       font-misc 0.4.0 misc\n", X11FontSetup},
		{"zig_toolchain.setup 0.4.0\n", ZigToolchainSetup},
	}
	for _, c := range cases {
		loc := mustLocate(t, c.src, info.Values{Name: "x", Version: "0.4.0"})
		require.Equal(t, c.style, loc.Style, c.src)
		require.Equal(t, "0.4.0", loc.Value)
	}
}

func TestLocateRealFixtureIvy(t *testing.T) {
	src := fixture(t, "math__ivy")
	loc := mustLocate(t, src, info.Values{Name: "ivy", Version: "0.4.0"})
	require.Equal(t, GoSetup, loc.Style)
}

// Corroboration decides between coexisting styles: cmake-devel has both
// github.setup and a computed version line; the evaluated version string
// appears in neither, so the decline is NotLiteral with both styles'
// spans as evidence.
func TestLocateComputedVersionDeclines(t *testing.T) {
	src := fixture(t, "devel__cmake-devel")
	d := mustDecline(t, src,
		info.Values{Name: "cmake-devel", Version: "20251208-4.2.1-485f11a7"}, NotLiteral)
	require.NotEmpty(t, d.Candidates)
}

func TestLocateUnknownStyle(t *testing.T) {
	src := "PortSystem 1.0\nname foo\n"
	_ = mustDecline(t, src, info.Values{Name: "foo", Version: "1.0"}, UnknownStyle)
}

func TestLocateUnsupportedField(t *testing.T) {
	b := []byte("version 1.0\n")
	tree, _ := syntax.Parse(b)
	_, err := Locate(b, tree, info.Values{Version: "1.0"}, info.FieldLicense)
	var d *Decline
	require.ErrorAs(t, err, &d)
	require.Equal(t, FieldUnsupported, d.Type)
}

// Subport contexts: an override inside the context's block wins; a context
// without an override inherits the top-level style's span.
func TestLocateSubportScopes(t *testing.T) {
	src := fixture(t, "devel__libftdi")

	inherited := mustLocate(t, src, info.Values{Name: "libftdi0", Version: "0.20"})
	override := mustLocate(t, src, info.Values{Name: "libftdi1", Version: "1.5"})
	top := mustLocate(t, src, info.Values{Name: "libftdi", Version: "0.20"})

	require.Equal(t, top.Span, inherited.Span,
		"libftdi0 has no override; its version lives in the top-level span")
	require.NotEqual(t, top.Span, override.Span,
		"libftdi1 overrides; its version lives in its block")
	require.Equal(t, "1.5", override.Span.Text([]byte(src)))
	require.Greater(t, override.Span.Start, top.Span.Start)
}

// Later assignment wins, as it does in Tcl: with two corroborating spans,
// the last in document order is the effective setter.
func TestLocateLaterAssignmentWins(t *testing.T) {
	src := "version 1.0\nversion 1.0\n"
	loc := mustLocate(t, src, info.Values{Name: "x", Version: "1.0"})
	require.Equal(t, 20, loc.Span.Start, "must pick the second occurrence")
}

// A style's version word containing substitutions cannot corroborate.
func TestLocateInterpolatedFindsItsSet(t *testing.T) {
	// Once the classic decline of the survey: version through a
	// variable. The set variable style now locates it — the 183-port
	// tier the census counted.
	src := "set v 1.0\nversion ${v}\n"
	loc := mustLocate(t, src, info.Values{Name: "x", Version: "1.0"})
	require.Equal(t, SetVariable, loc.Style)
}

func TestLocateInterpolatedWithoutAMatchingSetDeclines(t *testing.T) {
	src := "version ${v}\n"
	d := mustDecline(t, src, info.Values{Name: "x", Version: "1.0"}, NotLiteral)
	require.Len(t, d.Candidates, 1)
}

func TestDeclineIsBranchableError(t *testing.T) {
	_, err := locate(t, "name foo\n", info.Values{Name: "foo", Version: "1.0"})
	var d *Decline
	require.ErrorAs(t, err, &d)
	require.Equal(t, UnknownStyle, d.Type)
}

// Portfiles straddle styles: github.setup carrying a commit SHA while an
// explicit version line a few lines later sets the real version. The SHA
// can never corroborate, so the version line wins — location follows the
// evaluated value, not style precedence.
func TestLocateStraddledStyles(t *testing.T) {
	src := `PortSystem 1.0
github.setup        derailed k9s 268075063fbf5f796ee1bc419e5268f593c85f4d
name                k9s
version             0.50.9
`
	loc := mustLocate(t, src, info.Values{Name: "k9s", Version: "0.50.9"})
	require.Equal(t, VersionLine, loc.Style)
	require.Equal(t, "0.50.9", loc.Span.Text([]byte(src)))
}

// The real thing: beets has four github.setup calls (plugin fetch groups)
// and one top-level version line.
func TestLocateStraddledRealFixture(t *testing.T) {
	src := fixture(t, "audio__beets")
	loc := mustLocate(t, src, info.Values{Name: "beets", Version: "2.3.1"})
	require.Equal(t, VersionLine, loc.Style)
	require.Equal(t, "2.3.1", loc.Span.Text([]byte(src)))
}

// Platform-conditional styles, the z3/lnav shape: several github.setup
// calls in if branches, each a different version. Evaluation already took
// one branch on this host, so corroboration selects exactly that branch's
// span — the other branches' versions belong to other platforms and must
// not be candidates for editing.
func TestLocateConditionalBranchSelection(t *testing.T) {
	src := `PortSystem 1.0
name z3ish
if {${os.major} >= 20} {
    github.setup        Z3Prover z3 4.15.4 z3-
} elseif {${os.major} >= 16} {
    github.setup        Z3Prover z3 4.13.3 z3-
} else {
    github.setup        Z3Prover z3 4.8.5 Z3-
}
`
	loc := mustLocate(t, src, info.Values{Name: "z3ish", Version: "4.13.3"})
	require.Equal(t, GithubSetup, loc.Style)
	require.Equal(t, "4.13.3", loc.Span.Text([]byte(src)))
}

func TestLocatePlatformAndVariantScopes(t *testing.T) {
	src := `PortSystem 1.0
name scoped
platform darwin {
    version 2.0
}
variant legacy {
    version 1.0
}
`
	loc := mustLocate(t, src, info.Values{Name: "scoped", Version: "2.0"})
	require.Equal(t, "2.0", loc.Span.Text([]byte(src)))
}

// The whitelist is the safety boundary: prose in a long_description that
// happens to start a line with "version" must never become a candidate.
func TestLocateDoesNotReadProse(t *testing.T) {
	src := `PortSystem 1.0
name prosey
long_description {
    version 1.0 was the best release of prosey.
}
`
	_ = mustDecline(t, src, info.Values{Name: "prosey", Version: "1.0"}, UnknownStyle)
}

// Phase blocks might run at build time; a version assignment inside one is
// not the evaluated version and is not searched.
func TestLocateSkipsPhaseBlocks(t *testing.T) {
	src := `PortSystem 1.0
name phased
pre-configure {
    version 9.9
}
version 1.0
`
	loc := mustLocate(t, src, info.Values{Name: "phased", Version: "1.0"})
	require.Equal(t, 69, loc.Span.Start, "must locate the real version line, not the phase block")
}

func TestLocateNewVocabulary(t *testing.T) {
	loc := mustLocate(t, "pure.setup          pure-gen 0.25\n",
		info.Values{Name: "pure-gen", Version: "0.25"})
	require.Equal(t, PureSetup, loc.Style)

	loc = mustLocate(t, "aspelldict.setup    de-alt 2.1-1 {German (old rules)} 6\n",
		info.Values{Name: "aspell-dict-de-alt", Version: "2.1-1"})
	require.Equal(t, AspellDictSetup, loc.Style)
}

// Styles can alternate across exclusive branches: github.setup in one arm,
// a version line in the other. The recognizer has no model of branch
// semantics — corroboration is the branch resolver, because evaluation
// already took one arm on this host, and only that arm's value can match.
// The answer is host-relative because the value is.
func TestLocateAlternatingStylesAcrossBranches(t *testing.T) {
	src := `PortSystem 1.0
name alt
if {${os.major} >= 20} {
    github.setup        foo bar 2.0
} else {
    version             1.0
}
`
	newHost := mustLocate(t, src, info.Values{Name: "alt", Version: "2.0"})
	require.Equal(t, GithubSetup, newHost.Style)
	require.Equal(t, "2.0", newHost.Span.Text([]byte(src)))

	oldHost := mustLocate(t, src, info.Values{Name: "alt", Version: "1.0"})
	require.Equal(t, VersionLine, oldHost.Style)
	require.Equal(t, "1.0", oldHost.Span.Text([]byte(src)))
}

// KNOWN LIMITATION, pinned deliberately: when exclusive branches carry the
// same literal value, both corroborate and last-in-document wins — but
// exclusive branches are not sequential overrides, so the single span is
// an incomplete edit target (the other arm goes stale for hosts that take
// it). The fidelity check backstops the wrong-arm case: editing the arm
// this host does not take leaves the evaluated version unchanged, and the
// observed delta fails the prediction. The complete cross-branch edit is a
// composite, which is the planner's job.
func TestLocateSameValueAcrossBranchesPicksLast(t *testing.T) {
	src := `PortSystem 1.0
name same
if {${os.major} >= 20} {
    github.setup        foo bar 3.1
} else {
    version             3.1
}
`
	loc := mustLocate(t, src, info.Values{Name: "same", Version: "3.1"})
	require.Equal(t, VersionLine, loc.Style, "later span in document order wins")
}

func locateField(t *testing.T, src string, vals info.Values, field info.Field) (Located, error) {
	t.Helper()
	b := []byte(src)
	tree, errs := syntax.Parse(b)
	require.Empty(t, errs)
	return Locate(b, tree, vals, field)
}

func TestLocateRevision(t *testing.T) {
	src := "PortSystem 1.0\nversion 1.0\nrevision 3\n"
	loc, err := locateField(t, src, info.Values{Name: "x", Version: "1.0", Revision: "3"}, info.FieldRevision)
	require.NoError(t, err)
	require.Equal(t, RevisionLine, loc.Style)
	require.Equal(t, "3", loc.Span.Text([]byte(src)))

	// No revision line: nothing carries the field.
	_, err = locateField(t, "version 1.0\n", info.Values{Version: "1.0", Revision: "0"}, info.FieldRevision)
	var d *Decline
	require.ErrorAs(t, err, &d)
	require.Equal(t, UnknownStyle, d.Type)

	// A revision line whose literal is not the evaluated revision cannot
	// corroborate.
	_, err = locateField(t, "version 1.0\nrevision [expr 1+2]\n",
		info.Values{Version: "1.0", Revision: "3"}, info.FieldRevision)
	require.ErrorAs(t, err, &d)
	require.Equal(t, NotLiteral, d.Type)
}

func TestLocateRevisionInSubportBlock(t *testing.T) {
	src := `version 1.0
revision 1
subport foo-sub {
    revision 5
}
`
	loc, err := locateField(t, src, info.Values{Name: "foo-sub", Version: "1.0", Revision: "5"}, info.FieldRevision)
	require.NoError(t, err)
	require.Equal(t, "5", loc.Span.Text([]byte(src)))
}

func TestLocateCorroboratesASetVariable(t *testing.T) {
	src := "set myver 1.2.3\nversion ${myver}\n"
	loc := mustLocate(t, src, info.Values{Name: "foo", Version: "1.2.3"})
	require.Equal(t, SetVariable, loc.Style)
	require.Equal(t, "1.2.3", loc.Span.Text([]byte(src)))
}

func TestLocateNeverLetsASetShadowARealStyle(t *testing.T) {
	// The coincidental set comes LAST — position would pick it; rank
	// must not.
	src := "github.setup a b 1.2.3\nset unrelated 1.2.3\n"
	loc := mustLocate(t, src, info.Values{Name: "b", Version: "1.2.3"})
	require.Equal(t, GithubSetup, loc.Style)
}

func TestLocateKeepsSetsOutOfProbeCandidates(t *testing.T) {
	// Nothing corroborates; the decline's candidates must not offer
	// the set span to the counterfactual probe.
	src := "set frag 7.7.7\ngithub.setup a b ${v}\n"
	d := mustDecline(t, src, info.Values{Name: "b", Version: "9.9.9"}, NotLiteral)
	for _, c := range d.Candidates {
		require.NotEqual(t, SetVariable, c.Style)
	}
}

func TestLocateSetOnlyMiscorroborationStaysUnknown(t *testing.T) {
	// Only set candidates and none corroborate: an empty candidate
	// list must degrade to UnknownStyle, never a hollow NotLiteral.
	src := "set frag 7.7.7\n"
	_ = mustDecline(t, src, info.Values{Name: "foo", Version: "9.9.9"}, UnknownStyle)
}
