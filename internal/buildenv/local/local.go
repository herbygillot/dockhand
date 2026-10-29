// Package local is the local provider (Design v3 §7): it builds a check's
// targets in a MacPorts prefix on this Mac, /opt/local or one of the
// person's own, with the guest program Tart's clones run (package guest),
// under the prefix's port-tclsh and in MacPorts CI's order.
package local

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/buildenv/staging"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/procid"
	"github.com/herbygillot/dockhand/internal/subprocess"
)

// Local builds a check's targets in a MacPorts prefix on this Mac.
//
// The prefix is the person's, so the check changes it as little as it
// can: MacPorts reads the staged tree through a macports.conf of the
// check's own (PORTSRC), and the prefix's etc/macports is never written.
// It builds among whatever is active, unless the prefix is one dockhand
// may clean (Clean), makes a port snapshot before it changes anything,
// and puts the prefix back after, unless it keeps what it built (Keep, or
// check --keep). A check whose process ends first leaves its directory,
// which says the prefix is changed (Changes), and the next check there
// puts it back.
type Local struct {
	Repo *git.Repository
	// Index stages a tree's port index for a platform: the engine's.
	Index func() (portindex.Source, error)
	// Prefix is where --on local builds, /opt/local when empty.
	Prefix string
	// Keep leaves the branch's versions active after every check, as
	// check --keep does one.
	Keep bool
	// Clean are the prefixes dockhand may treat as its own: each target
	// builds with all but its dependencies deactivated, as in a clone.
	Clean []string
	// TestTimeout and BuildTimeout bound a target's tests and its build,
	// as Tart's do; Poll is how often its results are read, 10 seconds
	// when zero.
	TestTimeout  time.Duration
	BuildTimeout time.Duration
	Poll         time.Duration
	// Temp is where each check's directory is made, /var/tmp when empty:
	// MacPorts' unprivileged user reads the staged tree there, as it
	// can't in a home folder.
	Temp string
	// Env is the environment its programs run with, the command line's.
	Env []string
	// Locks is where a prefix's lock is kept, one for every dockhand
	// process on the Mac, whatever its user or database: dockhand-locks
	// in Temp when empty (own).
	Locks string

	workspaces workspace.Registry
	// sudo runs the guest program as root where the prefix's registry
	// isn't the person's to write; /usr/bin/sudo when nil.
	sudo []string
	// stager replaces staging the revision in tests: it writes the guest
	// program, its input, and a tree into the check's directory.
	stager func(ctx context.Context, job buildenv.Job, input guest.Input, dir string) error
	// building guards the check directories this process is building in,
	// which Changes says are building.
	building sync.Map
}

var (
	_ buildenv.ReleaseProvider  = (*Local)(nil)
	_ buildenv.ChangingProvider = (*Local)(nil)
	_ buildenv.LinkReader       = (*Local)(nil)
)

// SnapshotSince is the MacPorts that first has port snapshot and port
// restore, which a check's put-back stands on.
const SnapshotSince = "2.9.0"

// The capabilities local has, asserted, so a signature that drifts fails
// the build rather than quietly turning one off (the docs dig's I39).
var (
	_ buildenv.Provider         = (*Local)(nil)
	_ buildenv.LinkReader       = (*Local)(nil)
	_ buildenv.ReleaseProvider  = (*Local)(nil)
	_ buildenv.ChangingProvider = (*Local)(nil)
)

func (l *Local) Name() string { return buildenv.Local }

// ReadsLinks says the guest program reads what a passed build's binaries
// link, as in a clone.
func (l *Local) ReadsLinks() bool { return true }

// localDirectory begins the name of every check's directory in Temp.
const localDirectory = "dockhand-local-"

func (l *Local) temp() string { return cmp.Or(l.Temp, "/var/tmp") }

func (l *Local) prefix() string { return cmp.Or(l.Prefix, macports.DefaultPrefix) }

