// Package tart verifies ports in a macOS guest under tart.
//
// The environment is produced by cloning a prepared base VM, which is
// copy-on-write: a measured clone of an 86 GB image takes about a tenth
// of a second and claims no meaningful disk, and the guest is answering
// about ten seconds later. That is what makes a pristine environment
// per verification affordable, and it is the whole argument for this
// provider.
//
// Everything reaches the guest through `tart exec`, which the Cirrus
// Labs images' guest agent serves. That is not merely convenient: exec
// takes an argv, so a port or variant name never passes through a shell
// on its way in, and the guest needs no key, no listening sshd and no
// address. The one thing exec cannot do is run a command that outlives
// it, so the build is launched detached and records its own state in
// the guest — which is also what lets a Job survive dockhand exiting.
package tart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Base is one prepared VM: a Cirrus Labs image with MacPorts installed,
// and the macOS release it runs.
//
// dockhand does not build these — an image is a machine fact doctor
// probes for, like any other tool. Nothing else is required of it: the
// guest agent and passwordless sudo are already in those images.
//
// The release is stated rather than probed because an image is prepared
// once and its release does not change, and probing would mean booting
// a guest to answer a question about an image.
type Base struct {
	VM      string
	Release platform.Release
	// Xcode says whether this image carries a full Xcode installation,
	// when anything has said so.
	//
	// Nil is "nobody has said", which is not the same claim as false.
	// Nothing on this host records which image has an Xcode — the fact
	// is discovered by running xcodebuild in a booted guest — so an
	// image nothing has spoken for must still be asked rather than
	// refused, and a pointer is how the difference survives into the
	// capability.
	//
	// Nothing sets it today, and that emptiness is a gap rather than an
	// oversight: provisioning is the one component that knows, since it
	// is what installs the Xcode under --xcode, and it folds the fact
	// into a line it prints and then discards. Provisioned answers in
	// releases alone, so whoever assembles the provider has nothing to
	// read it back from. Filling this in means provisioning recording
	// the fact where Provisioned can find it; until then every base
	// arrives nil and the guest is asked.
	Xcode *bool
}

// Provider verifies against tart guests cloned from prepared bases.
//
// There is a list of them rather than one because a machine may hold a
// base per macOS release, and those are several platforms of one
// provider rather than several providers. Which one a verification uses
// is the request's business.
type Provider struct {
	// Bases are the prepared images, in preference order: the first is
	// what a request that names no platform gets.
	Bases []Base
	// MacPorts is the version Provision installs. Empty takes the
	// newest version dockhand has a shim for, which pins the
	// environment to something verified rather than to whatever is
	// newest upstream today.
	MacPorts string
	// Prefix is the MacPorts installation inside the guests. It is a
	// field rather than a constant because the next backend is an
	// ephemeral prefix, which is by definition not the conventional
	// one; the zero value means the conventional one.
	Prefix prefix.Prefix
	// Tools resolves tart and the host tar: the run's one finder,
	// handed in by whoever assembles the provider, so the tart doctor
	// reported is the tart every guest is driven with.
	Tools *tool.Finder
}

// The provider is the contract, provably: a Verifier that drifts
// fails to build.
var _ verify.Verifier = Provider{}

// baseFor picks the image a request asks for. A release this provider
// has no image for is refused, never substituted — a build on one macOS
// is not evidence about another.
func (p Provider) baseFor(r platform.Release) (Base, error) {
	if len(p.Bases) == 0 {
		return Base{}, fmt.Errorf("%w: no base images (see doctor)", verify.ErrNoEnvironment)
	}
	if r.IsZero() {
		return p.Bases[0], nil
	}
	for _, b := range p.Bases {
		if b.Release == r {
			return b, nil
		}
	}
	return Base{}, fmt.Errorf("%w: no base image for %s", verify.ErrUnsupported, r)
}

// prefixOf is the guest's installation, defaulting to the conventional
// location the base image installs into.
func (p Provider) prefixOf() prefix.Prefix {
	if p.Prefix == "" {
		return prefix.Prefix(macports.DefaultPrefix)
	}
	return p.Prefix
}

// BaseName is what dockhand calls the image it prepared for a release.
// dockhand names these itself, which is what makes reading the release
// back out of a name honest rather than a guess at someone else's
// scheme.
func BaseName(r platform.Release) string {
	return "dockhand-base-" + strings.ToLower(r.CompactName())
}

const (
	// WorkerPrefix names the guests this provider creates. A verdict
	// environment is interchangeable and short-lived, so it is named
	// for its role rather than for the port it happens to be testing —
	// which also keeps port names, which may contain characters a VM
	// name may not, out of the naming scheme entirely.
	WorkerPrefix = "dockhand-worker-"
	// overlayDir is where the edited portdirs are staged in the guest.
	overlayDir = "/tmp/dockhand-overlay"
	// stateDir holds the runner's own record of where it got to.
	stateDir = "/tmp/dockhand-verify"
	// concurrent is Apple's limit on macOS guests, not the machine's.
	concurrent = 2
	// Evidence is this provider's own phrase for what a pass proves. A
	// clone of a prepared base carries nothing from the last
	// verification, so a port that installed here installed against what
	// it declared and what the tree supplies and nothing else — a
	// stronger claim than a warm runner can make, which is why the
	// wording belongs to the provider rather than to whoever renders it.
	//
	// The PR body states these same words from its own literal today,
	// and composes around them: a tested run reads "built and tested in
	// a pristine VM", a linted one carries a clause in front. So this is
	// the provider's claim about its environment and not the finished
	// sentence. The two meet when a settled record carries the phrase;
	// until then they are deliberately separate strings, because render
	// must not learn to ask a provider anything.
	Evidence = "built in a pristine VM"
)

