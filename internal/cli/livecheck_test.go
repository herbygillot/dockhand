package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestLivecheckBumpNamedSeriesFromDiscoveryThroughStoredBranch(t *testing.T) {
	config, repo, original := preparationCLI(t)
	var listingReads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/" {
			listingReads.Add(1)
			fmt.Fprint(w, `>fixture_1.17.0< >fixture_1.16.2< >fixture_1.16.1< >fixture_1.16.2<`)
			return
		}
		if r.URL.Path == "/dist/fixture-1.16.2.tar.gz" {
			fmt.Fprint(w, "archive contents")
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	contents := fmt.Sprintf(`PortSystem 1.0
name fixture
version 0
categories devel
subport fixture-1.16 {
 set patchNumber 0
 version 1.16.${patchNumber}
 master_sites %s/dist
 distname fixture-${version}
 checksums sha256 %s size 1
 livecheck.type regex
 livecheck.url %s/releases/
 livecheck.regex {>fixture_(1\.16\.\d+)<}
}
subport fixture-1.15 {
 version 1.15.8
}
`, server.URL, strings.Repeat("0", 64), server.URL)
	_, tree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	before, _, err := repo.File(t.Context(), tree, "devel/fixture/Portfile")
	require.NoError(t, err)
	tree, err = repo.EditTree(t.Context(), tree, []git.FileEdit{{Path: "devel/fixture/Portfile", Before: before, After: []byte(contents), Mode: before.Mode}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{original}, Message: "series fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Expected: git.RefValue{Exists: true, Object: original}, Desired: git.RefValue{Exists: true, Object: commit}}}))
	var out, logs bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture-1.16", "1.16.2", "--diff", "--json"}, Streams{Out: &out, Err: &logs}, config), logs.String())
	var explicit app.Preview
	require.NoError(t, json.Unmarshal(out.Bytes(), &explicit))
	require.Contains(t, explicit.Diff, "+ set patchNumber 2")
	require.Zero(t, listingReads.Load())
	out.Reset()
	logs.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture-1.16", "--no-verify", "--json"}, Streams{Out: &out, Err: &logs}, config), logs.String())
	var result ActionResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	job := result.Status.Jobs[0].Job
	require.Equal(t, record.JobCompleted, job.State)
	require.Equal(t, "1.16.2", job.ResolvedRelease.Version)
	require.NotNil(t, job.ResolvedRelease.Listing)
	require.NotEmpty(t, job.ResultRevision)
	require.Equal(t, "fixture-1.16", result.Status.Changes[0].InitiatingTarget)
	require.Equal(t, int64(1), listingReads.Load())
	_, updated, err := repo.File(t.Context(), string(job.Prepared.Source.Tree), "devel/fixture/Portfile")
	require.NoError(t, err)
	require.Contains(t, string(updated), "version 1.15.8")
	out.Reset()
	logs.Reset()
	require.NoError(t, Run(t.Context(), []string{"status", "fixture-1.16", "--json"}, Streams{Out: &out, Err: &logs}, config))
	require.Contains(t, out.String(), string(job.ID))
	// A retry must not need a fresh upstream fetch or rewrite the original
	// automatic release provenance when the user supplies its explicit version.
	output, err := exec.CommandContext(t.Context(), "git", "-C", repo.Root, "config", "url./missing/upstream.insteadOf", "https://github.com/macports/macports-ports.git").CombinedOutput()
	require.NoError(t, err, "%s", output)
	out.Reset()
	logs.Reset()
	require.NoError(t, Run(t.Context(), []string{"bump", "fixture-1.16", "1.16.2", "--no-verify", "--json"}, Streams{Out: &out, Err: &logs}, config), logs.String())
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, job.ID, result.Status.Jobs[0].Job.ID)
	require.Empty(t, result.Status.Jobs[0].Job.ResolvedRelease.Requested)
	require.Equal(t, int64(1), listingReads.Load())
	require.Contains(t, logs.String(), "Continuing contribution")

}