// Environments is the prefix named, local:/opt/macports-test, or the
// configured one, on the platform its MacPorts reads this Mac as. A
// prefix whose MacPorts predates port snapshot is refused, saying so,
// since a check there couldn't be put back.
func (l *Local) Environments(ctx context.Context, prefix string) ([]model.Environment, error) {
	prefix = filepath.Clean(cmp.Or(prefix, l.prefix()))
	if !filepath.IsAbs(prefix) {
		return nil, fmt.Errorf("--on local:%s: a prefix is a path from the root, such as /opt/local", prefix)
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "port")); err != nil {
		return nil, fmt.Errorf("--on local:%s: no MacPorts is installed there; dockhand builds in a prefix MacPorts' own installer or source install made", prefix)
	}
	facts, err := installation.Inspect(ctx, l.command, prefix)
	if err != nil {
		return nil, err
	}
	if len(facts.Problems) > 0 {
		return nil, fmt.Errorf("--on local:%s: %s", prefix, strings.Join(facts.Problems, "; "))
	}
	if !macports.BaseAtLeast(facts.Version, SnapshotSince) {
		return nil, fmt.Errorf("--on local:%s: its MacPorts is %s, which predates port snapshot (2.9), so a check there couldn't put the prefix back; port selfupdate brings a newer one", prefix, facts.Version)
	}
	return []model.Environment{{Provider: buildenv.Local, Platform: facts.Platform, Prefix: prefix}}, nil
}

// command runs a program on this Mac, as installation.Inspect asks.
func (l *Local) command(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: filepath.Base(args[0]), Path: args[0], Args: args[1:], Env: l.Env, Stdin: input})
	return result.Output, err
}

// owner is who made a check's directory: the process, the check, and the
// prefix it builds in, which tell a directory a process left from one in
// use.
type owner struct {
	PID int `json:"pid"`
	// Start is when the process started (procid), so a PID the system
	// reused isn't taken for it; empty in a directory an older dockhand
	// made.
	Start  string `json:"start,omitempty"`
	Check  string `json:"check"`
	Prefix string `json:"prefix"`
}

// Execute builds the job's targets in the environment's prefix. A prefix
// an earlier check left changed is put back first. Trouble running the
// guest program is infrastructure; a target that fails to build is a
// result.
func (l *Local) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) (err error) {
	prefix := job.Environment.Prefix
	if prefix == "" {
		return buildenv.ErrDockhandFault.Because("a local environment named no prefix")
	}
	if err := os.MkdirAll(job.Directory, 0o755); err != nil {
		return err
	}
	cleanup := context.WithoutCancel(ctx)
	// One check owns the prefix from what it puts back first to its own
	// put-back: two, serve's and a person's, or two databases', each
	// snapshotted the other's half-built state (Codex's review of
	// 8e314b96, finding 1).
	letGo, err := l.own(ctx, prefix, job.Run.Name(), build)
	if err != nil {
		return err
	}
	defer letGo()
	if err := l.restoreLeft(ctx, prefix, build); err != nil {
		return err
	}
	keep := job.Plan.Keep || l.Keep
	input := guest.Input{Protocol: guest.Protocol, Run: job.Run.Name(), Attempt: job.Execution.Attempt, Prefix: prefix,
		Platform: job.Environment.Platform, Tests: cmp.Or(string(job.Plan.Tests), string(model.TestsDeclared)),
		TestTimeout: int(cmp.Or(l.TestTimeout, buildenv.DefaultTestTimeout) / time.Second), BuildTimeout: int(cmp.Or(l.BuildTimeout, buildenv.DefaultBuildTimeout) / time.Second),
		LintTimeout: int(guest.LintTimeout / time.Second), Targets: guest.Targets(job, build),
		Local: &guest.Local{Clean: slices.Contains(l.Clean, prefix), Keep: keep, Note: fmt.Sprintf("dockhand %s attempt %d", job.Run.Name(), job.Execution.Attempt)}}
	if !slices.ContainsFunc(input.Targets, func(t guest.Target) bool { return !t.Blocked }) {
		return nil
	}
	sudo, err := l.asRoot(ctx, prefix, build)
	if err != nil {
		return err
	}
	build.Progress("staging the revision for " + prefix)
	dir, err := os.MkdirTemp(l.temp(), localDirectory)
	if err != nil {
		return buildenv.Infrastructure("making the check's directory: %w", err)
	}
	// MacPorts' unprivileged user reads the staged tree, and root writes
	// the program's results beside it.
	if err := os.Chmod(dir, 0o755); err != nil {
		return buildenv.Infrastructure("making the check's directory: %w", err)
	}
	l.building.Store(dir, true)
	defer l.building.Delete(dir)
	stage := l.stage
	if l.stager != nil {
		stage = l.stager
	}
	err = stage(ctx, job, input, dir)
	if err == nil {
		err = configure(dir, prefix)
	}
	if err != nil {
		os.RemoveAll(dir)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("staging the revision: %w", err)
	}
	if err := build.Refer(dir); err != nil {
		os.RemoveAll(dir)
		return err
	}
	started, err := l.start(dir, prefix, job.Run.Name(), sudo)
	if err != nil {
		os.RemoveAll(dir)
		return buildenv.Infrastructure("starting the guest program: %w", err)
	}
	build.Progress(fmt.Sprintf("building in %s (%s)", prefix, map[bool]string{true: "as root", false: "as you"}[len(sudo) > 0]))
	follower := &guest.Follower{Prefix: prefix, Root: dir, Poll: l.Poll}
	release, _ := hostRelease(job.Environment.Platform)
	err = follower.Follow(ctx, hostGuest{}, started, job, build, release)
	select {
	case <-started.Done():
	default:
		_ = started.Stop(cleanup, 30*time.Second)
	}
	l.settle(cleanup, follower, dir, prefix, job.Run.Name(), sudo, build)
	return err
}

