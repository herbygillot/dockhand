package portedit

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/dependency"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/text"
)

// Assessment describes preparation evidence, not whether a port will build.
type Assessment struct {
	Scope          *record.ReleaseScope `json:",omitempty"`
	Coverage       []ContextCoverage    `json:",omitempty"`
	Contexts       []record.Platform    `json:",omitempty"`
	Outcome        string
	CurrentVersion string
	Portfile       string
	Inputs         []VersionInput
	Findings       []Finding
	Release        *record.Release `json:",omitempty"`
}

// VersionInput locates a literal candidate in the original committed Portfile.
// Its presence alone does not establish that any particular release is editable.
type VersionInput struct {
	Line, Column int
	Value        string
}

// Finding has a stable check/code and a human-readable explanation.
type Finding struct {
	Check  string
	Status string
	Code   string
	Detail string
}

const (
	InputFound       = "input-found"
	CandidateChecked = "candidate-checked"
	Passed           = "passed"
	Blocked          = "blocked"
	Unsupported      = "unsupported"
	Unknown          = "unknown"
	NotTested        = "not-tested"
)

// Problem preserves typed failure distinctions without interpreting error text.
func Problem(check string, err error) Finding {
	status, code := Unknown, "check-inconclusive"
	switch {
	case errors.Is(err, dependency.ErrToolUnavailable):
		status, code = Blocked, "missing-helper"
	case errors.Is(err, errProbeInconclusive):
		code = "probe-inconclusive"
	case errors.Is(err, portsource.ErrTagPattern):
		code = "tag-pattern-unknown"
	case errors.Is(err, ErrFidelity):
		status, code = Unsupported, "edit-fidelity"
	case errors.Is(err, ErrUnsupported), errors.Is(err, portsource.ErrUnsupported):
		status, code = Unsupported, "unsupported-convention"
	}
	return Finding{Check: check, Status: status, Code: code, Detail: err.Error()}
}

// Summarize retains all findings; the outcome prioritizes established limitations
// over missing prerequisites, then uncertainty. Not-tested stages remain explicit.
func (a *Assessment) Summarize() {
	a.Outcome = Unknown
	for _, f := range a.Findings {
		if f.Check == "version-input" && f.Status == Passed {
			a.Outcome = InputFound
		}
		if f.Check == "candidate" && f.Status == Passed {
			a.Outcome = CandidateChecked
		}
	}
	for _, status := range []string{Unknown, Blocked, Unsupported} {
		for _, f := range a.Findings {
			if f.Status == status {
				a.Outcome = status
				break
			}
		}
	}
}

