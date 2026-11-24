package observe

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestDecideFetchOrdersLeftPolicyVerdictAndUsablePlan(t *testing.T) {
	t.Parallel()
	port := "fixture"
	usableInfo := macports.PortInfo{Name: port, Options: map[string]string{
		"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0",
	}}
	observed := func(info macports.PortInfo, reading macports.PortObservation, modeled bool, unanswered string) macports.Observation {
		return macports.Observation{
			Modeled: modeled, Unanswered: unanswered,
			Snapshot: macports.Snapshot{Root: t.TempDir(), Ports: map[string]macports.PortInfo{port: info}},
			Ports:    map[string]macports.PortObservation{port: reading},
		}
	}
	tests := []struct {
		name     string
		context  macports.Observation
		need     FetchNeed
		want     FetchDecisionKind
		wantPlan int
	}{
		{name: "left before policy", context: observed(macports.PortInfo{}, macports.PortObservation{}, true, "exec"), need: FetchPlanNeed, want: FetchLeft},
		{name: "policy refusal for a plan", context: observed(macports.PortInfo{}, macports.PortObservation{Distfiles: []macports.Distfile{{Name: "fixture.tar.gz"}}}, false, ""), need: FetchPlanNeed, want: FetchRefused},
		{name: "empty plan has its own result", context: observed(usableInfo, macports.PortObservation{}, false, ""), need: FetchPlanNeed, want: FetchNoPlan},
		{name: "revision-only Git path skips policy and plan", context: observed(macports.PortInfo{Name: port, Options: map[string]string{"fetch.type": "git"}}, macports.PortObservation{Distfiles: []macports.Distfile{{Name: "fixture.tar.gz"}}}, false, ""), need: FetchRevisionOnly, want: FetchUsable},
		{name: "plan reader still applies Git fetch policy", context: observed(macports.PortInfo{Name: port, Options: map[string]string{"fetch.type": "git"}}, macports.PortObservation{Distfiles: []macports.Distfile{{Name: "fixture.tar.gz"}}}, false, ""), need: FetchPlanNeed, want: FetchRefused},
		{name: "native host read at plan level", context: observed(usableInfo, macports.PortObservation{HostAccess: true, Distfiles: []macports.Distfile{{Name: "fixture.tar.gz"}}}, false, ""), need: FetchPlanNeed, want: FetchUsable, wantPlan: 1},
		{name: "inconclusive verdict", context: observed(usableInfo, macports.PortObservation{HostAccess: true, Distfiles: []macports.Distfile{{Name: "fixture.tar.gz"}}}, false, ""), need: FetchJudgedPlan, want: FetchInconclusive},
		{name: "usable plan", context: observed(usableInfo, macports.PortObservation{Distfiles: []macports.Distfile{{Name: "fixture.tar.gz"}}}, false, ""), need: FetchPlanNeed, want: FetchUsable, wantPlan: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecideFetch(context.Background(), test.context, port, nil, "", test.need, false, nil)
			require.NoError(t, err)
			require.Equal(t, test.want, got.Kind)
			if got.Kind == FetchInconclusive {
				require.NotEmpty(t, got.Reason)
			}
			if test.want == FetchUsable && test.wantPlan > 0 {
				require.Equal(t, "fixture.tar.gz", got.Plan[0].Name)
			}
			if test.want == FetchRefused {
				require.Error(t, got.Err)
			}
			require.Len(t, got.Plan, test.wantPlan)
		})
	}
}

func TestDecideFetchDoesNotTurnAToleratedEmptyPlanIntoAnInconclusiveVerdict(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	declaration := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	problem := declaration.Values[0]
	port := "fixture"
	info := macports.PortInfo{Name: port, Options: map[string]string{
		"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0",
	}}
	observed := macports.Observation{
		Snapshot: macports.Snapshot{Root: t.TempDir(), Ports: map[string]macports.PortInfo{port: info}},
		Ports: map[string]macports.PortObservation{port: {
			HostAccess: true, Problems: []string{problem}, Declarations: []macports.Declaration{declaration}, BaseLibrary: b.lib,
		}},
	}

	decision, err := DecideFetch(t.Context(), observed, port, []byte("PortSystem 1.0\nbuild.args ${build.cmd}\n"), "", FetchJudgedPlan, false, nil)
	require.NoError(t, err)
	require.Equal(t, FetchNoPlan, decision.Kind, "Tolerate removed the only problem and the empty plan is not a host-state verdict")
	require.Empty(t, decision.Plan)
	require.Empty(t, decision.Metadata.Problems)
	require.False(t, decision.HadProblems)
}

func TestDecideFetchReturnsLoadFailuresAsErrors(t *testing.T) {
	t.Parallel()
	loadErr := errors.New("observation session unavailable")
	decision, err := DecideFetch(t.Context(), macports.Observation{
		Snapshot: macports.Snapshot{Ports: map[string]macports.PortInfo{"fixture": {Name: "fixture", Options: map[string]string{
			"fetch.type": "standard", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0", "fetch.has_credentials": "0",
		}}}},
	}, "fixture", nil, "", FetchPlanNeed, false, func(context.Context) (macports.Observation, error) {
		return macports.Observation{}, loadErr
	})
	require.ErrorIs(t, err, loadErr)
	require.Zero(t, decision)
}

func TestFetchDecisionRulesHaveOneProductionCaller(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	allowed := filepath.Join("internal", "macports", "portedit", "observe", "fetch_decision.go")
	fset := token.NewFileSet()
	var callers []string
	var decisionReaders []string
	requiredReaders := []string{
		"internal/macports/portedit/artifact_assess.go/assessArchives",
		"internal/macports/portedit/artifact_plan.go/bindArchivesOn",
		"internal/macports/portedit/artifact_plan.go/planObservedArchives",
		"internal/macports/portedit/checksums.go/checksumsToWrite",
		"internal/macports/portedit/revision_reset.go/resetRevision",
		"internal/macports/portedit/version.go/shippedPlan",
		"internal/macports/treequery/archives.go/planOfObserved",
	}
	err := filepath.WalkDir(filepath.Join(root, "internal", "macports"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "Left" || selector.Sel.Name == "Tolerate" || selector.Sel.Name == "CheckPolicy" {
				if filepath.Clean(relative) != allowed {
					callers = append(callers, relative+":"+fset.Position(call.Pos()).String())
				}
			}
			return true
		})
		for _, node := range file.Decls {
			function, ok := node.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			usesDecision := false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				usesDecision = usesDecision || ok && selector.Sel.Name == "DecideFetch"
				return true
			})
			if usesDecision {
				decisionReaders = append(decisionReaders, filepath.Join(relative, function.Name.Name))
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, callers, "fetch left, policy, and verdict checks belong to DecideFetch")
	require.Subset(t, requiredReaders, decisionReaders, "each listed reader calls DecideFetch")
}