// hostRelease is the macOS release a local environment's platform is.
func hostRelease(platform model.Platform) (macos.Release, error) {
	darwin, err := strconv.Atoi(platform.Version)
	if err != nil {
		return macos.Release{}, err
	}
	return macos.ReleaseForDarwin(darwin)
}

// settle says how the check left the prefix, from what the guest program
// wrote last, and removes the check's directory where nothing is left to
// put back. A prefix the program didn't put back, as when its check was
// stopped, is put back now; one that can't be is said, with how.
func (l *Local) settle(ctx context.Context, follower *guest.Follower, dir, prefix, check string, sudo []string, build buildenv.Build) {
	results, err := follower.Read(ctx, hostGuest{})
	snapshot := results.Environment["snapshot"]
	restored := results.Environment["restored"]
	switch {
	case err != nil || snapshot == "":
		// Nothing was changed before the program stopped: its snapshot
		// comes before anything does.
		remove(dir, build)
	case restored == "yes":
		remove(dir, build)
	case restored == "kept":
		// Its directory stays, marked kept, for dockhand undo's put-back,
		// which the next check leaves to it.
		build.Progress(fmt.Sprintf("kept the branch's versions active in %s; dockhand undo %s puts it back as it was", prefix, check))
	case restored == "":
		restoring, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		// A long build outlasts the sudo it was given at the start, five
		// minutes by default: root is asked again, on the terminal where
		// there is one, as at the start.
		if len(sudo) > 0 {
			if again, err := l.asRoot(restoring, prefix, build); err == nil {
				sudo = again
			}
		}
		if err := l.restore(restoring, dir, prefix, check, sudo); err != nil {
			build.Progress(fmt.Sprintf("%s wasn't put back as it was: %v; dockhand undo %s tries again, or the next check there does. %s", prefix, err, check, l.byHand(dir, check, snapshot)))
			return
		}
		remove(dir, build)
		build.Progress("put " + prefix + " back as it was")
	default:
		build.Progress(fmt.Sprintf("%s wasn't put back as it was: %s; dockhand undo %s tries again, or the next check there does. %s", prefix, strings.TrimPrefix(restored, "no: "), check, l.byHand(dir, check, snapshot)))
	}
}

// byHand is how a prefix is put back where dockhand's own put-back can't
// run: MacPorts' port restore brings back the ports requested before and
// what they need, not the active set, so the rest active before are
// activated from the check's record (G2's rerun on rc8, 2026-10-29).
func (l *Local) byHand(dir, check, snapshot string) string {
	words := fmt.Sprintf("Where dockhand's put-back can't run, sudo port restore --snapshot-id %s restores the ports requested before %s and what they need", snapshot, check)
	before, err := os.ReadFile(filepath.Join(dir, "before.txt"))
	if err != nil {
		return words
	}
	var activate []string
	for _, name := range slices.Sorted(maps.Keys(activePorts(string(before)))) {
		activate = append(activate, name+" "+activePorts(string(before))[name])
	}
	if len(activate) == 0 {
		return words
	}
	return words + ", and sudo port activate " + strings.Join(activate, " ") + " the rest that were active, from its record"
}

