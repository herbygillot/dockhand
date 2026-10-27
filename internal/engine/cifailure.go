package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv/ghactions"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// classifyCI reads, for an open pull request whose checks fail at its
// head, the log of each failed CI job not yet classified, and classifies
// it by what made it fail: garage #35174's macos-15 job failed fetching
// an archive a mirror answered 403, where a check on the same commit
// passed, and status said "CI ✗" and nothing more (field testing,
// 2026-10-03). A job's log for one attempt never changes, so each is read
// once (schema 37); one gone or withheld is unclassified, with why, and
// not asked again. What couldn't be asked now, as the network or the
// hour's allowance, is asked at the next refresh.
func (e *Engine) classifyCI(ctx context.Context, branch model.Branch, next model.PullRequestObservation) []model.CIFailure {
	pr := branch.PullRequest
	if next.State != model.PullRequestOpen || next.Checks != model.ChecksFailing || next.Head == "" {
		return nil
	}
	jobs, err := e.forge().FailedJobs(ctx, pr.Repository, string(next.Head))
	if err != nil || len(jobs) == 0 {
		return nil
	}
	var known []model.CIFailure
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		known, err = r.CIFailures(branch.ID, next.Head)
		return err
	}); err != nil {
		return nil
	}
	read := map[[2]int64]bool{}
	for _, k := range known {
		read[[2]int64{k.Job, int64(k.Attempt)}] = true
	}
	var classified []model.CIFailure
	for _, job := range jobs {
		if read[[2]int64{job.ID, int64(job.Attempt)}] {
			continue
		}
		failure := model.CIFailure{Branch: branch.ID, Head: next.Head, Run: job.Run, Attempt: max(job.Attempt, 1), Job: job.ID, Check: job.Name, At: e.now()}
		log, err := e.forge().JobLog(ctx, pr.Repository, job.ID)
		switch {
		case errors.Is(err, forge.ErrNotFound):
			failure.Cause, failure.Reason = model.CIUnclassified, "its log is gone, or GitHub won't give it to this login"
		case err != nil:
			continue
		default:
			failure = classifiedCI(failure, log)
		}
		classified = append(classified, failure)
	}
	return classified
}

// classifiedCI is a failure as its log says, by the readings dockhand
// knows: an archive MacPorts' fetch couldn't get (buildlog.FetchFailed);
// else unclassified, with the log's first error line, read as a local
// log's is (D10), else the workflow's own error marker, where status said
// the log said nothing of its cause (xapian-core). And each port the job
// built, with its result, which MacPorts' workflow marks a port at a time
// (Then's item 7).
func classifiedCI(failure model.CIFailure, log []byte) model.CIFailure {
	failure.Ports = ghactions.PortResults(log)
	if fetch, ok := buildlog.FetchFailed(bytes.NewReader(log)); ok {
		failure.Cause, failure.File, failure.URL, failure.Status, failure.Reason = model.CIFetch, fetch.File, fetch.URL, fetch.Status, fetch.Reason
		return failure
	}
	failure.Cause, failure.Reason = model.CIUnclassified, "its log names no cause dockhand reads"
	if cause, ok := buildlog.FirstFrom(bytes.NewReader(ghactions.Untimed(log)), 1); ok {
		failure.Error = cause.Line
	} else if line, ok := ghactions.FirstError(log); ok {
		failure.Error = line
	}
	return failure
}

// CIFailureWords say a classified CI failure as status does after "CI
// failed": "fetching jni-sys-macros-0.4.1.crate (mirror.sjtu.edu.cn
// answered 403) on macos-15"; one unclassified by its log's first error
// line, "on macos-15: foo.cpp:12:3: error: …", or its check alone.
func CIFailureWords(failure model.CIFailure) string {
	if failure.Cause == model.CIFetch {
		fetch := buildlog.FetchFailure{File: failure.File, URL: failure.URL, Status: failure.Status, Reason: failure.Reason}
		return fmt.Sprintf("%s on %s", fetch.Words(), failure.Check)
	}
	if failure.Error != "" {
		return fmt.Sprintf("on %s: %s", failure.Check, failure.Error)
	}
	return "on " + failure.Check
}

// CIPortWords are each port a pull request's failed CI jobs built, with
// its result, a job's result for a port where several jobs built it
// read worst first: "exiv2 ✓, gpsbabel ✗ (build)". Empty where no job's
// log said any (Then's item 7).
func CIPortWords(failures []model.CIFailure) string {
	results := map[string]model.CIPort{}
	var names []string
	for _, failure := range failures {
		for _, port := range failure.Ports {
			was, seen := results[port.Port]
			if !seen {
				names = append(names, port.Port)
			}
			if !seen || was.Outcome == model.OutcomePassed && port.Outcome != model.OutcomePassed {
				results[port.Port] = port
			}
		}
	}
	slices.Sort(names)
	var words []string
	for _, name := range names {
		port := results[name]
		switch {
		case port.Outcome == model.OutcomePassed:
			words = append(words, name+" ✓")
		case port.Phase == model.PhaseInstall || port.Phase == model.PhaseDependencies || port.Phase == "":
			words = append(words, name+" ✗ (build)")
		default:
			words = append(words, fmt.Sprintf("%s ✗ (%s)", name, port.Phase))
		}
	}
	return strings.Join(words, ", ")
}

// RerunCI runs the failed CI jobs of a branch's pull request again, at its
// head, as the person whose login it is: one re-run of each failed
// workflow run, as `gh run rerun --failed` did by hand for garage #35174
// (field testing, 2026-10-03). It needs write access to the repository,
// which a maintainer has; without it, it's refused, with what moves it on.
// It returns the runs it asked to run again.
func (e *Engine) RerunCI(ctx context.Context, branch model.Branch) ([]int64, error) {
	pr := branch.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.State != model.PullRequestOpen {
		return nil, refusal.New(refusal.Failed, "%s has no open pull request to run CI for", branch.ShortName())
	}
	jobs, err := e.forge().FailedJobs(ctx, pr.Repository, string(pr.Observed.Head))
	if err != nil {
		return nil, err
	}
	var runs []int64
	for _, job := range jobs {
		if !slices.Contains(runs, job.Run) {
			runs = append(runs, job.Run)
		}
	}
	if len(runs) == 0 {
		return nil, refusal.New(refusal.Failed, "nothing of #%d's CI failed at its head, %s", pr.Number, pr.Observed.Head.Short()).
			Then(refusal.Next{Verb: refusal.VerbRefresh})
	}
	for i, run := range runs {
		err := e.forge().RerunFailed(ctx, pr.Repository, run)
		switch {
		case errors.Is(err, forge.ErrRejected):
			return runs[:i], refusal.Wrap(refusal.Auth, err, "running #%d's failed CI again needs write access to %s, which a maintainer has", pr.Number, pr.Repository).
				Then(refusal.Next{External: fmt.Sprintf("ask a maintainer to run it again, or push to #%d, which runs it anew", pr.Number)})
		case err != nil:
			return runs[:i], err
		}
	}
	return runs, nil
}
