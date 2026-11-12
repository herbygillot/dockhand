// Package buildenv is the contract between the engine and the providers
// of build environments a check builds in (Design v3 §7), which live
// beneath it: tart, ghactions for github, and script for a person's own
// command. A provider is given a Job, one guest execution's work, and
// reports what happened through a Build; what else it can do, it says by
// the interfaces it implements. Providers import this package and model,
// never the engine, which drives them; the command layer composes them.
package buildenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// Provider builds a plan's targets in one environment.
type Provider interface {
	// Name is how people select it with --on.
	Name() string
	// Execute builds the job's targets in order, recording each target's
	// result through the build as it finishes. An error is trouble with
	// the environment itself; a target that fails to build is a result,
	// not an error.
	Execute(ctx context.Context, job Job, build Build) error
}

// The providers' names: how people select them with --on, what an
// environment records as its provider, and what the configuration names
// their settings by, as [providers.tart].
const (
	// Tart builds in clean Tart VMs of dockhand's images.
	Tart = "tart"
	// GitHub builds with MacPorts' own workflow, in your fork.
	GitHub = "github"
	// Command builds with a person's own script.
	Command = "command"
	// Local builds in a MacPorts prefix on this Mac, /opt/local or one of
	// the person's own (Design v3 §7).
	Local = "local"
)

// DefaultTestTimeout bounds a target's tests where the configuration sets
// none.
const DefaultTestTimeout = 30 * time.Minute

// DefaultBuildTimeout bounds a target's build where the configuration sets
// none: GitHub's cap on a job, ghactions.JobCap, which a build that's run
// that long would meet there too (D16).
const DefaultBuildTimeout = 6 * time.Hour

// Infrastructure is trouble with a provider's environment rather than
// with what it built, a VM that would not start, a script that wrote no
// result: a refusal of kind refusal.Infrastructure, which the runner tries
// again, up to model.MaxAttempts, as it does every refusal another try may
// meet (refusal.Transient); it never repeats a verdict. It wraps what it's
// made of, so ErrNeedsAttention inside it is still found, where
// ErrInfrastructure was a sentinel of its own (R3-6).
func Infrastructure(format string, args ...any) error {
	cause := fmt.Errorf(format, args...)
	return refusal.Wrap(refusal.Infrastructure, cause, "provider infrastructure failed: %s", cause)
}

// ErrNeedsAttention marks trouble another attempt won't fix, inside
// Infrastructure: a guest that refuses dockhand's login refuses it
// again. The runner tries such an execution no more, and says why, where
// it had spent every attempt on it.
var ErrNeedsAttention = errors.New("another attempt won't fix it")

// NeedsAttention is trouble another attempt won't fix, as a guest that
// refuses dockhand's login refuses it again: ErrNeedsAttention to the
// runner, of kind refusal.Infrastructure, in its own words alone, where
// Infrastructure("%w: …", ErrNeedsAttention) said "provider
// infrastructure failed: another attempt won't fix it:" before them and
// the runner said the second half again after (implementation's finding
// at 1efc5b7f).
func NeedsAttention(format string, args ...any) error {
	cause := needsAttention{fmt.Errorf(format, args...)}
	return refusal.Wrap(refusal.Infrastructure, cause, "%s", cause)
}

type needsAttention struct{ error }

func (n needsAttention) Unwrap() error      { return n.error }
func (needsAttention) Is(target error) bool { return target == ErrNeedsAttention }

// NotTriedAgain is trouble no further attempt is made at for a reason
// its words give, as a guest program someone killed: ErrNeedsAttention to
// the runner, which says it as it is, without that error's "another
// attempt won't fix it", since another attempt might (rc7's G9 on the
// M1, 2026-11-02).
func NotTriedAgain(format string, args ...any) error {
	cause := notTriedAgain{fmt.Errorf(format, args...)}
	return refusal.Wrap(refusal.Infrastructure, cause, "%s", cause)
}

type notTriedAgain struct{ error }

func (n notTriedAgain) Unwrap() error      { return n.error }
func (notTriedAgain) Is(target error) bool { return target == ErrNeedsAttention }

