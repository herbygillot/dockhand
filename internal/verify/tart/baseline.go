package tart

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The files the manifest protocol keeps in the guest, beside the
// runner's own. They live in stateDir because that is what survives
// dockhand exiting (D17) and what the runner does not truncate: it
// rewrites `state` and `log` and nothing else, so a baseline taken
// before the build is still there when the build has finished and
// somebody comes back for it.
const (
	// rosterFile names the subjects, one per line, in request order with
	// the headline first.
	//
	// It exists because verify.Job is {Provider, ID, Started} and carries
	// no port. Manifests is asked about a job and has to know whose
	// installation to describe, and recovering that by parsing the argv
	// files would read a name out of a position in port(1)'s command
	// line — which moves the first time a flag or a variant does.
	//
	// It is written only where a manifest was asked for. A request that
	// wants none produces exactly the bytes it always produced, which is
	// the compatibility claim the frozen runner rests on.
	rosterFile = stateDir + "/manifest.ports"
	// baselineFile records where the baseline came from: the source word
	// on the first line, the reason on the rest. It is written whatever
	// happens, because "we did not look" and "we looked and there was
	// nothing" are different answers and only one of them is an answer.
	baselineFile = stateDir + "/baseline"
	// preFile is the baseline capture itself, present only when there
	// is one.
	preFile = stateDir + "/manifest.pre"
)

// manifestFile and probeFile are the live captures, per subject
// position. They are indexed for the reason the cohort's argv files are:
// a file name is the one part of this protocol that reaches guest shell
// syntax, so a port name must never appear in one.
func manifestFile(i int) string { return fmt.Sprintf("%s/manifest.%d", stateDir, i) }

func probeFile(i int) string { return fmt.Sprintf("%s/probe.%d", stateDir, i) }

// put writes a file into the guest over stdin. The bytes travel on
// stdin for the same reason every argv file does: what is written is
// data, and only the destination is syntax.
func (p Provider) put(ctx context.Context, vm, dest, body string) error {
	out, err := CLI(ctx, p.Tools, strings.NewReader(body), "exec", "-i", vm, "/bin/sh", "-c", "cat > "+dest)
	if err != nil {
		return fmt.Errorf("%w: writing %s: %s", verify.ErrNoEnvironment, dest, strings.TrimSpace(out))
	}
	return nil
}

// get reads a file back out of the guest. A file that is not there is
// not an error: absence is an answer this protocol relies on — no
// baseline, no capture, no probe — and turning it into a failure would
// make the absence unreportable.
func (p Provider) get(ctx context.Context, vm, path string) (string, bool) {
	out, err := Exec(ctx, p.Tools, vm, "/bin/cat", path)
	if err != nil {
		return "", false
	}
	return out, true
}

// prepare puts the environment into the state the build needs: the
// baseline measured from the merge base first, and the change staged
// over the top of it second.
//
// The two steps are one function because their order is the recipe and
// not a detail of how Submit happens to be written. Both write the same
// overlay directory, so a baseline taken after the staging would install
// the change and measure it against itself — a comparison that always
// says nothing moved, which is the one wrong answer this whole step
// exists to avoid.
func (p Provider) prepare(ctx context.Context, vm string, req verify.Request) error {
	if req.Manifest {
		if err := p.baseline(ctx, vm, req); err != nil {
			return err
		}
	}
	return p.stage(ctx, vm, req.Portdirs)
}

