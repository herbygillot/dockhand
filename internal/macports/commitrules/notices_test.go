package commitrules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The review survey's other lint-shaped findings are notices on the lines a
// change touches, each on its survey example (§8, §10, and §11).
func TestTheLintShapedNoticesFireOnTheSurveysExamples(t *testing.T) {
	t.Parallel()
	codes := func(findings []Finding) []string {
		var found []string
		for _, finding := range findings {
			found = append(found, finding.Code+" "+finding.Where)
		}
		return found
	}
	head := "PortSystem 1.0\nname py-fixture\nversion 1.0\n"
	for _, c := range []struct {
		why  string
		p    Portfile
		want []string
	}{
		{"#34307's BSD-3-Clause", Portfile{Path: "python/py-fixture/Portfile", Before: head + "license BSD\n", After: head + "license BSD-3-Clause\n"},
			[]string{"license-spdx python/py-fixture/Portfile:4"}},
		{"a license line the change doesn't touch", Portfile{Path: "python/py-fixture/Portfile", Before: head + "license Apache-2.0\n", After: head + "license Apache-2.0\nrevision 1\n"}, nil},
		{"MIT, which MacPorts names the same", Portfile{Path: "python/py-fixture/Portfile", Before: head, After: head + "license {MIT Apache-2.0}\n"},
			[]string{"license-spdx python/py-fixture/Portfile:4"}},
		{"#34621's new noarch Python port", Portfile{Path: "python/py-fixture/Portfile", After: "PortSystem 1.0\nPortGroup python 1.0\nname py-fixture\nsupported_archs noarch\n"},
			[]string{"noarch-platforms python/py-fixture/Portfile:4"}},
		{"#34519's platforms any, kept", Portfile{Path: "python/py-fixture/Portfile", After: head + "supported_archs noarch\nplatforms {darwin any}\n"}, nil},
		{"noarch the change doesn't touch", Portfile{Path: "python/py-fixture/Portfile", Before: head + "supported_archs noarch\n", After: head + "supported_archs noarch\nrevision 1\n"}, nil},
		{"#32161's commented-out line", Portfile{Path: "python/py-fixture/Portfile", Before: head + "depends_lib port:a\n", After: head + "# depends_lib port:a\n# The tests need network access.\n"},
			[]string{"commented-code python/py-fixture/Portfile:4"}},
		{"#34072's stray Portfile under files/", Portfile{Path: "python/py-fixture/Portfile", Before: head, After: head + "patchfiles patch-setup.py.diff\n", Files: []string{"patch-setup.py.diff", "Portfile.orig"}},
			[]string{"unreferenced-file python/py-fixture/files/Portfile.orig"}},
	} {
		require.Equal(t, c.want, codes(CheckNotices([]Portfile{c.p})), c.why)
	}
}
