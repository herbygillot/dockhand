package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

type Provider struct {
	State      state.ProviderStore
	Repository record.RepositoryID
	Repo       *git.Repository
	Directory  string
	Actions    func(context.Context, string) (Actions, error)
}

type payload struct {
	Request  verify.Request
	Config   Config
	Expected git.RefValue
	Matrix   []string
}

type executionRun struct {
	ID      int64
	Attempt int
}

func (p *Provider) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: ProviderName, Isolated: true}, nil
}

func (p *Provider) locked(ctx context.Context, id record.RequestID, fn func(context.Context) error) error {
	if p.State == nil || p.Repo == nil || p.Repository == "" || !filepath.IsAbs(p.Directory) || id == "" || p.Actions == nil {
		return fmt.Errorf("github verification: state, repository, Actions client, and absolute coordination directory are required")
	}
	_, err := p.State.RegisterProviderPool(ctx, record.ProviderPool{ID: ProviderName, Scope: ProviderName, Directory: p.Directory, Capacity: 1})
	if err != nil {
		return err
	}
	lock, err := filelock.Acquire(ctx, filepath.Join(p.Directory, digest([]byte(id))+".lock"), filelock.Exclusive)
	if err != nil {
		return err
	}
	defer lock.Close()
	return fn(ctx)
}

func (p *Provider) read(ctx context.Context, id record.RequestID) (record.ProviderExecution, error) {
	var value record.ProviderExecution
	err := p.State.ProviderView(ctx, ProviderName, func(ctx context.Context, r state.ProviderReader) error {
		var err error
		value, err = r.Execution(ctx, id)
		if err == nil && value.RepositoryID != p.Repository {
			return state.ErrConflict
		}
		return err
	})
	return value, err
}
func (p *Provider) put(ctx context.Context, value record.ProviderExecution) error {
	return p.State.ProviderUpdate(ctx, ProviderName, func(ctx context.Context, tx state.ProviderTx) error { return tx.PutExecution(ctx, value) })
}

func (p *Provider) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	result := verify.Submission{State: verify.SubmissionUncertain}
	err := p.locked(ctx, request.ID, func(ctx context.Context) error {
		row, err := p.read(ctx, request.ID)
		if err == nil {
			if row.State == record.ExecutionClosed {
				result = verify.Submission{State: verify.Unsupported, Detail: "GitHub submission is permanently closed"}
				return nil
			}
			var saved payload
			if err := json.Unmarshal(row.Payload, &saved); err != nil {
				return err
			}
			if !reflect.DeepEqual(saved.Request, request) {
				return state.ErrConflict
			}
			result, err = p.advance(ctx, row)
			return err
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		reject := func(detail string) error {
			result = verify.Submission{State: verify.Unsupported, Detail: detail}
			return p.put(ctx, record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, State: record.ExecutionClosed, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)})
		}
		matrix, err := p.source(ctx, request)
		var config Config
		if err == nil {
			err = json.Unmarshal(request.Spec.Config.ProviderConfig, &config)
		}
		if err == nil {
			err = config.validate()
		}
		if err != nil {
			return reject(err.Error())
		}
		d := config.Destination
		return p.Repo.WithRemoteBranchLock(ctx, d.LockDirectory, ProviderName, d.HeadRepository, request.Spec.Branch, func(ctx context.Context) error {
			snapshot, err := changeset.CaptureBranch(ctx, p.Repo, request.Spec.Branch)
			if err != nil {
				return err
			}
			if snapshot.Commit != request.Spec.Source.Commit || snapshot.Tree != request.Spec.Source.Tree {
				return reject("github verification: local branch changed after acceptance; verify the new committed branch")
			}
			source, err := changeset.DeriveSingleCommit(ctx, p.Repo, request.Spec.Source)
			if err != nil {
				return err
			}
			if err := p.Repo.CheckContributionBase(ctx, d.BaseURL, d.BaseBranch, string(source.Source.Base), string(source.Source.Commit)); err != nil {
				return err
			}
			api, err := p.Actions(ctx, d.HeadRepository)
			if err != nil {
				return err
			}
			workflow, err := api.Workflow(ctx, "main.yml")
			if err != nil {
				return err
			}
			if workflow.GetID() != config.WorkflowID || workflow.GetPath() != WorkflowPath || workflow.GetState() != "active" {
				return reject("github verification: enable the expected main.yml workflow in your fork's Actions settings")
			}
			expected, err := p.Repo.RemoteHead(ctx, d.PushURL, request.Spec.Branch)
			if err != nil {
				return err
			}
			if expected.Exists && expected.Object != string(request.Spec.Source.Commit) && expected.Object != string(source.Source.Base) {
				return reject("github verification: remote branch already has another commit; reconcile and push it with Git before verifying")
			}
			raw, err := json.Marshal(payload{Request: request, Config: config, Expected: expected, Matrix: matrix})
			if err != nil {
				return err
			}
			row = record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, AttemptID: request.AttemptID, Payload: raw, Resource: string(request.ID), Occupied: true, State: record.ExecutionReserved, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
			if err = p.put(ctx, row); err != nil {
				return err
			}
			result, err = p.pushAndFind(ctx, row)
			return err
		})
	})
	return result, err
}