// baseline measures what the change is leaving, before anything of the
// change is staged.
//
// The guest's own ports tree is frozen at provisioning time. It may hold
// a newer version than the branch started from, an older one, or not the
// port at all — so asking it what libwidget looks like today answers a
// question about the image rather than about the change. The honest
// before is the merge-base portdir, which only the caller holding the
// repository can produce, staged here as the first overlay and installed
// binary-only.
//
// The order is the whole recipe and it is not adjustable: the baseline's
// overlay and the branch's overlay are the same directory, so a baseline
// taken after staging would install the new version and measure the
// change against itself. stage's own `rm -rf` is what makes the branch
// overlay a clean replacement rather than a merge of the two.
//
// Nothing here fails the submit except a baseline that will not come
// back off. A missing archive, a portdir that will not index, a guest
// that answers the install and then says the port is not installed —
// each is a check that could not be made, recorded by name, and the
// build that follows is still worth running. An uninstall that fails is
// different in kind: it leaves the old version active, which makes the
// branch's install a no-op or an upgrade, and the run would pass without
// ever having built the change.
func (p Provider) baseline(ctx context.Context, vm string, req verify.Request) error {
	if _, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", "mkdir -p "+stateDir); err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}
	if err := p.put(ctx, vm, rosterFile, strings.Join(req.Ports, "\n")+"\n"); err != nil {
		return err
	}

	decline := func(why string) error {
		return p.put(ctx, vm, baselineFile, verify.BaselineNone+"\n"+why+"\n")
	}
	switch {
	case req.Banked:
		// The caller already holds a measurement for this Portfile blob on
		// this platform. It says so at submit rather than discovering it at
		// settle, because what it buys is the download this would spend.
		return p.put(ctx, vm, baselineFile, verify.BaselineBanked+"\n")
	case len(req.Baseline) == 0:
		return decline("no merge-base portdir was staged, so there is nothing to install as the before")
	}

	if err := p.stage(ctx, vm, req.Baseline); err != nil {
		return decline("the merge-base portdir could not be staged: " + oneLine(err.Error()))
	}

	port := req.Ports[0]
	install, ierr := Exec(ctx, p.Tools, vm,
		sudo(p.prefixOf().Port(), build.BaselineArgs(port, req.Variants))...)

	// port(1) exiting zero is not the same claim as the port being
	// installed: `port -q installed` on a port that is not exits zero and
	// prints nothing, and taking the install's own status for an answer
	// records a baseline that was never taken. An empty baseline compares
	// as every library removed, which is the strongest false break there
	// is, so the confirmation is the gate and the exit code is not.
	held, _ := Exec(ctx, p.Tools, vm,
		append([]string{p.prefixOf().Port()}, build.InstalledArgs(port)...)...)
	version, verr := build.ParseInstalled(held)
	if verr != nil {
		if ierr != nil {
			return decline(build.BaselineFailure(install))
		}
		return decline("the binary-only install reported success and the port is not installed")
	}

	capture := build.ManifestScript(p.prefixOf().Port(), preFile, rosterFile, 0)
	captured, cerr := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", capture)

	// The uninstall runs whatever the capture did. A guest left holding
	// the old version is worse than a guest with no baseline.
	if out, err := Exec(ctx, p.Tools, vm,
		sudo(p.prefixOf().Port(), build.UninstallArgs(port))...); err != nil {
		return fmt.Errorf("%w: the baseline %s@%s would not uninstall, and the build would have verified it instead of the change: %s",
			verify.ErrNoEnvironment, port, version, strings.TrimSpace(out))
	}

	if cerr != nil {
		return decline("the installed baseline could not be described: " + oneLine(captured))
	}
	return p.put(ctx, vm, baselineFile, verify.BaselineArchive+"\n")
}

// sudo is port(1) run as root, as an argv. The privilege drop is
// spelled by absolute path because the guest agent runs with whatever
// PATH the agent has, and the runner script makes the same call the same
// way. Nothing is quoted because nothing reaches a shell: a port name
// and a variant travel as their own argv words.
func sudo(portCmd string, args []string) []string {
	return append([]string{"/usr/bin/sudo", "-n", portCmd}, args...)
}

// oneLine flattens a transcript into something that fits on the line a
// finding gets: the last thing said, which is where port(1) and tart
// both put what stopped them.
func oneLine(s string) string {
	var last string
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			last = t
		}
	}
	if last == "" {
		return "the environment said nothing"
	}
	return last
}
