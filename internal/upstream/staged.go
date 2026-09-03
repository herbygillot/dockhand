package upstream

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream/courtesy"
)

// The staged observer: Check, restaged for a thousand ports at a time.
//
// Check asks every witness about every port. Its first act is the
// port's own livecheck phase — a whole MacPorts target, with a fetch
// of somebody's web page inside it — and only then does it consult the
// forge. For one port that order is right: livecheck is the
// maintainer's declared policy, and reading it first is reading the
// port on its own terms.
//
// For a selector it is backwards, and expensively so. The cheapest
// witness is git ls-remote: unauthenticated, unmetered, one round trip
// to a host built to serve millions of them, and it answers the
// question that decides everything else — is there anything up there
// newer than what this port rides. On the maintainer's tree most ports
// are current, so most ports need no second witness at all. Asking the
// cheap one first is what turns a thousand livecheck phases into a
// few hundred.
//
// So this is Check's judgment with Check's order inverted:
//
//	stage one    git ls-remote, paced and cached, for every port
//	             that names a forge. Cheap enough to pay always.
//	stage two    the port's livecheck phase, and GitHub's releases
//	             API, for candidates only — a port whose forge holds
//	             something that outranks what it rides, and a port
//	             with no forge to have asked.
//
// What stage one can conclude on its own is narrow and worth stating
// plainly: no version the FORGE holds outranks this port's. That is
// not the same as "this port is current". A port whose upstream ships
// tarballs and never tags, or whose livecheck watches a download page,
// can be years behind with a forge that says nothing — and stage one
// will report it current. Deep pays for the second witness on every
// port and finds those; the default does not, and this is the cost of
// the default, named here rather than discovered in the field.
//
// Judge, Observation and every verdict stay exactly as they are. What
// changes is which witnesses get asked and in what order, which is a
// policy about cost — not a policy about truth.

// Livechecker runs a port's own livecheck phase. *portfetch.Fetcher is
// the answer in production, and the interface is transcribed from its
// method set rather than designed beside it, so the fetcher satisfies
// it without an adapter and the assertion below fails to compile if
// either drifts.
//
// It exists for the reason port.Oracle does. What this file decides —
// which witness is worth paying for, and in what order — is judgment,
// and judgment tested only through a live MacPorts session is judgment
// tested on the machines that have one. The staging is exactly the
// part that must be provable offline, because the bug it exists to
// prevent is a sweep that asks a forge too many questions, and no test
// that made real requests could ever be run.
type Livechecker interface {
	Livecheck(ctx context.Context, portdir, subport string) (portfetch.LivecheckResult, error)
}

// The fetcher is the livecheck witness, unchanged.
var _ Livechecker = (*portfetch.Fetcher)(nil)

// Observer asks upstream about many ports without leaning on any one
// host.
//
// Its politeness is not configuration a caller may forget: a nil Pacer
// means unpaced, so the constructor is the place to look and the
// zero value is deliberately not useful.
type Observer struct {
	// Tools resolves the git that ls-remote runs.
	Tools *tool.Finder
	// Gh is the authenticated channel the releases witness needs; nil
	// means tag observations only, which is every run without gh.
	Gh GhRunner
	// Fetcher drives the port's livecheck phase.
	Fetcher Livechecker
	// Pacer bounds how hard any one host is asked. Required.
	Pacer *courtesy.Pacer
	// Cache holds observations between runs; nil asks every time.
	Cache *courtesy.Cache
	// Agent identifies dockhand to the hosts it asks.
	Agent string
	// Deep pays for the second witness on every port rather than on
	// candidates only — the expensive setting that finds an update no
	// forge tag reveals.
	Deep bool

	// lcMu serializes the livecheck witness, because the fetch session
	// underneath it says outright that it is not safe for concurrent
	// use and that downloads through one of them are serial. A sweep
	// runs one worker per evaluator and every one of them may reach
	// this witness, so the constraint has to be honoured here — the
	// pacer cannot do it, since spacing is not mutual exclusion.
	//
	// The cost is real and is the reason to stage: with one fetch
	// session per run, livecheck phases happen one at a time, so a
	// --deep sweep is as long as the sum of its livechecks. The
	// default sweep pays that only for candidates. A pool of fetch
	// sessions is the way out and belongs with the pool that already
	// exists for evaluators, not here.
	//
	// It also, unplanned, is what politeness would have asked for:
	// one livecheck at a time is one upstream web site being fetched
	// at a time.
	lcMu sync.Mutex
}

