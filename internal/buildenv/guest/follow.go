// Package guest is the program dockhand's builds run where they build
// (Design v3 §7), guest.tcl, and the following of what it writes: its
// input and results, and the Follower that records each target's result
// as the program finishes it, so a program lost midway keeps what it
// finished (decision 44). Tart's provider runs it in a clone, and the
// local provider in a MacPorts prefix on this Mac; each reaches the
// program's directory through a Guest, and its process through a Run.
package guest

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// Program is the guest program, guest.tcl, which its provider stages
// beside its input.
//
//go:embed guest.tcl
var Program []byte

// Guest is where the program runs, as its provider reaches it: a clone
// over dockhand's SSH channel, or this Mac's own files.
type Guest interface {
	Command(ctx context.Context, input io.Reader, args ...string) ([]byte, error)
	Attach(ctx context.Context, input io.Reader, output io.Writer, terminal bool, directory string, args ...string) (int, error)
	Upload(ctx context.Context, local, path string, sudo bool) error
	Read(ctx context.Context, path string, sudo bool) ([]byte, error)
	Download(ctx context.Context, path, local string, sudo bool) error
	Append(ctx context.Context, path, local string, sudo bool) error
	Close(ctx context.Context)
}

// Run is what the program runs in, as its provider started it: a clone's
// VM, or the program's own process.
type Run interface {
	Done() <-chan struct{}
	Err() error
	Stop(ctx context.Context, grace time.Duration) error
}

// Follower reads the program's results in Root as they grow and records
// them: Prefix is the MacPorts it builds with, whose software directory a
// passed target's archive is kept from, and Poll how often the results
// are read, 10 seconds when zero.
type Follower struct {
	Prefix string
	Root   string
	Poll   time.Duration
	// Stopped says whether the program has exited, where its Run going on
	// doesn't say, as a clone's VM outlives it; nil where the program's
	// end is its Run's, as a process's is, and what it wrote last is read
	// once that ends.
	Stopped func(ctx context.Context, g Guest) (bool, error)
	// Drift reports where the guest's tools differ from the facts table's
	// row for its release, as a clone of dockhand's image should match.
	Drift bool
	// KeepDependencies keeps the archives a target's dependencies were
	// installed from, for a later guest to install (batch 90): a clone's,
	// never the person's own ports'.
	KeepDependencies bool
}

func (f *Follower) poll() time.Duration {
	if f.Poll > 0 {
		return f.Poll
	}
	return 10 * time.Second
}

func (f *Follower) prefix() string {
	if f.Prefix != "" {
		return f.Prefix
	}
	return macports.DefaultPrefix
}

// Protocol is the guest program's input and results format.
const Protocol = 1

// Input is the guest program's input.
type Input struct {
	Protocol    int            `json:"protocol"`
	Run         string         `json:"run"`
	Attempt     int            `json:"attempt"`
	Prefix      string         `json:"prefix"`
	Platform    model.Platform `json:"platform"`
	Tests       string         `json:"tests"`
	TestTimeout int            `json:"test_timeout"`
	// BuildTimeout and LintTimeout are seconds, as TestTimeout is.
	BuildTimeout int      `json:"build_timeout"`
	LintTimeout  int      `json:"lint_timeout"`
	Targets      []Target `json:"targets"`
	// Archives are the kept archives the guest installs targets from
	// rather than build them: in ArchiveSite, each in its port's
	// directory beside its signatures, which the keys at ArchiveKeys
	// verify. The guest program makes them an archive site of MacPorts'.
	Archives    []Archive `json:"archives,omitempty"`
	ArchiveSite string    `json:"archive_site,omitempty"`
	ArchiveKeys []string  `json:"archive_keys,omitempty"`
	// Local is a check in a MacPorts prefix of the person's, on this Mac
	// (Local); nil in a clone.
	Local *Local `json:"local,omitempty"`
}

// Local is how a check in a prefix of the person's leaves it: Clean
// builds each target with all but its dependencies deactivated, as in a
// clone, where the prefix is one dockhand may treat as its own; Keep
// leaves what it built active; Note is what its port snapshot is noted
// with. Restore runs the program only to put back the prefix a check
// left changed.
type Local struct {
	Clean   bool   `json:"clean"`
	Keep    bool   `json:"keep"`
	Note    string `json:"note"`
	Restore bool   `json:"restore,omitempty"`
}

