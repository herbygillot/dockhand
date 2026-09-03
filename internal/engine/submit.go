package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// VerifyDeferredError reports a verification that could not start —
// no bases, full slots, a mid-submit failure — after its branch was
// successfully minted. The branch stands (the git commit/push shape:
// nobody deletes the commit because the push failed), but the
// invocation's contract was mint AND submit, so the exit is nonzero.
// --no-verify narrows the contract to mint alone.
type VerifyDeferredError struct {
	Branch string
	Reason string
	// Cause is the underlying refusal when one exists — a typed
	// CapacityError is how status's deferred pump knows a full machine
	// from a missing capability.
	Cause error
}

func (e *VerifyDeferredError) Error() string {
	return fmt.Sprintf("verification not started: %s\nthe branch stands — `dockhand status` starts it when it can, or run `dockhand verify %s` yourself", e.Reason, e.Branch)
}

func (e *VerifyDeferredError) Unwrap() error { return e.Cause }

// DockhandExit reads the cause, because "the run did not start" is not one
// outcome: a full machine will free on its own, an unprovisioned
// release will not until someone provisions it, a capability refusal
// never will, and a submit that broke after the mint left half the
// work standing. All four used to answer the machine's band, which
// told a user waiting on a queue to go and fix something.
//
// The band is never the synchronous one here even when the cause could
// carry it: a deferral is by definition nobody standing there, and the
// run was recorded for status to start.
func (e *VerifyDeferredError) DockhandExit() int {
	var full *verify.CapacityError
	switch {
	case errors.As(e.Cause, &full):
		return exitcode.VerifyQueued
	case errors.Is(e.Cause, verify.ErrNoEnvironment):
		// Queued rather than refused: the run is on the note, and the
		// event that frees it is a provisioning the user may already be
		// running. The synchronous mirror — an ask with nobody to queue
		// for — is NoVerifyEnv, and cmd's table owns it.
		return exitcode.VerifyAwaitingSlot
	case errors.Is(e.Cause, verify.ErrUnsupported):
		// Nothing frees this one. The provider has said it cannot run
		// what was asked for, which is a verdict about the request.
		return exitcode.VerifyUnsupported
	}
	// The summary a multi-release verify returns carries no cause: each
	// release it counts was recorded deferred and status retries them,
	// which is what queued means. Everything else here is a submit that
	// broke after the branch was minted.
	if e.Cause == nil {
		return exitcode.VerifyQueued
	}
	return exitcode.MintedSubmitErrored
}

// Code names the deferral for a machine. The cause's own name is
// preferred where it has one: the band says a run did not start, and
// this says which of the four reasons it was.
func (e *VerifyDeferredError) Code() string {
	// Capacity first, and not through the cause's own name: a refusal
	// stamped synchronous somewhere upstream would name itself
	// "verifier-busy" while this exits queued, and a reason that
	// contradicts its band is worse than a coarse one.
	var full *verify.CapacityError
	if errors.As(e.Cause, &full) {
		return "verify-queued"
	}
	var namer exitcode.Reasoner
	if errors.As(e.Cause, &namer) {
		return namer.Code()
	}
	switch {
	case errors.Is(e.Cause, verify.ErrNoEnvironment):
		return "verify-awaiting-slot"
	case errors.Is(e.Cause, verify.ErrUnsupported):
		return "verification-unsupported"
	case e.Cause == nil:
		return "verify-queued"
	}
	return "minted-submit-errored"
}

// submission is what a submit needs beyond the branch: which subject,
// which release, and the two facts about the build that the record
// keeps — whether the port's test suite runs after the install, and
// whether the binary archive must be ignored.
//
// A struct rather than four more positional arguments, because two of
// the four are bools that read identically at a call site and the
// record is what they end up in: a caller that swapped them would
// write a note claiming a test nobody ran.
type submission struct {
	Port       string
	Release    platform.Release
	Test       bool
	FromSource bool
	Trace      bool
	// Members is every subject this submission builds, in build order,
	// for a change that has more than one. Empty is the single-subject
	// submission — the shape every caller but a cohort verification
	// makes — and Port with the Minted's RelPort is the whole of it.
	//
	// It is a field rather than a second entry point because a cohort is
	// not a different kind of submission: one guest, one staged overlay,
	// one job, and the members diverge only when the log comes back.
	Members []Member
}

