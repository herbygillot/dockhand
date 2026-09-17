package github

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"go.yaml.in/yaml/v3"
)

// source checks the immutable contribution and reads its own workflow matrix.
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
	return workflowMatrix(raw)
}

func workflowMatrix(raw []byte) ([]string, error) {
	var workflow struct {
		On map[string]struct {
			BranchesIgnore []string `yaml:"branches-ignore"`
			Branches       []string
			Paths          []string
			PathsIgnore    []string `yaml:"paths-ignore"`
		}
		Jobs map[string]struct {
			Name     string
			RunsOn   string `yaml:"runs-on"`
			If       string
			Strategy struct {
				Matrix struct {
					OS      []string
					Include []any
					Exclude []any
				}
			}
		}
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return nil, fmt.Errorf("github verification: unsupported workflow: %w", err)
	}
	push, ok := workflow.On["push"]
	if !ok || !slices.Equal(push.BranchesIgnore, []string{"master"}) || len(push.Branches)+len(push.Paths)+len(push.PathsIgnore) != 0 {
		return nil, fmt.Errorf("github verification: unsupported workflow push filters")
	}
	build, ok := workflow.Jobs["build"]
	if !ok || len(workflow.Jobs) != 1 || build.Name != "${{ matrix.os }}" || build.RunsOn != "${{ matrix.os }}" || build.If != "" || len(build.Strategy.Matrix.Include)+len(build.Strategy.Matrix.Exclude) != 0 || len(build.Strategy.Matrix.OS) == 0 {
		return nil, fmt.Errorf("github verification: expected the MacPorts build matrix in main.yml")
	}
	names := build.Strategy.Matrix.OS
	seen := map[string]bool{}
	for _, name := range names {
		if !strings.HasPrefix(name, "macos-") || strings.ContainsAny(name, " ${}\r\n") || seen[name] {
			return nil, fmt.Errorf("github verification: unsupported or duplicate matrix runner %q", name)
		}
		seen[name] = true
	}
	return names, nil
}