// Archive is a kept archive in the guest: the port it is of, and
// MacPorts' file name for it.
type Archive struct {
	Port string `json:"port"`
	Name string `json:"name"`
}

type Target struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Portfile string          `json:"portfile"`
	Variants map[string]bool `json:"variants,omitempty"`
	// DependsOn are the targets this one needs, among this run's.
	DependsOn []string `json:"depends_on,omitempty"`
	// Blocked is a target whose changed dependency failed in an earlier
	// attempt: the guest records it blocked without building it.
	Blocked bool `json:"blocked,omitempty"`
	// Git is what a Git-fetched target's fetch must check out; absent for
	// a port fetched otherwise.
	Git *Git `json:"git,omitempty"`
	// Log is the name of its log, by its target (logName).
	Log string `json:"log"`
	// Added is a port the branch adds, which lint checks with --nitpick
	// (the docs dig's I13).
	Added bool `json:"added,omitempty"`
}

// Git is what a Git-fetched target's fetch must check out (batch 20):
// its ref, for the guest's words, and the commit the ref named when the
// check was planned, or the abbreviation that commit begins with. Expect
// is empty where the plan couldn't resolve the ref, and the guest only
// reports what the fetch checked out.
type Git struct {
	Ref    string `json:"ref"`
	Expect string `json:"expect,omitempty"`
}

// Results is what the guest program writes as it goes.
type Results struct {
	Protocol    int               `json:"protocol"`
	State       string            `json:"state"`
	Detail      string            `json:"detail"`
	Environment map[string]string `json:"environment"`
	// Building is the target the program is building, until its result
	// is written; empty between targets. Phase is the step of its build
	// under way, as lint, fetch, or install; empty where the guest program
	// says none, as before it said them.
	Building string `json:"building"`
	Phase    string `json:"phase"`
	// FromSource are the dependencies of the target building that have no
	// binary archive for the guest, which install builds from source.
	FromSource []string `json:"from_source"`
	Targets    []Result `json:"targets"`
}

type Result struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Phase   string `json:"phase"`
	Tests   string `json:"tests"`
	Log     string `json:"log"`
	Detail  string `json:"detail"`
	// Active are the ports that were active as the target built; absent
	// where the guest didn't record them, and empty where there were none.
	Active []Port `json:"active"`
	// Archive is the digest of the target's own archive, and ArchiveFile
	// where it is in the guest.
	Archive     string `json:"archive"`
	ArchiveFile string `json:"archive_file"`
	// Fetched is the commit a Git-fetched target's fetch checked out;
	// absent where the guest couldn't read it.
	Fetched string `json:"fetched,omitempty"`
	// Steps are where each step of the build began in its log, in the
	// order they ran; absent for a target not built.
	Steps []Step `json:"steps,omitempty"`
	// Transient is the line of a failed step's log that says the network
	// failed it upstream, as a module proxy's stream reset; the build is
	// tried again, as infrastructure.
	Transient string `json:"transient,omitempty"`
	// Dependency is the dependency whose failure failed the target at its
	// dependencies phase, as MacPorts named it, and DependencyVersion its
	// version and revision as the ports tree has them, 15.2.0_1; absent
	// where MacPorts' line didn't say, or the tree didn't.
	Dependency        string `json:"dependency,omitempty"`
	DependencyVersion string `json:"dependency_version,omitempty"`
	// Links are what a passed target's installed Mach-O files link under
	// the prefix; absent where the guest couldn't read them, and empty
	// where nothing links.
	Links []Link `json:"links"`
	// Installed are the files a passed target installed, as port contents
	// lists them, and Libraries its own dynamic libraries' install names;
	// absent where the guest couldn't read them (Then's item 9).
	Installed []string  `json:"installed"`
	Libraries []Library `json:"libraries"`
	// Configure are its configure phase's summary lines, as its log said
	// them (Then's item 10); absent where they weren't read.
	Configure []string `json:"configure"`
	// Lint is what lint warned of, and Nitpick says it ran with
	// --nitpick (the docs dig's I13); Lint is absent where it wasn't read.
	Lint    []string `json:"lint"`
	Nitpick bool     `json:"nitpick"`
}