// Outcome is what a sweep concluded about one port. A closed set: the
// census's arithmetic is what tells a complete report from a short
// one, and an invented outcome would break it.
type Outcome string

const (
	// Outdated: a version dockhand would act on is newer than the one
	// the port rides.
	OutcomeOutdated Outcome = "outdated"
	// Current: nothing dockhand would act on outranks the port. It
	// covers both a forge with nothing newer and a newest that
	// dockhand judged unfit to move to — a prerelease, most often —
	// with the verdict saying which.
	OutcomeCurrent Outcome = "current"
	// Walled: a host refused dockhand and is being left alone. It is
	// not a fault of the port and not a fault of the sweep; the remedy
	// is to run it again later.
	OutcomeWalled Outcome = "walled"
	// Unresolved: the witnesses ran, were heard, and left nothing
	// between them anybody may act on. A livecheck whose regex has
	// rotted is the commonest shape.
	OutcomeUnresolved Outcome = "unresolved"
	// Excluded: the selector filter kept the port out before any
	// witness was asked — retired, replaced, or pinned by a person.
	OutcomeExcluded Outcome = "excluded"
	// Abandoned: the evaluator pool died before the sweep reached the
	// port.
	OutcomeAbandoned Outcome = "abandoned"
	// Failed: something broke.
	OutcomeFailed Outcome = "failed"
)

// Outcomes is the census's order: roughly most interesting first, so
// that the line a reader is looking for is near the top and the tail
// reads the same way twice.
var Outcomes = []Outcome{OutcomeOutdated, OutcomeCurrent, OutcomeUnresolved, OutcomeWalled, OutcomeExcluded, OutcomeAbandoned, OutcomeFailed}

// Hard reports whether an outcome means something broke, as against a
// port dockhand looked at and had an answer about.
//
// The ruling, once, over a closed set rather than over exit codes:
// being outdated is the report's subject and not an error; a host that
// refused us is somebody else's problem and says nothing about the
// other nine hundred ports; witnesses that left no answer are
// upstream's silence. Only a port the sweep could not examine at all
// is a hard error, and those are the two that make the process exit
// 83.
func (o Outcome) Hard() bool {
	switch o {
	case OutcomeOutdated, OutcomeCurrent, OutcomeWalled, OutcomeUnresolved, OutcomeExcluded:
		return false
	case OutcomeAbandoned, OutcomeFailed:
		return true
	}
	// An outcome outside the set cannot be argued to be benign.
	return true
}

// Witnessed is one witness a row cost, and what it cost: the sweep's
// request budget, itemized per port, so that the total is a sum of
// rows rather than a number a reader has to trust.
type Witnessed struct {
	Witness string `json:"witness"`
	Source  string `json:"source"`
}

// The witness names, as they appear in a row and in the budget tail.
const (
	WitnessLsRemote  = "ls-remote"
	WitnessReleases  = "releases"
	WitnessLivecheck = "livecheck"
)

// Row is one port's answer.
//
// The twin is embedded rather than restated so that a row's code and
// its family are one fact derived one way, and it fixes the field
// order for anyone reading the raw stream.
type Row struct {
	Port    string  `json:"port"`
	Portdir string  `json:"portdir"`
	Outcome Outcome `json:"outcome"`
	// Current is the version the port rides today.
	Current string `json:"current,omitempty"`
	// Latest is what dockhand would move it to; empty unless the
	// witnesses resolved one.
	Latest string `json:"latest,omitempty"`
	// Sha is the object the tag naming Latest points at — the exact
	// bytes a bump would fetch, and a fact no version string carries.
	// Empty when the answer came from livecheck alone, or from a
	// releases feed whose tag the ls-remote did not hold.
	Sha string `json:"sha,omitempty"`
	// Verdict is Judge's own word for what the witnesses said.
	Verdict string `json:"verdict,omitempty"`
	// Livecheck and Forge are the individual testimonies.
	Livecheck string `json:"livecheck,omitempty"`
	Forge     string `json:"forge,omitempty"`
	// Repo is the upstream repository the forge witness asked about.
	Repo string `json:"repo,omitempty"`
	// Stages is what this row cost.
	Stages []Witnessed `json:"stages,omitempty"`
	exitcode.Twin
	Detail string `json:"detail,omitempty"`
}

