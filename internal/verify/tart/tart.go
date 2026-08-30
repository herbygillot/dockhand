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
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Provider verifies against a tart guest cloned from Base.
type Provider struct {
	// Base is the prepared VM: a Cirrus Labs image with MacPorts
	// installed. dockhand does not build it — the image is a machine
	// fact doctor probes for, like any other tool. Nothing else is
	// required of it: the guest agent and passwordless sudo are already
	// in those images.
	Base string
	// Platform is the macOS release the guest runs. It is stated rather
	// than probed because a base image is prepared once and its release
	// does not change, and probing would mean booting a guest to answer
	// a question about an image.
	Platform platform.Release
	// Prefix is the MacPorts installation inside the guest. It is a
	// field rather than a constant because the next backend is an
	// ephemeral prefix, which is by definition not the conventional
	// one; the zero value means the conventional one.
	Prefix prefix.Prefix
}

// prefixOf is the guest's installation, defaulting to the conventional
// location the base image installs into.
func (p Provider) prefixOf() prefix.Prefix {
	if p.Prefix == "" {
		return prefix.Prefix(macports.DefaultPrefix)
	}
	return p.Prefix
}

const (
	// workerPrefix names the guests this provider creates. A verdict
	// environment is interchangeable and short-lived, so it is named
	// for its role rather than for the port it happens to be testing —
	// which also keeps port names, which may contain characters a VM
	// name may not, out of the naming scheme entirely.
	workerPrefix = "dockhand-worker-"
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
	return verify.Capabilities{
		Name:         "tart",
		Propositions: []verify.Proposition{verify.PortViability},
		Pristine:     true,
		Interactive:  true,
		Platform:     p.Platform,
		Concurrent:   concurrent,
	}
}

// tartRun executes a tart subcommand, optionally piping stdin.
func tartRun(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "tart", args...)
	cmd.Stdin = stdin
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// in runs a command in the guest. Arguments are argv, not a command
// line: nothing here is quoted because nothing here reaches a shell.
func in(ctx context.Context, vm string, argv ...string) (string, error) {
	return tartRun(ctx, nil, append([]string{"exec", vm}, argv...)...)
}