// Library is one of a target's own dynamic libraries: its file, its
// LC_ID_DYLIB install name, and its compatibility version.
type Library struct {
	File          string `json:"file"`
	Name          string `json:"name"`
	Compatibility string `json:"compatibility"`
}

// Link is a library a target's binary links, as the guest saw it,
// with the port that provides it.
type Link struct {
	Binary  string `json:"binary"`
	Library string `json:"library"`
	Port    string `json:"port"`
}

// Step is where a step of a target's build began in its log: the
// line its output starts on, counting from 1.
type Step struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// Port is a port active as a target built, as the guest saw it.
type Port struct {
	Name      string `json:"name"`
	Spec      string `json:"spec"`
	Directory string `json:"directory"`
	Archive   string `json:"archive"`
	// ArchiveFile is where its archive is in the guest; absent where the
	// guest kept none.
	ArchiveFile string `json:"archive_file,omitempty"`
}

// Targets are the job's targets as the guest program reads them, each
// marked blocked where a changed dependency of it didn't pass.
func Targets(job buildenv.Job, build buildenv.Build) []Target {
	var targets []Target
	for _, target := range job.Targets {
		name := target.Target.Name
		if target.Target.Subport != "" {
			name = target.Target.Subport
		}
		t := Target{ID: string(target.ID), Name: name, Portfile: target.Target.Portfile, Variants: target.Target.Variants, Log: buildenv.LogName(target.ID), Added: target.Added}
		for _, dependency := range target.DependsOn {
			t.DependsOn = append(t.DependsOn, string(dependency))
		}
		if target.Git != nil {
			t.Git = &Git{Ref: target.Git.Ref, Expect: target.Git.Expected()}
		}
		_, t.Blocked = build.Blocked(target.ID)
		targets = append(targets, t)
	}
	return targets
}

// LintTimeout bounds a target's lint, which takes seconds (D16).
const LintTimeout = 10 * time.Minute

