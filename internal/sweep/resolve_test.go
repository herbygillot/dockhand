package sweep

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// entry is one port as the fixture index records it: the name, the
// portdir it lives in, and whatever else a rule under test reads.
type entry struct {
	name    string
	portdir string
	fields  map[string]string
}

// fixture builds a ports tree and its PortIndex from a list of entries,
// one portdir per distinct portdir named. The index framing is the real
// one — a "name length" header, then the Tcl key/value payload with no
// separator, the declared length counting UTF-16 code units and the
// payload's own newline.
func fixture(t *testing.T, entries ...entry) *tree.Tree {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))

	var index strings.Builder
	for _, e := range entries {
		dir := filepath.Join(root, filepath.FromSlash(e.portdir))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		pf := filepath.Join(dir, macports.PortfileName)
		if _, err := os.Stat(pf); err != nil {
			require.NoError(t, os.WriteFile(pf, []byte("PortSystem 1.0\n"), 0o644))
		}
		body := fmt.Sprintf("name %s portdir %s", e.name, e.portdir)
		for _, k := range sortedKeys(e.fields) {
			body += fmt.Sprintf(" %s %s", k, e.fields[k])
		}
		body += "\n"
		fmt.Fprintf(&index, "%s %d\n%s", e.name, len(utf16.Encode([]rune(body))), body)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), []byte(index.String()), 0o644))

	tr, err := tree.Open(root)
	require.NoError(t, err)
	return tr
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Small maps, and the order only has to be stable within a run.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func sources(tr *tree.Tree) Sources {
	return Sources{Tree: func() (*tree.Tree, error) { return tr, nil }}
}

func portdirs(res Resolution) []string {
	out := make([]string, 0, len(res.Targets))
	for _, t := range res.Targets {
		out = append(out, filepath.Base(t.Portdir))
	}
	return out
}

// A portdir path says where a port is without being looked up, so it
// must resolve with no tree behind it at all.
func TestResolvePathNeedsNoTree(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte("PortSystem 1.0\n"), 0o644))

	res, err := Resolve(context.Background(), Sources{}, []string{dir})
	require.NoError(t, err)
	require.Len(t, res.Targets, 1)
	assert.Equal(t, dir, res.Targets[0].Portdir)
}

func TestResolveNoSelector(t *testing.T) {
	_, err := Resolve(context.Background(), Sources{}, nil)
	require.ErrorIs(t, err, ErrNoSelector)
}

