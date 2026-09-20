package portedit

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// countedPorts counts the shared session's family and selected evaluations.
type countedPorts struct {
	*eval.Evaluator
	family, selected int
}

func (c *countedPorts) OpenBatch(ctx context.Context, tree macports.Tree) (macports.Batch, error) {
	batch, err := c.Evaluator.OpenBatch(ctx, tree)
	if err != nil {
		return nil, err
	}
	return &countedBatch{Batch: batch, counts: c}, nil
}

type countedBatch struct {
	macports.Batch
	counts *countedPorts
}

func (b *countedBatch) Evaluate(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	b.counts.family++
	return b.Batch.Evaluate(ctx, source)
}

func (b *countedBatch) EvaluateSelected(ctx context.Context, source macports.Context) (macports.Snapshot, error) {
	b.counts.selected++
	return b.Batch.EvaluateSelected(ctx, source)
}

// familyFixture is a Portfile with one subport beside its main port.
func familyFixture(t *testing.T) (*countedPorts, Request) {
	t.Helper()
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("native MacPorts evaluator required")
	}
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel/fixture"), 0700))
	body := "PortSystem 1.0\nname fixture\nversion 1.0\ncategories devel\nlicense MIT\ndescription fixture\nlong_description fixture\nhomepage https://example.invalid\nmaster_sites https://example.invalid/\nchecksums sha256 " + strings.Repeat("0", 64) + "\nsubport fixture-child {\n version 2.0\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel/fixture/Portfile"), []byte(body), 0600))
	request := Request{Action: record.Bump, Source: record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, Root: root}
	return &countedPorts{Evaluator: &eval.Evaluator{Executable: executable}}, request
}

func names(ports map[string]macports.PortInfo) []string { return slices.Sorted(maps.Keys(ports)) }

// A subport's baseline is its own evaluation; its siblings cost nothing until
// a fidelity check asks for them, and then they are evaluated once and kept.
func TestASubportBaselineEvaluatesItselfAndItsFamilyOnDemand(t *testing.T) {
	t.Parallel()
	ports, request := familyFixture(t)
	request.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-child"}
	input, err := (&Service{Ports: ports}).load(t.Context(), &request)
	require.NoError(t, err)
	defer input.Close()
	require.Equal(t, 1, ports.selected, "the baseline is the subport alone")
	require.Equal(t, 0, ports.family, "siblings are not evaluated for an assessment")
	require.Equal(t, []string{"fixture-child"}, names(input.before.Ports))

	family, err := input.familySnapshot(t.Context(), ports)
	require.NoError(t, err)
	require.Equal(t, 1, ports.family, "evaluated on first demand")
	require.Equal(t, []string{"fixture", "fixture-child"}, names(family.Ports))
	_, err = input.familySnapshot(t.Context(), ports)
	require.NoError(t, err)
	require.Equal(t, 1, ports.family, "and kept")
}

// A main-port selection evaluates its Portfile whole at load, since stub
// detection reads the siblings; the family is then already known.
func TestAMainPortBaselineIsItsFamily(t *testing.T) {
	t.Parallel()
	ports, request := familyFixture(t)
	request.Selection = macports.Selection{Selector: "fixture"}
	input, err := (&Service{Ports: ports}).load(t.Context(), &request)
	require.NoError(t, err)
	defer input.Close()
	require.Equal(t, 1, ports.family)
	require.Equal(t, 0, ports.selected)
	require.Equal(t, []string{"fixture", "fixture-child"}, names(input.before.Ports))
	_, err = input.familySnapshot(t.Context(), ports)
	require.NoError(t, err)
	require.Equal(t, 1, ports.family, "known since load")
}

// A recorded stub is the main port; a subport baseline brings its entry in,
// each evaluated alone, so the stub check and its borrowed livecheck hold.
func TestARecordedStubJoinsASubportBaselineAlone(t *testing.T) {
	t.Parallel()
	ports, request := familyFixture(t)
	request.Stub = "fixture"
	request.Selection = macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-child"}
	input, err := (&Service{Ports: ports}).load(t.Context(), &request)
	require.NoError(t, err)
	defer input.Close()
	require.Equal(t, 2, ports.selected, "the carrier and the stub")
	require.Equal(t, 0, ports.family)
	require.Equal(t, []string{"fixture", "fixture-child"}, names(input.before.Ports))
	require.True(t, request.SharedRelease)
}
