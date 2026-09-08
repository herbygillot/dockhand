package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/doctor"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lockfile"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/report"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// dischargeGrace is how long an obligation must have been dead before a
// pass may seize it.
//
// IT IS A BUG FIX AND NOT A FEATURE, and it ships as a constant because
// the number matters and the spelling does not yet. app.Cycle
// reconciles FIRST on the argument that "a reconciler at the start meets
// only dead work" — true of a nightly pass at 03:00, false of one that
// ticks every five minutes — and lease decides liveness by PID plus
// start time, so a process that died four seconds ago is
// indistinguishable from a lease abandoned overnight. Under
// detach-always every in-flight attempt is owned by a process that has
// already exited, which makes that indistinguishability the ORDINARY
// case rather than the rare one.
//
// The sharpest instance is a window run.Start documents deliberately:
// the provider request carries a client-generated id the adapter puts
// in the worker's name, "so a crash between the provider call and the
// record leaves something the reconciler can find". That findable
// worker is a gift to a nightly reconciler and a trap for a
// five-minute one, which meets it while another process is still
// writing the record rather than after anybody abandoned it. Without
// this cutoff a resident pass discharges an environment while the
// person who lost the process is still typing the rerun.
//
// `--discharge-after` is the phase-two spelling of this number.
const dischargeGrace = 15 * time.Minute

// passFlags are the flags `cycle` spells and `dispatch` INHERITS
// VERBATIM. It is one type because dispatch is the same pass: it does
// not respell them and it does not re-police them, and only two are held
// back from a resident loop — see dispatchCmd.
type passFlags struct {
	noDischarge bool
	keepMerged  bool
	superseded  bool
	compact     int
	dryRun      bool
	reclaim     bool
}

func (f *passFlags) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.noDischarge, "no-discharge", false,
		"withhold the discharge stage; it is ON by default and covers both obligation kinds")
	c.Flags().BoolVar(&f.keepMerged, "keep-merged", false,
		"keep the branches of merged pull requests rather than retiring them, and say why each stands")
	c.Flags().BoolVar(&f.superseded, "superseded", false,
		"also remove branches a newer sibling replaced (an inference, so opt-in)")
	c.Flags().IntVar(&f.compact, "compact", 0,
		"trim closed records older than <days> out of the state tree")
	c.Flags().BoolVar(&f.dryRun, "dry-run", false,
		"withhold the irreversible stages and perform every other one — NOT a read-only pass")
	c.Flags().BoolVar(&f.reclaim, "reclaim-unattributed", false,
		"let this pass seize environments with no attribution at all")
}

// request turns the flags into what a pass is permitted to do. Every
// field on CycleRequest is a policy or a permission and none of them is
// a fact, which is why this function can be pure.
//
// --compact TAKES A VALUE AND CANNOT BE A BARE BOOLEAN.
// statestore.Retention refuses its own unset zero (rule 7), so "compact"
// with nothing said about how long a tail to keep is not a smaller ask,
// it is an ask nobody can answer. The flag is read as DAYS because that
// is the unit a person thinks a retention in.
func (f *passFlags) request(c *cobra.Command, publishing bool) (app.CycleRequest, error) {
	r := app.CycleRequest{
		Discharge:           !f.noDischarge,
		DischargeAfter:      dischargeGrace,
		ReclaimUnattributed: f.reclaim,
		// cycle refreshes the forge cache BY DEFAULT and cannot be told
		// otherwise; status is the road that serves what was recorded. A
		// five-minute dispatcher inherits this refresh until
		// --refresh-every exists, which is the traffic that flag is filed
		// to fix.
		Forge:      publish.ForgeRefresh,
		Retirement: retirement(f.keepMerged),
		Publish:    publishing,
		Superseded: f.superseded,
		DryRun:     f.dryRun,
	}
	if c.Flags().Changed("compact") {
		if f.compact <= 0 {
			return r, usagef("--compact takes the number of days of closed records to keep; a retention of zero is not one")
		}
		r.Compact = &statestore.Retention{Set: true, ClosedFor: f.compact}
	}
	return r, nil
}

// retirement is --keep-merged read as the policy it is. Demolish is the
// default: a merged change dockhand minted loses the branch dockhand
// made. ReportOnly is reachable by no flag — app.Retirement has three
// live values and one switch between them, which the surface files as an
// open spelling rather than a gap this step invents a flag for.
func retirement(keep bool) app.Retirement {
	if keep {
		return app.WithholdDeletion
	}
	return app.Demolish
}

