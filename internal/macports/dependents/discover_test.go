package dependents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

var platform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

type evaluationFunc func(context.Context, macports.Context) (macports.Snapshot, error)

func (f evaluationFunc) Evaluate(ctx context.Context, target macports.Context) (macports.Snapshot, error) {
	return f(ctx, target)
}

func (f evaluationFunc) Resolve(context.Context, macports.Tree, macports.Selection) ([]record.Target, error) {
	return nil, errors.New("unexpected resolution: indexed targets already identify their Portfiles")
}

func evaluated(_ context.Context, target macports.Context) (macports.Snapshot, error) {
	name := target.Target().Name
	return macports.Snapshot{Source: target.Source(), Platform: target.Platform(), Target: target.Target(), Ports: map[string]macports.PortInfo{name: {Name: name}}}, nil
}

type entry struct {
	name, directory, fields string
}

func indexData(entries ...entry) string {
	var data strings.Builder
	for _, value := range entries {
		payload := fmt.Sprintf("name %s portdir %s %s\n", value.name, value.directory, value.fields)
		fmt.Fprintf(&data, "%s %d\n%s", value.name, len(utf16.Encode([]rune(payload))), payload)
	}
	return data.String()
}

func put(t *testing.T, root, name, contents string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
	require.NoError(t, os.WriteFile(file, []byte(contents), 0600))
}

func fixture(t *testing.T, entries ...entry) (macports.Tree, *portindex.Index) {
	t.Helper()
	root := t.TempDir()
	for _, value := range entries {
		put(t, root, value.directory+"/Portfile", "frozen source\n")
	}
	put(t, root, "PortIndex", indexData(entries...))
	index, err := portindex.Open(root)
	require.NoError(t, err)
	tree, err := macports.NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, platform)
	require.NoError(t, err)
	return tree, index
}

func TestDirectCoverageMergesReasonsAndKeepsRootVariants(t *testing.T) {
	tree, index := fixture(t,
		entry{"a", "devel/a", "depends_run port:b"},
		entry{"b", "devel/b", ""},
		entry{"consumer", "apps/consumer", "depends_lib {port:a port:b} depends_fetch port:unindexed conflicts competitor"},
		entry{"competitor", "apps/competitor", "depends_build port:a conflicts consumer"},
		entry{"indirect", "apps/indirect", "depends_run port:consumer"},
	)
	rootA := record.Target{Name: "a", Portfile: "devel/a/Portfile", Variants: map[string]bool{"special": true}}
	rootB := record.Target{Name: "b", Portfile: "devel/b/Portfile"}
	roots := []record.Target{rootB, rootA, rootA}
	coverage, err := discover(t.Context(), evaluationFunc(evaluated), tree, index, roots)
	require.NoError(t, err)
	require.Equal(t, tree.Source(), coverage.Source)
	require.Equal(t, platform, coverage.Platform)
	require.Empty(t, coverage.Problems)
	require.Len(t, coverage.Targets, 4)
	var names []string
	for _, candidate := range coverage.Targets {
		names = append(names, candidate.Target.Name)
		require.NotNil(t, candidate.Evaluation)
		require.Empty(t, candidate.Problem)
	}
	require.Equal(t, []string{"a", "b", "competitor", "consumer"}, names)
	require.True(t, coverage.Targets[0].Root)
	require.Equal(t, rootA, coverage.Targets[0].Target)
	require.Equal(t, []string{"b: depends_run"}, coverage.Targets[0].Reasons)
	require.Equal(t, []string{"a: depends_build"}, coverage.Targets[2].Reasons)
	consumer := coverage.Targets[3]
	require.False(t, consumer.Root)
	require.Equal(t, "consumer", consumer.Target.Subport)
	require.Empty(t, consumer.Target.Variants)
	require.Equal(t, []string{"a: depends_lib", "b: depends_lib"}, consumer.Reasons)
	require.Equal(t, []string{"a", "b", "unindexed"}, consumer.IndexedDependencies)
	require.Equal(t, []string{"dependency not indexed: unindexed"}, consumer.CoverageProblems)

	other, err := discover(t.Context(), evaluationFunc(evaluated), tree, index, []record.Target{rootA, rootB})
	require.NoError(t, err)
	require.Equal(t, coverage, other)
	coverage.Targets[0].Target.Variants["special"] = false
	require.True(t, rootA.Variants["special"])
	require.Equal(t, "b", roots[0].Name)
}