// members is what this submission builds, headline first: the cohort
// when there is one, and otherwise the pair the caller already handed
// over as Port and the branch's own portdir.
func (s submission) members(headlineDir string) []Member {
	if len(s.Members) == 0 {
		return []Member{{Port: s.Port, Portdir: headlineDir}}
	}
	return s.Members
}

// fromSourcePorts is the request's list of ports whose binary archives
// must be ignored, out of the members actually being built. The list is
// the request's shape rather than a flag because a cohort ignores
// archives per member, and it is the headline's property: the change
// left ITS version and revision where they were, so the archive that
// matches them predates it, while a dependent riding along has an
// archive that is exactly what its own recorded verdict is about.
//
// Intersected with what is being built, not asserted over the roster.
// A headline that declined the platform at pre-flight is not in the
// guest at all, and naming it here would put a port in the request that
// no argv mentions.
func (s submission) fromSourcePorts(ports []string) []string {
	if !s.FromSource || !slices.Contains(ports, s.Port) {
		return nil
	}
	return []string{s.Port}
}

// memberPorts is the roster as the record spells it: one port per
// member, in build order, headline first.
func memberPorts(members []Member) []string {
	out := make([]string, 0, len(members))
	for _, mem := range members {
		out = append(out, mem.Port)
	}
	return out
}