// follow reads the guest's results as they grow, recording each target's
// as it appears with its log, until the program finishes. A program that
// stops without finishing, a VM that stops, or a guest that can't be read
// for a minute is infrastructure trouble; what was recorded stays, and the
// target the guest was building is recorded as interrupted.
func (f *Follower) Follow(ctx context.Context, g Guest, started Run, job buildenv.Job, build buildenv.Build, release macos.Release) error {
	recorded := 0
	failures := 0
	observed := false
	// building is the target the guest said it was building when last
	// read, and said is the progress line last said of it, which is said
	// again only once it changes: at most a line a read, and one a step
	// (SQL-S4's coalescing).
	building, said := "", ""
	// A lost guest's partial log of what it was building is fetched where
	// it still answers, as when its program stopped or errored; a stopped
	// VM's isn't asked for.
	lost := func(err error) error {
		return interrupted(job, build, building, err, func(name string) string { return f.partialLog(ctx, g, job, name) })
	}
	// take takes in what the guest has written: what it reported about
	// itself, once, and each target's result not yet recorded. Every read
	// goes through it, the last one too, since the guest writes its
	// facts only with its first result.
	take := func(results Results) error {
		if !observed && results.Environment["macos"] != "" {
			observed = true
			if message := driftReport(release, job.Environment.DeveloperTools, results.Environment); message != "" && f.Drift {
				build.Progress(message)
			}
			if err := build.Observe(reported(results.Environment)); err != nil {
				return err
			}
		}
		for ; recorded < len(results.Targets); recorded++ {
			if err := f.record(ctx, g, job, build, results.Targets[recorded]); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-started.Done():
			if f.Stopped == nil {
				// The program's end is its process's: what it wrote last
				// is read, as when it stopped below.
				return f.ended(ctx, g, job, build, take, building, lost)
			}
			return interrupted(job, build, building, buildenv.Infrastructure("the VM stopped: %v", started.Err()), nil)
		case <-time.After(f.poll()):
		}
		results, err := f.Read(ctx, g)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if failures++; time.Duration(failures)*f.poll() >= time.Minute {
				return lost(buildenv.Infrastructure("reading the guest's results: %w", err))
			}
			continue
		}
		failures = 0
		// The acceptance harness's X2 row, D-I7, makes a read fail once,
		// as a fault of dockhand's own or an error nothing classifies,
		// neither tried again, or as infrastructure, which is.
		switch failpoint.Fails("tart.results") {
		case "fault":
			return buildenv.ErrDockhandFault.Because("the guest's results were read wrong, as a failpoint asked")
		case "error":
			return errors.New("the guest's results were lost, as a failpoint asked")
		case "infrastructure":
			return lost(buildenv.Infrastructure("the guest stopped answering, as a failpoint asked"))
		}
		if results.Protocol != Protocol {
			return buildenv.ErrDockhandFault.Because("the guest program wrote protocol %d results; this dockhand reads %d", results.Protocol, Protocol)
		}
		if err := take(results); err != nil {
			return err
		}
		building = results.Building
		// What it's building has its log copied as it grows, for logs
		// --follow, in any process, where the log came back only when
		// the target ended (the gnome-voice session's notes, 2026-10-20).
		if building != "" {
			f.liveLog(ctx, g, job, model.TargetID(building))
		}
		if line := buildingWords(job, release, results); line != said {
			said = line
			if line != "" {
				build.Progress(line)
			}
		}
		switch results.State {
		case "finished":
			// A failure the network made upstream is the environment's, not
			// the port's: the build is tried again (dolt's go build on
			// proxy.golang.org's stream reset, field testing at 23c5e2aa).
			for _, target := range results.Targets {
				if target.Outcome == string(model.OutcomeFailed) && target.Transient != "" {
					return refusal.New(refusal.Network, "%s's build met a network fault upstream: %s", target.ID, target.Transient)
				}
			}
			return nil
		case "errored":
			return lost(buildenv.Infrastructure("the guest: %s", results.Detail))
		case "refused":
			// What the prefix holds keeps the build from being the
			// branch's, which another attempt won't change.
			return buildenv.Infrastructure("%w: %s", buildenv.ErrNeedsAttention, results.Detail)
		}
		if f.Stopped == nil {
			continue
		}
		if gone, err := f.Stopped(ctx, g); err == nil && gone {
			// It may have written its last results after the read above.
			if last, err := f.Read(ctx, g); err == nil && last.Protocol == Protocol {
				if err := take(last); err != nil || last.State == "finished" {
					return err
				}
				building = last.Building
			}
			log, _ := g.Read(ctx, f.Root+"/runner.log", true)
			return lost(buildenv.Infrastructure("the guest program stopped before it finished: %s", strings.TrimSpace(tail(string(log), 400))))
		}
	}
}

// ended reads what a local guest program wrote last, once its process
// has ended: its finish, or why it stopped short.
func (f *Follower) ended(ctx context.Context, g Guest, job buildenv.Job, build buildenv.Build, take func(Results) error, building string, lost func(error) error) error {
	last, err := f.Read(ctx, g)
	if err != nil || last.Protocol != Protocol {
		return lost(buildenv.Infrastructure("the guest program ended without its results: %v", err))
	}
	if err := take(last); err != nil {
		return err
	}
	switch last.State {
	case "finished":
		for _, target := range last.Targets {
			if target.Outcome == string(model.OutcomeFailed) && target.Transient != "" {
				return refusal.New(refusal.Network, "%s's build met a network fault upstream: %s", target.ID, target.Transient)
			}
		}
		return nil
	case "refused":
		return buildenv.Infrastructure("%w: %s", buildenv.ErrNeedsAttention, last.Detail)
	case "errored":
		return interrupted(job, build, last.Building, buildenv.Infrastructure("the guest: %s", last.Detail), func(name string) string { return f.partialLog(ctx, g, job, name) })
	}
	log, _ := g.Read(ctx, f.Root+"/runner.log", true)
	return lost(buildenv.Infrastructure("the guest program stopped before it finished: %s", strings.TrimSpace(tail(string(log), 400))))
}

