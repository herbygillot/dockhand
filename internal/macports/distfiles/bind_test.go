package distfiles_test

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBindingSeparatesEqualChecksumsAndPreservesAppendOwnership(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts required")
	}
	root := t.TempDir()
	path := filepath.Join(root, "devel/fixture/Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	src := []byte(`PortSystem 1.0
name fixture
version 1
master_sites https://example.invalid/$version
if {${build_arch} eq "arm64"} {
 checksums a.zip sha256 aaaa size 2
 distfiles a.zip
} else {
 checksums b.zip sha256 aaaa size 2
 distfiles b.zip
}
checksums-append pinned.zip sha256 bbbb size 3
distfiles-append pinned.zip
`)
	require.NoError(t, os.WriteFile(path, src, 0600))
	tree, err := macports.NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, record.Platform{})
	require.NoError(t, err)
	e := &eval.Evaluator{Executable: executable}
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	var bindings []distfiles.Binding
	for _, arch := range []string{"arm64", "x86_64"} {
		observed, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: arch}, Declarations: true})
		require.NoError(t, err)
		binding, err := distfiles.Bind(src, path, observed.Snapshot.Ports["fixture"], observed.Ports["fixture"])
		require.NoError(t, err)
		require.Len(t, binding.Artifacts, 2)
		require.Len(t, binding.Groups, 2)
		bindings = append(bindings, binding)
	}
	require.NotEqual(t, bindings[0].Groups[0].Values["sha256"].Span, bindings[1].Groups[0].Values["sha256"].Span)
	require.Equal(t, bindings[0].Groups[1].Values["sha256"].Span, bindings[1].Groups[1].Values["sha256"].Span)
}

func TestBindingAcceptsLegacyGroupsAndOwnsThemByFirstAlgorithm(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts required")
	}
	root := t.TempDir()
	path := filepath.Join(root, "devel/fixture/Portfile")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	src := []byte(`PortSystem 1.0
name fixture
version 1
master_sites https://example.invalid/$version
distfiles a.zip
checksums md5 aaaa \
    sha1 bbbb \
    rmd160 cccc
`)
	require.NoError(t, os.WriteFile(path, src, 0600))
	tree, err := macports.NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, record.Platform{})
	require.NoError(t, err)
	e := &eval.Evaluator{Executable: executable}
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	observed, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Declarations: true})
	require.NoError(t, err)
	binding, err := distfiles.Bind(src, path, observed.Snapshot.Ports["fixture"], observed.Ports["fixture"])
	require.NoError(t, err)
	require.Len(t, binding.Groups, 1)
	group := binding.Groups[0]
	require.True(t, group.Legacy())
	require.Equal(t, []string{"md5", "sha1", "rmd160"}, group.Kinds)
	require.Equal(t, strings.TrimSuffix(group.Values["md5"].Owner, "/1")+"/0", group.ID(), "owned by the first algorithm word")
	require.Equal(t, "md5 aaaa \\\n    sha1 bbbb \\\n    rmd160 cccc", group.Span().Text(src))
	require.Len(t, binding.Artifacts, 1)
}

// A checksum value the declaration reads from an array element or a digest
// table is owned by the one place the Portfile writes it; a value written
// twice has no owner.
func TestBindingTracesTableAndArrayValuesToTheirOneLiteral(t *testing.T) {
	t.Parallel()
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts required")
	}
	rmd, sha := strings.Repeat("a", 40), strings.Repeat("b", 64)
	for _, test := range []struct{ name, body string }{
		{"array elements", "set rmd160(fixture) " + rmd + "\nset sha256(fixture) " + sha + "\nset size(fixture) 646632\nchecksums rmd160 $rmd160(${subport}) sha256 $sha256(${subport}) size $size(${subport})\n"},
		{"lindex table", "array set modules {\n    fixture {\n        {\n            " + rmd + " \\\n            " + sha + " \\\n            646632\n        }\n    }\n}\nset info $modules(fixture)\nchecksums rmd160 [lindex [lindex ${info} 0] 0] sha256 [lindex [lindex ${info} 0] 1] size [lindex [lindex ${info} 0] 2]\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "devel/fixture/Portfile")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			src := []byte("PortSystem 1.0\nname fixture\nversion 1\nmaster_sites https://example.invalid/$version\ndistfiles fixture.zip\n" + test.body)
			require.NoError(t, os.WriteFile(path, src, 0600))
			tree, err := macports.NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, record.Platform{})
			require.NoError(t, err)
			e := &eval.Evaluator{Executable: executable}
			targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
			require.NoError(t, err)
			bound, err := tree.Select(targets[0])
			require.NoError(t, err)
			observed, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Declarations: true})
			require.NoError(t, err)
			binding, err := distfiles.Bind(src, path, observed.Snapshot.Ports["fixture"], observed.Ports["fixture"])
			require.NoError(t, err)
			require.Len(t, binding.Groups, 1)
			group := binding.Groups[0]
			require.True(t, group.Traced())
			require.Equal(t, rmd, group.Values["rmd160"].Span.Text(src))
			require.Equal(t, sha, group.Values["sha256"].Span.Text(src))
			require.Equal(t, "646632", group.Values["size"].Span.Text(src))
			require.False(t, group.Legacy())

			// The same digest written twice cannot be owned.
			twice := append([]byte("# "+sha+"\n"), src...)
			_, err = distfiles.Bind(twice, path, observed.Snapshot.Ports["fixture"], observed.Ports["fixture"])
			require.NoError(t, err, "a comment does not count")
			twice = append(src, []byte("set other "+sha+"\n")...)
			_, err = distfiles.Bind(twice, path, observed.Snapshot.Ports["fixture"], observed.Ports["fixture"])
			require.ErrorContains(t, err, "no unique literal owner")
		})
	}
}
