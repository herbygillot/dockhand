package github

import (
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestManagedCorrectionPushRequiresRecordedRemoteHead(t *testing.T) {
	t.Parallel()
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{false: "unexpected", true: "recorded"}[authorized], func(t *testing.T) {
			f := setup(t)
			sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
			previous, err := f.provider.Repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.request.Spec.Source.Tree), Parents: []string{string(f.request.Spec.Source.Base)}, Message: "previous contribution", Author: sig, Committer: sig})
			require.NoError(t, err)
			require.NoError(t, f.provider.Repo.Push(t.Context(), git.Push{Remote: f.remote, Branch: "candidate", Commit: previous}))
			if authorized {
				f.request.Spec.ReplaceRemoteHead = record.ObjectID(previous)
			}
			result, err := f.provider.Submit(t.Context(), f.request)
			require.NoError(t, err)
			head, err := f.provider.Repo.RemoteHead(t.Context(), f.remote, "candidate")
			require.NoError(t, err)
			if authorized {
				require.NotEqual(t, verify.Unsupported, result.State)
				require.Equal(t, string(f.request.Spec.Source.Commit), head.Object)
			} else {
				require.Equal(t, verify.Unsupported, result.State)
				require.Equal(t, previous, head.Object)
			}
		})
	}
}