// buildingWords is what the guest is building, and how far it is, as
// "building jq (2 of 5): fetch"; empty between targets.
func buildingWords(job buildenv.Job, release macos.Release, results Results) string {
	if results.Building == "" {
		return ""
	}
	name := results.Building
	if i := slices.IndexFunc(job.Targets, func(target buildenv.Target) bool { return string(target.ID) == results.Building }); i >= 0 {
		name = job.Targets[i].Target.Name
		if len(job.Targets) > 1 {
			name += fmt.Sprintf(" (%d of %d)", i+1, len(job.Targets))
		}
	}
	line := "building " + name
	if results.Phase != "" {
		line += ": " + results.Phase
	}
	// What builds from source takes the time, and is said before it does,
	// as "rust, cargo: no archive for macOS 27 arm64; building from source".
	if len(results.FromSource) > 0 {
		line += fmt.Sprintf("; %s: no archive for macOS %s %s; building from source", strings.Join(results.FromSource, ", "), release.Product, job.Environment.Platform.Architecture)
	}
	return line
}

// interrupted records the target a lost guest was building as
// interrupted, with why it was lost, and returns why. The runner tries it
// again, as it does a target the guest never reached; the result says the
// guest got as far as building it, with the log it had written, where
// fetch brings it: exiv2's check-448 kept an empty directory for its
// interrupted first attempt, and nothing said why (the exiv2 field notes,
// 2026-10-19).
func interrupted(job buildenv.Job, build buildenv.Build, building string, cause error, fetch func(name string) string) error {
	i := slices.IndexFunc(job.Targets, func(target buildenv.Target) bool { return string(target.ID) == building })
	if i < 0 {
		return cause
	}
	// What the environment's trouble was, without the refusal's own
	// words around it.
	detail := cause.Error()
	if r := refusal.Of(cause); r != nil && errors.Unwrap(r) != nil {
		detail = errors.Unwrap(r).Error()
	}
	result := model.TargetResult{Target: job.Targets[i].ID, Outcome: model.OutcomeInterrupted, Detail: detail}
	if fetch != nil {
		// The guest names each target's log by its target.
		result.Log = fetch(buildenv.LogName(job.Targets[i].ID))
	}
	if err := build.Record(buildenv.Reported(result)); err != nil {
		return fmt.Errorf("%w; %s wasn't recorded as interrupted: %v", cause, building, err)
	}
	return cause
}

func (f *Follower) Read(ctx context.Context, g Guest) (Results, error) {
	var results Results
	data, err := g.Read(ctx, f.Root+"/results.json", true)
	if err != nil {
		return results, err
	}
	return results, json.Unmarshal(data, &results)
}

// record copies a target's log out of the guest and records its result.
// partialLog fetches a log the guest was still writing into the job's
// directory, within half a minute, and says where it is; empty where the
// guest didn't give it.
func (f *Follower) partialLog(ctx context.Context, g Guest, job buildenv.Job, name string) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	local := filepath.Join(job.Directory, name)
	if err := g.Download(ctx, f.Root+"/"+name, local, true); err != nil {
		return ""
	}
	return local
}

// liveLog copies what the log of the target building has grown by since
// the last read into the job's directory, by its name (buildenv.LogName),
// within the poll's time; a copy that can't be made waits for the next
// read. The finished log's Download replaces it.
func (f *Follower) liveLog(ctx context.Context, g Guest, job buildenv.Job, id model.TargetID) {
	if job.Directory == "" || os.MkdirAll(job.Directory, 0o755) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, max(f.poll(), 5*time.Second))
	defer cancel()
	name := buildenv.LogName(id)
	_ = g.Append(ctx, f.Root+"/"+name, filepath.Join(job.Directory, name), true)
}

