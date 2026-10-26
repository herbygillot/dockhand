package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gh "github.com/google/go-github/v93/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// RateLimitError says a refusal of go-github's own, from GitHub's rate
// limit, as forge.RateLimitError, with when it lifts. The transport
// refuses with one already (rateLimited); what reaches here is a refusal
// it couldn't read from GitHub's headers, as a secondary limit's 403 that
// names no wait.
// limited is a rate limit as a refusal of kind refusal.Limited, which
// another try once it resets may meet, the forge's error inside it.
func limited(limit *forge.RateLimitError) error {
	return refusal.Wrap(refusal.Limited, limit, "%s", limit)
}

func RateLimitError(err error) error {
	var known *forge.RateLimitError
	if errors.As(err, &known) {
		return err
	}
	now := time.Now()
	var primary *gh.RateLimitError
	if errors.As(err, &primary) {
		at := primary.Rate.Reset.Time
		return limited(&forge.RateLimitError{RetryAt: at, Err: fmt.Errorf("%s: %w", limitWords(primaryLimit, "", at, now), err)})
	}
	var secondary *gh.AbuseRateLimitError
	if errors.As(err, &secondary) {
		delay := secondaryWait
		if secondary.RetryAfter != nil {
			delay = *secondary.RetryAfter
		}
		at := now.Add(delay)
		return limited(&forge.RateLimitError{RetryAt: at, Err: fmt.Errorf("%s: %w", limitWords(secondaryLimit, "", at, now), err)})
	}
	// A login GitHub rejected, or none to ask as, is said as itself,
	// without the request's address: the transport's refusal reaches here
	// in http.Client's *url.Error, itself a net.Error, which D-C3 on rc5
	// read as GitHub unreached, offering another try none would meet.
	// A write said as rejected already is publicationError's, said so.
	if errors.Is(err, forge.ErrAuthentication) {
		if errors.Is(err, forge.ErrRejected) {
			return err
		}
		if request := new(url.Error); errors.As(err, &request) {
			return request.Err
		}
		return err
	}
	// GitHub not reached, the network's failure, is one another try may
	// meet, as fetch says of any address (refusal.Network).
	if unreached, ok := networkError(err); ok {
		return refusal.Wrap(refusal.Network, err, "couldn't reach GitHub (%s)", unreachedWords(unreached))
	}
	return err
}

// networkError is the network's failure an error carries: a DNS
// failure's, a dial's, or a timeout's, inside a request's *url.Error,
// which is a net.Error whatever it wraps.
func networkError(err error) (net.Error, bool) {
	if request := new(url.Error); errors.As(err, &request) {
		err = request.Err
	}
	var unreached net.Error
	if !errors.As(err, &unreached) {
		return nil, false
	}
	if _, wrapped := unreached.(*url.Error); wrapped {
		return nil, false
	}
	return unreached, true
}

// unreachedWords say why GitHub wasn't reached, as the network's error
// says it, without the request's address and its dial: "connection reset
// by peer", "no such host", where git's or Go's whole words led (Later's
// "offline said as offline").
func unreachedWords(err net.Error) string {
	var dns *net.DNSError
	var op *net.OpError
	switch {
	case errors.As(err, &dns):
		return dns.Err
	case errors.As(err, &op) && op.Err != nil:
		var inner *os.SyscallError
		if errors.As(op.Err, &inner) {
			return inner.Err.Error()
		}
		return op.Err.Error()
	case err.Timeout():
		return "timed out"
	}
	return err.Error()
}

// rateWait is the longest a read waits out GitHub's rate limit before
// asking again, once. GitHub asks a client its secondary limit refused,
// without saying for how long, to wait at least a minute, and a primary
// limit may be that close to its hourly reset; two minutes covers both
// with room. Past that a person at a command would rather hear when the
// limit lifts than watch it sit, and serve asks again on its next pass
// anyway (the limits sweep, 2026-10-01).
const rateWait = 2 * time.Minute

// secondaryWait is how long GitHub asks a client to wait when its
// secondary limit refused a request and said nothing of how long: "at
// least one minute"
// (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).
const secondaryWait = time.Minute

// resetMargin is added to a primary limit's reset, which GitHub gives in
// whole seconds, so a read waiting it out doesn't ask a moment early.
const resetMargin = time.Second

type limitKind int

const (
	primaryLimit limitKind = iota
	secondaryLimit
)

// rateLimits is what GitHub has said, in its documented headers, of one
// login's rate limits, or of requests without one: a primary limit used
// up until its reset, by the resource it counts (core, search, and so
// on), and a secondary limit's wait. It's kept per client, as the limits
// are per login, or per address without one.
type rateLimits struct {
	// who is whose limit it is, in the words a person reads: "for your
	// login", or "for requests without a login".
	who string
	// sleep waits, as limitSleep does.
	sleep func(context.Context, time.Duration) error

	mu        sync.Mutex
	primary   map[gh.RateLimitCategory]time.Time
	secondary time.Time
}

