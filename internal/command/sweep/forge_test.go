package sweep

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
)

// recordingForge is the GitHub stand-in with each of its seven writes an
// effect, which fails as the plan says: before it happens, or after,
// its reply lost. A write that fails gives its caller nothing back: a
// lost reply had handed back the observation it lost (#129's fix list,
// 8).
type recordingForge struct {
	*forgetest.GitHub
	plan *plan
}

// write records a forge write and runs it, or fails it.
func (f recordingForge) write(name string, run func() error) error {
	e := f.plan.record("forge."+name, "")
	how, fires := f.plan.fires(e)
	// What it requires is read where it runs: unfailed, or lost after.
	if !fires || how == lost {
		f.plan.atEffect(e)
	}
	return f.answer(e, how, fires, run)
}

// answer runs a forge write or fails it, as the plan says.
func (f recordingForge) answer(e effect, how mode, fires bool, run func() error) error {
	if !fires {
		return run()
	}
	if how == lost {
		if err := run(); err != nil {
			return err
		}
		return fmt.Errorf("%w, its reply lost: %s", errFault, e.address())
	}
	return fmt.Errorf("%w: %s", errFault, e.address())
}

func (f recordingForge) Create(ctx context.Context, input forge.PullRequestInput) (observed forge.PullRequestObservation, err error) {
	if err = f.write("Create", func() (err error) { observed, err = f.GitHub.Create(ctx, input); return err }); err != nil {
		return forge.PullRequestObservation{}, err
	}
	return observed, nil
}

func (f recordingForge) Update(ctx context.Context, input forge.PullRequestInput) (observed forge.PullRequestObservation, err error) {
	if err = f.write("Update", func() (err error) { observed, err = f.GitHub.Update(ctx, input); return err }); err != nil {
		return forge.PullRequestObservation{}, err
	}
	return observed, nil
}

func (f recordingForge) MarkReady(ctx context.Context, ref forge.PullRequestRef) (observed forge.PullRequestObservation, err error) {
	if err = f.write("MarkReady", func() (err error) { observed, err = f.GitHub.MarkReady(ctx, ref); return err }); err != nil {
		return forge.PullRequestObservation{}, err
	}
	return observed, nil
}

func (f recordingForge) MarkDraft(ctx context.Context, ref forge.PullRequestRef) (observed forge.PullRequestObservation, err error) {
	if err = f.write("MarkDraft", func() (err error) { observed, err = f.GitHub.MarkDraft(ctx, ref); return err }); err != nil {
		return forge.PullRequestObservation{}, err
	}
	return observed, nil
}

func (f recordingForge) PostReview(ctx context.Context, input forge.ReviewInput) (url string, err error) {
	if err = f.write("PostReview", func() (err error) { url, err = f.GitHub.PostReview(ctx, input); return err }); err != nil {
		return "", err
	}
	return url, nil
}

func (f recordingForge) RequestReviewers(ctx context.Context, ref forge.PullRequestRef, logins []string) error {
	return f.write("RequestReviewers", func() error { return f.GitHub.RequestReviewers(ctx, ref, logins) })
}

func (f recordingForge) RerunFailed(ctx context.Context, repository string, run int64) error {
	e := f.plan.record("forge.RerunFailed", repository)
	how, fires := f.plan.fires(e)
	if !fires || how == lost {
		f.plan.atEffect(e)
		if f.plan.owner != nil {
			f.plan.breaks(e, f.plan.owner.forge("RerunFailed", repository))
		}
	}
	return f.answer(e, how, fires, func() error { return f.GitHub.RerunFailed(ctx, repository, run) })
}

// A write whose reply is lost has happened, and its caller gets nothing
// of it back, as a reply lost on the way back gives nothing: the stand-in
// had handed back the observation it was meant to lose (#129's fix list,
// 8, from the outside read's probe).
func TestALostReplyGivesNothingBack(t *testing.T) {
	t.Parallel()
	input := forge.PullRequestInput{Repository: forgetest.UpstreamRepository, HeadRepository: forgetest.ForkRepository, HeadBranch: "one", BaseBranch: "master"}
	clean := newPlan()
	_, err := recordingForge{GitHub: forgetest.New("", ""), plan: clean}.Create(t.Context(), input)
	require.NoError(t, err)
	p := newPlan()
	p.fault = &fault{address: clean.effects()[0].address(), mode: lost}
	g := forgetest.New("", "")
	got, err := recordingForge{GitHub: g, plan: p}.Create(t.Context(), input)
	require.True(t, p.reached)
	require.ErrorIs(t, err, errFault)
	require.Len(t, g.PRs, 1, "it happened")
	require.Equal(t, forge.PullRequestObservation{}, got, "and nothing of it came back")
}