// cycleCmd is THE PERSON'S PASS: one run over everything durable this
// checkout already owns, Human by construction, publishing nothing, and
// returning an exit band a resident process has no way to return.
//
// It keeps its name, its stages and its one-shot lifetime, and loses
// exactly one thing: the ability to be a machine. With --auto retired
// no typed verb can declare itself unattended, so the publish slot the
// machine declaration used to hand the reconciler is simply never built
// on this road — a construction rather than a runtime refusal, and the
// mirror image of what keeps `promote` human.
//
// IT DOES NOT BECOME REDUNDANT beside `dispatch --once`: it is the
// one-shot road that still returns a band, which is what a CI wrapper
// or a person checking by hand needs, and it is the road that publishes
// nothing.
func cycleCmd(s *Services) *cobra.Command {
	var f passFlags
	c := &cobra.Command{
		Use:   "cycle",
		Short: "Do what status only reports: discharge, retire, drain — once, as a person",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			req, err := f.request(cmd, false)
			if err != nil {
				return err
			}
			if err := s.Acquire(ctx, needsPass()); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			// THE PASS LOCK IS THE CALLER'S, and this is that caller.
			// app.Cycle opens no file: a pass that ran twice at once is a
			// composition mistake and never a race the operation's body could
			// have prevented. A second pass EXITS 0 NAMING THE HOLDER and
			// does not wait — the pending band's own answer, since nothing
			// was refused and the work is being done by somebody else this
			// second.
			me := s.Me(s.Now())
			held, holder, err := takePassLock(ctx, repo, me, "cycle")
			if err != nil {
				return err
			}
			if !held {
				fmt.Fprintf(s.Out, "a pass is already running here: %s\n", describe(holder))
				return nil
			}
			defer holder.release()
			op, stg, err := cycleOp(ctx, s, me, holder.token, record.Human, publish.Pace{})
			if err != nil {
				return err
			}
			defer stg.Cleanup()
			p, err := op.Run(ctx, req)
			report.Pass(s.Out, p, req.DryRun)
			if err != nil {
				return err
			}
			return exitWith(p.Exit())
		},
	}
	f.register(c)
	return c
}

// dispatchCmd is `dockhand dispatch`: app.Cycle under a MACHINE invoker,
// in a loop — cycle, then sleep, repeat.
//
// IT IS NOT AN OPERATION AND OWNS NO LIFECYCLE. A pass is a
// decision-and-effect sequence and a cadence is neither, so the loop,
// the residency lock, the signals and the sleep live here; app.Cycle
// gains no loop field, no interval and no signal handling.
//
// IT IS THE MACHINE, and that is what retires --auto. The invoker stops
// being a flag anyone may pass and becomes a property of WHICH PROCESS
// acted: a verb the operator typed is the most declared thing in the
// tool, and `dockhand dispatch --auto` is therefore a usage error rather
// than a redundancy.
//
// IT PUBLISHES BY DEFAULT, capped at --publish-max per --publish-every
// counted against a DURABLE last-published moment. The cap is what makes
// default-on safe, and it governs a RATE rather than a total: 20 per 6h
// clears a ~119-port backlog inside two days, where the shipped
// one-per-nightly-pass would have taken four months. Starting a
// dispatcher is therefore a standing grant rather than a convenience,
// and `status` prints the residency so the grant is inspectable.
//
// IT NEVER EXITS 84. 84 is a pass's code and a pass is one tick of this
// process; a resident dispatcher that exited non-zero because one branch
// needs a person would, under launchd KeepAlive, turn the attention
// channel into a restart loop. `--once` is the only place a pass's code
// reaches a shell, and `status` is the attention channel here.
func dispatchCmd(s *Services) *cobra.Command {
	var (
		f            passFlags
		every        time.Duration
		once         bool
		noPublish    bool
		publishMax   int
		publishEvery time.Duration
	)
	c := &cobra.Command{
		Use:   "dispatch",
		Short: "Keep the scheduler active: run a pass, sleep, repeat",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			pace, err := paceOf(cmd, noPublish, publishMax, publishEvery)
			if err != nil {
				return err
			}
			if err := checkLoopFlags(cmd, &f, once, every); err != nil {
				return err
			}
			req, err := f.request(cmd, !noPublish)
			if err != nil {
				return err
			}
			if err := s.Acquire(ctx, needsPass()); err != nil {
				return err
			}
			repo, err := s.Repo()
			if err != nil {
				return err
			}
			return dispatchLoop(ctx, s, repo, req, pace, loop{
				every: every, once: once, publishing: !noPublish,
			})
		},
	}
	f.register(c)
	c.Flags().DurationVar(&every, "every", 5*time.Minute,
		"the loop period, measured from the END of the previous pass so a pass that overruns cannot stack")
	c.Flags().BoolVar(&once, "once", false,
		"run exactly one pass and exit with THE PASS'S code")
	c.Flags().BoolVar(&noPublish, "no-publish", false,
		"withhold the publish stage; dispatch publishes by default and this is the only way to stop it")
	c.Flags().IntVar(&publishMax, "publish-max", publish.DefaultPace.Max,
		"publications permitted per --publish-every window, counted against a durable last-published moment")
	c.Flags().DurationVar(&publishEvery, "publish-every", publish.DefaultPace.Window,
		"the window --publish-max is counted over")
	return c
}

