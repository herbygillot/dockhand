package github

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"go.yaml.in/yaml/v3"
)

// source checks the immutable contribution and reads what its own workflow
// says about itself.
func (p *Provider) source(ctx context.Context, request verify.Request) ([]string, error) {
	spec := request.Spec
	if spec.ReplaceRemoteHead != "" && !git.ValidObjectID(string(spec.ReplaceRemoteHead)) {
		return nil, fmt.Errorf("github verification: invalid replacement precondition")
	}
	if request.AttemptID == "" {
		return nil, fmt.Errorf("github verification: attempt identity is required")
	}
	if err := verify.ValidateConfig(spec.Config); err != nil {
		return nil, err
	}
	if !git.ValidBranchName(spec.Branch) || spec.Branch == "master" || (spec.RemoteBranch != "" && (!git.ValidBranchName(spec.RemoteBranch) || spec.RemoteBranch == "master")) || !git.ValidObjectID(string(spec.Source.Commit)) || !git.ValidObjectID(string(spec.Source.Tree)) {
		return nil, fmt.Errorf("github verification: select a committed contribution with --branch")
	}
	if spec.Config.Provider != verify.ProviderGitHub || spec.Config.Tests != record.TestWorkflow || spec.Config.FromSource || len(spec.Target.Variants) != 0 || len(spec.Inputs) != 0 {
		return nil, fmt.Errorf("github verification: the existing workflow controls tests, variants, dependencies, and its runner matrix")
	}
	trees, err := p.Repo.CommitTrees(ctx, []string{string(spec.Source.Commit)})
	if err != nil {
		return nil, err
	}
	if trees[string(spec.Source.Commit)] != string(spec.Source.Tree) {
		return nil, fmt.Errorf("github verification: commit does not contain the requested tree")
	}
	commit, err := changeset.DeriveSingleCommit(ctx, p.Repo, spec.Source)
	if err != nil {
		return nil, err
	}
	if spec.Source.Base != "" && commit.Source.Base != spec.Source.Base {
		return nil, fmt.Errorf("github verification: contribution must contain one commit above its base")
	}
	directory := path.Dir(spec.Target.Portfile)
	if len(strings.Split(directory, "/")) != 2 || strings.HasPrefix(directory, ".") || strings.HasPrefix(directory, "_") || path.Base(spec.Target.Portfile) != "Portfile" || len(commit.Paths) == 0 {
		return nil, fmt.Errorf("github verification: one changed port directory is required")
	}
	for _, name := range commit.Paths {
		if !strings.HasPrefix(name, directory+"/") {
			return nil, fmt.Errorf("github verification: change outside the selected port: %s", name)
		}
	}
	paths, err := p.Repo.AddedOrModifiedPaths(ctx, string(commit.Source.Base), string(spec.Source.Commit))
	if err != nil {
		return nil, err
	}
	detected := false
	for _, name := range paths {
		if name == spec.Target.Portfile || strings.HasPrefix(name, directory+"/files/") {
			detected = true
		}
	}
	if !detected {
		return nil, fmt.Errorf("github verification: the workflow would not detect this port change")
	}
	raw, err := p.Repo.ReadBlob(ctx, string(spec.Source.Commit)+":"+macports.PortsWorkflowPath)
	if err != nil {
		return nil, fmt.Errorf("github verification: reading candidate workflow: %w", err)
	}
	return readWorkflow(raw, spec.PushBranch())
}

