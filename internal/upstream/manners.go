package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream/courtesy"
)

// Manners is the politeness one witness is consulted under: how often a
// host may be asked, what was already learned from it, and who dockhand
// says it is.
//
// It is one value rather than three parameters because the three are
// one decision, and because both roads into upstream have to make it
// the same way. A report over a selector and a bump over a selector ask
// the same forge the same questions in the same numbers; if only one of
// them were paced, the other would be the one that gets the IP address
// blocked, and the politeness of the first would be a comforting
// fiction.
//
// The zero value is the single-port road, deliberately: no pacer, no
// cache, no agent — one port asking one question of one host, which is
// what dockhand has always done and needs none of this. Nothing about a
// single target changes because this type exists. What changes is that
// a thousand targets can be given a Manners with all three fields set,
// and every witness underneath obeys it without any call site
// remembering to.
type Manners struct {
	// Pacer bounds how hard any one host is asked, and holds the walls.
	// Nil is unpaced.
	Pacer *courtesy.Pacer
	// Cache holds observations between runs. Nil asks every time.
	Cache *courtesy.Cache
	// Agent identifies dockhand to the hosts it asks. Empty sends
	// whichever tool's own default.
	Agent string
}

// ask runs one request under the host's budget and tells the pacer
// what it amounted to.
//
// f says what happened; ask does not work it out. That is the whole of
// the arrangement, and it is the fix to a real defect: this function
// used to read the classification off the error it was handed, which
// meant a substring match over prose, which meant `gh api`'s non-zero
// exit on a 304 — the cheapest and most successful answer a conditional
// request can get — was struck against the host that gave it, three in
// a row from a warm cache walling the forge for a quarter of an hour.
// Only the seam that ran the child can see the status line, so only
// the seam that ran the child may classify. See courtesy.Outcome.
//
// The outcome starts as Ours because f may never run. The pacer
// refuses a walled host, and abandons a request whose context ended
// while it queued; in both cases nothing was asked of anybody, so the
// streak must be left exactly as it was found — which is what
// courtesy.Ours means, and why it rather than the zero value is where
// this starts.
func (m Manners) ask(ctx context.Context, host string, f func(context.Context) (courtesy.Outcome, error)) error {
	if m.Pacer == nil {
		_, err := f(ctx)
		return err
	}
	out := courtesy.Ours
	err := m.Pacer.Ask(ctx, host, func(ctx context.Context) error {
		var e error
		out, e = f(ctx)
		return e
	})
	m.Pacer.Note(host, out, err)
	return err
}

// spokenOutcome classifies a witness that has no status code to show.
//
// git's ls-remote and a port's livecheck phase both fail in somebody
// else's words — a forge's, a web server's, a resolver's — relayed
// through a tool that formats them however it likes, and there is no
// second channel to read instead. So this is a substring match, and it
// stays one. What matters is that it is now confined to the witnesses
// that genuinely have nothing better: the witness that does have a
// status line reads the status line (readGhResponse), and no path in
// this package recovers an HTTP fact from prose any more.
//
// Getting it wrong in the cautious direction — walling a host over an
// error that was not a refusal — costs a sweep some ports it will pick
// up on the next run. Getting it wrong the other way is the
// abuse-detection trip the whole design is about.
//
// The strike is the answer to the failure that has no words at all. A
// forge that is simply unreachable — DNS, a captive network, an outage —
// matches no refusal phrase, so nothing would wall it; and for the
// staged observer an unanswered cheap witness promotes every port
// behind it to a full-cost candidate, so an outage silently converts a
// sweep into one livecheck per port against several thousand unrelated
// web sites. A run of three failures is a host to leave alone whatever
// it said.
//
// An interrupted request is neither a strike nor a success. A context
// that ended is this run's clock, and holding a host responsible for it
// would let a Ctrl-C wall the tree.
func spokenOutcome(err error) courtesy.Outcome {
	switch {
	case err == nil:
		return courtesy.Answered
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return courtesy.Ours
	case refused(err):
		return courtesy.Refused
	}
	return courtesy.Unanswered
}

