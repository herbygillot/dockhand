// Package courtesy is what dockhand owes a host it is about to ask a
// thousand questions.
//
// One port's update check is a couple of round trips and nobody
// notices. A selector's is thousands, aimed overwhelmingly at one
// host: measured on the maintainer's tree, 4561 of the 4764 ports that
// name a git forge name github.com. Issued as fast as an evaluator
// pool can produce them, that is not a fast tool, it is a tool that
// gets an IP address rate-limited and then blamed for the outage. So
// the pacing is not an optimization and not a nicety — it is part of
// being correct, and a design that would issue thousands of unpaced
// requests is a defect even when every test passes.
//
// Two mechanisms, and they answer different questions.
//
// The Pacer answers "may I ask this host something right now": a
// minimum interval between requests to one host, a ceiling on how many
// may be in flight to it at once, jitter so a sweep does not arrive in
// lockstep, and a wall that stops everything asking a host that has
// just refused us. Budgets are per host, because a limit is: github.com
// serving git and api.github.com serving REST are two services with two
// limits, and a self-hosted Gitea somebody runs on a Raspberry Pi is a
// third.
//
// The Cache answers "do I need to ask at all": an observation with a
// TTL and a validator, stored under the user's cache directory. It
// caches OBSERVATIONS and is addressed by TIME, which is the opposite
// of the decline memo — that one caches JUDGMENTS and is addressed by
// CONTENT. The distinction is stated in full on Cache below, because
// the two look like one cache-shaped problem and merging them would
// break both.
//
// Nothing here knows what a port is, what a forge is, or what a
// version means. That is what lets the whole politeness layer be
// tested with a fake clock and a fake transport, on a machine with no
// network, which is the only way to test it at all: proving a pacer
// paces by watching real requests would mean making the requests the
// pacer exists to prevent.
package courtesy

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// Clock is the passage of time, injected so that the pacer's promises
// can be tested without spending them. A sweep's spacing is measured in
// hundreds of milliseconds and a cache's TTL in hours; a test that
// waited for either would be a test nobody runs.
type Clock interface {
	Now() time.Time
	// Sleep blocks for d or until ctx is done, returning the context's
	// error in the second case. A non-positive d does not block.
	Sleep(ctx context.Context, d time.Duration) error
}

// RealClock is time itself.
type RealClock struct{}

// Now is time.Now.
func (RealClock) Now() time.Time { return time.Now() }

// Sleep waits out d, or until the context ends.
func (RealClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Host is the budget key for a URL: its host, lowercased, port
// included.
//
// The port is kept because a host serving two things on two ports is
// two services, and the scheme is dropped because http and https to
// one host are one service. A string that will not parse as a URL is
// its own key rather than an error — a budget for something
// unrecognized is still a budget, and refusing to pace what we could
// not parse would be the wrong way round.
func Host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(u.Host)
}

// UserAgent names dockhand and its version to every host it asks.
//
// An honest User-Agent is the cheapest half of politeness and the only
// one a host can act on: an operator looking at a log that says
// dockhand can find out what it is and mail somebody, where an
// operator looking at a log full of git/2.55 has no way to tell a
// maintainer's sweep from an attack. It is also the string that gets
// dockhand allow-listed rather than blocked, which is the selfish
// reason to send a real one.
//
// version is the running binary's, as the composition root knows it;
// an empty one says "dev" rather than pretending to a release.
func UserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return "dockhand/" + version + " (+https://github.com/herbygillot/dockhand)"
}