func TestFailuresAndUnreadFieldsRemainVisible(t *testing.T) {
	tree, index := fixture(t,
		entry{"core", "devel/core", ""},
		entry{"broken", "apps/broken", "depends_lib port:core"},
		entry{"healthy", "apps/healthy", "depends_lib port:core"},
		// The dictionary is valid; its depends_lib value is not a Tcl list.
		entry{"unknown", "apps/unknown", `depends_lib \{`},
	)
	reader := evaluationFunc(func(ctx context.Context, target macports.Context) (macports.Snapshot, error) {
		if target.Target().Name == "broken" {
			return macports.Snapshot{}, errors.New("fixture evaluation failure")
		}
		return evaluated(ctx, target)
	})
	coverage, err := discover(t.Context(), reader, tree, index, []record.Target{{Name: "core", Portfile: "devel/core/Portfile"}})
	require.NoError(t, err)
	require.Len(t, coverage.Targets, 3)
	require.Equal(t, "fixture evaluation failure", coverage.Targets[0].Problem)
	require.Nil(t, coverage.Targets[0].Evaluation)
	require.NotNil(t, coverage.Targets[2].Evaluation)
	require.Equal(t, []string{"reverse index unread: unknown depends_lib"}, coverage.Problems)
}

func TestEvaluationMustMatchFrozenQuestion(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*macports.Snapshot)
	}{
		{"source", func(s *macports.Snapshot) { s.Source.Tree = "other" }},
		{"platform", func(s *macports.Snapshot) { s.Platform.Version = "other" }},
		{"target", func(s *macports.Snapshot) { s.Target.Name = "other" }},
		{"missing port", func(s *macports.Snapshot) { s.Ports = nil }},
		{"wrong port", func(s *macports.Snapshot) { s.Ports["core"] = macports.PortInfo{Name: "other"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			tree, index := fixture(t, entry{"core", "devel/core", ""})
			reader := evaluationFunc(func(ctx context.Context, target macports.Context) (macports.Snapshot, error) {
				snapshot, err := evaluated(ctx, target)
				test.change(&snapshot)
				return snapshot, err
			})
			coverage, err := discover(t.Context(), reader, tree, index, []record.Target{{Name: "core", Portfile: "devel/core/Portfile"}})
			require.NoError(t, err)
			require.Nil(t, coverage.Targets[0].Evaluation)
			require.Contains(t, coverage.Targets[0].Problem, "does not match")
		})
	}
}

func TestMissingRootAndIndexedPathMismatch(t *testing.T) {
	tree, index := fixture(t, entry{"core", "devel/core", ""})
	coverage, err := discover(t.Context(), evaluationFunc(evaluated), tree, index, []record.Target{
		{Name: "core", Portfile: "other/core/Portfile"},
		{Name: "missing", Portfile: "devel/missing/Portfile"},
	})
	require.NoError(t, err)
	require.Contains(t, coverage.Targets[0].Problem, "indexed identity")
	require.Equal(t, []string{"dependency not indexed: missing"}, coverage.Targets[1].CoverageProblems)
	require.Contains(t, coverage.Targets[1].Problem, "not indexed")
	for _, target := range coverage.Targets {
		require.Nil(t, target.Evaluation)
	}
}

func TestCancellationDoesNotBecomePartialCoverage(t *testing.T) {
	tree, index := fixture(t, entry{"core", "devel/core", ""})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := evaluationFunc(func(ctx context.Context, target macports.Context) (macports.Snapshot, error) {
		cancel()
		return macports.Snapshot{}, ctx.Err()
	})
	coverage, err := discover(ctx, reader, tree, index, []record.Target{{Name: "core", Portfile: "devel/core/Portfile"}})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, coverage.Targets)
}

