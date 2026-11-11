package sweep

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
)

// recordingForge is the GitHub stand-in with each of its seven writes an
// effect, which fails as the plan says: before it happens, or after,
// its reply lost.
type recordingForge struct {
	*forgetest.GitHub
	plan *plan
}

// write records a forge write and runs it, or fails it.
func (f recordingForge) write(name string, run func() error) error {
	e := f.plan.record("forge."+name, "")
	how, fires := f.plan.fires(e)
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
	err = f.write("Create", func() (err error) { observed, err = f.GitHub.Create(ctx, input); return err })
	return observed, err
}

func (f recordingForge) Update(ctx context.Context, input forge.PullRequestInput) (observed forge.PullRequestObservation, err error) {
	err = f.write("Update", func() (err error) { observed, err = f.GitHub.Update(ctx, input); return err })
	return observed, err
}

func (f recordingForge) MarkReady(ctx context.Context, ref forge.PullRequestRef) (observed forge.PullRequestObservation, err error) {
	err = f.write("MarkReady", func() (err error) { observed, err = f.GitHub.MarkReady(ctx, ref); return err })
	return observed, err
}

func (f recordingForge) MarkDraft(ctx context.Context, ref forge.PullRequestRef) (observed forge.PullRequestObservation, err error) {
	err = f.write("MarkDraft", func() (err error) { observed, err = f.GitHub.MarkDraft(ctx, ref); return err })
	return observed, err
}

func (f recordingForge) PostReview(ctx context.Context, input forge.ReviewInput) (url string, err error) {
	err = f.write("PostReview", func() (err error) { url, err = f.GitHub.PostReview(ctx, input); return err })
	return url, err
}

func (f recordingForge) RequestReviewers(ctx context.Context, ref forge.PullRequestRef, logins []string) error {
	return f.write("RequestReviewers", func() error { return f.GitHub.RequestReviewers(ctx, ref, logins) })
}

func (f recordingForge) RerunFailed(ctx context.Context, repository string, run int64) error {
	return f.write("RerunFailed", func() error { return f.GitHub.RerunFailed(ctx, repository, run) })
}