// loop is the cadence, which is everything about dispatch that a pass is
// not.
type loop struct {
	every      time.Duration
	once       bool
	publishing bool
}

// minEvery is the floor under --every. A cadence faster than a minute
// is a pass that spends more time taking locks than doing work, and
// under a forge stage that refreshes every tick it is sustained traffic
// for facts that move on the order of days.
const minEvery = time.Minute

// checkLoopFlags is the two refusals a RESIDENT loop earns, and the one
// the cadence itself earns.
//
// --superseded is refused on a resident loop and ACCEPTED WITH --once. A
// supersession is dockhand's own inference from two branch names, and an
// unattended process deleting somebody's work on an inference, hourly,
// forever, is the one act in the pass that a re-read cannot undo.
//
// --dry-run is accepted ONLY with --once: a loop that previews forever
// is a process pretending to work.
func checkLoopFlags(c *cobra.Command, f *passFlags, once bool, every time.Duration) error {
	switch {
	case f.superseded && !once:
		return usagef("--superseded deletes branches on an inference; a resident dispatcher may not, and `dockhand dispatch --once --superseded` may")
	case f.dryRun && !once:
		return usagef("--dry-run needs --once; a loop that previews forever is a process pretending to work")
	case !once && c.Flags().Changed("every") && every < minEvery:
		return usagef("--every %s is below the %s floor", every, minEvery)
	}
	return nil
}

// paceOf is the machine's publication allowance, and it refuses a cap of
// zero.
//
// --publish-max 0 IS A USAGE ERROR AND NOT A WAY TO DISABLE
// PUBLICATION: a cap of zero and an unconfigured cap are the same value
// to every reader of one, and --no-publish already says the thing
// plainly. A window longer than publish.MaxWindow is refused for a
// different reason — that constant is the floor statestore.Compact keeps
// machine publication rows above, so a pace counted over a longer window
// could undercount against a store some other process compacted.
func paceOf(c *cobra.Command, noPublish bool, max int, window time.Duration) (publish.Pace, error) {
	if noPublish {
		// A road that publishes nothing is handed no pace, and never
		// reaches Authorize to be asked for one.
		return publish.Pace{}, nil
	}
	switch {
	case c.Flags().Changed("publish-max") && max <= 0:
		return publish.Pace{}, usagef("--publish-max 0 is not how publication is stopped; --no-publish says it plainly")
	case window > publish.MaxWindow:
		return publish.Pace{}, usagef("--publish-every may not exceed %s, which is the floor compaction keeps machine rows above", publish.MaxWindow)
	case window <= 0:
		return publish.Pace{}, usagef("--publish-every takes a window; a rate over no window is not a rate")
	}
	return publish.Pace{Set: true, Max: max, Window: window}, nil
}

