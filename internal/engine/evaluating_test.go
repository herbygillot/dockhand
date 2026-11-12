package engine

import (
	"cmp"
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
	revision, err := e.Revision(t.Context(), queued.Revision)
	require.NoError(t, err)
	// The author's commit is the one the revision was captured at.
	head := cmp.Or(revision.Source.Commit, revision.Head)
	require.NotEmpty(t, head)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		b, err := tx.Branch(queued.Branch)
		if err != nil {
			return err
		}
		b.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 34905, Head: "contributor:patch", Adopted: true, Author: "contributor", AuthorHead: head}
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
	require.NoError(t, e.AllowEvaluation(t.Context(), 34905, head))
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

// With no Portfile or PortGroup changed since the head the person allowed,
// the question has nothing to offer: an empty diff and no files, where an
// empty selection of paths diffed everything, a README included (Codex's
// confirmation of #162's fixes, finding 3's edge).
func TestNothingEvaluatedChangedSinceTheAllowedHeadOffersNothing(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	a := testsupport.Git(t, f.upstream, "rev-parse", "contrib")
	testsupport.Git(t, f.upstream, "switch", "-q", "contrib")
	testsupport.WriteFiles(t, f.upstream, map[string]string{"README.md": "only prose changed\n"})
	testsupport.Git(t, f.upstream, "add", "README.md")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "docs: explain jq")
	testsupport.Git(t, f.upstream, "update-ref", "refs/pull/34905/head", "HEAD")
	testsupport.Git(t, f.upstream, "switch", "-q", "master")
	require.NoError(t, e.AllowEvaluation(t.Context(), 34905, model.ObjectID(a)))
	target, err := e.OpenReview(t.Context(), 34905)
	require.NoError(t, err)
	diff, err := e.ReviewDiff(t.Context(), target)
	require.NoError(t, err)
	require.Empty(t, diff, "no Portfile or PortGroup changed since the allowed head")
	require.Empty(t, target.Portfiles)
}

// readsAt keeps the sources a reader was asked to read.
type readsAt struct {
	markedPorts
	reads []model.Source
}

func (p *readsAt) Ports(ctx context.Context, s model.Source, d string, e model.Environment, v map[string]bool) ([]macports.PortInfo, error) {
	p.reads = append(p.reads, s)
	return p.markedPorts.Ports(ctx, s, d, e, v)
}

// The driver asks consent for the author's commit behind the queued run's
// revision, not the branch's author's commit now: a check queued at head A,
// before the branch was refreshed to head B and B alone allowed, isn't
// driven on B's yes, since B may have taken back A's code (Codex's
// confirmation of #162's fixes, finding 1).
func TestTheDriverAsksConsentForTheQueuedRevisionsAuthorsCommit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	adopted, err := e.AdoptPullRequest(t.Context(), 34905, "")
	require.NoError(t, err)
	made := adopted
	old := adopted.Head
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: adopted.Branch, Mode: CaptureAuto})
	require.NoError(t, err)
	// A check queued before #162, when planning asked nothing: planned
	// with the branch's adoption set aside, then put back.
	setAside := func(adopted bool) {
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			b, err := tx.Branch(made.Branch.ID)
			if err != nil {
				return err
			}
			b.PullRequest.Adopted = adopted
			return tx.UpdateBranch(b)
		}))
	}
	setAside(false)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	setAside(true)
	queued, err := e.Enqueue(t.Context(), adopted.Branch, plan, model.OriginPerson)
	require.NoError(t, err)
	testsupport.Git(t, f.upstream, "switch", "-q", "contrib")
	testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\n# retracted old code\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "jq: retract the update")
	testsupport.Git(t, f.upstream, "update-ref", "refs/pull/34905/head", "HEAD")
	testsupport.Git(t, f.upstream, "switch", "-q", "master")
	target, err := e.OpenReview(t.Context(), 34905)
	require.NoError(t, err)
	moved, err := e.RefreshAdopted(t.Context(), adopted.Branch, model.ObjectID(target.Head))
	require.NoError(t, err)
	require.NoError(t, e.AllowAdopted(t.Context(), moved))
	allowed, err := e.Allowed(t.Context(), 34905, old)
	require.NoError(t, err)
	require.False(t, allowed, "A itself was never allowed")
	provider := &scriptedProvider{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	spy := &readsAt{markedPorts: markedPorts{fakePorts: fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}}}
	usePorts(e, spy)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.Error(t, err, "B's yes isn't A's")
	require.Equal(t, model.RunQueued, run.State)
	require.Empty(t, provider.jobs)
	require.Empty(t, spy.reads, "A's tree was never read")
}