// refs is the ls-remote witness, paced and cached.
//
// The cache key is the repository and nothing else — deliberately not
// the port, and deliberately not the port's tag scheme. Two ports built
// from one repository are one observation of one forge, and the scheme
// is applied to the cached answer rather than baked into it, so the
// second port costs nothing. Two ports asking at the same moment cost
// one round trip too: the cache collapses concurrent misses on one key.
func (m Manners) refs(ctx context.Context, tools *tool.Finder, repo Repo) ([]Ref, string, courtesy.Source, error) {
	key := WitnessLsRemote + "\x00" + repo.URL
	host := courtesy.Host(repo.URL)
	answer, src, err := m.Cache.Do(ctx, key, func(ctx context.Context, validator string) (courtesy.Answer, error) {
		var raw []RawRef
		if err := m.ask(ctx, host, func(ctx context.Context) (courtesy.Outcome, error) {
			var e error
			raw, e = LsRemote(ctx, tools, m.Agent, repo.URL)
			return spokenOutcome(e), e
		}); err != nil {
			return courtesy.Answer{}, err
		}
		d := Digest(raw)
		if d == validator {
			// The forge's whole answer is byte-identical to the one
			// already stored. There is no conditional git request, so
			// this round trip was paid in full; what it buys is the
			// releases observation keyed on this digest, which stays
			// valid.
			return courtesy.Answer{NotModified: true, Validator: d}, nil
		}
		body, err := json.Marshal(raw)
		if err != nil {
			return courtesy.Answer{}, fmt.Errorf("upstream: encoding %s tags: %w", repo.URL, err)
		}
		return courtesy.Answer{Validator: d, Body: body}, nil
	})
	if err != nil {
		return nil, "", src, err
	}
	var raw []RawRef
	if err := json.Unmarshal(answer.Body, &raw); err != nil {
		return nil, "", src, fmt.Errorf("upstream: decoding cached %s tags: %w", repo.URL, err)
	}
	return Scheme(raw, repo), answer.Validator, src, nil
}

// releases is the authoritative witness: upstream's own word on which
// of its tags are releases and which of those are stable.
//
// Three answers, told apart, because two of them used to be one and the
// confusion was dangerous. Versions with no error is a feed that spoke.
// No versions and no error is a repository that publishes none, which
// is common and legitimate, and the tags stand. An error is the call
// itself failing — no gh, a wall, a rate limit — and the caller is owed
// that separately: a sweep that could not reach the API silently loses
// its authoritative witness on every remaining port and judges them on
// the tag heuristic instead, which is precisely the wrong-answer class
// the releases feed was added to close.
//
// The digest belongs in the key because a forge that has moved has
// probably cut a release, and waiting out a six-hour TTL to notice
// would make the authoritative witness the stale one. It is an extra
// invalidator and never a substitute for the TTL: a release published
// against a tag that already existed moves no sha, and only the clock
// catches that.
//
// The response is read whether or not gh exited zero, which is what
// makes the conditional request worth making at all: a 304 is a
// non-2xx, gh reports every non-2xx as a failure, and the answer is on
// stdout regardless. readGhResponse turns those bytes into the
// observation and the outcome together, so the cache is told "not
// modified" and the pacer is told the host answered — the two facts
// that used to be reconstructed, wrongly and separately, from the same
// sentence.
func (m Manners) releases(ctx context.Context, gh GhRunner, repo Repo, digest string) ([]string, courtesy.Source, error) {
	if gh == nil {
		return nil, courtesy.Fresh, nil
	}
	owner, name, ok := githubPath(repo.URL)
	if !ok {
		return nil, courtesy.Fresh, nil
	}
	key := WitnessReleases + "\x00" + repo.URL + "\x00" + digest
	const host = "api.github.com"
	answer, src, err := m.Cache.Do(ctx, key, func(ctx context.Context, validator string) (courtesy.Answer, error) {
		var ans courtesy.Answer
		if err := m.ask(ctx, host, func(ctx context.Context) (courtesy.Outcome, error) {
			out, callErr := gh(ctx, releasesArgs(owner, name, validator, m.Agent)...)
			var outcome courtesy.Outcome
			ans, outcome, callErr = readGhResponse(out, validator, callErr)
			return outcome, callErr
		}); err != nil {
			return courtesy.Answer{}, err
		}
		return ans, nil
	})
	if err != nil {
		return nil, src, err
	}
	versions, _ := releaseVersions(answer.Body, repo)
	return versions, src, nil
}