// submit stages the minted commit's portdir out of the object database
// — the working tree is irrelevant to what the branch carries —
// submits it to the VM provider, and records the guest and its runs as
// the commit's note. Submission not starting is not a minting failure —
// the branch stands — but it is a contract failure: VerifyDeferredError
// carries that split.
func (e *Engine) submit(ctx context.Context, m *Minted, s submission) error {
	portName := s.Port
	members := s.members(m.RelPort)
	roster := memberPorts(members)
	prov, err := e.Verifier(ctx)
	if err != nil {
		if errors.Is(err, verify.ErrNoProvider) {
			// No provider, no contract: the machine cannot verify at all,
			// so this is a --no-verify bump that says so — and the branch
			// may be promoted as it is, unverified. A machine that HAS
			// the provider and no base images is the other refusal
			// below: there the remedy is provisioning, so the contract
			// failed rather than narrowed.
			fmt.Fprintln(e.Err, "no verification possible: no local verify provider (tart)")
			fmt.Fprintf(e.Err, "the branch is unverified; you may promote it as is, or install tart and run `dockhand verify %s`\n", m.Branch)
			return nil
		}
		if errors.Is(err, verify.ErrNoEnvironment) {
			return e.queue(ctx, m, s, err, roster)
		}
		return err
	}
	// The platform resolves before anything is recorded: a run is keyed
	// by release name, and "the default" is not a key. The provider is
	// asked what it offers only when the caller named nothing, because
	// Capabilities is an interface method with no purity promise on it —
	// a provider that answered by talking to a hypervisor would be doing
	// so on every submit for an answer already in hand.
	if s.Release.IsZero() {
		if s.Release, err = verdict.ResolveRelease(s.Release, prov.Capabilities().Platforms); err != nil {
			return e.queue(ctx, m, s, err, roster)
		}
	}
	release := s.Release
	root, err := e.Temp()
	if err != nil {
		return err
	}
	stage, _, err := root.MakeDir("stage-" + portName)
	if err != nil {
		return err
	}
	// Every member's directory into one staging root, and from there
	// into one overlay in front of one guest's ports tree. A cohort
	// that must be built together is one build: the capacity arithmetic
	// counts environments, and it does not change because the change
	// has more subjects.
	//
	// mpbb's list-time exclusion, borrowed, and now asked of each
	// member: evaluation answers known_fail in a second, before any VM
	// boots — and it answers for the branch's content, which is what was
	// materialized. The same session answers use_xcode, so a port that
	// needs a full Xcode is probed for one before the build starts, not
	// forty minutes in; one member needing it is the whole guest needing
	// it, because there is one guest.
	//
	// A member that declines is recorded and left out rather than
	// declining the change: the others are still buildable, and a
	// known_fail on one port is a fact about that port. At one subject
	// that is exactly today's behaviour, because leaving the only member
	// out leaves nothing to submit.
	// The roster goes onto the note, in build order, before any verdict
	// about any member does. Subjects are built by adoption for a change
	// nobody minted, adoption appends in call order, and the loop below
	// records a declining member's refusal as it meets it — so a record
	// that learned its members from those refusals would be headlined by
	// whichever port pre-flight threw out first, and Headline() drives
	// the branch's resolution, the pull request's target and the member
	// an unattributable failure lands on.
	//
	// Only for a cohort, and not because one member is a special case:
	// at one member there is no order to get wrong, and this is the step
	// that must not add a note write to the single-subject path.
	if len(roster) > 1 {
		if err := e.Ledger(m.Repo).AdoptSubjects(ctx, m.Sha, roster); err != nil {
			return err
		}
	}
	var ports, portdirs []string
	needsXcode := false
	for _, mem := range members {
		if err := m.Repo.Materialize(ctx, m.Sha, mem.Portdir, stage); err != nil {
			return err
		}
		staged := filepath.Join(stage, filepath.FromSlash(mem.Portdir))
		pre := map[string]verdict.Preflight{}
		if pf, kerr := e.preflightOn(ctx, staged, release); kerr != nil {
			// An evaluation that could not run is not evidence that the port
			// declines anything, so the release goes unlisted and is
			// scheduled as an ordinary build.
			fmt.Fprintf(e.Err, "warning: pre-flight evaluation: %v\n", kerr)
		} else {
			pre[release.Name] = pf
		}
		sched := verdict.SchedulePlatforms(mem.Port, []platform.Release{release}, pre)[0]
		if sched.Declined != nil {
			if rerr := e.recordRun(ctx, m.Repo, m.Sha, mem.Port, release.Name, *sched.Declined, sched.Message); rerr != nil {
				return rerr
			}
			continue
		}
		needsXcode = needsXcode || sched.NeedsXcode
		ports = append(ports, mem.Port)
		portdirs = append(portdirs, staged)
	}
	if len(ports) == 0 {
		// Every member declined this platform before anything booted.
		// Their verdicts are on the note; there is no environment to ask
		// for and nothing for one to do.
		return nil
	}
	// The archive is ignored where a pass earned against it would prove
	// nothing: the change left the port's version and revision where
	// they were, so the archive that matches them predates it. Read once
	// and used twice, because the request and the note must say the same
	// thing about the same member.
	fromSource := s.fromSourcePorts(ports)
	manifest, baseline := e.manifestAsk(ctx, m, prov, portName, members, root)
	job, err := prov.Submit(ctx, verify.Request{
		Ports:      ports,
		Portdirs:   portdirs,
		Baseline:   baseline,
		Platform:   release,
		Owner:      m.Repo.Root,
		Test:       s.Test,
		NeedsXcode: needsXcode,
		Manifest:   manifest,
		FromSource: fromSource,
	})
	if err != nil {
		// A full provider (two-slot cap), a capability refusal, or a
		// mid-submit failure: the branch is minted and the tip is
		// simply unverified. The deferred run is recorded here rather
		// than left to a later verify — a field run saw an intent-path
		// refusal show as bare "unverified" with the reason only in
		// scrollback. For the members that were going to be built, and
		// not for the roster: one that declined the platform has its
		// answer already, and a deferral written over it would claim a
		// build is coming for a port that said it cannot be built here.
		return e.queue(ctx, m, s, err, ports)
	}
	// One guest and every verdict started in it, in one note. The job
	// carries what is true of the environment — the test that was asked
	// of it, and who owns the submission — and the run carries what is
	// being concluded about the port.
	if err := e.Ledger(m.Repo).RecordSubmission(ctx, m.Sha, release.Name,
		record.JobRecord{
			Job:  job,
			Test: s.Test,
			// The owner is recorded, and the submit lock is what still
			// enforces it: this field is the claim the protocol will read
			// once that lock is retired, which is a concurrency change and
			// a commit of its own. Stamped from the guest's own start
			// rather than from a second clock read, because that is the
			// moment this session took the environment and the note should
			// not carry two answers to when.
			Claim: &record.Claim{By: claimant(), At: job.Started.UTC()},
		},
		ports,
		func(port string) record.Run {
			// Every member is running and every member led with lint,
			// because that is what one guest was told to do. Whether the
			// archive was ignored is the member's own: the argv says it of
			// one port, and a note that said it of the others would vouch
			// for a build from source that never happened.
			return record.Run{State: record.Running, Linted: true,
				FromSource: slices.Contains(fromSource, port)}
		},
	); err != nil {
		// Submit-and-record is a transaction: a job whose note cannot
		// be persisted is a running VM no settlement can ever find, so
		// the compensation is release, on a context that survives the
		// caller's cancellation. Strict note validation made this path
		// reachable — a malformed existing note now refuses instead of
		// being overwritten — which is exactly when a worker must not
		// be left running behind an error return.
		if rerr := prov.Release(context.WithoutCancel(ctx), job); rerr != nil {
			return fmt.Errorf("recording the run failed (%w) and releasing %s failed too: %w — `tart delete %s` frees the slot", err, job.ID, rerr, job.ID)
		}
		return fmt.Errorf("recording the run failed; the worker was released: %w", err)
	}
	// Every member the guest is building, because one guest builds them
	// all and a line naming only the headline would understate what the
	// slot is spending an hour on. One name joins to itself.
	fmt.Fprintf(e.Err, "verify: submitted %s on %s (job %s); `dockhand status` follows it\n", strings.Join(ports, ", "), release.Name, job.ID)
	if s.Trace {
		return e.Follow(ctx, m.Repo, m.Sha, portName, release.Name, prov, job)
	}
	return nil
}

