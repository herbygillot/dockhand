package tart

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The two capabilities are the contract, provably: a provider that
// drifts fails to build.
var (
	_ verify.Manifester = Provider{}
	_ verify.Prober     = Provider{}
)

// Manifests describes the installation this job made, and whatever it
// was measured against.
//
// It must be called while the guest is still held. Everything it needs
// is inside the environment — the baseline taken before the overlay was
// staged, the installation the build produced, and the bindings between
// them — and a released worker takes both sides of the comparison with
// it.
//
// The baseline is read rather than taken: it was measured at submit,
// before the change was staged, because that is the only moment the
// before exists. What is taken here is the after, live, and the link
// proof across the cohort's members.
//
// A guest that cannot describe itself is an error and never an empty
// comparison. An empty installed manifest reads as a port that laid
// nothing down, and an empty baseline reads as every library removed;
// both are findings about the port, and neither is what a guest that
// stopped answering means.
func (p Provider) Manifests(ctx context.Context, job verify.Job) (verify.Manifests, error) {
	if err := p.owns(ctx, job); err != nil {
		return verify.Manifests{}, err
	}
	roster, ok := p.get(ctx, job.ID, rosterFile)
	if !ok {
		return verify.Manifests{}, fmt.Errorf("%w: %s was submitted without a manifest, so there is nothing to describe",
			verify.ErrUnsupported, job.ID)
	}
	ports := rosterOf(roster)
	if len(ports) == 0 {
		return verify.Manifests{}, fmt.Errorf("%w: the roster in %s names no subject", verify.ErrNoEnvironment, job.ID)
	}

	out := verify.Manifests{}
	out.BaselineSource, out.BaselineReason = p.baselineSource(ctx, job.ID)
	if out.BaselineSource == verify.BaselineArchive {
		pre, ok := p.get(ctx, job.ID, preFile)
		switch {
		case !ok:
			out.BaselineSource, out.BaselineReason = verify.BaselineNone,
				"the environment recorded a baseline and the capture is not there"
		default:
			c, err := build.ParseManifest(pre)
			if err != nil {
				out.BaselineSource, out.BaselineReason = verify.BaselineNone,
					"the baseline capture could not be read: "+err.Error()
			} else {
				out.Baseline = &c.Manifest
			}
		}
	}

	// The headline is what the change is about, so its installation is
	// the one being described. A build that failed before it installed
	// anything leaves no manifest, and that is reported as nothing to
	// measure rather than as a port that laid nothing down.
	head, err := p.capture(ctx, job.ID, 0)
	if err != nil {
		return verify.Manifests{}, err
	}
	if head != nil {
		out.Installed = &head.Manifest
	}

	// The link proof: which of the cohort's members actually bound to
	// what the headline now publishes. It is asked of the members and
	// never of the headline, because the question is whether the ports
	// standing on the change still stand on it.
	//
	// Attributed per member, here, where the attribution still exists.
	// The roster's position IS the member's name, and a capture is read
	// per position; once the bindings are merged into one set the file
	// paths are all that is left, and no file path says which port
	// installed it. A member that installed and bound to nothing keeps
	// an EMPTY map rather than no entry — the sweep ran and found
	// nothing, which is what makes the build-only claim a measurement.
	if head != nil && len(ports) > 1 {
		out.Links = map[string]map[string][]string{}
		published := map[string]bool{}
		for _, d := range head.Manifest.Dylibs {
			published[d.InstallName] = true
		}
		for i := 1; i < len(ports); i++ {
			member, err := p.capture(ctx, job.ID, i)
			if err != nil || member == nil {
				// A member the cohort never reached installed nothing, so it
				// has nothing to link. That silence is the judge's evidence
				// and must not become this call's failure — and it is a
				// missing key rather than an empty one, because nobody
				// looked.
				continue
			}
			bound := map[string][]string{}
			for name, files := range member.LinksTo {
				if !published[name] {
					continue
				}
				for _, f := range files {
					if !slices.Contains(bound[name], f) {
						bound[name] = append(bound[name], f)
					}
				}
			}
			for name := range bound {
				slices.Sort(bound[name])
			}
			out.Links[ports[i]] = bound
		}
	}
	return out, nil
}

