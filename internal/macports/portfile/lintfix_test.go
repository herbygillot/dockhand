package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The lint findings dockhand fixes are fixed on the lines a change adds or
// alters, and nowhere else (the person's "we should have Dockhand do
// those", 2026-10-27).
func TestTheLintFindingsAreFixedOnTouchedLines(t *testing.T) {
	t.Parallel()
	head := Modeline + "\n\nPortSystem 1.0\n"
	for _, c := range []struct {
		why, before, after, want string
		fixes                    []LintFix
	}{
		{"trailing whitespace on an added line",
			head + "name fixture  \nversion 1.0\n",
			head + "name fixture  \nversion 1.1 \t\n",
			head + "name fixture  \nversion 1.1\n",
			[]LintFix{{Kind: FixTrailingSpace, Lines: []int{5}}}},
		{"an escaped space stays",
			head + "version 1.0\n",
			head + "version 1.1\nconfigure.args a\\ \n",
			head + "version 1.1\nconfigure.args a\\ \n", nil},
		{"leading tabs under MacPorts' modeline",
			head + "\tname fixture\n",
			head + "\tname fixture\nif {1} {\n\t  version 1.1\n}\n",
			head + "\tname fixture\nif {1} {\n      version 1.1\n}\n",
			[]LintFix{{Kind: FixTabs, Lines: []int{6}}}},
		{"tabs the maintainer's modeline sets",
			"# -*- mode: tcl; indent-tabs-mode: t -*- vim:noet\n\nversion 1.0\n",
			"# -*- mode: tcl; indent-tabs-mode: t -*- vim:noet\n\nif {1} {\n\tversion 1.1\n}\n",
			"# -*- mode: tcl; indent-tabs-mode: t -*- vim:noet\n\nif {1} {\n\tversion 1.1\n}\n", nil},
		{"a continuation that swallows the next option",
			head + "depends_lib port:a\nuse_configure no\n",
			head + "depends_lib port:a \\\n            port:b \\\nuse_configure no\n",
			head + "depends_lib port:a \\\n            port:b\nuse_configure no\n",
			[]LintFix{{Kind: FixContinuation, Lines: []int{5}}}},
		{"a continuation left by a removed line",
			head + "depends_lib port:a \\\n            port:b\nuse_configure no\n",
			head + "depends_lib port:a \\\nuse_configure no\n",
			head + "depends_lib port:a\nuse_configure no\n",
			[]LintFix{{Kind: FixContinuation, Lines: []int{4}}}},
		{"a swallowing continuation the change didn't touch",
			head + "depends_lib port:a \\\nuse_configure no\nversion 1.0\n",
			head + "depends_lib port:a \\\nuse_configure no\nversion 1.1\n",
			head + "depends_lib port:a \\\nuse_configure no\nversion 1.1\n", nil},
		{"an unindented value isn't an option",
			head + "version 1.0\n",
			head + "version 1.1\ndepends_lib port:a \\\nport:b\n",
			head + "version 1.1\ndepends_lib port:a \\\nport:b\n", nil},
		{"in a subport's body",
			head + "subport fixture-devel {\n}\n",
			head + "subport fixture-devel {\n    depends_lib port:a \\\nuse_configure no\n}\n",
			head + "subport fixture-devel {\n    depends_lib port:a\nuse_configure no\n}\n",
			[]LintFix{{Kind: FixContinuation, Lines: []int{5}}}},
		{"in a description's braces",
			head + "version 1.0\n",
			head + "version 1.1\nlong_description {a \\\nfetches things}\n",
			head + "version 1.1\nlong_description {a \\\nfetches things}\n", nil},
		{"openmaintainer alone",
			head + "maintainers {a.org:b @b} openmaintainer\n",
			head + "maintainers openmaintainer\n",
			head + "maintainers nomaintainer\n",
			[]LintFix{{Kind: FixOpenMaintainer, Lines: []int{4}}}},
		{"openmaintainer beside a maintainer",
			head + "maintainers nomaintainer\n",
			head + "maintainers {a.org:b @b} openmaintainer\n",
			head + "maintainers {a.org:b @b} openmaintainer\n", nil},
		{"nothing touched",
			head + "name fixture \n",
			head + "name fixture \n",
			head + "name fixture \n", nil},
	} {
		got, fixes := FixTouched([]byte(c.before), []byte(c.after))
		require.Equal(t, c.want, string(got), c.why)
		require.Equal(t, c.fixes, fixes, c.why)
	}
}

func TestALintFixSaysWhatItDid(t *testing.T) {
	t.Parallel()
	require.Equal(t, "removed trailing whitespace on lines 4 and 9", LintFix{Kind: FixTrailingSpace, Lines: []int{4, 9}}.Words())
	require.Equal(t, `dropped the \ ending line 12, which carried the next line's option into its command`, LintFix{Kind: FixContinuation, Lines: []int{12}}.Words())
}

// github.tarball_from tarball moves to archive where one literal command
// at the top, or in the subport's block, sets it.
func TestTheTarballMovesToTheArchive(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		why, src, subport, want string
	}{
		{"at the top", "github.setup a b 1.0\ngithub.tarball_from tarball\n", "", "github.setup a b 1.0\ngithub.tarball_from archive\n"},
		{"aligned, under another comment", "# Change github.tarball_from respectively\ngithub.tarball_from     tarball\n", "", "# Change github.tarball_from respectively\ngithub.tarball_from     archive\n"},
		{"under the stock comment", "github.setup a b 1.0\n# Change github.tarball_from to 'releases' or 'archive' next update\ngithub.tarball_from tarball\nrevision 0\n", "", "github.setup a b 1.0\ngithub.tarball_from archive\nrevision 0\n"},
		{"in the subport's block", "subport b-devel {\n    github.tarball_from tarball\n}\n", "b-devel", "subport b-devel {\n    github.tarball_from archive\n}\n"},
		{"in another subport's block", "subport b-devel {\n    github.tarball_from tarball\n}\n", "", ""},
		{"in an if", "if {1} {\n    github.tarball_from tarball\n}\n", "", ""},
		{"set twice", "github.tarball_from tarball\nsubport b-devel {\n    github.tarball_from releases\n}\n", "", ""},
		{"already archive", "github.tarball_from archive\n", "", ""},
	} {
		got, ok := ArchiveFromTarball([]byte(c.src), c.subport)
		require.Equal(t, c.want != "", ok, c.why)
		if ok {
			require.Equal(t, c.want, string(got), c.why)
		}
	}
}
