package ledger_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/perftest"
	"github.com/stretchr/testify/require"
)

func BenchmarkLedger(b *testing.B) {
	for _, n := range []int{10, 100} {
		for _, sources := range []int{1, n} {
			for _, op := range []string{"read", "noop", "edit", "new-revision", "encode", "decode"} {
				b.Run(fmt.Sprintf("jobs=%d/sources=%d/%s", n, sources, op), func(b *testing.B) {
					f, err := perftest.New(b.Context(), b.TempDir(), n, sources, 1)
					require.NoError(b, err)
					data, err := ledger.Encode(f.State)
					require.NoError(b, err)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if op == "edit" || op == "new-revision" {
							b.StopTimer()
							require.NoError(b, f.Reset(b.Context()))
							b.StartTimer()
						}
						switch op {
						case "read":
							_, err = f.Store.Read(b.Context())
						case "noop":
							err = f.Store.Update(b.Context(), func(context.Context, *ledger.Transaction) error { return nil })
						case "edit":
							err = f.Edit(b.Context(), "benchmark edit")
						case "new-revision":
							err = f.AddRevision(b.Context())
						case "encode":
							_, err = ledger.Encode(f.State)
						case "decode":
							_, err = ledger.Decode(data)
						}
						require.NoError(b, err)
					}
				})
			}
		}
	}
}