func newRateLimits(authenticated bool) *rateLimits {
	who := "for requests without a login"
	if authenticated {
		who = "for your login"
	}
	return &rateLimits{who: who, sleep: limitSleep, primary: map[gh.RateLimitCategory]time.Time{}}
}

// limitSleep is how a client waits for a limit; tests stand in for it.
var limitSleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// rateLimited is where dockhand meets GitHub's rate limits, below go-github,
// whose own check is off (newAPI), so that one place reads them. A read
// GitHub refused, or would refuse, as its headers say, waits for the limit
// to lift where that's within rateWait and the command's deadline, and is
// asked again, once. Anything else is refused at once, with when the
// limit lifts in words, without asking GitHub again while it's limited, as
// GitHub asks of a client: a write, which is never retried here, and a
// read the limit holds longer. Before, the reset was computed and nothing
// read it (the limits sweep, 2026-10-01).
// apiRequests counts the requests this process sent GitHub's API, each
// try of each, so a run can say what it spent of the hour's allowance
// (the M1's run at 10aac0c3: the test account's 5,000 went before B1,
// and nothing said which command spent them).
var apiRequests atomic.Int64

// apiUsed is the last X-RateLimit-Used GitHub answered with: how much of
// the login's hourly allowance is spent, by every process using it, which
// GitHub's rate_limit endpoint didn't say for a fine-grained token (the
// M1's run at 1da4fdbf). Zero before an answer.
var apiUsed atomic.Int64

// Used is the last X-RateLimit-Used GitHub answered this process with.
func Used() int64 { return apiUsed.Load() }

// Spend counts the requests a span of this process sends GitHub's API: a
// command's, for its -v line and its envelope (the M1's run at 10aac0c3).
type Spend struct{ start, answered int64 }

// StartSpend starts counting.
func StartSpend() Spend { return Spend{start: apiRequests.Load(), answered: conditionalHits.Load()} }

// Requests is how many requests sent since StartSpend counted against the
// hour's allowance: those GitHub answered 304, as nothing moved since an
// answer kept, count nothing (ConditionalCache).
func (s Spend) Requests() int64 {
	return apiRequests.Load() - s.start - (conditionalHits.Load() - s.answered)
}

// Log appends a command's line to the spend log at path,
// $DOCKHAND_GITHUB_LOG, where there's one, --json or not: how many
// requests it sent GitHub's API, how much of the hour's allowance GitHub
// last said was spent, and what it was asked, tab-separated, so a run can
// say which command spent it. One that can't be written is skipped, as a
// log is.
func (s Spend) Log(path string, args []string) {
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%d\t%d\t%s\n", s.Requests(), Used(), strings.Join(args, " "))
}

type rateLimited struct {
	next   http.RoundTripper
	limits *rateLimits
}

func (t rateLimited) RoundTrip(req *http.Request) (*http.Response, error) {
	category := gh.GetRateLimitCategory(req.Method, req.URL.Path)
	waited := false
	if kind, until, limited := t.limits.until(category); limited {
		if !t.limits.mayWait(req, until) {
			return nil, t.limits.refusal(kind, until)
		}
		if err := t.limits.wait(req.Context(), kind, until); err != nil {
			return nil, err
		}
		waited = true
	}
	for {
		// GitHub's rate_limit endpoint costs nothing of the allowance, as
		// its documentation says, and isn't counted: outdated's look asks
		// it each time, and D-N6's second look, every other read answered
		// on a 304, counted that one (rc2's quick stage).
		if !freeOfTheAllowance(req) {
			apiRequests.Add(1)
		}
		response, err := t.next.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if used, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Used"), 10, 64); err == nil {
			apiUsed.Store(used)
		}
		kind, until, limited := t.limits.observe(category, response)
		if !limited {
			return response, nil
		}
		// The refusal's body, GitHub's message, is set aside: the error
		// says when the limit lifts.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if waited || !t.limits.mayWait(req, until) {
			return nil, t.limits.refusal(kind, until)
		}
		if err := t.limits.wait(req.Context(), kind, until); err != nil {
			return nil, err
		}
		waited = true
	}
}

// until is when the limits a request to the resource counts against lift,
// and which of them lifts last; false when none holds.
func (l *rateLimits) until(category gh.RateLimitCategory) (limitKind, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	kind, until := primaryLimit, l.primary[category]
	if l.secondary.After(until) {
		kind, until = secondaryLimit, l.secondary
	}
	return kind, until, until.After(now)
}