// readWorkflow reads what the candidate commit's workflow says about itself.
//
// It does not pattern-match the workflow's shape, as an earlier version did.
// The checks above confine the contribution to one commit touching one port
// directory above a base present in upstream master, so the file that will run
// is upstream's own; how many jobs it has, what they are called, and which
// runners it names are MacPorts' business, and a shape match only refused
// changes it was entitled to make. It is also the weaker guarantee: a workflow
// could keep the expected skeleton and build nothing.
//
// Two things are still worth reading here, before anything is pushed. A push
// trigger that would not run for the branch about to be pushed is refused,
// because that run would never exist and waiting for it would be waiting for
// nothing. And a runner matrix, when the workflow names its jobs after it,
// says which jobs to expect; when it does not, that is not an error, and
// observation judges the run by the jobs it reports instead.
func readWorkflow(raw []byte, branch string) ([]string, error) {
	var workflow struct {
		On   map[string]workflowTrigger
		Jobs map[string]workflowJob
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return nil, fmt.Errorf("github verification: unsupported workflow: %w", err)
	}
	push, ok := workflow.On["push"]
	if !ok {
		return nil, fmt.Errorf("github verification: %s has no push trigger, so pushing a branch would start no run", macports.PortsWorkflowPath)
	}
	if !pushRuns(push, branch) {
		return nil, fmt.Errorf("github verification: the push filters in %s exclude %s, so pushing it would start no run", macports.PortsWorkflowPath, branch)
	}
	return expectedJobs(workflow.Jobs), nil
}

type workflowTrigger struct {
	Branches       []string
	BranchesIgnore []string `yaml:"branches-ignore"`
}

type workflowJob struct {
	Name     string
	Strategy struct {
		Matrix struct {
			OS      []string `yaml:"os"`
			Include []any
			Exclude []any
		}
	}
}

// pushRuns applies the trigger's branch filters the way GitHub does: a branch
// named by branches-ignore does not run, and with branches only a named branch
// does. Path filters are not read; a filter that excluded a port directory is
// not a thing this workflow would say, and a push that starts no run is
// already diagnosed as a missing run.
func pushRuns(trigger workflowTrigger, branch string) bool {
	for _, pattern := range trigger.BranchesIgnore {
		if matchRef(pattern, branch) {
			return false
		}
	}
	if len(trigger.Branches) == 0 {
		return true
	}
	for _, pattern := range trigger.Branches {
		if matchRef(pattern, branch) {
			return true
		}
	}
	return false
}

// matchRef applies one GitHub ref pattern, where * spans one path segment and
// ** spans any number. A pattern this does not understand matches nothing,
// which keeps an unreadable filter from silently excluding a branch.
func matchRef(pattern, branch string) bool {
	if pattern == branch {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return false
	}
	// ** is the only multi-segment wildcard; split on it and match the
	// single-segment remainder of each part with path.Match.
	parts := strings.Split(pattern, "**")
	rest := branch
	for i, part := range parts {
		if part == "" {
			continue
		}
		matched, index := segmentMatch(part, rest, i == 0, i == len(parts)-1)
		if !matched {
			return false
		}
		rest = rest[index:]
	}
	return true
}

func segmentMatch(pattern, branch string, anchored, final bool) (bool, int) {
	if anchored && final {
		ok, err := path.Match(pattern, branch)
		return err == nil && ok, len(branch)
	}
	for i := 0; i <= len(branch); i++ {
		if anchored && i > 0 {
			break
		}
		for j := len(branch); j >= i; j-- {
			if final && j != len(branch) {
				continue
			}
			ok, err := path.Match(pattern, branch[i:j])
			if err == nil && ok {
				return true, j
			}
		}
	}
	return false, 0
}

// expectedJobs names the jobs a run should carry, but only when the workflow
// makes that prediction sound: one job with a runner matrix, no include or
// exclude entries to widen it, and a job name that is the matrix value itself,
// which is what GitHub will then call each job. Anything else returns nothing,
// and the run speaks for itself.
func expectedJobs(jobs map[string]workflowJob) []string {
	var names []string
	for _, job := range jobs {
		matrix := job.Strategy.Matrix
		if len(matrix.OS) == 0 {
			continue
		}
		if names != nil || len(matrix.Include)+len(matrix.Exclude) != 0 || job.Name != "${{ matrix.os }}" {
			return nil
		}
		seen := map[string]bool{}
		for _, name := range matrix.OS {
			if name == "" || strings.ContainsAny(name, " ${}\r\n") || seen[name] {
				return nil
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}