func (p *Provider) advance(ctx context.Context, row record.ProviderExecution) (verify.Submission, error) {
	if row.State == record.ExecutionAdmitted {
		return admitted(row)
	}
	var saved payload
	if err := json.Unmarshal(row.Payload, &saved); err != nil {
		return verify.Submission{}, err
	}
	d := saved.Config.Destination
	var result verify.Submission
	err := p.Repo.WithRemoteBranchLock(ctx, d.LockDirectory, ProviderName, d.HeadRepository, saved.Request.Spec.Branch, func(ctx context.Context) error {
		var err error
		result, err = p.pushAndFind(ctx, row)
		return err
	})
	return result, err
}

func (p *Provider) pushAndFind(ctx context.Context, row record.ProviderExecution) (verify.Submission, error) {
	result := verify.Submission{State: verify.SubmissionUncertain, Detail: "Waiting for the fork workflow run to appear"}
	var saved payload
	if err := json.Unmarshal(row.Payload, &saved); err != nil {
		return result, err
	}
	spec, d := saved.Request.Spec, saved.Config.Destination
	api, err := p.Actions(ctx, d.HeadRepository)
	if err != nil {
		return result, err
	}
	// Observe first: a newer push may have moved the branch while the accepted run still exists.
	runs, err := api.Runs(ctx, saved.Config.WorkflowID, spec.Branch, string(spec.Source.Commit))
	if err != nil {
		return result, err
	}
	var selected *gh.WorkflowRun
	for _, run := range runs {
		if !matches(saved, run) {
			continue
		}
		if selected != nil && selected.GetID() != run.GetID() {
			return result, fmt.Errorf("github verification: multiple workflow runs match this commit and branch")
		}
		selected = run
	}
	if selected != nil {
		if selected.GetRunAttempt() <= 0 || selected.GetID() <= 0 {
			return result, fmt.Errorf("github verification: incomplete workflow run identity")
		}
		row.Result, err = json.Marshal(executionRun{ID: selected.GetID(), Attempt: selected.GetRunAttempt()})
		if err != nil {
			return result, err
		}
		row.State, row.Occupied = record.ExecutionAdmitted, false
		if err := p.put(ctx, row); err != nil {
			return result, err
		}
		return admitted(row)
	}
	// Repeating a confirmed push is a no-op. Never dispatch an uncorrelated second run.
	if err := p.Repo.Push(ctx, git.Push{Remote: d.PushURL, Branch: spec.Branch, Commit: string(spec.Source.Commit), ExpectedRemote: saved.Expected}); err != nil {
		return result, err
	}
	return result, nil
}

func admitted(row record.ProviderExecution) (verify.Submission, error) {
	var run executionRun
	if err := json.Unmarshal(row.Result, &run); err != nil {
		return verify.Submission{}, err
	}
	if run.ID <= 0 || run.Attempt <= 0 {
		return verify.Submission{}, fmt.Errorf("github verification: invalid stored run identity")
	}
	return verify.Submission{State: verify.Admitted, Run: record.ProviderRun{Provider: ProviderName, RequestID: row.ID, RunID: fmt.Sprintf("%d:%d", run.ID, run.Attempt)}}, nil
}

func (p *Provider) Reconcile(ctx context.Context, id record.RequestID) (verify.Reconciliation, error) {
	result := verify.Reconciliation{State: verify.RunUnknown}
	err := p.locked(ctx, id, func(ctx context.Context) error {
		row, err := p.read(ctx, id)
		if errors.Is(err, state.ErrNotFound) {
			err = p.put(ctx, record.ProviderExecution{ID: id, RepositoryID: p.Repository, State: record.ExecutionClosed, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)})
			result.State = verify.RequestClosed
			return err
		}
		if err != nil {
			return err
		}
		if row.State == record.ExecutionClosed {
			result.State = verify.RequestClosed
			return nil
		}
		result.Submission, err = p.advance(ctx, row)
		if err == nil && result.Submission.State == verify.Admitted {
			result.State = verify.RunFound
		}
		return err
	})
	return result, err
}