// remove removes a check's directory, saying where it can't, as where
// its guest program, root, left what dockhand, as the person, can't
// remove (G4 on rc8, 2026-10-29).
func remove(dir string, build buildenv.Build) {
	if err := os.RemoveAll(dir); err != nil {
		build.Progress(fmt.Sprintf("%s couldn't be removed: %v; sudo rm -rf %s removes it", dir, err, dir))
	}
}

// stage packs the revision's tree, its port index, and the guest program
// and its input, and unpacks them in the check's directory.
func (l *Local) stage(ctx context.Context, job buildenv.Job, input guest.Input, dir string) error {
	if l.Index == nil {
		return errors.New("no port index source")
	}
	index, err := l.Index()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return err
	}
	var rest []model.Target
	for _, target := range job.Targets[1:] {
		rest = append(rest, target.Target)
	}
	archive := filepath.Join(job.Directory, "input.tar")
	defer os.Remove(archive)
	payload := map[string][]byte{"guest.tcl": guest.Program, "input.json": data}
	if err := staging.Archive(ctx, l.Repo, &l.workspaces, staging.Request{Source: job.Revision.Source, Target: job.Targets[0].Target, AdditionalTargets: rest,
		Platform: job.Environment.Platform, Index: index}, archive, payload, nil); err != nil {
		return err
	}
	if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "tar", Path: "/usr/bin/tar", Args: []string{"xf", archive, "-C", dir}, Env: l.Env, Combined: true}); err != nil {
		return fmt.Errorf("unpacking the staged tree: %w", err)
	}
	return nil
}

// configure writes the macports.conf MacPorts reads the staged tree
// through, which PORTSRC names, read after the prefix's own so it wins:
// its sources_conf names the staged tree first, then the prefix's own
// sources, with their default, so MacPorts finds the branch's ports
// before anything else, and the prefix's own files stay as they are.
func configure(dir, prefix string) error {
	sources := "file://" + filepath.Join(dir, "ports") + " [nosync]\n"
	if theirs, err := os.ReadFile(sourcesConf(prefix)); err == nil {
		sources += string(theirs)
	}
	if err := os.WriteFile(filepath.Join(dir, "sources.conf"), []byte(sources), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "macports.conf"), []byte("sources_conf "+filepath.Join(dir, "sources.conf")+"\n"), 0o644)
}

// sourcesConf is the sources.conf a prefix's macports.conf names, its
// etc/macports/sources.conf where it names none.
func sourcesConf(prefix string) string {
	conf := filepath.Join(prefix, "etc", "macports", "macports.conf")
	if data, err := os.ReadFile(conf); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "sources_conf" {
				return fields[1]
			}
		}
	}
	return filepath.Join(prefix, "etc", "macports", "sources.conf")
}

// asRoot is how the guest program runs in the prefix: as the person,
// where they can write its registry, as a prefix MacPorts' source install
// made without root is; otherwise under sudo, which asks for their
// password once, where it must. Without a terminal to ask on, as under
// serve, it's refused unless sudo runs the prefix's port-tclsh asking no
// password, which it's asked of that command, the one every root step
// runs (start), saying the one sudoers line that lets it.
func (l *Local) asRoot(ctx context.Context, prefix string, build buildenv.Build) ([]string, error) {
	if syscall.Access(filepath.Join(prefix, "var", "macports", "registry"), 2) == nil {
		return nil, nil
	}
	sudo := l.sudo
	if sudo == nil {
		sudo = []string{"/usr/bin/sudo"}
	}
	// An empty script: the interpreter as root, asked to do nothing.
	tclsh := filepath.Join(prefix, "bin", macports.TclShell)
	if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "sudo", Path: sudo[0], Args: append(slices.Clone(sudo[1:]), "-n", tclsh, os.DevNull), Env: l.Env}); err == nil {
		return append(slices.Clone(sudo), "-n"), nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, buildenv.Infrastructure("%w: %s's registry is root's, and sudo asks for a password, which this process has no terminal to ask on. This sudoers line, added with visudo, lets it: %s. It's root for anything a Portfile does, as a rule for port is, and no narrower: port-tclsh runs whatever script it's given, from a directory you can write. A prefix MacPorts installed without root needs no sudo, at the cost of MacPorts' binary archives", buildenv.ErrNeedsAttention, prefix, l.sudoersLine(tclsh))
	}
	defer tty.Close()
	build.Progress(fmt.Sprintf("%s's registry is root's: MacPorts builds there as root, so sudo asks for your password", prefix))
	if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "sudo", Path: sudo[0], Args: append(slices.Clone(sudo[1:]), "-v"), Env: l.Env,
		Stdin: tty, Stdout: tty, StdoutOnly: true, Combined: true}); err != nil {
		return nil, buildenv.Infrastructure("%w: sudo didn't let the build run as root in %s: %v", buildenv.ErrNeedsAttention, prefix, err)
	}
	return append(slices.Clone(sudo), "-n"), nil
}