// queue is the one way a submit gives up: the run nothing could start
// is written onto the note, and the deferral goes back to the caller.
//
// The note is what makes a deferral recoverable — `dockhand status`
// retries what it finds recorded — so it is written before the error
// travels, and a failure to write it is a warning rather than the
// answer: the branch stands either way, and losing the reason to a
// second failure would leave the tip reading as bare "unverified".
//
// A run is keyed by release, so one that never resolved a release
// cannot be recorded. That is only reachable on a machine whose
// provider offers no platform at all, where there is nothing for
// status to retry against until a base exists.
//
// A queued run has no job, and none is invented for it. Nothing was
// submitted, so there is no guest to describe, and a job record
// written empty to keep the maps the same length would be the record
// claiming an environment that does not exist.
//
// One queued run per member of the set handed in, because the deferral
// is true of all of them: a cohort that recorded only its headline
// would leave the others reading as unverified for no stated reason,
// and the drain walks runs — so the members it never wrote would never
// be retried.
//
// The set is the caller's to name and is not re-derived here, because
// which members a deferral is about depends on where it happened. A
// machine with no environment at all defers the whole roster; a
// provider that refused after pre-flight defers only the members that
// were going to be built, and writing "queued" over a member that has
// already declined the platform would replace its verdict with a
// promise, leaving the change reading as still verifying until a drain
// re-evaluated it.
func (e *Engine) queue(ctx context.Context, m *Minted, s submission, cause error, ports []string) error {
	if !s.Release.IsZero() {
		fromSource := s.fromSourcePorts(ports)
		for _, port := range ports {
			if rerr := e.recordRun(ctx, m.Repo, m.Sha, port, s.Release.Name, record.Run{
				State: record.Queued, Detail: cause.Error(),
				FromSource: slices.Contains(fromSource, port),
			}, ""); rerr != nil {
				fmt.Fprintf(e.Err, "warning: recording the queued run: %v\n", rerr)
			}
		}
	}
	return &VerifyDeferredError{Branch: m.Branch, Reason: cause.Error(), Cause: cause}
}