func TestDiscoverUsesFrozenTreeAndReleasesMaterialization(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"core", "consumer"} {
		put(t, root, "devel/"+name+"/Portfile", "frozen "+name+"\n")
	}
	put(t, root, ".fixture-index", indexData(entry{"core", "devel/core", ""}, entry{"consumer", "devel/consumer", "depends_lib port:core"}))
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), "git", "write-tree")
	cmd.Dir = root
	output, err := cmd.Output()
	require.NoError(t, err)
	source := record.Source{Tree: record.ObjectID(strings.TrimSpace(string(output)))}
	put(t, root, "devel/core/Portfile", "dirty checkout\n")
	put(t, root, ".fixture-index", "not an index\n")
	indexer := filepath.Join(t.TempDir(), "portindex")
	testsupport.WriteExecutable(t, indexer, `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) destination=$2; shift 2 ;;
    -p) shift 2 ;;
    -*) shift ;;
    *) source_root=$1; shift ;;
  esac
done
cp "$source_root/.fixture-index" "$destination/PortIndex"
printf 'core 0\n' > "$destination/PortIndex.quick"
`)
	var materialized string
	reader := evaluationFunc(func(ctx context.Context, target macports.Context) (macports.Snapshot, error) {
		materialized = target.Root()
		content, err := os.ReadFile(filepath.Join(target.Root(), target.Target().Portfile))
		require.NoError(t, err)
		require.Equal(t, "frozen "+target.Target().Name+"\n", string(content))
		require.NotEqual(t, root, target.Root())
		return evaluated(ctx, target)
	})
	service := Service{Repo: repo, Ports: reader, Index: &portindex.Stager{Repo: repo, Config: portindex.Config{Executable: indexer, CacheDirectory: t.TempDir()}}}
	for range 2 {
		coverage, err := service.Discover(t.Context(), source, platform, []record.Target{{Name: "core", Portfile: "devel/core/Portfile"}})
		require.NoError(t, err)
		require.Len(t, coverage.Targets, 2)
		for _, target := range coverage.Targets {
			require.Empty(t, target.Problem)
			require.NotNil(t, target.Evaluation)
		}
		require.NotEmpty(t, materialized)
		require.NoDirExists(t, materialized)
	}
	content, err := os.ReadFile(filepath.Join(root, "devel/core/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "dirty checkout\n", string(content))
	require.NoFileExists(t, filepath.Join(root, "PortIndex"))
}

func TestNativeEvaluationSelectsIndexedSubports(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts port-tclsh is required")
	}
	evaluator := &eval.Evaluator{Executable: executable}
	native, err := evaluator.NativePlatform(t.Context())
	require.NoError(t, err)
	tree, index := fixture(t,
		entry{"core", "devel/core", ""},
		entry{"consumer", "apps/bundle", "depends_lib port:core"},
		entry{"top", "apps/top", "depends_lib port:core"},
	)
	put(t, tree.Root(), "devel/core/Portfile", "PortSystem 1.0\nname core\nversion 1\ncategories devel\n")
	put(t, tree.Root(), "apps/bundle/Portfile", "PortSystem 1.0\nname bundle\nversion 1\ncategories apps\nsubport consumer { depends_lib port:core }\n")
	put(t, tree.Root(), "apps/top/Portfile", "PortSystem 1.0\nname top\nversion 1\ncategories apps\ndepends_lib port:core\n")
	tree, err = macports.NewTree(tree.Source(), tree.Root(), native)
	require.NoError(t, err)
	coverage, err := discover(t.Context(), evaluator, tree, index, []record.Target{{Name: "core", Portfile: "devel/core/Portfile"}})
	require.NoError(t, err)
	require.Len(t, coverage.Targets, 3)
	for _, candidate := range coverage.Targets {
		require.Empty(t, candidate.Problem, "%s", candidate.Target.Name)
		require.NotNil(t, candidate.Evaluation)
	}
	require.Equal(t, "consumer", coverage.Targets[0].Target.Name)
	require.Equal(t, "consumer", coverage.Targets[0].Target.Subport)
	require.Equal(t, coverage.Targets[0].Target, coverage.Targets[0].Evaluation.Target)
}

func TestDiscoveryProjectsToolRequirementsAndRejectsUnreadXcode(t *testing.T) {
	for _, value := range []string{"yes", "no", "invalid", "unread"} {
		t.Run(value, func(t *testing.T) {
			tree, index := fixture(t, entry{"core", "devel/core", ""})
			reader := evaluationFunc(func(ctx context.Context, target macports.Context) (macports.Snapshot, error) {
				snapshot, err := evaluated(ctx, target)
				info := snapshot.Ports["core"]
				info.Options = map[string]string{"use_xcode": value}
				if value == "unread" {
					info.OptionErrors = map[string]string{"use_xcode": "unavailable option"}
				}
				snapshot.Ports["core"] = info
				return snapshot, err
			})
			coverage, err := discover(t.Context(), reader, tree, index, []record.Target{{Name: "core", Portfile: "devel/core/Portfile"}})
			require.NoError(t, err)
			candidate := coverage.Targets[0]
			if value == "invalid" || value == "unread" {
				require.Nil(t, candidate.Evaluation)
				require.Contains(t, candidate.Problem, "use_xcode")
			} else {
				require.Empty(t, candidate.Problem)
				require.NotNil(t, candidate.Evaluation)
				require.Equal(t, value == "yes", candidate.Evaluation.NeedsXcode)
				require.Equal(t, tree.Source(), candidate.Evaluation.Source)
				require.Equal(t, tree.Platform(), candidate.Evaluation.Platform)
			}
		})
	}
}