// Submit clones the base, stages the edited ports, and starts a
// detached build. It returns once the build is running — about eleven
// seconds for this provider, because the clone is free and the boot is
// not.
func (p Provider) Submit(ctx context.Context, req verify.Request) (verify.Job, error) {
	if req.Port == "" {
		return verify.Job{}, fmt.Errorf("%w: no port named", verify.ErrUnsupported)
	}
	if out, err := tartRun(ctx, nil, "list", "--source", "local"); err != nil || !strings.Contains(out, p.Base) {
		return verify.Job{}, fmt.Errorf("%w: no base VM %q (see doctor)", verify.ErrNoEnvironment, p.Base)
	}

	name := workerPrefix + stamp()
	if out, err := tartRun(ctx, nil, "clone", p.Base, name); err != nil {
		return verify.Job{}, fmt.Errorf("%w: clone: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	job := verify.Job{Provider: "tart", ID: name, Started: time.Now()}

	// The guest outlives this call, so every failure from here on must
	// take it with it: a leaked worker holds one of two licence slots
	// and blocks the next verification outright.
	fail := func(err error) (verify.Job, error) {
		_ = p.Release(context.WithoutCancel(ctx), job)
		return verify.Job{}, err
	}
	//nolint:errcheck // the guest is detached from this process by design
	go tartRun(context.WithoutCancel(ctx), nil, "run", "--no-graphics", name)

	if err := p.waitAgent(ctx, name); err != nil {
		return fail(err)
	}
	if err := p.stage(ctx, name, req); err != nil {
		return fail(err)
	}
	if err := p.launch(ctx, name, req); err != nil {
		return fail(err)
	}
	return job, nil
}

// waitAgent waits for the guest agent to answer, which is the only
// readiness signal that matters: there is no address to wait for.
func (p Provider) waitAgent(ctx context.Context, vm string) error {
	for i := 0; i < 120; i++ {
		if _, err := in(ctx, vm, "/usr/bin/true"); err == nil {
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
	if _, err := in(ctx, vm, "/bin/sh", "-c", "rm -rf "+overlayDir+" && mkdir -p "+overlayDir); err != nil {
		return fmt.Errorf("%w: preparing the overlay: %w", verify.ErrNoEnvironment, err)
	}
	for _, dir := range req.Portdirs {
		category, name, err := build.Layout(dir)
		if err != nil {
			return fmt.Errorf("%w: %w", verify.ErrUnsupported, err)
		}
		// tar rather than a file copy: the portdir's files/ carries the
		// patchfiles, and a port staged without them fails in a way that
		// looks like the port's fault.
		root := filepath.Dir(filepath.Dir(filepath.Clean(dir)))
		tar := exec.CommandContext(ctx, "tar", "cf", "-", "-C", root, filepath.Join(category, name))
		pipe, err := tar.StdoutPipe()
		if err != nil {
			return err
		}
		if err := tar.Start(); err != nil {
			return fmt.Errorf("%w: reading %s: %w", verify.ErrNoEnvironment, dir, err)
		}
		out, xerr := tartRun(ctx, pipe, "exec", "-i", vm, "/usr/bin/tar", "xf", "-", "-C", overlayDir)
		werr := tar.Wait()
		if xerr != nil || werr != nil {
			return fmt.Errorf("%w: staging %s: %s", verify.ErrNoEnvironment, dir, strings.TrimSpace(out))
		}
	}

	out, err := in(ctx, vm, "/bin/sh", "-c", "cd "+overlayDir+" && exec "+p.prefixOf().Portindex())
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
	if out, err := in(ctx, vm, "/bin/sh", "-c", script, "sh", line); err != nil {
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
nohup /bin/sh -c '
  set --
  while IFS= read -r a; do set -- "$@" "$a"; done < ` + stateDir + `/argv
  if sudo -n ` + portCmd + ` "$@" > ` + stateDir + `/log 2>&1
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
	if _, err := in(ctx, vm, "/bin/sh", "-c", "mkdir -p "+stateDir); err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	body := strings.NewReader(strings.Join(argv, "\n") + "\n")
	if out, err := tartRun(ctx, body, "exec", "-i", vm, "/bin/sh", "-c", "cat > "+stateDir+"/argv"); err != nil {
		return fmt.Errorf("%w: writing the argv: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	if out, err := in(ctx, vm, "/bin/sh", "-c", runner(p.prefixOf().Port())); err != nil {
		return fmt.Errorf("%w: launching the build: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
}

// Poll reads the guest's own record of where the build got to.
func (p Provider) Poll(ctx context.Context, job verify.Job) (verify.Status, error) {
	if job.Provider != "tart" {
		return verify.Status{}, fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	out, err := tartRun(ctx, nil, "list", "--source", "local")
	if err != nil || !strings.Contains(out, job.ID) {
		return verify.Status{}, fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	state, err := in(ctx, job.ID, "/bin/cat", stateDir+"/state")
	if err != nil {
		// The guest is not answering yet, or no longer is. Either way it
		// has not reported an outcome, and inventing one would be worse.
		return verify.Status{State: verify.Running}, nil
	}
	log, _ := in(ctx, job.ID, "/usr/bin/tail", "-200", stateDir+"/log")

	switch strings.TrimSpace(state) {
	case "passed":
		return verify.Status{State: verify.Passed, Log: log, Handle: job.ID}, nil
	case "failed":
		// Kept deliberately: what a failed verification hands back is the
		// environment it failed in.
		return verify.Status{State: verify.Failed, Log: log, Handle: job.ID}, nil
	case "running":
		return verify.Status{State: verify.Running, Log: log}, nil
	}
	return verify.Status{State: verify.Errored, Log: log,
		Detail: "the guest reported no state; the runner did not start"}, nil
}

// Release discards the worker, and with it any debug handle.
func (p Provider) Release(ctx context.Context, job verify.Job) error {
	_, _ = tartRun(ctx, nil, "stop", job.ID)
	if out, err := tartRun(ctx, nil, "delete", job.ID); err != nil {
		return fmt.Errorf("verify/tart: releasing %s: %s", job.ID, strings.TrimSpace(out))
	}
	return nil
}