// Assess checks declarations and optional candidate fidelity without downloading
// archives or executing dependency generators. It restores each temporary edit.
func (p *VersionProbe) Assess(ctx context.Context, release *record.Release) (Assessment, error) {
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	a := Assessment{CurrentVersion: p.input.info.Version, Portfile: p.input.target.Portfile, Release: release}
	add := func(check, detail string, err error) {
		if err != nil {
			a.Findings = append(a.Findings, Problem(check, err))
		} else {
			a.Findings = append(a.Findings, Finding{Check: check, Status: Passed, Code: check + "-checked", Detail: detail})
		}
	}
	add("evaluation", "Committed Portfile evaluated successfully", nil)
	spec, err := portsource.Interpret(p.input.info, portsource.Edit)
	sourceDetail := fmt.Sprintf("%s %s; source version %s", spec.Forge, spec.Repository, spec.SourceVersion)
	if err == nil && spec.Forge == "" {
		sourceDetail = "Archive source version " + spec.SourceVersion
	}
	add("source", sourceDetail, err)
	if discovery, discoveryErr := portsource.Interpret(p.input.info, portsource.Discovery); discoveryErr == nil {
		detail := "Supported " + string(discovery.Catalog) + " discovery; remote availability is untested"
		if discovery.Livecheck.Overridden {
			detail = "Supported discovery through the port's own livecheck, proven against the " + string(discovery.Catalog) + " catalog; remote availability is untested"
		}
		a.Findings = append(a.Findings, Finding{Check: "discovery", Status: Passed, Code: "discovery-supported", Detail: detail})
	} else {
		a.Findings = append(a.Findings, Finding{Check: "discovery", Status: NotTested, Code: "explicit-version-required", Detail: discoveryErr.Error() + "; supply an explicit version"})
	}
	if err == nil {
		err = p.prepare(ctx)
		add("version-input", "Literal input candidates found; a specific release still needs edit-fidelity checks", err)
		if err == nil {
			for _, carrier := range p.carriers {
				line, col := text.Position(p.input.data, carrier.candidate.Span.Start)
				a.Inputs = append(a.Inputs, VersionInput{Line: line, Column: col, Value: carrier.candidate.Value})
			}
		}
	} else {
		a.Findings = append(a.Findings, Finding{Check: "version-input", Status: NotTested, Code: "source-required", Detail: "Version probing requires an evaluable version convention"})
	}
	base := p.input
	plan, depErr := inspectDependencies(p.input)
	detail := "No generated dependency block requires a helper"
	if depErr == nil && plan != nil {
		detail = "Recognized " + plan.Kind + " declaration"
	}
	add("dependencies", detail, depErr)
	if depErr == nil && plan != nil {
		executable, toolErr := p.editor.DependencyTools.Resolve(plan.Kind)
		add("helper", executable, toolErr)
		base, _, depErr = p.editor.dependencyBase(ctx, p.request, p.input, plan)
		if depErr != nil {
			add("dependency-source", "", depErr)
		}
		a.Findings = append(a.Findings, Finding{Check: "regeneration", Status: NotTested, Code: "archives-required", Detail: "Manifest regeneration and preservation of maintained overrides require source archives"})
	}
	if depErr == nil {

		coverage, fetchErr, checksumErr := p.editor.assessArchives(ctx, p.request, base)
		a.Coverage = coverage
		for _, context := range coverage {
			if context.Fetch != nil && context.Fetch.Rejected {
				a.Findings = append(a.Findings, Finding{Check: "platform-restriction", Status: NotTested, Code: "fetch-rejected", Detail: fmt.Sprintf("%s %s %s: archive metadata is available, but the preserved pre-fetch guard rejects this platform", context.Platform.OS, context.Platform.Version, context.Platform.Architecture)})
			}
		}
		fetchDetail, checksumDetail := "MacPorts archive locations are observed; availability is untested", "Checksum declarations are associated across the observed contexts"
		if gitFetched(p.input.info) {
			fetchDetail, checksumDetail = "Git source; the build clones git.url at git.branch", "No checksums: the source is cloned, not downloaded"
		}
		add("fetch", fetchDetail, fetchErr)
		if fetchErr == nil {
			if checksumErr == nil {
				for _, context := range coverage {
					a.Contexts = append(a.Contexts, context.Platform)
				}
			}
			add("checksums", checksumDetail, checksumErr)
		} else {
			a.Findings = append(a.Findings, Finding{Check: "checksums", Status: NotTested, Code: "sources-required", Detail: "Checksum association requires supported archive sources"})
		}
	} else {
		a.Findings = append(a.Findings, Finding{Check: "fetch", Status: NotTested, Code: "dependency-source-required", Detail: "Primary archive checks require a supported dependency declaration"})
	}
	if release != nil && depErr == nil {
		request := p.request
		request.Version, request.Release = release.Requested, release
		plan, candidateErr := p.editor.planArchiveVersion(ctx, request, base)
		a.Scope = plan.result.Scope
		add("candidate", "Resolved release passes pre-download version, source, checksum, and edit-fidelity checks", candidateErr)
	} else {
		a.Findings = append(a.Findings, Finding{Check: "candidate", Status: NotTested, Code: "candidate-unchecked", Detail: "A resolved release and supported dependency source are required to check a specific update"})
	}
	a.Findings = append(a.Findings,
		Finding{Check: "archives", Status: NotTested, Code: "archives-not-downloaded", Detail: "Archive downloads and checksum regeneration were not performed"},
		Finding{Check: "verification", Status: NotTested, Code: "build-not-run", Detail: "Lint, builds, tests, and installation were not performed"})
	a.Summarize()
	return a, ctx.Err()
}