// sudoersLine is the one sudoers line that lets serve check in a prefix
// whose registry is root's: the person's, asking no password for the
// prefix's port-tclsh. dockhand never writes sudoers itself.
func (l *Local) sudoersLine(tclsh string) string {
	name := ""
	for _, variable := range l.Env {
		if value, ok := strings.CutPrefix(variable, "USER="); ok {
			name = value
		}
	}
	if name == "" {
		if current, err := user.Current(); err == nil {
			name = current.Username
		}
	}
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s", cmp.Or(name, "<you>"), tclsh)
}

// start runs the guest program in the check's directory under the
// prefix's port-tclsh, with MacPorts reading the check's macports.conf,
// as root where sudo is given, its output in runner.log. It's a process
// group of its own, so a terminal's interrupt reaches dockhand, which
// stops it.
func (l *Local) start(dir, prefix, check string, sudo []string) (*localRun, error) {
	self, _ := procid.Self()
	owned, err := json.Marshal(owner{PID: os.Getpid(), Start: self.Start, Check: check, Prefix: prefix})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(dir, "runner.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	// One command, the prefix's port-tclsh, which one sudoers line names;
	// the program sets MacPorts' PORTSRC and PATH from its directory.
	args := append(slices.Clone(sudo), filepath.Join(prefix, "bin", macports.TclShell), filepath.Join(dir, "guest.tcl"), dir)
	ctx, stop := context.WithCancel(context.Background())
	run := &localRun{done: make(chan struct{}), stop: stop}
	spec := subprocess.Spec{Tool: macports.TclShell, Command: "guest", Path: args[0], Args: args[1:], Env: l.Env, Stdout: log, StdoutOnly: true, Combined: true, Group: true,
		WaitDelay: 30 * time.Second}
	go func() {
		defer log.Close()
		defer close(run.done)
		_, run.err = subprocess.Run(ctx, spec)
	}()
	return run, nil
}

// restore puts back the prefix a check's directory says was changed, with
// the guest program run to do only that, by the check that changed it.
func (l *Local) restore(ctx context.Context, dir, prefix, check string, sudo []string) error {
	path := filepath.Join(dir, "input.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var input guest.Input
	if err := json.Unmarshal(data, &input); err != nil || input.Local == nil {
		return fmt.Errorf("the check's input couldn't be read: %v", err)
	}
	input.Local.Restore = true
	if data, err = json.MarshalIndent(input, "", "  "); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	run, err := l.start(dir, prefix, check, sudo)
	if err != nil {
		return err
	}
	select {
	case <-run.Done():
	case <-ctx.Done():
		_ = run.Stop(context.WithoutCancel(ctx), 30*time.Second)
		return ctx.Err()
	}
	results, err := (&guest.Follower{Root: dir}).Read(ctx, hostGuest{})
	if err != nil {
		return err
	}
	if results.State != "finished" || results.Environment["restored"] != "yes" {
		return errors.New(cmp.Or(results.Detail, "the guest program didn't say it put the prefix back"))
	}
	return nil
}

// left are the check directories in Temp no live process is building in,
// with who made each, its snapshot where it made one, and whether it put
// its prefix back.
type leftDirectory struct {
	dir      string
	owner    owner
	snapshot string
	restored bool
	// kept is a check that kept its versions active, check --keep's,
	// whose directory stays for dockhand undo.
	kept bool
}

// awaitOrphans waits for the guest programs a crashed dockhand left
// building in the prefix: one runs under sudo in a process group of its
// own and outlives its dockhand, whose lock died with it, and the next
// check, taking the lock, snapshotted and built beside it (the
// architecture thread's finding on rc7). Once each ends, its directory is
// an ordinary leftover. A directory whose dockhand runs still is that
// dockhand's, which the lock waits on already.
func (l *Local) awaitOrphans(ctx context.Context, prefix string, build buildenv.Build) error {
	said := map[string]bool{}
	for {
		orphans := l.orphans(prefix)
		if len(orphans) == 0 {
			return nil
		}
		for _, orphan := range orphans {
			if !said[orphan.dir] {
				said[orphan.dir] = true
				build.Progress(fmt.Sprintf("waiting for %s's program (pid %d), which its dockhand left running, to finish in %s", orphan.owner.Check, orphan.program, prefix))
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cmp.Or(l.Poll, 10*time.Second)):
		}
	}
}

// orphan is a check's directory whose dockhand ended while its guest
// program, program, runs still.
type orphan struct {
	dir     string
	owner   owner
	program int
}

// orphans are the prefix's check directories whose dockhand ended and
// whose guest program runs still.
func (l *Local) orphans(prefix string) []orphan {
	entries, err := os.ReadDir(l.temp())
	if err != nil {
		return nil
	}
	var found []orphan
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), localDirectory) {
			continue
		}
		dir := filepath.Join(l.temp(), entry.Name())
		if _, ours := l.building.Load(dir); ours {
			continue
		}
		var made owner
		data, err := os.ReadFile(filepath.Join(dir, "owner.json"))
		if err != nil || json.Unmarshal(data, &made) != nil || made.Prefix != prefix || running(made.PID, made.Start) {
			continue
		}
		results, _ := (&guest.Follower{Root: dir}).Read(context.Background(), hostGuest{})
		if program, _ := strconv.Atoi(results.Environment["pid"]); alive(program) {
			found = append(found, orphan{dir: dir, owner: made, program: program})
		}
	}
	return found
}