// capture runs the manifest script for one subject and reads the result
// back. A port that is not installed produces a capture with no version
// and no files, which is nothing to measure rather than a measurement of
// nothing — so it comes back nil.
func (p Provider) capture(ctx context.Context, vm string, i int) (*build.Capture, error) {
	dest := manifestFile(i)
	script := build.ManifestScript(p.prefixOf().Port(), dest, rosterFile, i)
	if out, err := Exec(ctx, p.Tools, vm, "/bin/sh", "-c", script); err != nil {
		return nil, fmt.Errorf("%w: describing subject %d in %s: %s", verify.ErrNoEnvironment, i, vm, strings.TrimSpace(out))
	}
	body, ok := p.get(ctx, vm, dest)
	if !ok {
		return nil, fmt.Errorf("%w: subject %d in %s wrote no manifest", verify.ErrNoEnvironment, i, vm)
	}
	c, err := build.ParseManifest(body)
	if err != nil {
		return nil, fmt.Errorf("%w: subject %d in %s: %w", verify.ErrNoEnvironment, i, vm, err)
	}
	if c.Manifest.Version == "" && len(c.Manifest.Files) == 0 {
		return nil, nil
	}
	return &c, nil
}

// baselineSource reads what the submit recorded about the before: the
// source word and, where there is none, the reason.
//
// A job whose baseline file is missing entirely gets "none" with a
// reason saying so, rather than an empty source. An empty source would
// be read as a field nobody filled in, and the difference between "we
// did not look" and "we looked and there was nothing" is the whole
// content of the finding this produces.
func (p Provider) baselineSource(ctx context.Context, vm string) (source, reason string) {
	body, ok := p.get(ctx, vm, baselineFile)
	if !ok {
		return verify.BaselineNone, "the environment recorded no baseline"
	}
	lines := strings.SplitN(strings.TrimRight(body, "\n"), "\n", 2)
	source = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		reason = strings.TrimSpace(lines[1])
	}
	switch source {
	case verify.BaselineArchive, verify.BaselineBanked:
		return source, ""
	case verify.BaselineNone:
		if reason == "" {
			reason = "the environment did not say why"
		}
		return verify.BaselineNone, reason
	}
	return verify.BaselineNone, "the environment recorded an unreadable baseline: " + oneLine(body)
}

// rosterOf reads the subjects the submit wrote, in order.
func rosterOf(body string) []string {
	var out []string
	for line := range strings.Lines(body) {
		if p := strings.TrimSpace(line); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Probe runs the port's own binaries in the environment that built them
// and reports what they said.
//
// It is the cheapest evidence that a build which succeeded also produced
// something that runs — an install can lay down a program the loader
// refuses, and every phase up to it passes. A port the job did not build
// gets no lines rather than an error: a caller asking about a member the
// cohort never reached has learned that nothing was run, which is true.
func (p Provider) Probe(ctx context.Context, job verify.Job, port string) ([]verify.ProbeLine, error) {
	if err := p.owns(ctx, job); err != nil {
		return nil, err
	}
	roster, ok := p.get(ctx, job.ID, rosterFile)
	if !ok {
		return nil, fmt.Errorf("%w: %s was submitted without a manifest, so nothing named its subjects",
			verify.ErrUnsupported, job.ID)
	}
	i := slices.Index(rosterOf(roster), port)
	if i < 0 {
		return nil, fmt.Errorf("%w: %s did not build %s", verify.ErrUnknownJob, job.ID, port)
	}
	dest := probeFile(i)
	// Cleaned, because the prefix reaches the script as the head of a
	// glob: a trailing slash would make <prefix>//bin/* match nothing and
	// the probe would silently find no programs at all.
	script := build.ProbeScript(p.prefixOf().Port(), dest, rosterFile, i, filepath.Clean(string(p.prefixOf())))
	if out, err := Exec(ctx, p.Tools, job.ID, "/bin/sh", "-c", script); err != nil {
		return nil, fmt.Errorf("%w: probing %s in %s: %s", verify.ErrNoEnvironment, port, job.ID, strings.TrimSpace(out))
	}
	body, ok := p.get(ctx, job.ID, dest)
	if !ok {
		return nil, nil
	}
	return build.ParseProbes(body), nil
}

// owns is the guard Exec and Poll take, in one place: a job belonging to
// another provider is a contract error, and a worker that is gone is
// unknown rather than empty.
func (p Provider) owns(ctx context.Context, job verify.Job) error {
	if job.Provider != "tart" {
		return fmt.Errorf("%w: %s is not a tart job", verify.ErrUnknownJob, job.Provider)
	}
	if ok, err := HasVM(ctx, p.Tools, job.ID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %s", verify.ErrUnknownJob, job.ID)
	}
	return nil
}