// dispatchLoop is the cadence: take the residency lock for this
// process's whole life, then pass and sleep until the context ends.
//
// A SECOND DISPATCHER ON A CHECKOUT EXITS 0 NAMING THE HOLDER. It does
// not wait and it does not run a degraded pass: the queue is the state
// ref and the resident process is already draining it, so there is
// nothing for this one to do and nothing was refused.
//
// SLEEP IS MEASURED FROM THE END OF A PASS, in WALL-CLOCK SLICES. From
// the end, so a pass that overruns its period cannot stack. In slices,
// because a laptop that slept eight hours wakes owing either one tick
// (a monotonic timer) or ninety-six (a wall-clock schedule), and the
// first is a scheduler that stopped for a night while the second either
// stampedes or silently coalesces. Slices wake, notice the wall clock
// has moved, and run ONE pass.
func dispatchLoop(ctx context.Context, s *Services, repo *git.Repo, req app.CycleRequest, pace publish.Pace, l loop) error {
	start := s.Now()
	me := s.Me(start)
	path, err := lockPath(ctx, repo, dispatchLock)
	if err != nil {
		return err
	}
	release, err := lockfile.Hold(ctx, path, holderOf(me, "dispatch"), probeDeadline)
	if err != nil {
		if !errors.Is(err, lockfile.ErrHeld) {
			return err
		}
		h, resident, perr := lockfile.Probe(ctx, path)
		if perr != nil || !resident {
			// The lock refused us and then read as free: a probe holding a
			// shared lock for a microsecond, or a peer that exited between
			// the two calls. Say what happened rather than claiming a
			// scheduler is up.
			fmt.Fprintln(s.Err, "the residency lock could not be taken and names no holder; try again")
			return nil
		}
		fmt.Fprintf(s.Out, "a dispatcher is already resident here: %s\n", describeHolder(h))
		return nil
	}
	defer release()

	fmt.Fprintf(s.Out, "dispatch resident on %s (pid %d)\n", me.Root, me.PID)
	if l.publishing {
		fmt.Fprintf(s.Out, "publishing ENABLED (--no-publish withholds it), max %d per %s, counted against a durable last-published moment\n",
			pace.Max, pace.Window)
	} else {
		fmt.Fprintln(s.Out, "publishing withheld")
	}

	// The dispatcher's own memory: what it has already announced, so the
	// 20s band is EDGE-TRIGGERED rather than repeated on every tick. At a
	// five-minute cadence a stable human-blocked refusal would otherwise
	// print 288 identical lines a day, which is precisely how an operator
	// learns to ignore the channel the exit-band ruling exists to
	// protect. It lives in process memory and not in a record because it
	// is a fact about THIS process's output, and a new dispatcher should
	// re-announce what it has never said.
	said := map[record.ChangeID]string{}

	for {
		passStart := s.Now()
		code, err := onePass(ctx, s, repo, req, pace, said)
		if err != nil {
			return err
		}
		if l.once {
			// --once IS THE ONLY PLACE A PASS'S CODE REACHES A SHELL, and
			// with it exit 62 — the spent-allowance band — gets back the
			// producer it lost when `cycle --auto` retired.
			return exitWith(code)
		}
		if err := sleepFrom(ctx, s.Now, passStart, l.every); err != nil {
			// A canceled context is a clean shutdown: dispatch exits 0 on
			// one, because a person who stopped the scheduler asked for
			// exactly that.
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(s.Err, "dispatch stopping")
				return nil
			}
			return err
		}
	}
}

// onePass runs a single tick and reports the band it landed in.
//
// The pass lock is taken PER PASS and released across the sleep, which
// is why it cannot answer "is a scheduler active on this checkout" and
// why the residency lock exists beside it. A tick that cannot take it
// says so and is not an error: another pass is doing the work.
func onePass(ctx context.Context, s *Services, repo *git.Repo, req app.CycleRequest, pace publish.Pace, said map[record.ChangeID]string) (int, error) {
	// OwnerID.Since IS RESTAMPED PER PASS, and this line is the whole of
	// it. record/lease.go documents Since as the process's start time,
	// which under a one-shot verb is also the pass's start; in a resident
	// dispatcher it would never advance, so lease.Outstanding could not
	// tell this pass's live work from last week's residue owned by the
	// same live PID, and the reconcile stage would be inert or
	// destructive. The PROCESS start is kept separately, for the
	// residency stamp, which answers a different question — "since when
	// has a scheduler been here".
	me := s.Me(s.Now())
	held, holder, err := takePassLock(ctx, repo, me, "dispatch")
	if err != nil {
		return exitcode.OK, err
	}
	if !held {
		fmt.Fprintf(s.Err, "skipping this tick: %s\n", describe(holder))
		return exitcode.OK, nil
	}
	defer holder.release()
	op, stg, err := cycleOp(ctx, s, me, holder.token, record.Machine, pace)
	if err != nil {
		return exitcode.OK, err
	}
	// THE PER-PASS CLEANUP THE DRAIN'S RE-PLAN CREATES. Every attempt the
	// drain starts materializes a tree under the run's temporary root,
	// and tempdir.Root only removes those when the PROCESS ends — which
	// in a dispatcher is a month. Dropped at the pass boundary and never
	// inside one, because the guest is served from the staged tree.
	defer stg.Cleanup()
	p, err := op.Run(ctx, req)
	if err != nil {
		return exitcode.OK, err
	}
	report.Pass(s.Err, p, req.DryRun)
	report.Refusals(s.Err, unsaid(p.Refusals, said))
	return p.Exit(), nil
}