func (l *Local) left() []leftDirectory {
	entries, err := os.ReadDir(l.temp())
	if err != nil {
		return nil
	}
	var found []leftDirectory
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), localDirectory) {
			continue
		}
		dir := filepath.Join(l.temp(), entry.Name())
		if _, ours := l.building.Load(dir); ours {
			continue
		}
		var left leftDirectory
		left.dir = dir
		data, err := os.ReadFile(filepath.Join(dir, "owner.json"))
		if err != nil || json.Unmarshal(data, &left.owner) != nil {
			continue
		}
		results, err := (&guest.Follower{Root: dir}).Read(context.Background(), hostGuest{})
		if building(left.owner, results) {
			continue
		}
		if err == nil {
			left.snapshot = results.Environment["snapshot"]
			left.restored = results.Environment["restored"] == "yes"
			left.kept = results.Environment["restored"] == "kept"
		}
		found = append(found, left)
	}
	return found
}

// building says whether a check's directory is still being built in: by
// the dockhand that made it, or by its guest program, which goes on where
// dockhand ended first.
func building(made owner, results guest.Results) bool {
	program, _ := strconv.Atoi(results.Environment["pid"])
	return running(made.PID, made.Start) || alive(program)
}

// running reports whether the process a record names runs still: by its
// PID and start time where the record has one, so a reused PID isn't it,
// else by its PID alone.
func running(pid int, start string) bool {
	if start == "" {
		return alive(pid)
	}
	ok, err := procid.System{}.Alive(procid.Process{PID: pid, Start: start})
	return ok || err != nil && alive(pid)
}

// alive says whether a process is running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// restoreLeft puts back the prefix a check left changed when its process
// ended first, before a check builds there again, and removes what checks
// left that changed nothing. One that can't be put back stops the check,
// saying how.
func (l *Local) restoreLeft(ctx context.Context, prefix string, build buildenv.Build) error {
	if err := l.awaitOrphans(ctx, prefix, build); err != nil {
		return err
	}
	for _, left := range l.left() {
		if left.kept {
			continue
		}
		if left.snapshot == "" || left.restored {
			remove(left.dir, build)
			continue
		}
		if left.owner.Prefix != prefix {
			continue
		}
		sudo, err := l.asRoot(ctx, prefix, build)
		if err != nil {
			return err
		}
		build.Progress(fmt.Sprintf("putting %s back as it was before an earlier check, which ended before it could", prefix))
		if err := l.restore(ctx, left.dir, prefix, left.owner.Check, sudo); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return buildenv.Infrastructure("%w: %s couldn't be put back as it was before %s: %v; dockhand undo %s tries again. %s", buildenv.ErrNeedsAttention, prefix, left.owner.Check, err, left.owner.Check, l.byHand(left.dir, left.owner.Check, left.snapshot))
		}
		remove(left.dir, build)
	}
	return nil
}