// Capabilities reports what this provider answers. Only viability is
// implemented, and declaration completeness may never be: an image with
// MacPorts already installed is precisely the warm state that
// proposition exists to detect.
func (p Provider) Capabilities() verify.Capabilities {
	platforms := make([]platform.Release, 0, len(p.Bases))
	// Only the bases something has actually spoken for get an entry. A
	// release left out of the map is one this provider has not been told
	// about, and the caller asks the guest; filling it in with false for
	// every unspoken base would refuse every use_xcode port, including
	// the ones that would have built.
	var xcode map[platform.Release]bool
	for _, b := range p.Bases {
		platforms = append(platforms, b.Release)
		if b.Xcode != nil {
			if xcode == nil {
				xcode = make(map[platform.Release]bool, len(p.Bases))
			}
			xcode[b.Release] = *b.Xcode
		}
	}
	return verify.Capabilities{
		Name:         "tart",
		Propositions: []verify.Proposition{verify.PortViability},
		Pristine:     true,
		Interactive:  true,
		Platforms:    platforms,
		// Apple's limit on macOS guests, and a property of the machine
		// rather than of any one image: two guests total, not two per
		// platform.
		Concurrent: concurrent,
		Evidence:   Evidence,
		// The guest is still holding the installation when anyone asks,
		// which is the whole reason this provider can answer and a
		// provider whose environment is gone by settle cannot.
		InstalledManifest: true,
		Xcode:             xcode,
	}
}

