package github

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	gh "github.com/google/go-github/v91/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
)

const ProviderName = "github"
const WorkflowPath = ".github/workflows/main.yml"

// Config freezes the remote destination; command defaults cannot redirect recovery.
type Config struct {
	Destination record.PublicationDestination
	WorkflowID  int64
}

type actionsAPI interface {
	Workflow(context.Context, string) (*gh.Workflow, error)
	Runs(context.Context, int64, string, string) ([]*gh.WorkflowRun, error)
	Run(context.Context, int64, int) (*gh.WorkflowRun, error)
	Jobs(context.Context, int64, int) ([]*gh.WorkflowJob, error)
	JobLog(context.Context, int64) (io.ReadCloser, error)
}

func BuildConfig(platform record.Platform, config Config, needsXcode bool) (record.BuildConfig, error) {
	if err := config.validate(); err != nil {
		return record.BuildConfig{}, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return record.BuildConfig{}, err
	}
	// The workflow controls mutable hosted runners. This identifies the recipe, not a VM image.
	return record.BuildConfig{Provider: ProviderName, Platform: platform, EnvironmentDigest: "workflow:" + digest(raw), VerifierDigest: "github-workflow-v2", ProviderConfig: raw, Tests: record.TestWorkflow, NeedsXcode: needsXcode}, nil
}

func (c Config) validate() error {
	d := c.Destination
	if d.Forge != ProviderName || !strings.EqualFold(d.Repository, "macports/macports-ports") || strings.EqualFold(d.HeadRepository, d.Repository) || d.HeadRepository == "" || d.PushURL == "" || d.BaseURL == "" || !filepath.IsAbs(d.LockDirectory) || d.BaseBranch != "master" || c.WorkflowID <= 0 {
		return fmt.Errorf("github verification: a personal macports-ports fork, upstream master, and active workflow are required")
	}
	return nil
}

func digest(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

// Configure observes the supported fork workflow and freezes its identity.
func Configure(ctx context.Context, client *githubapi.Client, platform record.Platform, destination record.PublicationDestination, needsXcode bool) (record.BuildConfig, error) {
	api, err := newActions(ctx, client, destination.HeadRepository)
	if err != nil {
		return record.BuildConfig{}, err
	}
	flow, err := api.Workflow(ctx, "main.yml")
	if err != nil {
		return record.BuildConfig{}, fmt.Errorf("github verification: reading main.yml in your fork: %w", err)
	}
	if flow.GetState() != "active" || flow.GetPath() != WorkflowPath {
		return record.BuildConfig{}, fmt.Errorf("github verification: enable main.yml in your fork's Actions settings")
	}
	return BuildConfig(platform, Config{Destination: destination, WorkflowID: flow.GetID()}, needsXcode)
}