// Observe answers about one port, paying for as few witnesses as the
// answer allows. It is the eval func a sweep drives: one target, one
// row, no shared state but the pacer and the cache, both of which are
// safe for concurrent use.
func (o *Observer) Observe(ctx context.Context, h port.Handle) Row {
	row := Row{Portdir: h.Target.Portdir, Port: PortName(h.Target)}

	vals, err := h.Values(ctx)
	if err != nil {
		return row.fail(err)
	}
	// The evaluated name wins where there is one, and the target's own
	// name stands where there is not: an evaluation that succeeded and
	// named nothing must not erase the only name the row had.
	if vals.Name != "" {
		row.Port = vals.Name
	}
	row.Current = vals.Version

	repo, onForge, err := o.locate(ctx, h, vals)
	if err != nil {
		return row.fail(err)
	}

	m := o.manners()
	obs := Observation{Current: vals.Version}
	var refs []Ref
	var digest string
	if onForge {
		row.Repo = repo.URL
		var src courtesy.Source
		refs, digest, src, err = m.refs(ctx, o.Tools, repo)
		switch {
		case err == nil:
			row.stage(WitnessLsRemote, src)
			obs.ForgeVersions = Versions(refs)
		case errors.Is(err, courtesy.ErrWalled):
			// The forge is behind a wall this sweep raised. Livecheck
			// might still answer, but reporting a port on one witness
			// while the other is being deliberately not asked would
			// publish a verdict the words of which claim two.
			return row.walled(err)
		default:
			// The witness could not run. Not fatal — livecheck may
			// still answer, and a sweep that stopped on one bad
			// repository would be useless — but a port whose forge
			// would not answer becomes a candidate, because stage one
			// concluded nothing about it.
			row.stage(WitnessLsRemote, src)
			row.Detail = err.Error()
			slog.Debug("forge witness unavailable", "portdir", h.Target.Portdir, "err", err)
		}
	}

	// Stage one's whole purpose: decide whether a second witness is
	// worth paying for. A port whose forge holds nothing that outranks
	// it is answered here, and its livecheck is never run.
	if !o.Deep && len(obs.ForgeVersions) > 0 && !candidate(vals.Version, obs.ForgeVersions) {
		return row.stageOne(obs, refs)
	}

	// Stage two. Livecheck first, because the observation it produces
	// is what decides whether the releases feed is worth asking for at
	// all — and because a port with no forge has nothing else.
	lc, lcSrc, err := m.livecheck(ctx, serial{mu: &o.lcMu, lc: o.Fetcher},
		h.Target.Portdir, h.Target.Subport, vals.Version)
	if err != nil {
		if errors.Is(err, courtesy.ErrWalled) {
			return row.walled(err)
		}
		return row.fail(Unreachable("livecheck", err))
	}
	row.stage(WitnessLivecheck, lcSrc)
	applyLivecheck(&obs, lc, vals.Version)

	if onForge && len(obs.ForgeVersions) > 0 {
		versions, src, rerr := m.releases(ctx, o.Gh, repo, digest)
		switch {
		case rerr != nil:
			// The authoritative witness could not be reached. The tags
			// stand, which is the right fallback and a worse one — the
			// tag heuristic is exactly what the releases feed exists to
			// correct — so the row says so rather than reading as a
			// repository that publishes no releases. A reader comparing
			// two runs has to be able to tell which evidence each was
			// judged on.
			row.Detail = joinDetail(row.Detail, "releases witness unavailable: "+rerr.Error())
			slog.Debug("releases witness unavailable", "repo", repo.URL, "err", rerr)
		case len(versions) > 0:
			// Releases REPLACE tags rather than supplement them: a
			// repository that publishes them has said which of its
			// tags it means, and that is a statement no heuristic can
			// improve on. The tags stay in hand for the corroboration
			// below.
			row.stage(WitnessReleases, src)
			obs.ForgeVersions, obs.Authoritative = versions, true
		}
	}
	return row.settled(obs, refs)
}