// readGhResponse turns one `gh api --include` call into the three
// things that must come out of it: the observation, what the request
// amounted to for the host's budget, and whether it failed.
//
// One function, because one fact decides all three. The status line is
// on stdout whether gh exited zero or not — gh prints the response
// head before it decides the status is an error — so a 304 is a status
// check, a 403 is a status check, and neither is a sentence to be
// pattern-matched. That is the point of D5 and D23 together: the whole
// defect was that the status line existed and was thrown away, leaving
// "http 304" in an error message as the only surviving trace of the
// most successful answer a revalidation can get.
//
// A 304 is honoured only against a validator we actually sent. Without
// one there is nothing in the cache for the stored body to be, and
// courtesy would rightly refuse it as a transport revalidating against
// nothing; a forge that answered 304 to an unconditional request has
// malfunctioned, and it is banded as an ordinary failure.
//
// A response with no status line at all falls back to the words, which
// is the defensive reading and the one that survives a gh that stops
// honouring --include: the observation stays correct, it simply stops
// being conditional and costs a body every time. Only refusal survives
// that fallback, deliberately — a 304 read out of prose is the defect
// this function exists to close, and it is better to lose one
// revalidation to a full fetch than to reintroduce it.
func readGhResponse(out, validator string, err error) (courtesy.Answer, courtesy.Outcome, error) {
	head, body, headed := splitHead(out)
	status := statusCode(head)
	switch {
	case headed && status == httpNotModified && validator != "":
		// The conditional request's payoff, and it arrives with a
		// non-nil err because gh calls every non-2xx a failure. The
		// stored body stands, its clock is refreshed, and the host is
		// credited with an answer rather than charged for one.
		return courtesy.Answer{NotModified: true, Validator: validator}, courtesy.NotModified, nil
	case err == nil && !headed:
		// No status line: a gh that stopped honouring --include, or an
		// endpoint that answered with a bare document. The whole of
		// stdout is the body, so the observation is still right; it is
		// simply no longer conditional, and costs a body every time.
		return courtesy.Answer{Body: json.RawMessage(out)}, courtesy.Answered, nil
	case err == nil:
		return courtesy.Answer{Validator: etagOf(head), Body: json.RawMessage(body)}, courtesy.Answered, nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return courtesy.Answer{}, courtesy.Ours, err
	case headed && refusalStatus(status):
		return courtesy.Answer{}, courtesy.Refused, err
	case headed:
		return courtesy.Answer{}, courtesy.Unanswered, err
	}
	return courtesy.Answer{}, spokenOutcome(err), err
}

// The statuses this witness reads by number: one that means the
// observation stands, and three that mean stop asking. Every other
// status is a failure of the ordinary kind, which is the right answer
// for a status with no special meaning to a conditional GET — a 404 or
// a 500 is worth a strike and is not worth walling a forge over.
const (
	// httpNotModified is the conditional request's payoff: the feed is
	// unchanged, no body was sent, and the stored observation stands.
	httpNotModified = 304
	// httpForbidden is how GitHub says both "rate limit" and "secondary
	// rate limit", which is the refusal the pacer exists for.
	httpForbidden = 403
	// httpTooManyRequests is the primary-limit spelling.
	httpTooManyRequests = 429
	// httpUnavailable is a forge that has stopped serving; asking
	// harder is the wrong response to it whether or not it is about us.
	httpUnavailable = 503
)

// refusalStatus reports a status that means "stop asking".
func refusalStatus(code int) bool {
	switch code {
	case httpForbidden, httpTooManyRequests, httpUnavailable:
		return true
	}
	return false
}