// recordRun writes one platform's run into the commit's note — the
// read-modify-write every per-platform update goes through — and tells
// the user what was recorded.
//
// The note half is the ledger's, lock and re-read included; the
// sentence about it is this package's, because what a verb says
// belongs to the verb.
func (e *Engine) recordRun(ctx context.Context, repo *git.Repo, sha, portName, releaseName string, r record.Run, msg string) error {
	if err := e.Ledger(repo).RecordRun(ctx, sha, portName, releaseName, r); err != nil {
		return err
	}
	if msg != "" {
		fmt.Fprintln(e.Err, msg)
	}
	return nil
}

// SubmitRelease claims one release's run for the branch and submits it,
// under the repository's submit lock from the re-read through the
// record — the claim pumpRun makes, made the same way, so a verify and
// a status over one queued run cannot both start it. The note is
// read under the lock, because what it says outside is what a peer
// may already have changed: a run already running is left alone, and
// a deferral is re-recorded with its reason before the lock goes, so
// the record can never land on top of a peer's start. started reports
// a submit that went through, for --trace to follow once the claim is
// released.
//
// What the note already knows about the build is carried into the new
// one: whether the archive must be ignored is a property of the change
// and not of the invocation, and a user who asked once should not have
// to ask again to have the same thing verified.
//
// It also settles the destination. `dockhand verify <branch>` is a
// person asking for a verdict about that branch, which is exactly what
// the field records — so a change minted with --no-verify and then
// verified by hand stops being a change nobody asked a verdict of, and
// the drain will retry it if this attempt is queued.
//
// The members are the change's, headline first, and they are what is
// submitted: a cohort goes into one environment as one build, so the
// claim is made once for all of them and the note gains one job with
// one run per member.
func (e *Engine) SubmitRelease(ctx context.Context, repo *git.Repo, branch, tip string, members []Member, r platform.Release, test bool) (started bool, err error) {
	if len(members) == 0 {
		return false, fmt.Errorf("verify: %s has no subject to verify", branch)
	}
	unlock, err := repo.LockSubmit(ctx, SubmitLockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		// The expected contention — a peer's pump booting a guest — is
		// not a hung process, so the lock's own advice would mislead.
		return false, fmt.Errorf("%w: a verification is being submitted in this repository; `dockhand status` shows what it started, then `dockhand verify %s` again", lockfile.ErrHeld, branch)
	}
	if err != nil {
		return false, err
	}
	defer unlock()
	portName := members[0].Port
	s := submission{Port: portName, Release: r, Test: test, Members: members}
	if n, nerr := e.Ledger(repo).Read(ctx, tip); nerr == nil {
		if run, ok := n.Runs[record.RunKey(portName, r.Name)]; ok {
			if run.State == record.Running {
				fmt.Fprintf(e.Err, "already verifying on %s (%s); `dockhand status` follows it\n",
					r.Name, time.Since(n.Jobs[r.Name].Job.Started).Round(time.Second))
				return false, nil
			}
			s.FromSource = run.FromSource
		}
	}
	e.askVerdict(ctx, repo, tip)
	err = e.submit(ctx, &Minted{Repo: repo, Branch: branch, Sha: tip, RelPort: members[0].Portdir}, s)
	var vde *VerifyDeferredError
	if errors.As(err, &vde) {
		// One line and no second write. The deferral is one event, so a
		// sentence per member would report one full machine as several —
		// and the runs are already down: queue wrote them, for exactly
		// the members the deferral was about. Re-recording the headline
		// here would overwrite whatever the submit had already concluded
		// about it, which for a headline that declined the platform at
		// pre-flight means replacing its refusal with a promise.
		fmt.Fprintf(e.Err, "deferred %s: %s\n", r.Name, vde.Reason)
		return false, err
	}
	return err == nil, err
}

