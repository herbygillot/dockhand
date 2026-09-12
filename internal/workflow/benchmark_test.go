package workflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/perftest"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func BenchmarkWorkflow(b *testing.B) {
	for _, n := range []int{10, 100} {
		for _, op := range []string{"status", "observe", "complete", "idle-all"} {
			b.Run(fmt.Sprintf("jobs=%d/%s", n, op), func(b *testing.B) {
				active := 1
				if op == "idle-all" {
					active = 0
				}
				f, err := perftest.New(b.Context(), b.TempDir(), n, 1, active)
				require.NoError(b, err)
				e := workflow.Engine{Ledger: f.Store, Provider: &perftest.Provider{Finish: op == "complete"}}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					require.NoError(b, f.Reset(b.Context()))
					ctx, cancel := context.WithTimeout(b.Context(), 30*time.Second)
					b.StartTimer()
					if op == "status" {
						_, err = e.Status(ctx, workflow.Scope{All: true})
					} else {
						scope := workflow.Scope{Jobs: f.Active}
						if op == "idle-all" {
							scope = workflow.Scope{All: true}
						}
						var result workflow.CycleResult
						result, err = e.Cycle(ctx, scope)
						require.Empty(b, result.Problems)
					}
					require.NoError(b, err)
					b.StopTimer()
					cancel()
					b.StartTimer()
				}
			})
		}
	}
}
