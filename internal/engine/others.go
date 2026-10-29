package engine

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
)

// OtherPullRequest is another open pull request for a port: its number,
// title, and page.
type OtherPullRequest = forge.PullRequestSummary

// othersKept is how long a port's open pull requests, as found, stand
// within a run: start's or update's look serves the submit that follows,
// and serve, whose run lasts days, looks again after it.
const othersKept = 10 * time.Minute

// othersWait bounds one port's search, which runs on its own, beside the
// work that asked for it.
const othersWait = 30 * time.Second

// othersCache is one run's searches for ports' open pull requests, by
// port: each port searched once, one search at a time, since GitHub's
// search takes 30 a minute and no OR across titles (the bazel session's
// notes, 2026-10-24; the person's "Add this to 0.7.0 as well",
// 2026-10-29). A failed search isn't kept, so the next look tries again.
type othersCache struct {
	mu     sync.Mutex
	looked map[string]*othersLook
	slot   chan struct{}
}

// othersLook is one port's search, done once it's answered.
type othersLook struct {
	done  chan struct{}
	found []forge.PullRequestSummary
	err   error
	at    time.Time
}

// lookForOthers starts searching for a port's open pull requests, where
// the run has no answer standing, or fresh, returning at once; its look.
func (e *Engine) lookForOthers(ctx context.Context, port string, fresh bool) *othersLook {
	c := &e.others
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.looked == nil {
		c.looked = map[string]*othersLook{}
		c.slot = make(chan struct{}, 1)
	}
	if look, ok := c.looked[port]; ok && !fresh {
		select {
		case <-look.done:
			if look.err == nil && e.now().Sub(look.at) < othersKept {
				return look
			}
		default:
			return look
		}
	}
	look := &othersLook{done: make(chan struct{})}
	c.looked[port] = look
	repository, from := e.PullRequestRepository(), e.forge()
	ctx = context.WithoutCancel(ctx)
	go func() {
		c.slot <- struct{}{}
		searching, cancel := context.WithTimeout(ctx, othersWait)
		found, err := from.OpenPullRequests(searching, repository, port)
		cancel()
		<-c.slot
		// GitHub's search matches a word anywhere in a title; one for the
		// port names it as a MacPorts subject does, after a tag such as
		// [WIP].
		found = slices.DeleteFunc(found, func(pr forge.PullRequestSummary) bool {
			return !slices.Contains(commitmsg.TitlePorts(pr.Title), port)
		})
		c.mu.Lock()
		look.found, look.err, look.at = found, err, e.now()
		if err != nil && c.looked[port] == look {
			delete(c.looked, port)
		}
		c.mu.Unlock()
		close(look.done)
	}()
	return look
}

// LookForOthers starts looking for the ports' other open pull requests,
// beside the work a person asked for, returning at once: start's and an
// attended update's, which OtherPullRequests then reads.
func (e *Engine) LookForOthers(ctx context.Context, ports []string) {
	for _, port := range ports {
		e.lookForOthers(ctx, port, false)
	}
}

// OtherPullRequests are the ports' open pull requests but except, each
// once, as a person starting work on them hears of them: never a hold,
// and nothing where the search failed, since a person can look.
func (e *Engine) OtherPullRequests(ctx context.Context, ports []string, except int) []forge.PullRequestSummary {
	others, _ := e.pullRequestsFor(ctx, ports, except, false)
	return others
}

// openPullRequests are the open pull requests for any of the ports, but
// except, each once, from the run's searches; or why they couldn't be
// looked for, which holds an unattended run.
func (e *Engine) openPullRequests(ctx context.Context, ports []string, except int) ([]forge.PullRequestSummary, string) {
	return e.pullRequestsFor(ctx, ports, except, false)
}

// pullRequestsNow are openPullRequests searched afresh, the run's answers
// replaced: submit's, since one may have opened while the branch was
// checked, which a bump or serve submitting would open a second beside.
func (e *Engine) pullRequestsNow(ctx context.Context, ports []string, except int) ([]forge.PullRequestSummary, string) {
	return e.pullRequestsFor(ctx, ports, except, true)
}

func (e *Engine) pullRequestsFor(ctx context.Context, ports []string, except int, fresh bool) ([]forge.PullRequestSummary, string) {
	var looks []*othersLook
	for _, port := range ports {
		looks = append(looks, e.lookForOthers(ctx, port, fresh))
	}
	var others []forge.PullRequestSummary
	for _, look := range looks {
		select {
		case <-look.done:
		case <-ctx.Done():
			return nil, ctx.Err().Error()
		}
		if look.err != nil {
			return nil, look.err.Error()
		}
		for _, pr := range look.found {
			if pr.Number != except && !slices.ContainsFunc(others, func(o forge.PullRequestSummary) bool { return o.Number == pr.Number }) {
				others = append(others, pr)
			}
		}
	}
	return others, ""
}
