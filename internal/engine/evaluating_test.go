package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// markedPorts counts the reads that reach the port reader with no owner
// on their context, and those whose owner refused.
type markedPorts struct {
	fakePorts
	unmarked, refused atomic.Int32
}

func (p *markedPorts) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	owner, ok := workspace.OwnerOf(ctx)
	switch {
	case !ok:
		p.unmarked.Add(1)
	case owner.Refused != nil:
		p.refused.Add(1)
		return nil, owner.Refused
	}
	return p.fakePorts.Ports(ctx, source, directory, environment, variants)
}

// A queued check of an adopted pull request whose author's commit the
// person hasn't allowed, as one queued before #162 landed, is never driven:
// the driver names the run's owner once, as it takes the run and before
// its first read, and a run that isn't allowed stays queued with its
// reason. One that is allowed reads with its owner on every read (Codex's
// review of #162, finding 1, which drove such a run to a pass with three
// unmarked reads).
func TestADriverNamesTheOwnerBeforeItReadsAQueuedRun(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		b, err := tx.Branch(queued.Branch)
		if err != nil {
			return err
		}
		b.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 34905, Head: "contributor:patch", Adopted: true, Author: "contributor", AuthorHead: "1111111111111111111111111111111111111111"}
		return tx.UpdateBranch(b)
	}))
	spy := &markedPorts{fakePorts: harborPorts()}
	usePorts(e, spy)

	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.ErrorContains(t, err, "dockhand adopt --pr 34905 --evaluate")
	require.Equal(t, model.RunQueued, run.State, "it stays queued")
	require.Zero(t, spy.unmarked.Load(), "nothing read it unmarked")
	require.Zero(t, spy.refused.Load(), "nor tried to")
	require.Empty(t, provider.jobs, "nothing was built")
	events, err := e.RunEvents(t.Context(), queued.ID, 0)
	require.NoError(t, err)
	said := false
	for _, event := range events {
		said = said || event.Kind == model.EventWaiting && event.Message != ""
	}
	require.True(t, said, "the run's journal says why it waits")

	// Allowed, it's driven, every read with its owner.
	require.NoError(t, e.AllowEvaluation(t.Context(), 34905, "1111111111111111111111111111111111111111"))
	run, err = e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Zero(t, spy.unmarked.Load(), "every read carried its owner")
}

// A finding is resolved only where its rule ran this time: a text-only
// review, which runs neither restated's rule nor path-style's and leaves
// the subject's port unjudged where only the head defines it, carries an
// earlier review's findings of those rules forward, unresolved, and
// records them with its own, so the next comparison still has them
// (Codex's review of #162, finding 2).
func TestATextOnlyReviewResolvesOnlyWhatItsRulesRan(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	target, err := e.OpenReview(t.Context(), 34905)
	require.NoError(t, err)
	restated := commitrules.Finding{Code: "restated", Where: "textproc/jq/Portfile", Message: "an evaluated finding still present"}
	stale := commitrules.Finding{Code: "subject-vague", Where: "abc1234", Message: "a text rule's finding fixed since"}
	require.NoError(t, e.RecordReview(t.Context(), ReviewReport{Ref: target.Ref, Head: target.Head, Evaluated: true, Findings: []commitrules.Finding{restated, stale}}, ""))
	report, err := e.Review(t.Context(), target, false)
	require.NoError(t, err)
	require.Len(t, report.Resolved, 1, "a rule that ran and found nothing resolves its finding")
	require.Equal(t, "subject-vague", report.Resolved[0].Code)
	require.Len(t, report.Carried, 1, "a rule that didn't run resolves nothing")
	require.Equal(t, "restated", report.Carried[0].Code)
	require.NotContains(t, report.Markdown(), "~~an evaluated finding still present~~")

	// Recorded with it, the next full review compares against it still.
	require.NoError(t, e.RecordReview(t.Context(), report, ""))
	again, err := e.Review(t.Context(), target, false)
	require.NoError(t, err)
	require.Len(t, again.Carried, 1, "the text-only review's record kept it")
}

// What the question offers since the head the person allowed is what
// changed between that head and the new one: a PortGroup the allowed head
// added and the new head removes is in its diff and its files, where the
// paths were the base's to the new head's, which left it out (Codex's
// review of #162, finding 3).
func TestTheQuestionSinceAnAllowedHeadShowsWhatChangedBetweenThem(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	group := "_resources/port1.0/group/codex-1.0.tcl"
	testsupport.Git(t, f.upstream, "switch", "-q", "contrib")
	testsupport.WriteFiles(t, f.upstream, map[string]string{group: "# code allowed in A\n"})
	testsupport.Git(t, f.upstream, "add", group)
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq: add a group")
	a := testsupport.Git(t, f.upstream, "rev-parse", "HEAD")
	testsupport.Git(t, f.upstream, "rm", "-q", group)
	testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# B\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "jq: remove that group")
	testsupport.Git(t, f.upstream, "update-ref", "refs/pull/34905/head", "HEAD")
	testsupport.Git(t, f.upstream, "switch", "-q", "master")
	require.NoError(t, e.AllowEvaluation(t.Context(), 34905, model.ObjectID(a)))
	target, err := e.OpenReview(t.Context(), 34905)
	require.NoError(t, err)
	require.NotNil(t, target.Since)
	diff, err := e.ReviewDiff(t.Context(), target)
	require.NoError(t, err)
	require.Contains(t, string(diff), group, "the group the allowed head added and this one removed")
	require.Contains(t, string(diff), "textproc/jq/Portfile")
	var files []string
	for _, file := range target.Portfiles {
		files = append(files, file.Path)
	}
	require.Contains(t, files, group)
}
