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
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/build"
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
)

// Capabilities reports what this provider answers. Only viability is
// implemented, and declaration completeness may never be: an image with
// MacPorts already installed is precisely the warm state that
// proposition exists to detect.
func (p Provider) Capabilities() verify.Capabilities {
	platforms := make([]platform.Release, 0, len(p.Bases))
	for _, b := range p.Bases {
		platforms = append(platforms, b.Release)
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
func (p Provider) Submit(ctx context.Context, req verify.Request) (verify.Job, error) {
	if req.Port == "" {
		return verify.Job{}, fmt.Errorf("%w: no port named", verify.ErrUnsupported)
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
		// Derived, never recorded (D19): the worker is already booted,
		// so asking it costs a second — and refusing here releases the
		// slot instead of keeping a guaranteed failure as a debug
		// environment nobody needs.
		if out, xerr := Exec(ctx, p.Tools, name, "/usr/bin/xcodebuild", "-version"); xerr != nil || !strings.Contains(out, "Xcode") {
			return fail(fmt.Errorf("%w: %s requires a full Xcode installation and this base has none — provision with --xcode, or promote unverified", verify.ErrNoEnvironment, req.Port))
		}
	}
	if err := p.stage(ctx, name, req); err != nil {
		return fail(err)
	}
	if err := p.launch(ctx, name, req); err != nil {
		return fail(err)
	}
	return job, nil
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

// stage copies the edited portdirs in and indexes them ahead of the
// guest's own tree. The index is checked rather than assumed: a
// Portfile that does not parse indexes to nothing, and the build would
// then quietly test the tree's copy of the port instead of the one
// under test.
func (p Provider) stage(ctx context.Context, vm string, req verify.Request) error {
	if _, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", "rm -rf "+overlayDir+" && mkdir -p "+overlayDir); err != nil {
		return fmt.Errorf("%w: preparing the overlay: %w", verify.ErrNoEnvironment, err)
	}
	for _, dir := range req.Portdirs {
		category, name, err := build.Layout(dir)
		if err != nil {
			return fmt.Errorf("%w: %w", verify.ErrUnsupported, err)
		}
		// tar rather than a file copy: the portdir's files/ carries the
		// patchfiles, and a port staged without them fails in a way that
		// looks like the port's fault. The host tar streams into the
		// guest's over `tart exec -i`, so this stays a pipeline rather
		// than a one-shot command.
		root := filepath.Dir(filepath.Dir(filepath.Clean(dir)))
		tarBin, err := p.Tools.Find(tool.Tar)
		if err != nil {
			return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
		}
		tar := exec.CommandContext(ctx, tarBin, "cf", "-", "-C", root, filepath.Join(category, name))
		pipe, err := tar.StdoutPipe()
		if err != nil {
			return err
		}
		if err := tar.Start(); err != nil {
			return fmt.Errorf("%w: reading %s: %w", verify.ErrNoEnvironment, dir, err)
		}
		out, xerr := CLI(ctx, p.Tools, pipe, "exec", "-i", vm, "/usr/bin/tar", "xf", "-", "-C", overlayDir)
		werr := tar.Wait()
		if xerr != nil || werr != nil {
			return fmt.Errorf("%w: staging %s: %s", verify.ErrNoEnvironment, dir, strings.TrimSpace(out))
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
func runner(portCmd string) string {
	return `set -u
mkdir -p ` + stateDir + `
echo running > ` + stateDir + `/state
: > ` + stateDir + `/log
nohup /bin/sh -c '
  ok=yes
  for f in ` + stateDir + `/argv.lint ` + stateDir + `/argv.test ` + stateDir + `/argv; do
    [ -f "$f" ] || continue
    set --
    while IFS= read -r a; do set -- "$@" "$a"; done < "$f"
    sudo -n ` + portCmd + ` "$@" >> ` + stateDir + `/log 2>&1 || { ok=no; break; }
  done
  if [ "$ok" = yes ]
  then echo passed > ` + stateDir + `/state
  else echo failed > ` + stateDir + `/state
  fi
' >/dev/null 2>&1 &
`
}

// launch starts the build detached, so the job belongs to the guest
// rather than to this process.
func (p Provider) launch(ctx context.Context, vm string, req verify.Request) error {
	argv := build.InstallArgs(req.Port, req.Variants, len(req.FromSource) > 0)
	if _, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", "mkdir -p "+stateDir); err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	body := strings.NewReader(strings.Join(argv, "\n") + "\n")
	if out, err := CLI(ctx, p.Tools, body, "exec", "-i", vm, "/bin/sh", "-c", "cat > "+stateDir+"/argv"); err != nil {
		return fmt.Errorf("%w: writing the argv: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	lint := strings.NewReader(strings.Join(build.LintArgs(req.Port), "\n") + "\n")
	if out, err := CLI(ctx, p.Tools, lint, "exec", "-i", vm, "/bin/sh", "-c", "cat > "+stateDir+"/argv.lint"); err != nil {
		return fmt.Errorf("%w: writing the lint argv: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	if req.Test {
		body := strings.NewReader(strings.Join(build.TestArgs(req.Port, req.Variants), "\n") + "\n")
		if out, err := CLI(ctx, p.Tools, body, "exec", "-i", vm, "/bin/sh", "-c", "cat > "+stateDir+"/argv.test"); err != nil {
			return fmt.Errorf("%w: writing the test argv: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
		}
	}
	if out, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", runner(p.prefixOf().Port())); err != nil {
		return fmt.Errorf("%w: launching the build: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
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