// locate derives the port's upstream repository, or reports that it
// has none.
//
// A carrier dockhand cannot locate is not an error here. bump needs a
// located span because it is going to write into it; this needs only
// to know which forge to ask, and a port whose style is unrecognized
// simply has no forge — which drops it to the livecheck witness, which
// is exactly what it deserves and not a failure of anything.
func (o *Observer) locate(ctx context.Context, h port.Handle, vals info.Values) (Repo, bool, error) {
	src, cst, err := h.Source()
	if err != nil {
		return Repo{}, false, err
	}
	loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if err != nil {
		var d *portstyle.Decline
		if errors.As(err, &d) {
			return Repo{}, false, nil
		}
		return Repo{}, false, err
	}
	names := coordOptions(loc.Style)
	if len(names) == 0 {
		return Repo{}, false, nil
	}
	opts, err := h.Options(ctx, names...)
	if err != nil {
		return Repo{}, false, err
	}
	repo, ok := coords(loc.Style, opts)
	return repo, ok, nil
}

// manners is the politeness this observer asks under, as the one
// value both roads into upstream share. The Observer's own fields stay
// because they are what a composition root fills in; this is how they
// reach the witnesses.
func (o *Observer) manners() Manners {
	return Manners{Pacer: o.Pacer, Cache: o.Cache, Agent: o.Agent}
}

// serial wraps the livecheck witness so that only one phase runs at a
// time. That is the fetch session's own requirement rather than the
// pacer's — spacing is not mutual exclusion — and it is why the lock
// lives on the observer and travels into the witness rather than round
// it: the cache must be able to serve a second port while the first is
// still inside its livecheck.
type serial struct {
	mu *sync.Mutex
	lc Livechecker
}

func (s serial) Livecheck(ctx context.Context, portdir, subport string) (portfetch.LivecheckResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lc.Livecheck(ctx, portdir, subport)
}

// applyLivecheck folds a livecheck phase's result into an observation,
// in exactly the reading Check uses.
func applyLivecheck(obs *Observation, lc portfetch.LivecheckResult, declared string) {
	switch {
	case !lc.Ran:
		obs.LivecheckDisabled = true
	case lc.Version != "":
		obs.Livecheck = lc.Version
	case lc.UpToDate:
		// Up to date means livecheck found exactly the version it was
		// checking against.
		obs.Livecheck = declared
	case lc.NoMatch:
		// Ran and matched nothing: the rot signal. Livecheck stays
		// empty.
	}
}

// candidate reports whether the forge holds anything that could make
// this port outdated — the one question stage one exists to answer.
//
// It is asked against the same stable subset Judge compares against,
// and for the same reason: a stable port is not made outdated by a
// newer release candidate, and saying so would send a maintainer to
// look at a beta. A port that already rides a prerelease is judged
// against every tag, because upstream has cut nothing else and a
// higher prerelease is the only move it has — the lateral move
// PrereleaseLateral exists to keep open, which a stable-only test here
// would close before any verdict was reached.
//
// A port with no version to compare against is always a candidate: no
// answer can be given cheaply, so the expensive witnesses are owed.
func candidate(current string, forge []string) bool {
	if current == "" {
		return true
	}
	against := forge
	if Stable(current) {
		against = stableOf(forge)
	}
	return macports.VerCmp(newest(against), current) > 0
}