// observe keeps what a response's headers say of the limits, and says
// whether GitHub refused it for one, and until when. A primary limit is
// used up when x-ratelimit-remaining is 0, until x-ratelimit-reset, in
// epoch seconds, whether or not this request was refused; a refusal, a 403
// or a 429, names its wait in retry-after, in seconds, where it's a
// secondary limit's. A 429 naming neither is a secondary limit's, and
// waits secondaryWait. A 403 naming neither may be a refusal of another
// kind, which go-github reads from its body.
func (l *rateLimits) observe(category gh.RateLimitCategory, response *http.Response) (limitKind, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	remaining := response.Header.Get(gh.HeaderRateRemaining)
	reset, resetErr := strconv.ParseInt(response.Header.Get(gh.HeaderRateReset), 10, 64)
	switch {
	case remaining == "0" && resetErr == nil:
		l.primary[category] = time.Unix(reset, 0)
	case remaining != "":
		delete(l.primary, category)
	}
	refused := response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests
	retryAfter, retryErr := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64)
	switch {
	case !refused:
		return 0, time.Time{}, false
	case retryErr == nil && retryAfter >= 0:
		l.secondary = now.Add(time.Duration(retryAfter) * time.Second)
	case remaining == "0" && resetErr != nil, remaining != "0" && response.StatusCode == http.StatusTooManyRequests:
		l.secondary = now.Add(secondaryWait)
	case remaining != "0":
		return 0, time.Time{}, false
	}
	kind, until := primaryLimit, l.primary[category]
	if !l.secondary.Before(until) {
		kind, until = secondaryLimit, l.secondary
	}
	return kind, until, true
}

// mayWait says whether a request may wait for a limit lifting at until: a
// read, which asking again can't repeat an effect of, whose wait is within
// rateWait, and ends before the request's own deadline.
func (l *rateLimits) mayWait(req *http.Request, until time.Time) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	if time.Until(until) > rateWait {
		return false
	}
	deadline, ok := req.Context().Deadline()
	return !ok || until.Before(deadline)
}

// wait waits for a limit to lift, saying so once for those waiting on the
// same lifting, or until ctx ends.
func (l *rateLimits) wait(ctx context.Context, kind limitKind, until time.Time) error {
	progress.ReportOnce(ctx, "waiting for %s", l.waitWords(kind, until))
	if kind == primaryLimit {
		until = until.Add(resetMargin)
	}
	return l.sleep(ctx, time.Until(until))
}

func (l *rateLimits) waitWords(kind limitKind, until time.Time) string {
	at := until.Local().Format("15:04:05")
	if kind == secondaryLimit {
		return "GitHub's secondary rate limit, on requests made close together, to lift at " + at
	}
	return "GitHub's rate limit " + l.who + " to reset at " + at
}

// refusal is a request refused for a limit, saying when it lifts.
func (l *rateLimits) refusal(kind limitKind, until time.Time) error {
	words := limitWords(kind, l.who, until, time.Now())
	if kind == primaryLimit && l.who == "for requests without a login" {
		words += "; dockhand setup github raises it"
	}
	return limited(&forge.RateLimitError{RetryAt: until, Err: errors.New(words)})
}

// limitWords says when a limit lifts, as "GitHub's rate limit for your
// login resets in 23 minutes, at 14:05 EDT"; who may be empty where it
// isn't known.
func limitWords(kind limitKind, who string, until, now time.Time) string {
	// The wait first, then the time with its zone, which a log read later,
	// or a Mac in another zone, can't take for granted (the rc6 full stage).
	when := "in " + duration(until.Sub(now)) + ", at " + until.Local().Format("15:04 MST")
	if kind == secondaryLimit {
		return "GitHub's secondary rate limit, on requests made close together, lifts " + when
	}
	if who != "" {
		who += " "
	}
	return "GitHub's rate limit " + who + "resets " + when
}

// duration is a wait in words, rounded up: seconds under a minute,
// minutes under two hours, and hours past that.
func duration(d time.Duration) string {
	count := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.FormatInt(n, 10) + " " + unit + "s"
	}
	switch {
	case d <= 0:
		return "a moment"
	case d < time.Minute:
		return count(int64((d+time.Second-1)/time.Second), "second")
	case d < 2*time.Hour:
		return count(int64((d+time.Minute-1)/time.Minute), "minute")
	}
	return count(int64((d+time.Hour-1)/time.Hour), "hour")
}

// freeOfTheAllowance says whether a request costs nothing of the hour's
// allowance: GitHub's rate_limit endpoint, which says what's left of it.
func freeOfTheAllowance(req *http.Request) bool {
	return req.Method == http.MethodGet && strings.HasSuffix(strings.TrimSuffix(req.URL.Path, "/"), "/rate_limit")
}