func TestResolveAll(t *testing.T) {
	tr := fixture(t,
		entry{name: "kubectl", portdir: "sysutils/kubectl"},
		entry{name: "ivy", portdir: "devel/ivy"},
	)
	res, err := Resolve(context.Background(), sources(tr), []string{"all"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ivy", "kubectl"}, portdirs(res), "lexical by portdir")
}

// The token only sweeps the tree when it is alone. That is the cheap
// mitigation for the day a port is named "all": it stays reachable
// beside any second argument, and by portdir either way.
func TestResolveAllOnlyAsSoleArgument(t *testing.T) {
	tr := fixture(t, entry{name: "kubectl", portdir: "sysutils/kubectl"})
	_, err := Resolve(context.Background(), sources(tr), []string{"all", "kubectl"})
	require.ErrorIs(t, err, tree.ErrPortNotFound)
}

func TestResolveCategoryForms(t *testing.T) {
	tr := fixture(t,
		entry{name: "ruby", portdir: "lang/ruby"},
		entry{name: "perl", portdir: "lang/perl"},
		entry{name: "kubectl", portdir: "sysutils/kubectl"},
	)
	for _, form := range []string{"category:lang", "lang", "lang/", "./lang"} {
		t.Run(form, func(t *testing.T) {
			if strings.HasPrefix(form, "./") || strings.HasSuffix(form, "/") {
				// The path spellings are relative to where the user is
				// standing, which for a category is the tree root.
				t.Chdir(tr.Root())
			}
			res, err := Resolve(context.Background(), sources(tr), []string{form})
			require.NoError(t, err)
			assert.Equal(t, []string{"perl", "ruby"}, portdirs(res))
		})
	}
}

// A bare <category>/<port> token names the port, from anywhere.
//
// This is a behaviour that MOVED, and it is pinned here because the
// house rule is byte-identity for a single target and this is the one
// invocation that is not. Before, the category test was a bare stat of
// <root>/<arg>, so "sysutils/jq" stat'd as a directory, was read as a
// CATEGORY, expanded to the portdirs under it — of which there are none
// — and the invocation silently named zero ports. `classify
// sysutils/jq` said "0 ports classified"; `bump --to X --plan
// sysutils/jq` exited 2 with "names 0" rather than reaching the port at
// all, which is a usage refusal where the answer was a decline.
//
// categoryName refuses any token containing a separator, so the token
// falls through to Tree.Resolve and finds the portdir. The silent
// zero-target expansion was the bug; this is the fix, and it is a fact
// of the suite rather than a side effect nothing covers.
func TestResolveReadsCategorySlashPortAsThePort(t *testing.T) {
	tr := fixture(t,
		entry{name: "jq", portdir: "sysutils/jq"},
		entry{name: "kubectl", portdir: "sysutils/kubectl"},
	)
	// From outside the tree, so nothing resolves as a relative path.
	t.Chdir(t.TempDir())

	res, err := Resolve(context.Background(), sources(tr), []string{"sysutils/jq"})
	require.NoError(t, err)
	assert.Equal(t, []string{"jq"}, portdirs(res),
		"a category/port token named the category and swept nothing")
	assert.Empty(t, res.Ambiguous, "one port is not a collision")

	// The bare category still sweeps, which is the reading that must
	// not have moved.
	res, err = Resolve(context.Background(), sources(tr), []string{"sysutils"})
	require.NoError(t, err)
	assert.Equal(t, []string{"jq", "kubectl"}, portdirs(res))
}

func TestResolveUnknownCategoryIsAnError(t *testing.T) {
	tr := fixture(t, entry{name: "kubectl", portdir: "sysutils/kubectl"})
	_, err := Resolve(context.Background(), sources(tr), []string{"category:nope"})
	require.ErrorIs(t, err, tree.ErrPortNotFound)
}

// A bare token that names both a category and a port resolves to the
// category, as it always has, and the collision is reported so a verb
// that must not silently sweep hundreds of ports can refuse.
func TestResolveReportsCategoryNameCollision(t *testing.T) {
	tr := fixture(t,
		entry{name: "ruby", portdir: "lang/ruby"},
		entry{name: "rb-foo", portdir: "ruby/rb-foo"},
		entry{name: "rb-bar", portdir: "ruby/rb-bar"},
	)
	res, err := Resolve(context.Background(), sources(tr), []string{"ruby"})
	require.NoError(t, err)
	assert.Equal(t, []string{"rb-bar", "rb-foo"}, portdirs(res), "the category wins")
	require.Len(t, res.Ambiguous, 1)
	assert.Equal(t, "ruby", res.Ambiguous[0].Token)
	assert.Equal(t, 2, res.Ambiguous[0].Category)
	assert.Equal(t, filepath.Join(tr.Root(), "lang", "ruby"), res.Ambiguous[0].Port.Portdir)
}

func TestResolveDedupesAndKeepsArgumentOrder(t *testing.T) {
	tr := fixture(t,
		entry{name: "kubectl", portdir: "sysutils/kubectl"},
		entry{name: "ivy", portdir: "devel/ivy"},
	)
	res, err := Resolve(context.Background(), sources(tr), []string{"kubectl", "ivy", "kubectl"})
	require.NoError(t, err)
	assert.Equal(t, []string{"kubectl", "ivy"}, portdirs(res))
}

// Two spellings of a bare handle, because the index keeps them apart on
// purpose and picking one silently loses ports.
func TestResolveMaintainerTriesBothSpellings(t *testing.T) {
	tr := fixture(t,
		entry{name: "a", portdir: "devel/a", fields: map[string]string{"maintainers": "@dbevans"}},
		entry{name: "b", portdir: "devel/b", fields: map[string]string{"maintainers": "devans"}},
		entry{name: "c", portdir: "devel/c", fields: map[string]string{"maintainers": "nomaintainer"}},
	)
	res, err := Resolve(context.Background(), sources(tr), []string{"maintainer:dbevans"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, portdirs(res))
	require.Len(t, res.Notes, 1)
	assert.Contains(t, res.Notes[0], "gh:dbevans names 1 port")

	res, err = Resolve(context.Background(), sources(tr), []string{"maintainer:devans"})
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, portdirs(res))
	assert.Contains(t, res.Notes[0], "mail:devans@macports.org names 1 port")
}

func TestResolveMaintainerExplicitKey(t *testing.T) {
	tr := fixture(t,
		entry{name: "a", portdir: "devel/a", fields: map[string]string{"maintainers": "@dbevans"}},
		entry{name: "b", portdir: "devel/b", fields: map[string]string{"maintainers": "devans"}},
	)
	res, err := Resolve(context.Background(), sources(tr), []string{"maintainer:gh:dbevans"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, portdirs(res))
}

// A typo'd handle that resolved to nothing would exit 0 having done
// nothing, which reads as success.
func TestResolveMaintainerUnknownIsAnError(t *testing.T) {
	tr := fixture(t, entry{name: "a", portdir: "devel/a", fields: map[string]string{"maintainers": "@dbevans"}})
	_, err := Resolve(context.Background(), sources(tr), []string{"maintainer:nobody"})
	require.ErrorIs(t, err, ErrNoMaintainer)
}

// "me" is the union of the forge handle and the git identity, because
// neither is complete: on the maintainer's own tree the handle names
// 1070 ports and the mail key 1072, and the two stragglers spell the
// handle differently inside a braced maintainers list.
func TestResolveMaintainerMe(t *testing.T) {
	tr := fixture(t,
		entry{name: "onlygh", portdir: "devel/onlygh",
			fields: map[string]string{"maintainers": "@herbygillot"}},
		entry{name: "onlymail", portdir: "devel/onlymail",
			fields: map[string]string{"maintainers": "gmail.com:herby.gillot"}},
		entry{name: "watcher", portdir: "devel/watcher",
			fields: map[string]string{"maintainers": "{@herby gmail.com:herby.gillot}"}},
		entry{name: "stranger", portdir: "devel/stranger",
			fields: map[string]string{"maintainers": "@someoneelse"}},
	)
	src := sources(tr)
	src.Login = func(context.Context) (string, error) { return "herbygillot", nil }
	src.Email = func(context.Context) (string, error) { return "herby.gillot@gmail.com", nil }

	res, err := Resolve(context.Background(), src, []string{"maintainer:me"})
	require.NoError(t, err)
	assert.Equal(t, []string{"onlygh", "onlymail", "watcher"}, portdirs(res))

	notes := strings.Join(res.Notes, "\n")
	assert.Contains(t, notes, "gh:herbygillot names 1 port")
	assert.Contains(t, notes, "mail:herby.gillot@gmail.com names 2 port")
	// The other spelling of the same person is offered, never folded
	// in — the index refuses identity guesses on purpose, so this hands
	// the guess to the one person who can make it.
	assert.Contains(t, notes, "near-miss")
	assert.Contains(t, notes, "gh:herby ")
	assert.NotContains(t, notes, "someoneelse")
}

// Half an identity still sweeps, and says which half is missing. None
// of it is an error with a named remedy, never a silent empty set.
func TestResolveMaintainerMeHalfResolved(t *testing.T) {
	tr := fixture(t, entry{name: "a", portdir: "devel/a",
		fields: map[string]string{"maintainers": "@herbygillot"}})
	src := sources(tr)
	src.Login = func(context.Context) (string, error) { return "herbygillot", nil }

	res, err := Resolve(context.Background(), src, []string{"maintainer:me"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, portdirs(res))
	assert.Contains(t, strings.Join(res.Notes, "\n"), "half-resolved")
}

func TestResolveMaintainerMeWithNoIdentity(t *testing.T) {
	tr := fixture(t, entry{name: "a", portdir: "devel/a",
		fields: map[string]string{"maintainers": "@herbygillot"}})
	_, err := Resolve(context.Background(), sources(tr), []string{"maintainer:me"})
	require.ErrorIs(t, err, ErrNoIdentity)
}

// A branch edits a file, so two subports of one Portfile are two
// targets and one unit of work — and the collapse is reported, because
// a user who selected more names than they get rows is owed the reason.
func TestCollapseByPortdir(t *testing.T) {
	in := []tree.Target{
		{Portdir: "/t/devel/libftdi", Subport: "libftdi0"},
		{Portdir: "/t/devel/libftdi", Subport: "libftdi1"},
		{Portdir: "/t/devel/ivy"},
	}
	out, names := CollapseByPortdir(in)
	assert.Equal(t, 3, names)
	require.Len(t, out, 2)
	assert.Equal(t, tree.Target{Portdir: "/t/devel/libftdi"}, out[0],
		"what survives addresses the Portfile, which is what an edit changes")
}