// buildingNow are the checks building in a prefix now, by their dockhand
// or their program, as Changes says them.
func (l *Local) buildingNow() []buildenv.Change {
	changes, _ := l.Changes(context.Background())
	return slices.DeleteFunc(changes, func(change buildenv.Change) bool { return !change.Building })
}

// Changes are the prefixes checks are building in now, and those checks
// left changed when their process ended first, each with how to put it
// back (buildenv.ChangingProvider). One the person has put back already,
// its active ports as they were before the check, is forgotten.
func (l *Local) Changes(ctx context.Context) ([]buildenv.Change, error) {
	var changes []buildenv.Change
	entries, err := os.ReadDir(l.temp())
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), localDirectory) {
			continue
		}
		dir := filepath.Join(l.temp(), entry.Name())
		var made owner
		data, err := os.ReadFile(filepath.Join(dir, "owner.json"))
		if err != nil || json.Unmarshal(data, &made) != nil {
			continue
		}
		results, err := (&guest.Follower{Root: dir}).Read(ctx, hostGuest{})
		if err != nil || results.Environment["snapshot"] == "" || results.Environment["restored"] == "yes" {
			continue
		}
		if results.Environment["restored"] == "kept" {
			changes = append(changes, buildenv.Change{Check: made.Check, Place: made.Prefix, Kept: true, Remedy: "dockhand undo " + made.Check})
			continue
		}
		if _, ours := l.building.Load(dir); ours || building(made, results) {
			changes = append(changes, buildenv.Change{Check: made.Check, Place: made.Prefix, Building: true, Holder: l.holder(made.Prefix)})
			continue
		}
		if l.putBackAlready(ctx, dir, made.Prefix) {
			os.RemoveAll(dir)
			continue
		}
		changes = append(changes, buildenv.Change{Check: made.Check, Place: made.Prefix, Remedy: "dockhand undo " + made.Check})
	}
	return changes, nil
}

// PutBack puts back the prefix a check kept, or left changed when its
// process ended first, as dockhand undo check-N asks: the check's own
// put-back, exact, run by its guest program from its record, where port
// restore brings back requested ports alone (G2's rerun on rc8). It takes
// the prefix as a check does, and says false where the check left nothing
// here to put back (buildenv.PuttingBackProvider).
func (l *Local) PutBack(ctx context.Context, check string, progress func(string)) (string, bool, error) {
	build := saying{say: progress}
	// One building still, by its dockhand or its program, puts the prefix
	// back itself when it ends.
	for _, change := range l.buildingNow() {
		if change.Check == check {
			return change.Place, true, fmt.Errorf("%s is building in %s now, and puts it back when it ends; dockhand cancel %s stops it, putting it back", check, change.Place, check)
		}
	}
	for _, left := range l.left() {
		if left.owner.Check != check || left.snapshot == "" || left.restored {
			continue
		}
		prefix := left.owner.Prefix
		letGo, err := l.own(ctx, prefix, check, build)
		if err != nil {
			return prefix, true, err
		}
		defer letGo()
		sudo, err := l.asRoot(ctx, prefix, build)
		if err != nil {
			return prefix, true, err
		}
		build.Progress(fmt.Sprintf("putting %s back as it was before %s", prefix, check))
		if err := l.restore(ctx, left.dir, prefix, check, sudo); err != nil {
			if ctx.Err() != nil {
				return prefix, true, ctx.Err()
			}
			return prefix, true, buildenv.Infrastructure("%w: %s couldn't be put back as it was before %s: %v. %s", buildenv.ErrNeedsAttention, prefix, check, err, l.byHand(left.dir, check, left.snapshot))
		}
		remove(left.dir, build)
		return prefix, true, nil
	}
	return "", false, nil
}

// saying is a put-back's progress, as a check's build says it: the prefix
// waited for and root asked for are all a put-back says, and none of a
// build's other methods is called.
type saying struct {
	buildenv.Build
	say func(string)
}

