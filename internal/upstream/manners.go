package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// ask runs one request under the host's budget, and raises the wall
// when the host says it has had enough — or, failing words, when it has
// simply stopped answering.
//
// Reading the refusal is the caller's because only the caller can:
// there is no status code to test, just a forge's own words on the
// error of whichever tool asked it. Getting it wrong in the cautious
// direction — walling a host over an error that was not a refusal —
// costs a sweep some ports it will pick up on the next run. Getting it
// wrong the other way is the abuse-detection trip the whole design is
// about.
//
// The strike is the answer to the failure that has no words. A forge
// that is simply unreachable — DNS, a captive network, an outage —
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
func (m Manners) ask(ctx context.Context, host string, f func(context.Context) error) error {
	if m.Pacer == nil {
		return f(ctx)
	}
	err := m.Pacer.Ask(ctx, host, f)
	switch {
	case err == nil:
		m.Pacer.Cleared(host)
	case errors.Is(err, courtesy.ErrWalled):
		// The wall is already up; this request never happened.
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Ours, not the host's.
	case refused(err):
		m.Pacer.Wall(host, err)
	default:
		m.Pacer.Struck(host, err)
	}
	return err
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
		if err := m.ask(ctx, host, func(ctx context.Context) error {
			var e error
			raw, e = LsRemote(ctx, tools, m.Agent, repo.URL)
			return e
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
		var out string
		if err := m.ask(ctx, host, func(ctx context.Context) error {
			var e error
			out, e = gh(ctx, releasesArgs(owner, name, validator, m.Agent)...)
			return e
		}); err != nil {
			if validator != "" && notModified(err) {
				// gh returns an HTTP error for any status outside 2xx and
				// 304 is one, so the whole point of asking conditionally
				// arrives as a failure. The stored body stands; see
				// notModified.
				return courtesy.Answer{NotModified: true, Validator: validator}, nil
			}
			return courtesy.Answer{}, err
		}
		return parseGhResponse(out, validator)
	})
	if err != nil {
		return nil, src, err
	}
	versions, _ := releaseVersions(answer.Body, repo)
	return versions, src, nil
}

// notModified reads a 304 out of gh's own failure.
//
// It exists because of a seam. gh's api command answers any status
// outside 2xx with an error, 304 included, and tool.Output discards a
// failed command's stdout — so the conditional request's whole payoff
// arrives as an error with the status in its text and nothing else. The
// alternative reading, "an error means ask again with no validator",
// would make every unchanged releases feed lose its authoritative
// witness the moment the TTL expired, which is the opposite of what
// asking conditionally is for.
func notModified(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 304") || strings.Contains(msg, "not modified")
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
		if err := m.ask(ctx, livecheckHost, func(ctx context.Context) error {
			var e error
			res, e = lc.Livecheck(ctx, portdir, subport)
			return e
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

// parseGhResponse splits an --include response into its validator and
// its body.
//
// A 304 is the whole point of asking conditionally, and it is read
// from the status line rather than inferred from an empty body: an
// endpoint that legitimately returns an empty array must not be
// mistaken for one that returned nothing. It is read from gh's error
// text as well, in notModified, because gh answers a 304 with a
// non-zero exit and the stdout does not survive that.
//
// A response with no status line at all is treated as a plain body.
// That is the defensive reading, and it is what makes this survive a
// gh that stops honouring --include: the observation is still correct,
// it simply stops being conditional and costs a body every time.
func parseGhResponse(out, validator string) (courtesy.Answer, error) {
	head, body, ok := splitHead(out)
	if !ok {
		return courtesy.Answer{Body: json.RawMessage(out)}, nil
	}
	lines := strings.Split(head, "\n")
	status := strings.TrimSpace(lines[0])
	etag := ""
	for _, l := range lines[1:] {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "etag") {
			etag = strings.TrimSpace(v)
		}
	}
	if strings.Contains(status, " 304") {
		return courtesy.Answer{NotModified: true, Validator: validator}, nil
	}
	return courtesy.Answer{Validator: etag, Body: json.RawMessage(body)}, nil
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
// arrive through three different tools' error formatting and none of
// them hands us a status code.
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