// SaysWhyNotTried says whether err is NotTriedAgain's, whose words say
// why no other attempt was made.
func SaysWhyNotTried(err error) bool {
	return errors.As(err, new(notTriedAgain))
}

// ErrDockhandFault marks a fault in dockhand's own handling, rather than in
// the environment or what it built: a guest program reporting what this
// dockhand can't read, or a target its job didn't ask for. Another attempt
// would repeat it, so the runner says it at once, as dockhand's. What's
// marked none of the three is said at once too, as dockhand's: only
// infrastructure is tried again.
var ErrDockhandFault = refusal.New(refusal.Failed, "a fault in dockhand")

// Job is one guest execution's work.
type Job struct {
	Run         model.Run
	Execution   model.GuestExecution
	Revision    model.Revision
	Plan        model.Plan
	Environment model.Environment
	// Targets are the plan's targets still without a complete verdict
	// here, in this environment's order.
	Targets []Target
	// Commit holds the revision's files: the commit itself, or for a
	// snapshot a commit made of its tree on the base, which only the
	// provider sees.
	Commit string
	// Directory is where the execution may keep files, such as logs.
	Directory string
	// Installs are the kept archives of targets the job doesn't build that
	// the ones it builds need: reused, or finished in an earlier attempt.
	// The guest installs each from its archive rather than build it
	// (decision 28). A provider that can't install archives leaves them,
	// and MacPorts gets such a target however it would.
	Installs []Archive
	// Matrix are the environments one run of a MatrixProvider builds,
	// each with its own execution, the targets left to build there, and
	// the Build it records through; the first is the job's Environment,
	// Execution, and Targets. Empty for any other provider.
	Matrix []MatrixCell
	// Launched are the notes the check's earlier attempts on this
	// provider saved before they started work (model.GuestExecution.Launch),
	// oldest first: what a provider that reads no store knows of what the
	// check already launched, as a branch already at the commit that no
	// attempt of the check pushed.
	Launched []string
}

// MatrixCell is one environment of a MatrixProvider's run: its execution,
// the targets left to build there, and the Build that records its results.
type MatrixCell struct {
	Environment model.Environment
	Execution   model.GuestExecution
	Targets     []Target
	Build       Build
}

// A MatrixProvider builds several environments of a check in one run, as
// GitHub's workflow builds every macOS release of its matrix: the runner
// gives it each of its environments together, as Job.Matrix, where it
// gives any other provider one at a time. Each environment's results are
// recorded through its own cell's Build.
type MatrixProvider interface {
	Provider
	Matrix() bool
}

// Unrun is a MatrixProvider's run in which some environments' builds
// never ran, as a job of GitHub's matrix no runner took: those cells are
// left unrecorded, each with why, and the others recorded as they built.
// The runner ends each unrun cell's execution with its words, and spends
// no attempt on it; a run where nothing ran is Infrastructure.
type Unrun struct {
	// Cells are each unrun environment's words, by EnvironmentKey.
	Cells map[string]string
}

func (u *Unrun) Error() string {
	var words []string
	for _, key := range slices.Sorted(maps.Keys(u.Cells)) {
		words = append(words, key+": "+u.Cells[key])
	}
	return strings.Join(words, "; ")
}

// EnvironmentKey is an environment's key in Unrun.Cells.
func EnvironmentKey(environment model.Environment) string {
	return environment.Provider + "/" + environment.Platform.OS + environment.Platform.Version + "/" + environment.Platform.Architecture + "/" + environment.Prefix
}

// Archive is a kept archive a guest installs a target from.
type Archive struct {
	Target model.TargetID
	// Port is the target's port name: its subport's, where it has one.
	Port string
	// Name is MacPorts' file name for the archive, and Digest its sha256,
	// sha256:<hex>.
	Name, Digest string
	// Path is where it is kept on the host.
	Path string
}