// askVerdict records that somebody has now asked for a verdict about
// this change, whatever it was minted for.
//
// A destination is where the contract reaches and not where it started:
// a branch minted with --no-verify is never drained, and the one thing
// that can honestly change that is a person naming it to `dockhand
// verify`. Written best-effort, because the submit that follows is the
// answer to what was asked and a bookkeeping failure must not stand in
// its way.
func (e *Engine) askVerdict(ctx context.Context, repo *git.Repo, tip string) {
	if err := e.Ledger(repo).Update(ctx, tip, func(r *record.Record) error {
		if r.Destination == record.ToVerdict || r.Destination == record.ToPublished {
			return ledger.ErrUnchanged
		}
		r.Destination = record.ToVerdict
		return nil
	}); err != nil {
		fmt.Fprintf(e.Err, "warning: recording that a verdict was asked for: %v\n", err)
	}
}

// manifestAsk decides whether this submission asks the environment to
// describe what it installed, and stages the "before" it will be
// measured against.
//
// Two conditions, and both of them are about whether anybody will read
// the answer. The port must have dependents, because the one consumer
// of an ABI measurement is the cohort decision and a port nothing
// depends on has no cohort; and the provider must declare it can
// describe an installation, because the walk happens inside the
// environment while the build is there to be walked, so a request that
// had not said so up front would be asking for something the provider
// was never told to collect.
//
// The baseline is the MERGE-BASE portdirs, staged on the host. The
// guest's own ports tree cannot answer the question: it is frozen at
// provisioning time and may hold a newer version than the branch
// started from, an older one, or not carry the port at all. What the
// change is leaving is what the base commit said, and only the caller
// holding the repository can produce it.
//
// Every refusal here is silent and none of them fails the submit. A
// tree with no index, a base that cannot be resolved, a portdir that
// did not exist at the merge base — a port ADDED on the branch is
// exactly that last one — all mean the same thing: there is no honest
// before, so none is staged, and the finding at settle says the check
// was unavailable and names why. A verification refused for want of a
// baseline would be a build nobody asked to skip.
func (e *Engine) manifestAsk(ctx context.Context, m *Minted, prov verify.Verifier, port string, members []Member, root tempdir.Root) (bool, []string) {
	if !prov.Capabilities().InstalledManifest {
		return false, nil
	}
	deps, _, err := e.dependentsOf(port)
	if err != nil || len(deps) == 0 {
		// The unreadable fields are the settlement's to report, not this
		// call's: what is decided here is only whether to collect a
		// manifest, and a short reverse index changes nothing about that.
		return false, nil
	}
	base, err := e.Ledger(m.Repo).Read(ctx, m.Sha)
	if err != nil || base.Base.Sha == "" {
		// The manifest is still worth asking for: the installed side is
		// a real observation, and a comparison with no before is a
		// named refusal rather than a reason to collect nothing.
		return true, nil
	}
	stage, _, err := root.MakeDir("baseline-" + port)
	if err != nil {
		return true, nil
	}
	var out []string
	for _, mem := range members {
		if merr := m.Repo.Materialize(ctx, base.Base.Sha, mem.Portdir, stage); merr != nil {
			// Absent at the base. For the headline that is a port this
			// branch added, which is the honest no-baseline case; for a
			// member it is one the cohort brought with it.
			continue
		}
		out = append(out, filepath.Join(stage, filepath.FromSlash(mem.Portdir)))
	}
	return true, out
}