func (f *Follower) record(ctx context.Context, g Guest, job buildenv.Job, build buildenv.Build, got Result) error {
	var target buildenv.Target
	found := false
	for _, t := range job.Targets {
		if string(t.ID) == got.ID {
			target, found = t, true
		}
	}
	if !found {
		return buildenv.ErrDockhandFault.Because("the guest reported %q, which this job didn't ask for", got.ID)
	}
	result := model.TargetResult{Target: target.ID, Outcome: model.Outcome(got.Outcome), Phase: model.Phase(got.Phase), Tests: model.TestOutcome(got.Tests)}
	switch result.Outcome {
	case model.OutcomePassed, model.OutcomeFailed, model.OutcomeBlocked:
	default:
		return buildenv.ErrDockhandFault.Because("the guest reported %s %q", got.ID, got.Outcome)
	}
	if result.Outcome != model.OutcomeFailed {
		result.Phase = ""
	}
	if got.Lint != nil {
		result.Lint = slices.Clone(got.Lint)
	}
	result.Nitpicked = got.Nitpick
	if result.Phase == model.PhaseDependencies {
		result.Dependency = model.FailedDependency{Name: got.Dependency, Version: got.DependencyVersion}
	}
	if result.Tests == "" {
		result.Tests = model.TestsNone
	}
	if got.Log != "" && filepath.Base(got.Log) == got.Log {
		local := filepath.Join(job.Directory, got.Log)
		if err := g.Download(ctx, f.Root+"/"+got.Log, local, true); err == nil {
			result.Log = local
			// Where each step began is a place in this log, kept with it.
			for _, step := range got.Steps {
				result.Steps = append(result.Steps, model.LogStep{Name: model.Step(step.Name), Line: step.Line})
			}
		}
	}
	report := buildenv.TargetReport{Fetched: got.Fetched}
	if got.Fetched != "" && !git.ValidObjectID(got.Fetched) {
		return buildenv.ErrDockhandFault.Because("the guest reported %s fetched %q, which isn't a commit", got.ID, got.Fetched)
	}
	if got.Active != nil {
		report.Active = make([]model.ActivePort, len(got.Active))
		for i, port := range got.Active {
			report.Active[i] = model.ActivePort{Name: port.Name, Spec: port.Spec, Directory: port.Directory, Archive: port.Archive}
		}
	}
	if got.Links != nil {
		report.Links = make([]model.Link, len(got.Links))
		for i, link := range got.Links {
			report.Links[i] = model.Link{Binary: link.Binary, Library: link.Library, Port: link.Port}
		}
	}
	if got.Installed != nil {
		report.Installed = slices.Clone(got.Installed)
	}
	if got.Configure != nil {
		report.Configure = slices.Clone(got.Configure)
	}
	if got.Libraries != nil {
		report.Libraries = make([]model.Library, len(got.Libraries))
		for i, library := range got.Libraries {
			report.Libraries[i] = model.Library{File: library.File, Name: library.Name, Compatibility: library.Compatibility}
		}
	}
	result.Archive, result.Detail = got.Archive, got.Detail
	report.Result = result
	if err := build.Record(report); err != nil {
		return err
	}
	if got.Detail != "" {
		build.Progress(got.ID + ": " + got.Detail)
	}
	// A passed target's archive is kept, for a later build of a target
	// that needs it to install rather than build it again (decision 28).
	// One that can't be kept leaves the result as it is.
	if result.Outcome == model.OutcomePassed && got.Archive != "" {
		if err := f.keep(ctx, g, build, target.ID, got.ArchiveFile); err != nil {
			build.Progress(fmt.Sprintf("%s: its archive wasn't kept: %v", got.ID, err))
		}
	}
	// The archives its dependencies were installed from are kept too, for
	// a later guest to install them rather than build them again: rust
	// and cargo, built from source where MacPorts had no archive for the
	// release yet, took hours of a check on macOS 27 (the M1's run at
	// d302e744, batch 90). One that isn't kept is said, and builds again
	// next time.
	for _, port := range got.Active {
		// Where they're the person's own, as in a local check's prefix,
		// built however they were, they aren't offered to a clean guest.
		if !f.KeepDependencies || port.Archive == "" || port.ArchiveFile == "" || isTarget(job, port.Name) {
			continue
		}
		active := model.ActivePort{Name: port.Name, Spec: port.Spec, Directory: port.Directory, Archive: port.Archive}
		if err := f.keepDependency(ctx, g, build, active, port.ArchiveFile); err != nil {
			build.Progress(fmt.Sprintf("%s: the archive of its dependency %s wasn't kept: %v", got.ID, port.Name, err))
		}
	}
	return nil
}