// Target is one target of a job, with what it needs built first in the
// job's environment.
type Target struct {
	model.PlanTarget
	DependsOn []model.TargetID
	// Git is the source a target fetched with Git is expected to fetch in
	// the job's environment, from the plan: the commit its git.branch
	// named when the check was planned (batch 20); nil for a port fetched
	// otherwise. A provider that can see what the fetch checked out stops
	// a build that fetched another commit, which built another source, and
	// reports what it fetched (Build.Fetched).
	Git *model.GitSource
	// Tag is the GitHub tag a target's archive is fetched by, from the
	// plan, with the commit it named when the check was planned; nil for
	// a port fetched otherwise. Reuse compares it (reuse.Target.TagMoved).
	Tag *model.GitSource
}

// TargetReport is what a provider saw of one target's build, recorded whole
// (CX-10, where what it observed came by callbacks that had to come before
// its result): the result, and each observation of it, kept apart from
// none where the provider couldn't see it.
type TargetReport struct {
	Result model.TargetResult
	// Fetched is the commit a Git-fetched target's build checked out
	// (Target.Git): the source it built, which its result keeps with its
	// inputs, and which must be the commit the plan expected, or the
	// result fails at fetch. Empty where the provider couldn't see it:
	// the result then stands for its own check, and for no later one.
	Fetched string
	// Active are the ports that were active as the target built, other
	// than the target: inputs of its build, which its result keeps
	// (decision 28). Nil where the provider couldn't see them, and the
	// result's inputs are then unknown; empty where there were none.
	Active []model.ActivePort
	// Sourced are the PortGroup files the target's build loaded, as the
	// environment itself recorded them, each by its blob ID, by its path
	// in the ports tree: its inputs then name those, and what every port
	// reads of _resources, in place of all of _resources (reuse.Inputs).
	// Nil where the provider couldn't see them, and empty where it loaded
	// none.
	Sourced map[string]model.ObjectID
	// Links are what a passed target's installed Mach-O files link under
	// the prefix (model.TargetResult.Links): an observation, not an input,
	// so an unknown stops no reuse. Nil where the provider couldn't read
	// them, empty where nothing links.
	Links []model.Link
	// Declared is what the target's declared ports install that a build
	// could link (model.TargetResult.Declared); nil where unread.
	Declared *model.Declared
	// Installed are the files a passed target installed, and Libraries its
	// own dynamic libraries' install names (model.TargetResult's): an
	// observation, which a later check of its next version compares. Nil
	// where the provider couldn't read them.
	Installed []string
	Libraries []model.Library
	// Configure are its configure phase's summary lines
	// (model.TargetResult.Configure).
	Configure []string
}

// Reported is a result as a report with nothing seen beside it, as for a
// target that was blocked or never built.
func Reported(result model.TargetResult) TargetReport { return TargetReport{Result: result} }

// ErrNotLaunched is a launch refused under the lock: the check's cancel is
// recorded, or it is another driver's now, so nothing was started.
var ErrNotLaunched = errors.New("nothing was launched: the check's cancel is recorded, or another process drives it now")

// LaunchLock is an attempt's launch lock as Stop takes it, the lock every
// launch of the attempt and every child that can start work holds: once
// Stop holds it, nothing more can be launched for the attempt, since a
// launcher that takes it later reads the cancel and starts nothing.
type LaunchLock interface {
	// Hold takes the lock within the bound, and release lets it go. An
	// error is a lock still held by something alive, named where the
	// launcher recorded itself; the lock is never taken from it.
	Hold(ctx context.Context, within time.Duration) (release func(), err error)
}

// StopRequest is what Stop is asked of: one attempt of a check (Execution),
// every attempt of the check on the provider, that one among them, whose
// launches a stop answers for together, and the asked attempt's launch
// lock, which Stop takes before it reads what was launched.
type StopRequest struct {
	Execution model.GuestExecution
	Attempts  []model.GuestExecution
	Lock      LaunchLock
}

// Stopper is a provider whose work can outlive its driver, as GitHub's
// workflow runs on when a resumable driver stops, so the next can find it
// again. Stop ends the work an execution recorded and gives one of three
// answers (the lifecycle design's "Stopping"): no error and nothing left
// is ended; no error and something left is ended, each thing named with
// what removes it; an error is not known, in the provider's words, and
// the cancel stays requested. Every cancel asks it, by whoever settles
// the check, before the check is called canceled (#115, #193).
type Stopper interface {
	Provider
	Stop(ctx context.Context, request StopRequest) (left []string, err error)
}

