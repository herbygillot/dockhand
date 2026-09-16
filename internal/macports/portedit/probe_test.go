package portedit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func probeFixture(t *testing.T, declaration string) (*Service, Request, *sourceInput) {
	t.Helper()
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("native MacPorts evaluator required")
	}
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel/fixture"), 0700))
	body := `PortSystem 1.0
name fixture
categories devel
options github.author github.project github.version github.tag_prefix github.tag_suffix git.branch
github.author owner
github.project fixture
github.tag_prefix v
github.tag_suffix {}
proc github.setup {owner project raw prefix} {
 github.version $raw
 git.branch ${prefix}${raw}
 version $raw
}
` + declaration + "\nrevision 3\nchecksums sha256 " + strings.Repeat("0", 64) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel/fixture/Portfile"), []byte(body), 0600))
	request := Request{Action: record.Bump, Source: record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, Root: root, Selection: macports.Selection{Selector: "fixture"}}
	service := &Service{Ports: &eval.Evaluator{Executable: executable}}
	input, err := service.load(t.Context(), request)
	require.NoError(t, err)
	return service, request, input
}

func TestForwardVersionProbing(t *testing.T) {
	for _, test := range []struct{ name, body, source, version, kept string }{
		{"date", "github.setup owner fixture 2026-09-07 v\nversion [string map {- {}} ${github.version}]", "2026-09-14", "20260914", "version [string map"},
		{"validated date", `github.setup owner fixture 2026-09-07 v
version [clock format [clock scan ${github.version} -format %Y-%m-%d -gmt 1] -format %Y%m%d -gmt 1]`, "2026-09-14", "20260914", "version [clock format"},
		{"arithmetic", "set release 12\ngithub.setup owner fixture $release v\nversion [expr {${github.version} * 10 + 7}]", "13", "137", "version [expr"},
		{"procedure and fragment", "set patchNumber 3\nproc release {} {global patchNumber; return 1.2.${patchNumber}}\ngithub.setup owner fixture [release] v", "1.2.4", "1.2.4", "set patchNumber 4"},
		{"mapped separators", "set real_version 7.5\ngithub.setup owner fixture [string map {. _} $real_version] v\nversion $real_version", "7_6", "7.6", "set real_version 7.6"},
		{"equivalent numeric and substitution mappings", "set release 3\ngithub.setup owner fixture [expr {$release * 10 + 1}] v", "41", "41", "set release 4"},
		{"arithmetic source", "set patch 3\ngithub.setup owner fixture 1.2.[expr {$patch + 1}] v", "1.2.5", "1.2.5", "set patch 4"},
		{"inactive assignments", "set unused 1.2.3\nif {0} {set release 1.2.3}\nset release {1.2.3}\ngithub.setup owner fixture $release v", "1.2.4", "1.2.4", "set unused 1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, request, input := probeFixture(t, test.body)
			carriers, err := service.versionCarriers(t.Context(), request, input)
			require.NoError(t, err)
			contents, snapshot, err := service.probeVersion(t.Context(), request, input, carriers, test.source)
			require.NoError(t, err)
			require.Equal(t, test.version, snapshot.Ports["fixture"].Version)
			require.Equal(t, "v"+test.source, snapshot.Ports["fixture"].Options["git.branch"])
			require.Contains(t, string(contents), test.kept)
			original, err := os.ReadFile(filepath.Join(request.Root, "devel/fixture/Portfile"))
			require.NoError(t, err)
			require.Equal(t, input.data, original)
		})
	}
}
func TestVersionProbingRejectsAmbiguityAndSiblingChanges(t *testing.T) {
	for _, test := range []struct {
		name, body, detail string
		expected           error
	}{
		{"ambiguous", `set a 1.2.3
set b 1.2.3
if {$a ne "1.2.3"} {github.setup owner fixture $a v} else {github.setup owner fixture $b v}`, "2 version inputs", ErrUnsupported},
		{"sibling", "github.setup owner fixture 1.2.3 v\nsubport fixture-child {}", "fixture-child", ErrFidelity},
		{"unchanged evaluated version", "github.setup owner fixture 1.2.3 v\nversion 5", "did not change the evaluated version", ErrUnsupported},
		{"failed probe", `set release 1.2.3
if {$release ne "1.2.3"} {error "candidate is not evaluable"}
github.setup owner fixture $release v`, "inconclusive", ErrUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, request, input := probeFixture(t, test.body)
			carriers, err := service.versionCarriers(t.Context(), request, input)
			if err == nil {
				_, _, err = service.probeVersion(t.Context(), request, input, carriers, "1.2.4")
			}
			require.ErrorIs(t, err, test.expected)
			require.ErrorContains(t, err, test.detail)
			original, readErr := os.ReadFile(filepath.Join(request.Root, "devel/fixture/Portfile"))
			require.NoError(t, readErr)
			require.Equal(t, input.data, original)
		})
	}
}
func TestProbeCancellation(t *testing.T) {
	service, request, input := probeFixture(t, "github.setup owner fixture 1.2.3 v")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := service.versionCarriers(ctx, request, input)
	require.ErrorIs(t, err, context.Canceled)
}