func (s saying) Progress(message string) { s.say(message) }

// putBackAlready says whether a prefix's active ports are those a check's
// directory says were active before it, as after the person's own port
// restore.
func (l *Local) putBackAlready(ctx context.Context, dir, prefix string) bool {
	before, err := os.ReadFile(filepath.Join(dir, "before.txt"))
	if err != nil {
		return false
	}
	now, err := l.command(ctx, nil, filepath.Join(prefix, "bin", "port"), "-q", "installed", "active")
	return err == nil && maps.Equal(activePorts(string(before)), activePorts(string(now)))
}

// Installed are a prefix's active ports, as port -q installed active says
// them, and the global variants its variants.conf sets, the person's own,
// for a check's estimate (buildenv.InstalledProvider).
func (l *Local) Installed(ctx context.Context, environment model.Environment) (active, variants []string, err error) {
	prefix := cmp.Or(environment.Prefix, l.prefix())
	listing, err := l.command(ctx, nil, filepath.Join(prefix, "bin", "port"), "-q", "installed", "active")
	if err != nil {
		return nil, nil, err
	}
	active = slices.Sorted(maps.Keys(activePorts(string(listing))))
	// A variants.conf that can't be read sets none.
	if conf, err := os.ReadFile(filepath.Join(prefix, "etc", "macports", "variants.conf")); err == nil {
		for _, line := range strings.Split(string(conf), "\n") {
			if comment := strings.IndexByte(line, '#'); comment >= 0 {
				line = line[:comment]
			}
			for _, word := range strings.Fields(line) {
				if strings.HasPrefix(word, "+") || strings.HasPrefix(word, "-") {
					variants = append(variants, word)
				}
			}
		}
	}
	return active, variants, nil
}

var _ buildenv.InstalledProvider = (*Local)(nil)

// activePorts are the ports a listing of port -q installed active says
// are active, each with its version, revision, and variants.
func activePorts(listing string) map[string]string {
	active := map[string]string{}
	for _, line := range strings.Split(listing, "\n") {
		if fields := strings.Fields(line); len(fields) == 3 && fields[2] == "(active)" {
			active[fields[0]] = fields[1]
		}
	}
	return active
}

// localRun is the guest program running on this Mac.
type localRun struct {
	done chan struct{}
	err  error
	stop context.CancelFunc
}

func (r *localRun) Done() <-chan struct{} { return r.done }
func (r *localRun) Err() error            { return r.err }

// Stop asks the program to stop, as subprocess's Group does: run as the
// person, its whole group, port's builds among it; under sudo, sudo
// passes the signal to the program. It's killed once subprocess's wait is
// up, and Stop waits for that, or for ctx.
func (r *localRun) Stop(ctx context.Context, _ time.Duration) error {
	r.stop()
	select {
	case <-r.done:
	case <-ctx.Done():
	}
	return nil
}

// hostGuest is the guest program's directory on this Mac, as follow reads
// a clone's: its files are read as they are, since root writes them
// readable to all.
type hostGuest struct{}

func (hostGuest) Command(context.Context, io.Reader, ...string) ([]byte, error) {
	return nil, errors.New("a local check runs no commands through its guest")
}

func (hostGuest) Attach(context.Context, io.Reader, io.Writer, bool, string, ...string) (int, error) {
	return 0, errors.New("a local check has no guest to attach to")
}

func (hostGuest) Upload(context.Context, string, string, bool) error {
	return errors.New("a local check uploads nothing")
}

func (hostGuest) Read(_ context.Context, path string, _ bool) ([]byte, error) {
	return os.ReadFile(path)
}

// Download copies a file whole, as a clone's is downloaded.
func (hostGuest) Download(_ context.Context, path, local string, _ bool) error {
	if path == local {
		return nil
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(local)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Append copies what a file has grown by past the copy's length.
func (hostGuest) Append(_ context.Context, path, local string, _ bool) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(local, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := out.Stat()
	if err == nil {
		_, err = in.Seek(info.Size(), io.SeekStart)
	}
	if err == nil {
		_, err = io.Copy(out, in)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (hostGuest) Close(context.Context) {}

// Supported says whether the local provider can build here: on a Mac,
// MacPorts' own.
func Supported() bool { return runtime.GOOS == "darwin" }