// isTarget reports whether a port is one of the plan's targets, built in
// this job or not, whose archives are their results' and aren't kept as
// dependencies'.
func isTarget(job buildenv.Job, port string) bool {
	targets := slices.Clone(job.Plan.Targets)
	for _, target := range job.Targets {
		targets = append(targets, target.PlanTarget)
	}
	return slices.ContainsFunc(targets, func(target model.PlanTarget) bool {
		return strings.EqualFold(target.Target.Name, port) || strings.EqualFold(target.Target.Subport, port)
	})
}

// keep fetches a target's archive from where the guest's MacPorts keeps
// it, its prefix's software directory, into the build's store.
func (f *Follower) keep(ctx context.Context, g Guest, build buildenv.Build, target model.TargetID, file string) error {
	software := path.Join(f.prefix(), "var/macports/software") + "/"
	if file == "" || path.Clean(file) != file || !strings.HasPrefix(file, software) {
		return fmt.Errorf("the guest named %q, not a file in %s", file, software)
	}
	return build.Keep(target, path.Base(file), func(local string) error { return g.Download(ctx, file, local, true) })
}

// keepDependency fetches the archive a dependency was installed from,
// from the guest's software directory, as keep does a target's.
func (f *Follower) keepDependency(ctx context.Context, g Guest, build buildenv.Build, port model.ActivePort, file string) error {
	software := path.Join(f.prefix(), "var/macports/software") + "/"
	if path.Clean(file) != file || !strings.HasPrefix(file, software) {
		return fmt.Errorf("the guest named %q, not a file in %s", file, software)
	}
	return build.KeepDependency(port, path.Base(file), func(local string) error { return g.Download(ctx, file, local, true) })
}

// reported is what the guest reported about itself, for the pull request's
// Tested on.
func reported(environment map[string]string) model.Observed {
	// The guest records port version's own words, "Version: 2.12.6".
	macports, _ := installation.ParseVersion([]byte(environment["macports"]))
	observed := model.Observed{MacOS: environment["macos"], Build: environment["build"], Architecture: environment["architecture"],
		Xcode: environment["xcode"], XcodeBuild: environment["xcode_build"], Tools: environment["tools"],
		DeveloperDir: environment["developer_dir"], MacPorts: macports}
	// A local check's prefix: how many ports were active, and the other
	// package managers on the Mac.
	observed.ActivePorts, _ = strconv.Atoi(environment["active_ports"])
	if foreign := environment["foreign"]; foreign != "" {
		observed.Foreign = strings.Split(foreign, ",")
	}
	return observed
}

// driftReport compares the guest's tools with the facts table's row for
// its release and profile (decision 10), which is what its plan was read
// with: a difference is reported, never judged. An image with Xcode is
// compared with the Xcode row, Xcode's version as well.
func driftReport(release macos.Release, developer model.DeveloperTools, environment map[string]string) string {
	profile := macos.ProfileTools
	if developer == model.DeveloperToolsXcode {
		profile = macos.ProfileXcode
	}
	facts, ok := macos.Table().Lookup(release.Darwin, "arm64", profile)
	if !ok {
		return ""
	}
	var differences []string
	if xcode := environment["xcode"]; profile == macos.ProfileXcode && xcode != "" && xcode != facts.Xcode {
		differences = append(differences, fmt.Sprintf("Xcode %s; the facts table has %s", xcode, facts.Xcode))
	}
	if tools := environment["tools"]; tools != "" && tools != facts.Tools {
		differences = append(differences, fmt.Sprintf("Command Line Tools %s; the facts table has %s", tools, facts.Tools))
	}
	if len(differences) == 0 {
		return ""
	}
	return fmt.Sprintf("drift: the guest has %s, from %s on %s", strings.Join(differences, ", and "), facts.Source.From, facts.Source.Date)
}

func tail(text string, n int) string {
	if len(text) > n {
		return "…" + text[len(text)-n:]
	}
	return text
}
