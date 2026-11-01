package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A pull request's merge fitness is in its status: its ports' maintainers
// as their change records keep them, who its description mentions, who
// approved it, and when MacPorts' maintainer timeout ends, 72 hours from
// its opening, by the engine's clock; a maintainer's approval ends the
// wait. The reading is the one status already makes of it.
func TestAPullRequestsMergeFitnessIsInItsStatus(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	opened := at.Add(-31 * time.Hour)
	fake.PRs[number].CreatedAt, fake.PRs[number].Author, fake.PRs[number].Mentions = opened, "herbygillot", []string{"ryandesign"}

	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, statuses[0].Merge, "nothing read of the pull request yet")

	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, statuses[0].Merge)
	require.Equal(t, []string{"jq"}, statuses[0].MaintainersUnread, "no change record keeps its maintainers")

	tree := model.ObjectID(strings.TrimSpace(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD^{tree}")))
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		return tx.RecordChange(model.ChangeRecord{Branch: branch.ID, Tree: tree, Base: branch.Base, Directory: "textproc/jq", Policy: fidelity.ChangePolicy, At: at,
			Ports: []model.SubportChange{{Port: "jq", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "version", From: "1.7.1", To: "1.8.1"}}, Maintainers: "{@ryandesign ryandesign} openmaintainer"}}})
	}))
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	merge := statuses[0].Merge
	require.Empty(t, statuses[0].MaintainersUnread)
	require.Equal(t, []string{"jq"}, merge.Waiting())
	require.True(t, merge.Ports[0].OpenMaintainer)
	require.Equal(t, opened, merge.Opened, "the timeout counts from the opening")
	require.Equal(t, opened.Add(72*time.Hour), merge.Until)
	require.Equal(t, 41*time.Hour, merge.Left, "by the engine's clock")
	require.Equal(t, []string{"ryandesign"}, merge.Mentioned)

	later := opened.Add(73 * time.Hour)
	e.options.Now = func() time.Time { return later }
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	require.True(t, statuses[0].Merge.Passed())

	fake.Statuses = map[int]forge.PullRequestStatus{number: {Review: "approved", Approvals: 1, ApprovedBy: []string{"ryandesign"}}}
	e.options.Now = func() time.Time { return at }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	merge = statuses[0].Merge
	require.Equal(t, []string{"ryandesign"}, merge.Approvers)
	require.Equal(t, []string{"@ryandesign"}, merge.Ports[0].ApprovedBy)
	require.Empty(t, merge.Waiting(), "its maintainer's approval ends the wait")
	require.True(t, merge.Until.IsZero())
}

// cacheIndexGeneration writes a completed port index generation of a tree
// into an indexing environment of the engine's cache, as a build publishes
// one, each entry a port's name, directory, and maintainers.
func cacheIndexGeneration(t *testing.T, cache, tree string, ports map[string][2]string) {
	t.Helper()
	profile := filepath.Join(cache, strings.Repeat("e", 64))
	entry := filepath.Join(profile, "generations", tree)
	require.NoError(t, os.MkdirAll(entry, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(profile, "cache.lock"), nil, 0o600))
	var index, quick strings.Builder
	for name, port := range ports {
		fmt.Fprintf(&quick, "%s %d\n", name, index.Len())
		payload := fmt.Sprintf("name %s portdir %s maintainers {%s}\n", name, port[0], port[1])
		fmt.Fprintf(&index, "%s %d\n%s", name, len(payload), payload)
	}
	require.NoError(t, os.WriteFile(filepath.Join(entry, "PortIndex"), []byte(index.String()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(entry, "PortIndex.quick"), []byte(quick.String()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(entry, "generation.json"), []byte(`{"tree":"`+tree+`"}`), 0o600))
}

// Where no change record keeps a port's maintainers, they're read from
// master's port index, the newest generation cached for master or one of
// its first-parent ancestors, so a pull request shows them before any
// record is made; with none cached they're not read yet. Status only
// looks at the cache: no portindex runs and the mirror isn't asked.
func TestAPullRequestsMaintainersAreReadFromMastersCachedIndex(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	fake.PRs[submitted.PullRequest.Ref.Number].CreatedAt = at.Add(-31 * time.Hour)
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)

	// Anything that builds or fetches an index says so.
	ran := filepath.Join(t.TempDir(), "ran")
	portindexStandIn := filepath.Join(t.TempDir(), "portindex")
	require.NoError(t, os.WriteFile(portindexStandIn, []byte("#!/bin/sh\ntouch "+ran+"\nexit 1\n"), 0o755))
	var fetched atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetched.Add(1)
		http.NotFound(w, nil)
	}))
	defer mirror.Close()
	e.options.Programs.Portindex, e.options.IndexMirror = portindexStandIn, mirror.URL

	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	require.NotNil(t, statuses[0].Merge)
	require.Equal(t, []string{"jq"}, statuses[0].MaintainersUnread, "no generation is cached")

	// A generation of master's parent's tree is the newest at or behind
	// master; one of a tree master never had isn't master's.
	master := testsupport.Git(t, e.Repo.Root, "rev-parse", "refs/dockhand/master")
	parent := testsupport.Git(t, e.Repo.Root, "rev-parse", master+"^^{tree}")
	cacheIndexGeneration(t, e.options.Indexes, parent, map[string][2]string{"jq": {"textproc/jq", "{@ryandesign ryandesign} openmaintainer"}})
	cacheIndexGeneration(t, e.options.Indexes, strings.Repeat("9", 40), map[string][2]string{"jq": {"textproc/jq", "nomaintainer"}})
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	merge := statuses[0].Merge
	require.Empty(t, statuses[0].MaintainersUnread)
	require.Len(t, merge.Ports, 1)
	require.Equal(t, []string{"@ryandesign"}, merge.Ports[0].People)
	require.True(t, merge.Ports[0].OpenMaintainer)
	require.Equal(t, []string{"jq"}, merge.Waiting())

	_, err = os.Stat(ran)
	require.ErrorIs(t, err, os.ErrNotExist, "no portindex ran")
	require.Zero(t, fetched.Load(), "the mirror wasn't asked")
	trees, err := portindex.CachedTrees(e.options.Indexes)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{parent: true, strings.Repeat("9", 40): true}, trees, "no generation was built")
}
