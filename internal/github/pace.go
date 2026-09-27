package github

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// publicAPIHost is GitHub's own REST API, whose secondary rate limit the
// pacer keeps under.
const publicAPIHost = "api.github.com"

// apiPace spaces requests to GitHub's REST API across the process. GitHub
// refuses a client that sends more than 900 requests a minute, a GET
// costing one point of that budget, however much of its hourly allowance
// is left (its documented secondary rate limit). One request every 80 ms
// is 750 a minute, so a command asking about many repositories at once,
// as outdated does, stays under it however many ports it looks up
// together. A command asking a few questions never waits.
var apiPace = &pacer{interval: 80 * time.Millisecond}

// pacer lets one request go per interval, in the order they ask.
type pacer struct {
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

// wait returns when the caller's turn comes, or when ctx ends first; a
// turn given up is not handed back.
func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	turn := p.next
	if turn.Before(now) {
		turn = now
	}
	p.next = turn.Add(p.interval)
	p.mu.Unlock()
	delay := time.Until(turn)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// paced waits its turn for a request to GitHub's public API; any other
// host, such as an Enterprise server or a test's, goes at once.
func paced(req *http.Request) error {
	if req.URL.Host != publicAPIHost {
		return nil
	}
	return apiPace.wait(req.Context())
}