// LinkReader is a provider that reads what a passed build's binaries link
// (TargetReport.Links), as Tart's guest does: where its results' links
// are nil, they couldn't be read, where another provider's are nil
// because it never reads them.
type LinkReader interface {
	ReadsLinks() bool
}

// Build is how a provider learns what to skip and records what happened.
type Build interface {
	// Blocked names a changed dependency of the target that did not pass,
	// when there is one; the provider records the target as blocked
	// rather than building it.
	Blocked(target model.TargetID) (model.TargetID, bool)
	// Record checkpoints one target's report, its result with what its
	// provider saw of its build. A complete verdict is final. An error is
	// the store's: the result wasn't recorded, and the provider stops,
	// whose execution then ends as an infrastructure failure and spends an
	// attempt, since what it built can't be said.
	Record(report TargetReport) error
	// Keep keeps the archive a target's build made, after its Record, for
	// a later build of a target that needs it to install rather than build
	// it again (decision 28). fetch writes the archive whole to the path
	// it is given, in the file system of the store it is kept in, and it
	// is kept only if it matches the digest the result reported. One kept
	// already isn't fetched again. A provider that can't fetch archives
	// doesn't call it.
	Keep(target model.TargetID, name string, fetch func(path string) error) error
	// KeepDependency keeps the archive a port active as a target built was
	// installed from, after the target's Record, for a later guest in the
	// environment to install rather than build (batch 90): rust and cargo,
	// built from source where MacPorts had no archive for the release yet.
	// fetch writes it as Keep's does, and it is kept only if it matches the
	// digest Consumed reported of the port. A port the plan builds is a
	// target, whose archive Keep keeps, and isn't kept this way. A
	// provider that can't fetch archives doesn't call it.
	KeepDependency(port model.ActivePort, name string, fetch func(path string) error) error
	// Progress reports a step to whoever is watching.
	Progress(message string)
	// Waiting reports a wait on something outside the build, such as the
	// Mac's VM slots, which is said at every level: a silent wait reads
	// as a hang (rc1's D-R2).
	Waiting(message string)
	// Holding is the check running in one of this provider's VMs, by the
	// name Refer recorded for it, as "check-616"; empty where none of
	// this database's is.
	Holding(ref string) string
	// Observe records what the environment reported about itself, for
	// the pull request's Tested on.
	Observe(observed model.Observed) error
	// Refer records the provider's own name for this run: a workflow
	// run's URL, a VM clone's name. dockhand logs finds the run by it.
	Refer(ref string) error
	// Launch starts the attempt's work under its launch lock, the one
	// call every provider starts work through (the lifecycle design's
	// "Launching and stopping exclude each other"). Holding the lock, it
	// reads again that the check is still this driver's and that no
	// cancel is recorded, saves note as the attempt's Launch, and only
	// then runs start, whose context carries the lock's file for every
	// child start forks to hold (filelock.Carried). The note start
	// returns, where it changed, is saved before the lock is let go.
	// ErrNotLaunched where the check isn't this driver's any more or its
	// cancel is recorded: start isn't run.
	Launch(note string, start func(ctx context.Context) (string, error)) error
	// Canceled reports, once the context is done, that the run was
	// canceled or interrupted for good, rather than its driver stopping
	// with the run left for the next one: only then does a provider stop
	// work it started elsewhere.
	Canceled() bool
}

// Detailer is a Build that takes a step said only at --verbose, beside
// Progress's: each archive a guest is given, where the default says them
// once (the omnidiff session's note 9).
type Detailer interface {
	Detail(message string)
}

// Detail reports a step at --verbose where the build takes one, and
// nowhere otherwise.
func Detail(build Build, message string) {
	if detailer, ok := build.(Detailer); ok {
		detailer.Detail(message)
	}
}

// A ReleaseProvider builds on releases a person names, as Tart does: each
// release --on <provider>:<releases> selects is an environment of its own.
type ReleaseProvider interface {
	Provider
	// Environments are the environments the releases select: the
	// provider's default with none, as Tart builds on the Mac's own
	// release. Each states its developer tools where the provider knows
	// them.
	Environments(ctx context.Context, releases string) ([]model.Environment, error)
}