// stageOne is the conclusion the cheap witness can reach alone: the
// forge was asked, it holds nothing that outranks the port, and so
// nothing else was asked.
//
// It does not go through Judge, and that is the point — see
// ForgeCurrent, which exists because handing Judge an observation with
// no livecheck reading in it produces LivecheckRot for a livecheck
// that was never run.
//
// The detail names a prerelease when one is the reason: a port on
// 1.2 whose forge's newest tag is 1.3-rc1 is current in the sense that
// matters, and a reader who sees only "current" beside a repository
// they know has moved is owed the sentence that explains it.
func (r Row) stageOne(obs Observation, refs []Ref) Row {
	newestAny := newest(obs.ForgeVersions)
	r.Verdict = ForgeCurrent.String()
	r.Forge = newestAny
	r.Sha = ShaOf(refs, newestAny)
	r.Outcome = OutcomeCurrent
	r.Twin = exitcode.Of(exitcode.OK, "")
	detail := "the forge's newest tag is " + newestAny
	if newestAny != "" && macports.VerCmp(newestAny, obs.Current) > 0 {
		detail = "the forge's newest tag " + newestAny +
			" is prerelease-style; nothing stable outranks " + obs.Current
	}
	r.Detail = joinDetail(r.Detail, detail)
	return r
}

// settled turns testimony into a row: Judge rules, and the outcome is
// read off the ruling.
func (r Row) settled(obs Observation, refs []Ref) Row {
	rep := Judge(obs)
	if obs.Authoritative && rep.Verdict == LivecheckAhead && len(refs) > 0 {
		// The corroboration Check pays a second ls-remote for, free
		// here: livecheck outran the releases feed, which speaks only
		// for tags upstream blessed, and the tags are already in hand
		// from stage one. A version tagged and never released is
		// invisible to the feed and visible to this.
		rep = corroborate(rep, Versions(refs))
	}
	r.Verdict = rep.Verdict.String()
	r.Livecheck = rep.Livecheck
	r.Forge = rep.ForgeNewest
	r.Latest = rep.Latest
	r.Sha = ShaOf(refs, rep.Latest)
	if r.Sha == "" {
		// No tag named the resolved version — a release cut against a
		// tag the scheme did not match, or a livecheck-only answer. The
		// newest tag is still the object a reader can look at.
		r.Sha = ShaOf(refs, rep.ForgeNewest)
	}

	r.Detail = joinDetail(r.Detail, rep.Detail)
	switch {
	case rep.Latest != "" && macports.VerCmp(rep.Latest, r.Current) > 0:
		r.Outcome = OutcomeOutdated
		r.Twin = exitcode.Of(exitcode.OK, "")
	case rep.Latest != "":
		r.Outcome = OutcomeCurrent
		r.Twin = exitcode.Of(exitcode.OK, "")
	case Judged(rep.Verdict):
		// The witnesses were sound and dockhand judged their newest
		// unfit to move to — a prerelease, almost always. The port
		// stays where it is, and the verdict says why; it is not
		// upstream's silence and must not be banded as though it were.
		r.Outcome = OutcomeCurrent
		r.Twin = exitcode.Of(exitcode.OK, "")
	default:
		r.Outcome = OutcomeUnresolved
		r.Twin = exitcode.Of(exitcode.LatestUnresolved, "witness-unresolved")
	}
	return r
}

// joinDetail keeps what an earlier stage said when a later one has
// something of its own to add.
//
// It exists because a forge that would not answer is written on the
// row before the livecheck witness runs, and a plain assignment
// afterwards would drop it: the reader would see a verdict reached on
// one witness with no sign that the other had failed.
func joinDetail(had, add string) string {
	switch {
	case had == "":
		return add
	case add == "":
		return had
	}
	return had + "; " + add
}

// fail is a row for a port the sweep could not examine.
func (r Row) fail(err error) Row {
	r.Outcome = OutcomeFailed
	r.Twin = exitcode.TwinOf(err)
	r.Detail = err.Error()
	return r
}

// walled is a row for a port whose witness sits behind a host dockhand
// has stopped asking.
func (r Row) walled(err error) Row {
	r.Outcome = OutcomeWalled
	r.Twin = exitcode.TwinOf(err)
	r.Detail = err.Error()
	return r
}

// stage records what a witness cost this row.
func (r *Row) stage(witness string, src courtesy.Source) {
	r.Stages = append(r.Stages, Witnessed{Witness: witness, Source: src.String()})
}