// CLI executes a tart subcommand, optionally piping stdin. It is
// exported because provisioning drives the same tool: one place knows
// how tart is invoked. The transcript is stdout and stderr merged, and
// it comes back whether or not tart succeeded, with the exec error as
// it came — tart's diagnostics land on either stream, and callers read
// the output after a non-zero exit (HasVM parses a listing that may
// have exited non-zero). tart is resolved through the run's finder,
// and a miss is the no-environment fact, not a raw exec error.
func CLI(ctx context.Context, tools *tool.Finder, stdin io.Reader, args ...string) (string, error) {
	bin, err := tools.Find(tool.Tart)
	if err != nil {
		return "", fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	return tool.Run(ctx, bin, tool.Opts{Args: args, Stdin: stdin})
}

// HasVM reports whether a local VM of exactly this name exists. Exact,
// not substring: dockhand-base-sonoma must not be found inside
// dockhand-base-sonoma-anything — the same hazard GoldenName's naming
// avoids, and one that substring matching against `tart list` output
// walked straight into.
func HasVM(ctx context.Context, tools *tool.Finder, name string) (bool, error) {
	out, err := CLI(ctx, tools, nil, "list", "--source", "local")
	if err != nil {
		return false, fmt.Errorf("%w: listing local VMs: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return true, nil
		}
	}
	return false, nil
}

// Exec runs a command in the guest. Arguments are argv, not a command
// line: nothing here is quoted because nothing here reaches a shell.
func Exec(ctx context.Context, tools *tool.Finder, vm string, argv ...string) (string, error) {
	return CLI(ctx, tools, nil, append([]string{"exec", vm}, argv...)...)
}

// Submit clones the base, stages the edited ports, and starts a
// detached build. It returns once the build is running — about eleven
// seconds for this provider, because the clone is free and the boot is
// not.
//
// A request asking for a manifest takes longer, and by a different kind
// of amount. Before anything of the change is staged, the merge base's
// portdir is staged and installed binary-only so the change has an
// honest before to be measured against — a download of the port's
// published archive and its runtime dependencies, bounded (nothing is
// compiled and no build dependency is pulled) but not eleven seconds.
// Saying so here matters because the remedy for a submit that looks hung
// is to kill it, and killing this one leaks the worker and the licence
// slot with it.
func (p Provider) Submit(ctx context.Context, req verify.Request) (verify.Job, error) {
	// An empty slice and an empty headline are the same malformed
	// request: both name nothing to build, and a request that named
	// nothing would boot a guest to install it.
	if len(req.Ports) == 0 || req.Ports[0] == "" {
		return verify.Job{}, fmt.Errorf("%w: no port named", verify.ErrUnsupported)
	}
	for _, port := range req.Ports {
		if !portName(port) {
			return verify.Job{}, fmt.Errorf("%w: %q is not a port name", verify.ErrUnsupported, port)
		}
	}
	base, err := p.baseFor(req.Platform)
	if err != nil {
		return verify.Job{}, err
	}
	if ok, err := HasVM(ctx, p.Tools, base.VM); err != nil {
		return verify.Job{}, err
	} else if !ok {
		return verify.Job{}, fmt.Errorf("%w: no base VM %q (see doctor)", verify.ErrNoEnvironment, base.VM)
	}

	// Admission before the clone: occupancy is counted live under the
	// machine lock, and a full machine refuses in a second with a typed
	// CapacityError instead of being discovered through the agent
	// timeout. The lock is held until the new VM is itself visible as
	// running, so concurrent submitters cannot both count the same
	// free slot.
	unlockAdmission, err := Admit(ctx, p.Tools, p.Capabilities().Concurrent)
	if err != nil {
		return verify.Job{}, err
	}
	admitted := true
	defer func() {
		if admitted {
			unlockAdmission()
		}
	}()

	name := WorkerPrefix + stamp()
	if out, err := CLI(ctx, p.Tools, nil, "clone", base.VM, name); err != nil {
		return verify.Job{}, fmt.Errorf("%w: clone: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	job := verify.Job{Provider: "tart", ID: name, Started: time.Now()}
	writeAttribution(name, req.Owner)

	// The guest outlives this call, so every failure from here on must
	// take it with it: a leaked worker holds one of two licence slots
	// and blocks the next verification outright.
	// Every failure from here must take the guest with it: a leaked
	// worker holds one of two licence slots and blocks the next
	// verification outright. Release can itself fail — a delete races
	// the guest still coming up — so the leak is reported rather than
	// swallowed, because a silent one is discovered as a capacity error
	// much later and somewhere else.
	fail := func(err error) (verify.Job, error) {
		if rerr := p.Release(context.WithoutCancel(ctx), job); rerr != nil {
			slog.Warn("worker leaked after a failed submit", "worker", job.ID, "err", rerr)
			err = fmt.Errorf("%w (worker %s was left behind: %w)", err, job.ID, rerr)
		}
		return verify.Job{}, err
	}
	// The run's error is captured, not discarded: a start that fails —
	// at capacity, out of disk — surfaces as itself.
	runErr := make(chan error, 1)
	go func() {
		_, err := CLI(context.WithoutCancel(ctx), p.Tools, nil, "run", "--no-graphics", name)
		runErr <- err
	}()
	if err := WaitRunning(ctx, p.Tools, name, runErr); err != nil {
		return fail(err)
	}
	// The slot is visibly occupied; the machine lock can pass on.
	admitted = false
	unlockAdmission()

	if err := WaitAgent(ctx, p.Tools, name); err != nil {
		return fail(err)
	}
	if err := p.assertClean(ctx, name); err != nil {
		return fail(err)
	}
	if req.NeedsXcode {
		if !p.hasXcode(ctx, base.Release, name) {
			return fail(fmt.Errorf("%w: %s requires a full Xcode installation and this base has none — provision with --xcode, or promote unverified", verify.ErrNoEnvironment, req.Ports[0]))
		}
	}
	if err := p.prepare(ctx, name, req); err != nil {
		return fail(err)
	}
	if err := p.launch(ctx, name, req); err != nil {
		return fail(err)
	}
	return job, nil
}

// portName reports whether a name can be carried into the guest as
// itself.
//
// Two of this provider's file formats are line-oriented and neither
// quotes: an argv file becomes one word of port(1)'s argv per line, and
// a subject marker becomes one line of the log that the judge splits a
// cohort on. So a name carrying a newline is not one port with an odd
// name — it is two argv words, or a second marker line naming whatever
// the rest of it says, at a boundary the attribution then trusts. The
// refusal is here, at the door, because everything past it treats these
// names as data that cannot bite.
//
// Whitespace and the control characters are what it refuses, rather
// than an allowed alphabet: MacPorts port names are a narrow class in
// practice, but the guest is what defines them and this provider is not
// the place to invent a stricter tree.
func portName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// hasXcode answers whether this environment can meet use_xcode, through
// the capability rather than past it. A base the provider was told
// about is answered from what it was told; one nothing has spoken for
// is asked, because an absent entry is "nobody said" and not "no
// Xcode".
//
// The absent entry is the whole point. Derived, never recorded (D19):
// nothing on this host knows which image carries an Xcode, so the
// honest answer comes from a booted guest, where it costs a second and
// the refusal releases the slot instead of keeping a guaranteed failure
// as a debug environment nobody needs. Nothing speaks for a base today,
// so every one of them is asked — the same question, of the same guest,
// at the same point in the submit, as before the capability existed.
//
// What the capability buys is the seam, not a new decision. The refusal
// stays where it is, after the clone and the boot, even for a base the
// map declares has no Xcode: the refusal there goes through fail(),
// which releases the worker it is refusing on behalf of, so hoisting it
// ahead of the clone would rework an error path that nothing can reach
// yet. The shape lands; the decision does not move.
func (p Provider) hasXcode(ctx context.Context, r platform.Release, vm string) bool {
	if told, known := p.Capabilities().Xcode[r]; known {
		return told
	}
	out, err := Exec(ctx, p.Tools, vm, "/usr/bin/xcodebuild", "-version")
	return err == nil && strings.Contains(out, "Xcode")
}

// WaitAgent waits for the guest agent to answer, which is the only
// readiness signal that matters once an image is provisioned: there is
// no address to wait for.
func WaitAgent(ctx context.Context, tools *tool.Finder, vm string) error {
	for i := 0; i < 120; i++ {
		if _, err := Exec(ctx, tools, vm, "/usr/bin/true"); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("%w: guest agent never answered in %s", verify.ErrNoEnvironment, vm)
}

// stage copies portdirs in and indexes them ahead of the guest's own
// tree. The index is checked rather than assumed: a Portfile that does
// not parse indexes to nothing, and the build would then quietly test
// the tree's copy of the port instead of the one under test.
//
// It takes the directories rather than the request because it is called
// twice for two different sets of them: once with the merge base's, to
// take a baseline, and once with the branch's, to build. The `rm -rf` is
// what makes the second call a replacement of the first rather than a
// merge — an overlay holding both versions would index two ports where
// the tree has one.
// tarInto streams one directory of the host's ports tree into the
// guest's overlay, rel being its path below root.
//
// tar rather than a file copy: a portdir's files/ carries the
// patchfiles, and a port staged without them fails in a way that looks
// like the port's fault. The host tar streams into the guest's over
// `tart exec -i`, so this stays a pipeline rather than a one-shot
// command.
func (p Provider) tarInto(ctx context.Context, vm, root, rel string) error {
	tarBin, err := p.Tools.Find(tool.Tar)
	if err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	tar := exec.CommandContext(ctx, tarBin, "cf", "-", "-C", root, rel)
	pipe, err := tar.StdoutPipe()
	if err != nil {
		return err
	}
	if err := tar.Start(); err != nil {
		return fmt.Errorf("%w: reading %s: %w", verify.ErrNoEnvironment, filepath.Join(root, rel), err)
	}
	out, xerr := CLI(ctx, p.Tools, pipe, "exec", "-i", vm, "/usr/bin/tar", "xf", "-", "-C", overlayDir)
	werr := tar.Wait()
	if xerr != nil || werr != nil {
		return fmt.Errorf("%w: staging %s: %s", verify.ErrNoEnvironment, filepath.Join(root, rel), strings.TrimSpace(out))
	}
	return nil
}

func (p Provider) stage(ctx context.Context, vm string, portdirs []string) error {
	if _, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", "rm -rf "+overlayDir+" && mkdir -p "+overlayDir); err != nil {
		return fmt.Errorf("%w: preparing the overlay: %w", verify.ErrNoEnvironment, err)
	}
	var root string
	for _, dir := range portdirs {
		category, name, err := build.Layout(dir)
		if err != nil {
			return fmt.Errorf("%w: %w", verify.ErrUnsupported, err)
		}
		root = filepath.Dir(filepath.Dir(filepath.Clean(dir)))
		if err := p.tarInto(ctx, vm, root, filepath.Join(category, name)); err != nil {
			return err
		}
	}
	// The overlay is a ports tree, not a bag of portdirs, and MacPorts
	// asks a ports tree for more than its ports. archive_sites.tcl is
	// the one that matters and the one that bites: portarchivefetch
	// looks it up under the port's OWN tree and passes fallback=no —
	// "look up archive sites only from this ports tree, do not fallback
	// to the default" — so a port served from an overlay without
	// _resources has no archive site at all, and `port -b install`
	// fails with "no usable archive sites configured". That is the
	// baseline's entire second step, which means the ABI comparison
	// cannot be made for any port anywhere until this is staged.
	//
	// The whole directory rather than the one file: it is 1.4 MB beside
	// a 100 GB guest, every other resource MacPorts resolves this way
	// gets the same treatment for free, and a tree missing a resource
	// nobody thought of fails the same silent way this did.
	if root != "" {
		if err := p.tarInto(ctx, vm, root, build.ResourcesDir); err != nil {
			return err
		}
	}

	out, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", "cd "+overlayDir+" && exec "+p.prefixOf().Portindex())
	if err != nil {
		return fmt.Errorf("%w: indexing the overlay: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	tally, err := build.ParseTally(out)
	if err != nil {
		return fmt.Errorf("%w: %w: %s", verify.ErrNoEnvironment, err, strings.TrimSpace(out))
	}
	if !tally.Complete() {
		return fmt.Errorf("%w: the overlay indexed %d port(s), %d failed:\n%s",
			verify.ErrUnsupported, tally.Succeeded, tally.Failed, strings.TrimSpace(out))
	}

	// Ahead of the guest's own tree, so the edited port wins while
	// everything else still comes from it — which is what keeps
	// dependencies on binary archives instead of source.
	conf := p.prefixOf().SourcesConf()
	line := build.SourcesLine(overlayDir)
	script := fmt.Sprintf(`grep -qxF "$1" %[1]s || { printf '%%s\n' "$1" | cat - %[1]s > /tmp/sc && sudo -n cp /tmp/sc %[1]s; }`, conf)
	if out, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", script, "sh", line); err != nil {
		return fmt.Errorf("%w: adding the overlay source: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
}

// runner drives the build and records its own outcome. The argv it runs
// is read from a file rather than interpolated, so a port or variant
// name is data to this script and never syntax.
//
// These bytes are frozen. This script is one argv word the guest
// executes, so a change to it is a change to the build itself, and a
// single-subject verification must run today what it ran yesterday. The
// cohort is a second script rather than a generalization of this one
// (cohortRunner): a loop that could serve both would have to differ in
// its own structure at one subject, and there is no way to write that
// difference without moving these bytes. guest_test.go pins them.
func runner(portCmd string) string { return runnerAt(stateDir, portCmd) }

// runnerAt is runner with the state directory named rather than assumed,
// so the script can be run for real against a scratch directory in a
// test. At stateDir it produces runner's exact bytes, and that is the
// claim the golden makes.
func runnerAt(dir, portCmd string) string {
	return `set -u
mkdir -p ` + dir + `
echo running > ` + dir + `/state
: > ` + dir + `/log
nohup /bin/sh -c '
  ok=yes
  for f in ` + dir + `/argv.lint ` + dir + `/argv.test ` + dir + `/argv; do
    [ -f "$f" ] || continue
    set --
    while IFS= read -r a; do set -- "$@" "$a"; done < "$f"
    sudo -n ` + portCmd + ` "$@" >> ` + dir + `/log 2>&1 || { ok=no; break; }
  done
  if [ "$ok" = yes ]
  then echo passed > ` + dir + `/state
  else echo failed > ` + dir + `/state
  fi
' >/dev/null 2>&1 &
`
}

// cohortRunner drives a cohort: n subjects in one environment, each
// linted, optionally tested, then installed, in the order launch wrote
// them. It is reached only when a request names more than one port.
//
// The differences from the single-subject runner are all consequences of
// there being members to tell apart:
//
// The marker line comes out of a file rather than an echo. The runner
// must announce whose output follows, and the announcement carries a
// port name — so the name arrives the way every other name arrives, as
// the contents of a file written over stdin, and never as a word this
// script interpolates. Nothing here is syntax that a port could have
// written.
//
// A state file per subject, written by rename rather than by truncation.
// The guest's own record is the only thing that survives dockhand
// exiting (D17), and at n subjects the aggregate file says only where
// the JOB got to — so these say where each member did. Rename because
// `echo x > f` is truncate-then-write: a reader that lands in that
// window sees an empty file, and an empty file is how this protocol
// spells "the runner never started". One such window per job was
// already a hazard; n of them is n times the same wrong answer.
//
// Nothing reads them yet, and that is worth stating plainly rather than
// leaving a reader to infer a durability guarantee that is only half
// built. Poll reads the aggregate state and the judge attributes from
// the log's markers, so today a guest that died mid-cohort still polls
// as running forever. Two readers are waiting on these files: a
// reconciler that could tell "died after member 1" from "still
// building", and the judge, for which a state file is the one piece of
// corroboration a build under test cannot write into the log. Whether
// the second is worth having — the guest log is written by the change
// under test, and a maintainer's own bump is not hostile — is a ruling
// this step deliberately leaves to the maintainer.
//
// The break is kept, and it is what makes a later member's silence
// meaningful. A cohort stops at its first failure: the members after it
// leave no marker in the log and no state file, which is exactly the
// difference between a port that was disproven and one that was never
// reached.
//
// The link proof runs only where a caller asked for a manifest, and only
// for a dependent — the members after the headline. What it proves is
// what those dependents actually bound to, which is a question only the
// environment holding the whole installation can answer. Its read of
// port(1)'s output drops the IFS= that every other read here keeps,
// deliberately: `port contents` indents its paths, and a path read with
// its indent intact is a file that does not exist.
func cohortRunner(portCmd string, n int) string { return cohortRunnerAt(stateDir, portCmd, n) }

func cohortRunnerAt(dir, portCmd string, n int) string {
	return `set -u
mkdir -p ` + dir + `
echo running > ` + dir + `/.state && mv -f ` + dir + `/.state ` + dir + `/state
: > ` + dir + `/log
nohup /bin/sh -c '
  d=` + dir + `
  n=` + strconv.Itoa(n) + `
  ok=yes
  i=0
  while [ "$i" -lt "$n" ]; do
    [ -f "$d/subject.$i" ] && cat "$d/subject.$i" >> "$d/log"
    member=yes
    for f in "$d/argv.$i.lint" "$d/argv.$i.test" "$d/argv.$i"; do
      [ -f "$f" ] || continue
      set --
      while IFS= read -r a; do set -- "$@" "$a"; done < "$f"
      sudo -n ` + portCmd + ` "$@" >> "$d/log" 2>&1 || { member=no; break; }
    done
    if [ "$member" = yes ] && [ -f "$d/links.$i" ]; then
      set --
      while IFS= read -r a; do set -- "$@" "$a"; done < "$d/links.$i"
      sudo -n ` + portCmd + ` "$@" 2>/dev/null | while read -r p; do
        [ -f "$p" ] || continue
        /usr/bin/otool -L "$p" 2>/dev/null
      done >> "$d/log"
    fi
    if [ "$member" = yes ]
    then echo passed > "$d/.state.$i" && mv -f "$d/.state.$i" "$d/state.$i"
    else echo failed > "$d/.state.$i" && mv -f "$d/.state.$i" "$d/state.$i"
         ok=no
         break
    fi
    i=$((i+1))
  done
  if [ "$ok" = yes ]
  then echo passed > "$d/.state" && mv -f "$d/.state" "$d/state"
  else echo failed > "$d/.state" && mv -f "$d/.state" "$d/state"
  fi
' >/dev/null 2>&1 &
`
}

// argvFile is one instruction file for the runner: where it lands in
// the guest, what it is called in a failure the user reads, and the
// exact bytes that go in it.
type argvFile struct {
	Name string
	What string
	Body string
}

// Dest is where the file lands. The shell redirect is the transport, so
// a change here is as fatal to a build as a change to the words.
func (f argvFile) Dest() string { return stateDir + "/" + f.Name }

// argvFiles is everything the guest is told to do, in the order launch
// writes it. The runner consumes them lint, then test, then install —
// the order is its own, and one line of a file becomes exactly one word
// of port(1)'s argv, so a stray, missing or reordered line is a
// different build.
//
// It is a pure function of the request, separate from the writing, so
// that what a guest would be asked to run can be asserted without
// booting one. That matters more than the tidiness: this is the whole
// instruction set, and the tests that pin it are the only thing standing
// between a refactor and a build nobody in the field has run.
//
// A request naming one port produces exactly the files it always has,
// under exactly the names it always used, and nothing else: no marker,
// no per-subject state, no index in a filename. That is not a
// convenience for the reader but the whole compatibility claim — one
// subject is what every caller in the tree asks for, and a guest that
// received one extra byte would be running a build nobody has verified.
// A cohort is therefore a separate shape, reached only when there is
// more than one port to build.
//
// argv.test is absent rather than empty when no test was asked for. The
// runner skips a file that is not there, so absence is the control
// flow; an empty file would run port(1) with no arguments at all.
func argvFiles(req verify.Request) []argvFile {
	if len(req.Ports) < 2 {
		return soloArgvFiles(req)
	}
	return cohortArgvFiles(req)
}

// soloArgvFiles is the single-subject instruction set, frozen: the three
// names, the two orders, and the bodies a one-port request has always
// produced.
func soloArgvFiles(req verify.Request) []argvFile {
	port := req.Ports[0]
	files := []argvFile{
		{Name: "argv", What: "the argv",
			Body: argvBody(build.InstallArgs(port, req.Variants, fromSource(req, port)))},
		{Name: "argv.lint", What: "the lint argv",
			Body: argvBody(build.LintArgs(port))},
	}
	if req.Test {
		files = append(files, argvFile{Name: "argv.test", What: "the test argv",
			Body: argvBody(build.TestArgs(port, req.Variants))})
	}
	return files
}

// cohortArgvFiles is the instruction set for several subjects in one
// environment: the same three files per member, plus the marker that
// says whose output follows, plus the link proof where one was asked
// for.
//
// Every name is built from the member's position and never from its
// name. A file's name is the one part of this protocol that does reach
// guest shell syntax — launch writes each one with `cat > <dest>` — so a
// scheme that spelled the port into the path would put a string the
// change under test controls into a command, and would break outright on
// any port name carrying a character a shell reads. The port names
// travel where every other name travels: inside the files.
//
// The subjects are written in the request's order, which is the order
// they are to be built, headline first.
func cohortArgvFiles(req verify.Request) []argvFile {
	files := make([]argvFile, 0, 5*len(req.Ports))
	for i, port := range req.Ports {
		// The variant frame is the request's, and a request is about its
		// headline: a cohort's other members are the dependents that ride
		// along, and handing +ssl to one that declares no such variant is
		// a refusal from port(1) rather than a build. A member that needs
		// its own frame needs a request that can carry one, which this
		// shape does not have and no caller has asked for.
		var variants info.VariantSet
		if i == 0 {
			variants = req.Variants
		}
		files = append(files,
			argvFile{
				Name: fmt.Sprintf("subject.%d", i),
				What: "the subject marker for " + port,
				// The runner cats this into the log; the newline is the
				// runner's to deliver and SubjectMarker does not carry one.
				Body: verify.SubjectMarker(port) + "\n",
			},
			argvFile{
				Name: fmt.Sprintf("argv.%d", i),
				What: "the argv for " + port,
				Body: argvBody(build.InstallArgs(port, variants, fromSource(req, port))),
			},
			argvFile{
				Name: fmt.Sprintf("argv.%d.lint", i),
				What: "the lint argv for " + port,
				Body: argvBody(build.LintArgs(port)),
			},
		)
		if req.Test {
			files = append(files, argvFile{
				Name: fmt.Sprintf("argv.%d.test", i),
				What: "the test argv for " + port,
				Body: argvBody(build.TestArgs(port, variants)),
			})
		}
		// The headline is what the change is about; the proof a caller
		// wants is that the members standing on it still bind to what it
		// now publishes. Asking the headline what it links against would
		// answer a question nobody posed.
		if req.Manifest && i > 0 {
			files = append(files, argvFile{
				Name: fmt.Sprintf("links.%d", i),
				What: "the link proof argv for " + port,
				Body: argvBody(contentsArgs(port)),
			})
		}
	}
	return files
}

// contentsArgs asks port(1) what an installed port laid down, one path
// per line and no header. It is spelled here rather than in the build
// package because nothing outside this provider's link proof asks the
// question yet, and a shared helper with one caller is a guess about the
// second one.
func contentsArgs(port string) []string { return []string{"-q", "contents", port} }

// fromSource reports whether this member must ignore its binary archive.
//
// It asks about the member, not about the list. -s is a property of the
// port being built: a cohort where the headline is a re-derivation at an
// unchanged version and the rest are untouched dependents must build
// exactly the headline from source, and reading the list as a flag would
// drag every member into a source build that takes minutes instead of
// seconds.
//
// At one subject this is the same answer the old request-wide test gave
// for every caller in the tree — both pass a list whose only entry is
// the headline — but it is a different rule, and a request that named a
// port it was not building no longer forces a source build of the one it
// was.
func fromSource(req verify.Request, port string) bool {
	return slices.Contains(req.FromSource, port)
}

// argvBody is the file format the runner reads back: one argv word per
// line, newline-terminated, because the runner appends every line it
// reads to "$@" and reads until the file ends.
func argvBody(argv []string) string { return strings.Join(argv, "\n") + "\n" }

// launch starts the build detached, so the job belongs to the guest
// rather than to this process.
func (p Provider) launch(ctx context.Context, vm string, req verify.Request) error {
	if _, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", "mkdir -p "+stateDir); err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	for _, f := range argvFiles(req) {
		body := strings.NewReader(f.Body)
		if out, err := CLI(ctx, p.Tools, body, "exec", "-i", vm, "/bin/sh", "-c", "cat > "+f.Dest()); err != nil {
			return fmt.Errorf("%w: writing %s: %s", verify.ErrNoEnvironment, f.What, strings.TrimSpace(out))
		}
	}
	if out, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", launchScript(p.prefixOf().Port(), req)); err != nil {
		return fmt.Errorf("%w: launching the build: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
}

// launchScript picks the script this request needs. The choice is made
// on the request and nowhere else: a request naming one port gets the
// frozen single-subject runner, byte for byte, whatever else it asks
// for — a test, a variant frame, a manifest. Nothing but a second port
// reaches the cohort.
func launchScript(portCmd string, req verify.Request) string {
	if len(req.Ports) > 1 {
		return cohortRunner(portCmd, len(req.Ports))
	}
	return runner(portCmd)
}

// Exec implements verify.Executor: one command inside the worker,
// through the same guest agent verification drives it by. The job is
// checked the way Poll checks it — wrong provider is a contract
// error, a vanished worker says so.
func (p Provider) Exec(ctx context.Context, job verify.Job, argv ...string) (string, error) {
	if job.Provider != "tart" {
		return "", fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	if ok, err := HasVM(ctx, p.Tools, job.ID); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	return Exec(ctx, p.Tools, job.ID, argv...)
}

// Shell implements verify.InteractiveShell: a login shell inside the
// worker, on the process's real terminal. The TTY is requested only
// when there is one: -t on a piped stdin dies on the terminal-size
// ioctl, and a pipe of commands is a legitimate way to use a shell.
// Whether there is one is the kernel's answer (tool.IsTerminal), not
// the file mode's: /dev/null is a character device too, and a shell
// fed from it must run without a terminal rather than die asking for
// its size.
func (p Provider) Shell(ctx context.Context, job verify.Job) error {
	if job.Provider != "tart" {
		return fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	bin, err := p.Tools.Find(tool.Tart)
	if err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	args := []string{"exec", "-i"}
	if tool.IsTerminal(os.Stdin.Fd()) {
		args = append(args, "-t")
	}
	args = append(args, job.ID, "/bin/zsh", "-l")
	// The process's own streams, not a transcript: this is the one
	// interactive command, and it stays on os/exec for that reason.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		// The shell's own exit status is the user's business.
		return nil
	}
	return err
}

// listQuiet reads `tart list --quiet` — VM names, one per line, from
// every source. A variable for the same reason listVMs is one: the
// audit has to be provable without tart on the machine.
var listQuiet = func(ctx context.Context, tools *tool.Finder) (string, error) {
	return CLI(ctx, tools, nil, "list", "--quiet")
}

// Workers implements verify.WorkerLister: every guest this provider
// names as a worker, running or not, whichever checkout started it.
//
// Every one of them, deliberately — a worker no note here accounts for
// is exactly what the audit exists to find, so filtering by anyone's
// records would filter away the answer. The attribution sidecar names
// the checkout when it can; a worker it says nothing about comes back
// unattributed rather than omitted, because an unattributed worker
// still holds a slot.
func (p Provider) Workers(ctx context.Context) ([]verify.Worker, error) {
	out, err := listQuiet(ctx, p.Tools)
	if err != nil {
		// Both halves, because they carry different failures: tart's own
		// diagnostics land in the transcript, and a tart that is not on
		// the machine at all produces no transcript and an error that
		// already says so.
		if detail := strings.TrimSpace(out); detail != "" {
			return nil, fmt.Errorf("%w: listing workers: %s", verify.ErrNoEnvironment, detail)
		}
		return nil, fmt.Errorf("%w: listing workers: %w", verify.ErrNoEnvironment, err)
	}
	names := workerNames(out)
	workers := make([]verify.Worker, 0, len(names))
	for _, vm := range names {
		workers = append(workers, verify.Worker{Name: vm, Owner: OwnerOf(vm)})
	}
	return workers, nil
}

// The capability is the contract, provably.
var _ verify.WorkerLister = Provider{}

// workerNames picks this provider's guests out of a `tart list
// --quiet` listing. Prefix, not substring: the name is dockhand's own
// and a base or a golden must never read as a worker.
func workerNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		vm := strings.TrimSpace(line)
		if strings.HasPrefix(vm, WorkerPrefix) {
			names = append(names, vm)
		}
	}
	return names
}

// Poll reads the guest's own record of where the build got to.
func (p Provider) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	if job.Provider != "tart" {
		return verify.Status{}, fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	if ok, err := HasVM(ctx, p.Tools, job.ID); err != nil {
		return verify.Status{}, err
	} else if !ok {
		return verify.Status{}, fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	state, err := Exec(ctx, p.Tools, job.ID, "/bin/cat", stateDir+"/state")
	if err != nil {
		// The guest is not answering yet, or no longer is. Either way it
		// has not reported an outcome, and inventing one would be worse.
		return verify.Status{State: verify.Running}, nil
	}

	switch strings.TrimSpace(state) {
	case "passed":
		return verify.Status{State: verify.Passed, Handle: job.ID}, nil
	case "failed":
		// Kept deliberately: what a failed verification hands back is the
		// environment it failed in.
		return verify.Status{State: verify.Failed, Handle: job.ID}, nil
	case "running":
		return verify.Status{State: verify.Running}, nil
	}
	return verify.Status{State: verify.Errored,
		Detail: "the guest reported no state; the runner did not start"}, nil
}

// Log reads the build's output from the guest, in full — the fetch is
// deliberate, so completeness beats the tail Poll used to carry.
func (p Provider) Log(ctx context.Context, job verify.Job) (string, error) {
	if job.Provider != "tart" {
		return "", fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	if ok, err := HasVM(ctx, p.Tools, job.ID); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	log, err := Exec(ctx, p.Tools, job.ID, "/bin/cat", stateDir+"/log")
	if err != nil {
		return "", fmt.Errorf("reading the build log from %s: %w", job.ID, err)
	}
	return log, nil
}

// Release discards the worker, and with it any debug handle.
func (p Provider) Release(ctx context.Context, job verify.Job) error {
	defer clearAttribution(job.ID)
	_, _ = CLI(ctx, p.Tools, nil, "stop", job.ID)
	// A delete can race a guest that is still coming up — tart refuses
	// to remove a running VM, and stop is not instantaneous. Retrying
	// briefly costs nothing and is the difference between a released
	// slot and one lost until someone notices.
	var out string
	var err error
	for i := 0; i < 10; i++ {
		if out, err = CLI(ctx, p.Tools, nil, "delete", job.ID); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		_, _ = CLI(ctx, p.Tools, nil, "stop", job.ID)
	}
	return fmt.Errorf("verify/tart: releasing %s: %s", job.ID, strings.TrimSpace(out))
}

// GoldenName is the untouched reference copy of a base image. It is
// never started, so it is the one thing on the machine that provisioned
// state cannot drift out of; restoring a base means cloning from it,
// which under copy-on-write costs neither time nor disk.
//
// It is deliberately not derived from BaseName by suffixing: a golden
// named dockhand-base-sequoia-golden would contain the base's own name,
// and everything that looks a base up by substring would find two.
func GoldenName(r platform.Release) string {
	return "dockhand-golden-" + strings.ToLower(r.CompactName())
}

// assertClean refuses to verify in an environment that is not what it
// claims to be.
//
// It runs against the worker rather than the base, which is both
// cheaper and stronger: the worker is booted anyway, so the check costs
// about a second instead of the ten a base boot would, and it observes
// the environment the build will actually run in rather than the one it
// was cloned from. Nothing is cached and no manifest is consulted —
// a record of these facts could be stale or, if a base were
// contaminated, contaminated alongside it.
//
// A finding here is ErrNoEnvironment, never Failed. A base that has
// drifted is a fact about this machine; blaming the port for it would
// be the worst kind of wrong answer, because it looks like a real one.
func (p Provider) assertClean(ctx context.Context, vm string) error {
	out, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", build.CleanScript(p.prefixOf().Port()))
	if err != nil {
		return fmt.Errorf("%w: checking the environment: %w", verify.ErrNoEnvironment, err)
	}
	if found := strings.TrimSpace(out); found != "" {
		return fmt.Errorf("%w: the environment is not clean:\n%s", verify.ErrNoEnvironment, found)
	}
	return nil
}