// A Remedier is a provider that can say how to give an environment what an
// unmet target needs: Tart, the command that makes a release's Xcode image.
type Remedier interface {
	Provider
	Remedy(unmet model.Unmet) string
}

// An OwnTestsProvider runs a port's declared tests whatever a check's
// policy, as GitHub's workflow does: it is MacPorts' own, and dockhand
// doesn't change it. Under --tests skip, the tests still run there and
// don't count.
type OwnTestsProvider interface {
	Provider
	RunsOwnTests() bool
}

// A MacPortsProvider says where an environment builds with an older
// MacPorts than this Mac's, which reads its ports, as a Tart image keeps
// the MacPorts it was made with: the environment's version and this
// Mac's, or empty where it isn't behind or can't be told.
type MacPortsProvider interface {
	Provider
	MacPortsBehind(ctx context.Context, environment model.Environment) (environmentVersion, hostVersion string)
}

// An IdentityProvider says what an environment is made from and with: its
// identity by origin (decision 28), such as a Tart image's source by
// digest, the setup that made it, its tools and MacPorts, and the guest
// program's protocol. An environment of the same identity builds a port as
// another did, so its results stand for it; one of another doesn't. It is
// the environment as it is now, read without starting anything; empty when
// the provider can't say, as for an image made before origins were
// recorded.
type IdentityProvider interface {
	Provider
	Identity(ctx context.Context, environment model.Environment) (string, error)
}

// An IdentityExplainer says, in words for a person, what changed between
// an identity it recorded and its identity now, as a clause ("dockhand
// has begun to build each target from its source"), or nothing where it
// can't say, and the engine says the environment was made again.
type IdentityExplainer interface {
	IdentityChange(environment model.Environment, recorded, now string) string
}

// A LeftoverProvider makes environments that can outlast the process that
// made them, as a Tart clone does when the process checking in it dies and
// no later attempt of its run comes to remove it. It lists them, each by
// the reference its execution recorded, and removes one the engine has
// found no process using.
type LeftoverProvider interface {
	Provider
	// Leftovers are the environments it made that are still there.
	Leftovers(ctx context.Context) ([]Leftover, error)
	// RemoveLeftover removes one, and refuses anything it didn't make for
	// a check.
	RemoveLeftover(ctx context.Context, ref string) error
}

// A ChangingProvider changes something of the person's for a check and
// puts it back after, as the local provider does the prefix it builds in.
// Changes are what its checks have changed and not yet put back: each one
// building now, and each a check left changed when its process ended
// first, with how to put it back, which status says.
type ChangingProvider interface {
	Provider
	Changes(ctx context.Context) ([]Change, error)
}

// Change is something of the person's a check changed: Check names the
// check, check-12, and Place what it changed, /opt/local. Building is a
// check building there now, which puts it back when it ends; Kept one
// that kept what it built there, check --keep's; otherwise it stopped
// before it could put it back. Remedy is how to put it back, "dockhand
// undo check-12".
type Change struct {
	Check, Place string
	Building     bool
	Kept         bool
	Remedy       string
	// Holder is who holds the place for its check where the provider
	// keeps a lock on it, as a check waiting on it says: "check-12 (pid
	// 4711)"; empty where none is held.
	Holder string
}

// A PuttingBackProvider puts back what a check of its changed and left, as
// dockhand undo check-N asks: kept, or stopped before it could put it
// back. It says where, and false where the check left nothing to put
// back.
type PuttingBackProvider interface {
	ChangingProvider
	PutBack(ctx context.Context, check string, progress func(string)) (place string, found bool, err error)
}

// An InstalledProvider says what an environment has installed already, for
// a check's estimate of what it installs: a local prefix's active ports,
// and the global variants its variants.conf sets, under which a dependency
// isn't the archive MacPorts' builders made. Tart's and GitHub's start
// each build from MacPorts with no ports and no global variants, by their
// setup; a command provider's script says neither, so its environments
// get no estimate.
type InstalledProvider interface {
	Provider
	Installed(ctx context.Context, environment model.Environment) (active, variants []string, err error)
}

