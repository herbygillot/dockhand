package ghactions

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/remotewrite"
)

// launchNote is what an attempt notes before it starts work on GitHub, in
// the form Stop reads (model.GuestExecution.Launch): the launch's own ID,
// so a matrix's cells, which share one, count it once; the repository, the
// workflow, the branch, the whole commit and the check's whole ID; and
// what it is about to do. A push keeps the newest run listed for the
// branch and commit before it (Before); a rerun the run and the attempt
// it had; an attach the run it took, having pushed nothing. Nothing says
// why a launch is known to have started nothing, where it is.
type launchNote struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Check      string `json:"check"`
	Action     string `json:"action"`
	Before     int64  `json:"before,omitempty"`
	Run        int64  `json:"run,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	Nothing    string `json:"nothing,omitempty"`
}

// The actions a launch note names.
const (
	launchPush   = "push"
	launchRerun  = "rerun"
	launchAttach = "attach"
)

// newLaunch is a note for a launch of the job's check on branch, with an ID
// of its own.
func newLaunch(repository, branch string, job buildenv.Job, action string) launchNote {
	var id [8]byte
	_, _ = rand.Read(id[:])
	return launchNote{ID: hex.EncodeToString(id[:]), Repository: repository, Workflow: Workflow, Branch: branch, Commit: job.Commit, Check: string(job.Run.ID), Action: action}
}

func (n launchNote) String() string {
	data, _ := json.Marshal(n)
	return string(data)
}

// stopWait is how long Stop waits for an attempt's launch lock, and then
// reads the runs it canceled for GitHub to say they completed, every
// stopPoll.
const (
	stopWait = 30 * time.Second
	stopPoll = 5 * time.Second
)

// Stop ends what a check launched on GitHub (buildenv.Stopper), holding the
// asked attempt's launch lock, so nothing more can be pushed or run again
// for it (the lifecycle design's "GitHub"). Every run GitHub lists for the
// check's workflow, branch and commit is the check's, since its branch is
// its own: each that isn't completed is canceled, and they are read until
// they complete, for stopWait. It is ended only when every run listed has
// completed and every launch of the check is answered for (answered); else
// not known, naming what is still running or what has no run yet, however
// long ago it was pushed. An attempt recorded before launches were noted,
// a run's URL and no note, has that run canceled and read the same way.
// A launch whose runs can't be named, one from before notes with no run
// or a note that can't be read, is not known, and said with the rest once
// the runs that can be named are canceled: it never keeps them running
// (the confirmation of #193's fixes, note 1).
func (p *Provider) Stop(ctx context.Context, request buildenv.StopRequest) ([]string, error) {
	release, err := request.Lock.Hold(ctx, stopWait)
	if err != nil {
		return nil, err
	}
	defer release()
	var notes []launchNote
	seen := map[string]bool{}
	var legacy []int64
	// unnamed are the launches whose runs can't be named, said as not
	// known once the rest are canceled.
	var unnamed []string
	for _, attempt := range request.Attempts {
		switch {
		case attempt.Launch == model.LaunchUnrecorded && attempt.ProviderRef == "":
			// Unfinished when dockhand began noting launches, its push
			// came before its run's URL was saved: a run may have started
			// that nothing names (Codex's review of #193 steps 5 and 6,
			// finding 3).
			unnamed = append(unnamed, fmt.Sprintf("attempt %d was started before dockhand noted its launches, and recorded no run: a push of it may have started one that nothing names", attempt.Attempt))
		case attempt.Launch != "" && attempt.Launch != model.LaunchUnrecorded:
			var note launchNote
			if err := json.Unmarshal([]byte(attempt.Launch), &note); err != nil {
				unnamed = append(unnamed, fmt.Sprintf("attempt %d's note of what it launched can't be read: %v", attempt.Attempt, err))
				continue
			}
			if key := cmp.Or(note.ID, attempt.Launch); !seen[key] {
				seen[key] = true
				notes = append(notes, note)
			}
		case attempt.ProviderRef != "":
			id, err := p.legacyRun(ctx, attempt.ProviderRef)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(legacy, id) {
				legacy = append(legacy, id)
			}
		}
	}
	if len(notes) == 0 && len(legacy) == 0 {
		// Nothing named was pushed or run again for the check, and holding
		// the lock, nothing can be.
		if len(unnamed) > 0 {
			return nil, fmt.Errorf("%s", prose.And(unnamed))
		}
		return nil, nil
	}
	fork, err := p.Fork(ctx)
	if err != nil {
		return nil, err
	}
	for _, note := range notes {
		if !strings.EqualFold(note.Repository, fork.Repository) {
			return nil, refusal.New(refusal.Unsupported, "%s pushed to %s, not your fork %s, so dockhand doesn't cancel its runs", note.Check, note.Repository, fork.Repository)
		}
	}
	type place struct{ branch, commit string }
	var places []place
	for _, note := range notes {
		if p := (place{note.Branch, note.Commit}); !slices.Contains(places, p) {
			places = append(places, p)
		}
	}
	list := func() ([]Run, error) {
		var runs []Run
		for _, at := range places {
			listed, err := p.API.Runs(ctx, fork.Repository, at.branch, at.commit)
			if err != nil {
				return nil, err
			}
			runs = append(runs, listed...)
		}
		for _, id := range legacy {
			if slices.ContainsFunc(runs, func(r Run) bool { return r.ID == id }) {
				continue
			}
			run, err := p.API.Run(ctx, fork.Repository, id)
			if err != nil {
				return nil, err
			}
			runs = append(runs, run)
		}
		return runs, nil
	}
	runs, err := list()
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.Status != "completed" {
			if err := remotewrite.Cancel(ctx, p.API, remotewrite.Made, fork.Repository, run.ID); err != nil {
				return nil, err
			}
		}
	}
	for waited := time.Duration(0); ; waited += stopPoll {
		if !slices.ContainsFunc(runs, func(r Run) bool { return r.Status != "completed" }) || waited >= stopWait {
			break
		}
		if err := p.sleep(ctx, stopPoll); err != nil {
			return nil, err
		}
		if runs, err = list(); err != nil {
			return nil, err
		}
		// A run listed since is canceled as it's seen.
		for _, run := range runs {
			if run.Status == "queued" || run.Status == "in_progress" || run.Status == "waiting" || run.Status == "pending" || run.Status == "requested" {
				if err := remotewrite.Cancel(ctx, p.API, remotewrite.Made, fork.Repository, run.ID); err != nil {
					return nil, err
				}
			}
		}
	}
	var words []string
	for _, run := range runs {
		if run.Status != "completed" {
			words = append(words, fmt.Sprintf("GitHub was asked to cancel %s, and it is still %s", run.URL, strings.ReplaceAll(cmp.Or(run.Status, "not completed"), "_", " ")))
		}
	}
	words = append(append(words, answered(notes, runs)...), unnamed...)
	if len(words) > 0 {
		return nil, fmt.Errorf("%s", prose.And(words))
	}
	return nil, nil
}

// legacyRun is the run an attempt recorded before launches were noted, by
// its URL; a reference of another form is an error, where it was taken as
// nothing to stop.
func (p *Provider) legacyRun(ctx context.Context, ref string) (int64, error) {
	match := runURL.FindStringSubmatch(ref)
	if match == nil {
		return 0, fmt.Errorf("%q isn't a workflow run's URL, and the attempt noted nothing of what it launched, so what it started can't be found", ref)
	}
	id, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q isn't a workflow run's URL: %w", ref, err)
	}
	fork, err := p.Fork(ctx)
	if err != nil {
		return 0, err
	}
	// Only in the fork: a recorded URL naming another repository is none
	// of dockhand's to cancel (#128).
	if !strings.EqualFold(match[1], fork.Repository) {
		return 0, refusal.New(refusal.Unsupported, "%s is a run in %s, not your fork %s, so dockhand doesn't cancel it", ref, match[1], fork.Repository)
	}
	return id, nil
}

// answered says each launch of the check that no run listed answers for
// (the lifecycle design's "a Launch is answered for"), empty where every
// one is. A launch noted as having started nothing needs no run. A push
// is answered for by a run newer than the newest it noted, a rerun by an
// attempt of the run past the one it noted, an attach by its run being
// listed. Each push and each rerun is counted once, by its own note: two
// pushes that noted the same newest run need two runs newer than it, and
// two reruns that noted the same attempt need the run two attempts on.
func answered(notes []launchNote, runs []Run) []string {
	listed := map[int64]Run{}
	var ids []int64
	for _, run := range runs {
		listed[run.ID] = run
		ids = append(ids, run.ID)
	}
	slices.Sort(ids)
	var words []string
	var pushes []launchNote
	reruns := map[int64][]int{}
	var rerunOrder []int64
	for _, note := range notes {
		if note.Nothing != "" {
			continue
		}
		switch note.Action {
		case launchPush:
			pushes = append(pushes, note)
		case launchRerun:
			if _, ok := reruns[note.Run]; !ok {
				rerunOrder = append(rerunOrder, note.Run)
			}
			reruns[note.Run] = append(reruns[note.Run], note.Attempt)
		case launchAttach:
			if _, ok := listed[note.Run]; !ok {
				words = append(words, fmt.Sprintf("GitHub lists no run %d, which an attempt took", note.Run))
			}
		}
	}
	// The most demanding push first, each taking the oldest run newer
	// than what it noted that no other push took.
	slices.SortStableFunc(pushes, func(a, b launchNote) int { return cmp.Compare(b.Before, a.Before) })
	taken := map[int64]bool{}
	missing := 0
	for _, push := range pushes {
		i := slices.IndexFunc(ids, func(id int64) bool { return id > push.Before && !taken[id] })
		if i < 0 {
			missing++
			continue
		}
		taken[ids[i]] = true
	}
	if missing > 0 {
		words = append(words, fmt.Sprintf("GitHub lists no run yet for %s of %s it accepted, and one can still start", prose.Plural(missing, "push"), pushes[0].Branch))
	}
	for _, id := range rerunOrder {
		attempts := reruns[id]
		slices.Sort(attempts)
		run, ok := listed[id]
		short := false
		for j, attempt := range attempts {
			// The reruns that noted this attempt or a later one each need
			// an attempt of their own past it.
			if !ok || run.Attempt-attempt < len(attempts)-j {
				short = true
				break
			}
		}
		if short {
			words = append(words, fmt.Sprintf("run %d hasn't shown the %s asked of it", id, prose.Plural(len(attempts), "attempt")))
		}
	}
	return words
}

var _ buildenv.Stopper = (*Provider)(nil)