// unsaid is the edge-trigger: the refusals this process has not already
// announced with the same words, remembered so the next tick is quiet
// about them.
//
// It keys on the change AND on the sentence, so a change whose refusal
// CHANGES is announced again — "held" becoming "an upstream PR already
// proposes this" is news, and a memory that only remembered the change
// would swallow it.
func unsaid(rs []app.Refusal, said map[record.ChangeID]string) []app.Refusal {
	var out []app.Refusal
	for _, r := range rs {
		text := ""
		if r.Err != nil {
			text = r.Err.Error()
		}
		if said[r.Change] == text {
			continue
		}
		said[r.Change] = text
		out = append(out, r)
	}
	return out
}

// sleepFrom sleeps until `every` after the pass STARTED, in wall-clock
// slices. See dispatchLoop for why the slices matter; the arithmetic
// here is the whole of it — a pass that took longer than the period
// sleeps not at all, and a laptop that woke eight hours late runs one
// pass rather than ninety-six.
func sleepFrom(ctx context.Context, now func() time.Time, passStart time.Time, every time.Duration) error {
	const slice = 5 * time.Second
	deadline := passStart.Add(every)
	for now().Before(deadline) {
		wait := slice
		if left := deadline.Sub(now()); left < wait {
			wait = left
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil
}

// passHolder is a taken pass lock and the token it stamps onto every
// claim the pass makes.
type passHolder struct {
	token   string
	release func()
}

// takePassLock TRY-locks the pass lock and stamps this process into it.
// It returns held=false with the current holder rather than an error,
// because a second pass is not a failure: it exits 0 naming who is
// working and does not wait.
func takePassLock(ctx context.Context, repo *git.Repo, me record.OwnerID, verb string) (bool, passHolder, error) {
	path, err := lockPath(ctx, repo, passLock)
	if err != nil {
		return false, passHolder{}, err
	}
	// The pass TOKEN is derived from the moment this pass started and the
	// process that started it, so record.Claim.Pass names a pass rather
	// than a process — which is what lets lease.Standing tell this pass's
	// own work from an earlier pass's residue under the same live PID.
	token := fmt.Sprintf("%d-%d", me.PID, me.Since.UnixNano())
	h := holderOf(me, verb)
	release, err := lockfile.Hold(ctx, path, h, 0)
	if err != nil {
		if !errors.Is(err, lockfile.ErrHeld) {
			return false, passHolder{}, err
		}
		other, _, perr := lockfile.Probe(ctx, path)
		if perr != nil {
			other = lockfile.Holder{}
		}
		return false, holderFor(other), nil
	}
	return true, passHolder{token: token, release: release}, nil
}

// holderFor is the losing caller's view of the lock: who has it, with no
// release to call.
func holderFor(h lockfile.Holder) passHolder {
	return passHolder{token: describeHolder(h), release: func() {}}
}

// describe names the holder of a pass lock for the line a losing caller
// prints.
func describe(h passHolder) string { return h.token }

// describeHolder is the sentence a stamp earns. A stamp that could not
// be read leaves "another dockhand", which is honest and still a true
// subject: the lock is held, and by something.
func describeHolder(h lockfile.Holder) string {
	if h.PID == 0 {
		return "another dockhand holds the lock"
	}
	what := h.Verb
	if what == "" {
		what = "a pass"
	}
	if h.Since.IsZero() {
		return fmt.Sprintf("%s (pid %d)", what, h.PID)
	}
	return fmt.Sprintf("%s (pid %d, since %s)", what, h.PID, h.Since.Local().Format("15:04:05"))
}

// cycleOp builds the pass operation and the stager it drains through.
//
// Grants.Invoker is a CONSTANT of the road and never an ambient value:
// record.Human from `cycle`, record.Machine from `dispatch`. The zero
// Driver is unset and is a wiring gap, never a person.
func cycleOp(ctx context.Context, s *Services, me record.OwnerID, passID string, invoker record.Driver, pace publish.Pace) (app.Cycle, *stager, error) {
	repo, err := s.Repo()
	if err != nil {
		return app.Cycle{}, nil, err
	}
	st, err := s.State()
	if err != nil {
		return app.Cycle{}, nil, err
	}
	led, err := s.Ledger()
	if err != nil {
		return app.Cycle{}, nil, err
	}
	// The drain's stager is framed on the HOST's release, because a
	// queued attempt names its own platform and the preflight's frame is
	// per attempt. Framing it here on one release is the honest
	// approximation the Stager's shape allows — see stager.release — and
	// a preflight that could not be read is scheduled as an ordinary
	// build rather than declined, so the cost of the approximation is a
	// known_fail discovered in the guest instead of before it.
	stg := &stager{repo: repo, temp: s.Temp(), session: s.session, release: drainFrame()}
	var ev = evaluatorFor(s)
	return app.Cycle{
		Repo: repo, State: st, Ledger: led, Env: s.PublishEnv(),
		Grants:   app.Grants{Invoker: invoker, Grant: s.Grant},
		Plan:     planningFor(s),
		Eval:     ev,
		Stage:    stg,
		Local:    s.ProposeTree(),
		Verifier: s.VerifyProvider(),
		Pace:     pace,
		Me:       me,
		PassID:   passID,
		Now:      s.Now,
		Progress: sink{w: s.Err},
	}, stg, ctx.Err()
}

// evaluatorFor is change.Reconstruct's evaluator, or nil.
//
// A NIL Eval IS NOT A FAILURE: Reconstruct answers ErrUnreconstructable,
// change.Judge reads that as Unjudged, and Unjudged WITHHOLDS — which is
// exactly what a machine road with no way to re-plan should do.
func evaluatorFor(s *Services) *blobEvaluatorPtr {
	if s.ev == nil {
		return nil
	}
	return &blobEvaluatorPtr{blobEvaluator{ev: s.ev}}
}

// blobEvaluatorPtr is blobEvaluator behind a pointer, so that "no
// evaluator" is a nil the interface value can actually be — a non-nil
// interface holding a zero struct would pass every nil check and panic
// on the first call.
type blobEvaluatorPtr struct{ blobEvaluator }

// drainFrame is the platform frame a DRAIN's preflight is asked under,
// and it is the zero Release on purpose.
//
// A queued attempt names its own platform and the preflight's frame is
// per attempt, but run.Stager's signature is the QUEUE's — a sha and the
// subjects — so a stager built for a whole pass cannot know which
// attempt it is about to stage. The zero Release makes the evaluator use
// its own default frame, which is the host's.
//
// WHAT THE APPROXIMATION COSTS IS BOUNDED AND STATED. The preflight
// answers known_fail and use_xcode, both per-platform options; asked
// under the host's frame for an attempt bound for another release, it
// can miss a known_fail the target release declares. run.Plan then
// schedules the member as an ordinary build and the guest discovers the
// same fact — so the cost is a VM spent, never a wrong verdict, and it
// is exactly the cost Preflight.Read exists to keep from becoming a
// silent decline. A per-attempt frame needs the platform on Stage's
// signature, which is run's to widen.
func drainFrame() platform.Release { return platform.Release{} }

// doctorCmd reports which tools are present and which capabilities they
// enable. It is filed under Setup rather than Reports because it reports
// on the MACHINE and not on the ports — the one verb here that works
// with no checkout at all.
func doctorCmd(s *Services) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report which tools are present and which capabilities they enable",
		Args:  noArgs,
		// RunE rather than Run: the report is what this command produces,
		// and a truncated one written to a file must not exit 0.
		RunE: func(cmd *cobra.Command, _ []string) error {
			// It declares NOTHING. doctor probes the machine's tools: no
			// repository, no tree, no evaluator, no provider, no forge.
			if err := s.Acquire(cmd.Context(), app.Needs{}); err != nil {
				return err
			}
			_, err := fmt.Fprint(s.Out, doctor.Probe(cmd.Context(), s.Tools))
			return err
		},
	}
}