// A ParallelProvider can build several of one check's environments at once,
// as Tart builds two releases in the Mac's two VMs. The runner builds at
// most Parallel of its environments together; a provider that isn't one
// builds its environments one at a time.
type ParallelProvider interface {
	Provider
	Parallel() int
}

// A CacheProvider keeps downloads its environments are made from, as Tart
// keeps the vanilla images it pulled for dockhand, and removes those unused
// for a while (decision 36).
type CacheProvider interface {
	Provider
	// Storage is where the cache is, whose volume's free space cleanup
	// watches.
	Storage() (string, error)
	// PruneCache removes what has gone unused for longer than unused, and
	// says what it removed.
	PruneCache(ctx context.Context, unused time.Duration) ([]string, error)
}

// Leftover is an environment a provider made for a check that is still
// there, such as a Tart clone.
type Leftover struct {
	// Provider made it, and sets it; Ref is its name for it, as the
	// check's execution recorded it; What says it for a person, "Tart
	// clone dockhand-…".
	Provider string
	Ref      string
	What     string
	// KeptAt is when a check asked to keep it (model.Plan.Keep) kept it,
	// for dockhand shell; zero for one left by a process that died.
	// Failed is a clone kept unasked, for a target's failure in it
	// (providers.tart.keep_failed).
	KeptAt time.Time
	Failed bool
}

// ShellProvider starts a clone a check kept (Leftover.KeptAt) for a person
// to look in: Ref is it, Portfile the port's, as the plan names it,
// Command what to run in its work directory, a shell where it's empty,
// and Input and Output its streams, on a terminal the guest gives it
// where Terminal. It returns the command's exit status.
type ShellProvider interface {
	Shell(ctx context.Context, job ShellJob) (int, error)
}

// ShellJob is one command in a kept clone (ShellProvider).
type ShellJob struct {
	Ref, Portfile string
	Command       []string
	// Running is a running check's guest, Environment's, which the check
	// owns: reached as it is, never started or stopped.
	Running     bool
	Environment model.Environment
	// Test, where it's set, is the port's directory as it is now, which
	// replaces the staged one, and the test phase alone runs again, on
	// the build as it stands (port -o), with Variants: the command then.
	Test     string
	Variants string
	Input    io.Reader
	Output   io.Writer
	Terminal bool
}

// Fork is your fork of MacPorts' repository, and the Git remote that
// pushes to it: where the github provider pushes a check's commit, and
// submit a branch.
type Fork struct {
	// Repository is its GitHub name, such as ada/macports-ports.
	Repository string
	Remote     string
	PushURL    string
}

// CheckBranchPrefix names the branches a check pushes to your fork, as the
// github provider does. The provider removes each once the check is done
// with its run; clean removes a merged branch's that are left.
const CheckBranchPrefix = "dockhand-check/"

// CheckBranch is a check's own branch in your fork: the first twelve
// characters of the commit and the whole random part of the check's ID,
// the same for its matrix and every attempt, so no other check pushes it
// and every run on it is this check's (the lifecycle design's "GitHub").
// clean finds a check's branch by it too.
func CheckBranch(commit string, check model.RunID) string {
	id := string(check)
	if i := strings.LastIndex(id, "_"); i >= 0 {
		id = id[i+1:]
	}
	return CheckBranchPrefix + commit[:min(12, len(commit))] + "-" + id
}

// LegacyCheckBranch is the branch a check pushed before branches were the
// check's own, named for the commit alone, which clean still removes.
func LegacyCheckBranch(commit string) string {
	return CheckBranchPrefix + commit[:min(12, len(commit))]
}

// LogName is a target's log's name in its build's log directory, and in
// a Tart guest,
// by its target, as luminance-hdr.log or s2n-tls+tests.log: named for its
// place in the job, target-3.log, nothing in a multi-port check's
// directory said which log was whose (the exiv2 session's notes,
// 2026-10-21). A character a file name shouldn't hold is written _.
func LogName(id model.TargetID) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r) {
			return r
		}
		return '_'
	}, string(id))
	if name == "" || strings.HasPrefix(name, ".") {
		name = "_" + name
	}
	return name + ".log"
}