// statusCode reads the numeric status out of an HTTP head's first
// line — "HTTP/2.0 304 Not Modified" — and returns 0 when there is
// none to read, which is what makes "did the child show us a status at
// all" a question with an answer rather than a guess.
func statusCode(head string) int {
	line, _, _ := strings.Cut(head, "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return code
}

// livecheckHost is the pacer budget every livecheck phase shares. It
// is spelled with a leading NUL so that it cannot collide with a real
// host name, which is the only way a synthetic budget key stays
// synthetic.
const livecheckHost = "\x00livecheck"

// livecheck runs the port's own update-checking phase — the most
// expensive witness there is: a whole MacPorts target, with whatever
// fetch the maintainer declared inside it.
//
// It is cached by portdir, subport and the version it was checking
// against, which is the observation's own identity: a livecheck's
// answer is about a port at a version, and a Portfile that moved to a
// new version has a different question to ask.
//
// It is paced under a synthetic host, and what that does and does not
// promise is worth being exact about. The fetch livecheck makes
// happens inside MacPorts, over MacPorts' own curl, to whatever host
// the port declared — dockhand cannot see it and cannot pace it per
// host. What the pacer bounds here is how often dockhand STARTS one,
// which bounds the rate at which a sweep pokes several hundred
// unrelated web sites. It is the only lever there is on this witness,
// and it is a real one.
func (m Manners) livecheck(ctx context.Context, lc Livechecker, portdir, subport, version string) (portfetch.LivecheckResult, courtesy.Source, error) {
	key := strings.Join([]string{WitnessLivecheck, portdir, subport, version}, "\x00")
	answer, src, err := m.Cache.Do(ctx, key, func(ctx context.Context, _ string) (courtesy.Answer, error) {
		var res portfetch.LivecheckResult
		if err := m.ask(ctx, livecheckHost, func(ctx context.Context) (courtesy.Outcome, error) {
			var e error
			res, e = lc.Livecheck(ctx, portdir, subport)
			return spokenOutcome(e), e
		}); err != nil {
			return courtesy.Answer{}, err
		}
		body, err := json.Marshal(res)
		if err != nil {
			return courtesy.Answer{}, fmt.Errorf("upstream: encoding livecheck of %s: %w", portdir, err)
		}
		return courtesy.Answer{Body: body}, nil
	})
	if err != nil {
		return portfetch.LivecheckResult{}, src, err
	}
	var res portfetch.LivecheckResult
	if err := json.Unmarshal(answer.Body, &res); err != nil {
		return portfetch.LivecheckResult{}, src, fmt.Errorf("upstream: decoding cached livecheck of %s: %w", portdir, err)
	}
	return res, src, nil
}

// releasesArgs is the conditional form of the releases call: the same
// endpoint the single-port path asks for, with the response headers
// kept so that the ETag can be read back, and the ETag sent so that an
// unchanged feed costs a 304 and no body.
//
// The User-Agent is a header rather than an environment variable
// because gh sends its own and a header is the one thing that
// overrides it.
func releasesArgs(owner, name, etag, agent string) []string {
	args := []string{"api", fmt.Sprintf("repos/%s/%s/releases?per_page=100", owner, name), "--include"}
	if etag != "" {
		args = append(args, "-H", "If-None-Match: "+etag)
	}
	if agent != "" {
		args = append(args, "-H", "User-Agent: "+agent)
	}
	return args
}

// etagOf reads the validator to send back next time out of a response
// head; empty when the endpoint issued none, which simply makes the
// next request unconditional.
//
// The header name is matched case-insensitively because HTTP/2 sends
// it lowercased and HTTP/1.1 does not, and gh prints whichever it was
// given.
func etagOf(head string) string {
	etag := ""
	lines := strings.Split(head, "\n")
	for _, l := range lines[1:] {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "etag") {
			etag = strings.TrimSpace(v)
		}
	}
	return etag
}

// splitHead separates an HTTP head from its body at the blank line,
// tolerating both line endings. false when there is no status line, so
// that a bare JSON body is recognized as one.
func splitHead(out string) (head, body string, ok bool) {
	if !strings.HasPrefix(out, "HTTP/") {
		return "", "", false
	}
	if h, b, found := strings.Cut(out, "\r\n\r\n"); found {
		return strings.ReplaceAll(h, "\r", ""), b, true
	}
	if h, b, found := strings.Cut(out, "\n\n"); found {
		return h, b, true
	}
	return strings.ReplaceAll(out, "\r", ""), "", true
}

// refusedPhrases are what a host says when it wants to be left alone.
// Matched against the whole error text, lowercased, because the words
// arrive through a tool's error formatting and there is no status code
// to test instead.
//
// "No status code" is now a claim about particular witnesses rather
// than about all of them. ls-remote and livecheck have none: git and a
// port's fetch phase relay a forge's or a web server's words and
// nothing else. The releases witness DOES have one — it asks with
// --include and reads the status line off stdout — and it uses it;
// these phrases are its fallback for a response that arrived with no
// head at all, and nothing here is consulted about a 304 by any
// witness, which is the point of the whole exercise.
var refusedPhrases = []string{
	"rate limit", "rate-limit", "ratelimit",
	"429", "too many requests",
	"abuse", "secondary rate",
	"403", "forbidden",
	"retry-after", "retry after",
	"temporarily unavailable", "service unavailable", "503",
}

func refused(err error) bool {
	msg := strings.ToLower(spoken(err))
	for _, p := range refusedPhrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// spoken is the host's own words, with dockhand's framing of them
// dropped.
//
// The framing is what makes this necessary: ls-remote's message puts
// the repository URL in front of git's words, and the refusal test is
// a substring match, so a repository at .../rate-limiter would wall
// github.com — and one wall stands for every port behind that host.
// A witness that recorded what it was told answers with that; anything
// else has nothing but its own message, and it is matched whole.
func spoken(err error) string {
	var w *WitnessError
	if errors.As(err, &w) && w.Said != "" {
		return w.Said
	}
	return err.Error()
}
