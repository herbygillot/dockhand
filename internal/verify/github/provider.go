package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"net/http"
	"path/filepath"
	"reflect"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
)

type Provider struct {
	Client     *githubapi.Client
	State      state.ProviderStore
	Repository record.RepositoryID
	Repo       *git.Repository
	Directory  string
	backend    func(context.Context, string) (actionsAPI, error)
}

type payload struct {
	Request  verify.Request
	Config   Config
	Expected git.RefValue
	Matrix   []string
}

type rejection struct {
	Detail string
}

type executionRun struct {
	ID      int64
	Attempt int
	URL     string `json:",omitempty"`
}

func (p *Provider) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: verify.ProviderGitHub, Isolated: true}, nil
}

// locked runs fn with the request's ledger entry open, so its row is read
// and written under the request's lock.
func (p *Provider) locked(ctx context.Context, id record.RequestID, fn func(context.Context, *ledger.Entry) error) error {
	if p.State == nil || p.Repo == nil || p.Repository == "" || !filepath.IsAbs(p.Directory) || id == "" || p.backend == nil && p.Client == nil {
		return fmt.Errorf("github verification: state, repository, Actions client, and absolute coordination directory are required")
	}
	entry, err := p.executions().Open(ctx, p.pool(), id)
	if err != nil {
		return err
	}
	defer entry.Close()
	return fn(ctx, entry)
}

// pool is the one execution pool GitHub verification shares: every driver
// on the same coordination directory takes turns through it.
func (p *Provider) pool() record.ProviderPool {
	return record.ProviderPool{ID: verify.ProviderGitHub, Scope: verify.ProviderGitHub, Directory: p.Directory, Capacity: 1}
}

// executions is the provider's ledger: its execution rows for the repository.
func (p *Provider) executions() ledger.Ledger {
	return ledger.Ledger{Store: p.State, Repository: p.Repository}
}

// read is the request's row as it stands, for observation and pruning,
// which take no lock.
func (p *Provider) read(ctx context.Context, id record.RequestID) (record.ProviderExecution, error) {
	return p.executions().Read(ctx, verify.ProviderGitHub, id)
}

func (p *Provider) Submit(ctx context.Context, request verify.Request) (verify.Submission, error) {
	result := verify.Submission{State: verify.SubmissionUncertain}
	err := p.locked(ctx, request.ID, func(ctx context.Context, e *ledger.Entry) error {
		row, err := e.Read(ctx)
		if err == nil {
			if row.State == record.ExecutionClosed || row.State == record.ExecutionReleased {
				result, err = rejectedSubmission(row)
				if result.State == "" {
					result = verify.Submission{State: verify.Unsupported, Detail: "GitHub submission is permanently closed"}
				}
				return err
			}
			var saved payload
			if err := json.Unmarshal(row.Payload, &saved); err != nil {
				return err
			}
			if !reflect.DeepEqual(saved.Request, request) {
				return state.ErrConflict
			}
			result, err = p.advance(ctx, e, row)
			return err
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		reject := func(detail string) error {
			result = verify.Submission{State: verify.Unsupported, Detail: detail}
			raw, err := json.Marshal(rejection{Detail: detail})
			if err != nil {
				return err
			}
			return e.Put(ctx, record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, State: record.ExecutionClosed, Result: raw, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)})
		}
		preflightError := func(err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var response *gh.ErrorResponse
			if errors.Is(err, git.ErrRefConflict) || errors.Is(err, git.ErrBranchMissing) || errors.Is(err, forge.ErrAuthentication) ||
				errors.As(err, &response) && response.Response != nil && (response.Response.StatusCode == http.StatusUnauthorized || response.Response.StatusCode == http.StatusNotFound) {
				return reject(err.Error())
			}
			return err
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
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return reject(err.Error())
		}
		d := config.Destination
		return p.Repo.WithRemoteBranchLock(ctx, d.LockDirectory, d.Forge, d.HeadRepository, request.Spec.PushBranch(), func(ctx context.Context) error {
			snapshot, err := changeset.CaptureBranch(ctx, p.Repo, request.Spec.Branch)
			if err != nil {
				return preflightError(err)
			}
			if snapshot.Commit != request.Spec.Source.Commit || snapshot.Tree != request.Spec.Source.Tree {
				return reject("github verification: local branch changed after acceptance; verify the new committed branch")
			}
			source, err := changeset.DeriveSingleCommit(ctx, p.Repo, request.Spec.Source)
			if err != nil {
				return preflightError(err)
			}
			if err := p.Repo.CheckContributionBase(ctx, d.BaseURL, d.BaseBranch, string(source.Source.Base), string(source.Source.Commit)); err != nil {
				return preflightError(err)
			}
			api, err := p.actions(ctx, d.HeadRepository)
			if err != nil {
				return preflightError(err)
			}
			workflow, err := api.Workflow(ctx, "main.yml")
			if err != nil {
				return preflightError(err)
			}
			if workflow.GetID() != config.WorkflowID || workflow.GetPath() != macports.PortsWorkflowPath || workflow.GetState() != "active" {
				return reject("github verification: enable the expected main.yml workflow in your fork's Actions settings")
			}
			expected, err := p.Repo.RemoteHead(ctx, d.PushURL, request.Spec.PushBranch())
			if err != nil {
				return preflightError(err)
			}
			if expected.Exists && expected.Object != string(request.Spec.Source.Commit) && expected.Object != string(source.Source.Base) && expected.Object != string(request.Spec.ReplaceRemoteHead) {
				return reject("github verification: remote branch already has another commit; reconcile and push it with Git before verifying")
			}
			raw, err := json.Marshal(payload{Request: request, Config: config, Expected: expected, Matrix: matrix})
			if err != nil {
				return err
			}
			row = record.ProviderExecution{ID: request.ID, RepositoryID: p.Repository, AttemptID: request.AttemptID, Payload: raw, Resource: string(request.ID), Occupied: true, State: record.ExecutionReserved, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
			if err = e.Put(ctx, row); err != nil {
				return err
			}
			result, err = p.pushAndFind(ctx, e, row)
			return err
		})
	})
	return result, err
}

func (p *Provider) advance(ctx context.Context, e *ledger.Entry, row record.ProviderExecution) (verify.Submission, error) {
	if row.State == record.ExecutionAdmitted || row.State == record.ExecutionReleased {
		return admitted(row)
	}
	var saved payload
	if err := json.Unmarshal(row.Payload, &saved); err != nil {
		return verify.Submission{}, err
	}
	d := saved.Config.Destination
	var result verify.Submission
	err := p.Repo.WithRemoteBranchLock(ctx, d.LockDirectory, d.Forge, d.HeadRepository, saved.Request.Spec.PushBranch(), func(ctx context.Context) error {
		var err error
		result, err = p.pushAndFind(ctx, e, row)
		return err
	})
	return result, err
}

func (p *Provider) pushAndFind(ctx context.Context, e *ledger.Entry, row record.ProviderExecution) (verify.Submission, error) {
	result := verify.Submission{State: verify.SubmissionUncertain, Detail: "Waiting for the fork workflow run to appear"}
	var saved payload
	if err := json.Unmarshal(row.Payload, &saved); err != nil {
		return result, err
	}
	spec, d := saved.Request.Spec, saved.Config.Destination
	result.Detail = fmt.Sprintf("Waiting for GitHub Actions on %s:%s at %s", d.HeadRepository, spec.PushBranch(), spec.Source.Commit)
	api, err := p.actions(ctx, d.HeadRepository)
	if err != nil {
		return result, err
	}
	// Observe first: a newer push may have moved the branch while the accepted run still exists.
	runs, err := api.Runs(ctx, saved.Config.WorkflowID, spec.PushBranch(), string(spec.Source.Commit))
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
		// The run for this commit already finished without succeeding, and the
		// engine asked for a verdict anyway, which it does only when recorded
		// evidence did not satisfy it. Adopting the old conclusion would
		// answer with what is already known, so ask GitHub to run the
		// unsuccessful jobs again and observe the attempt that produces.
		if selected.GetStatus() == "completed" && selected.GetConclusion() != "success" {
			again, rerunErr := p.rerun(ctx, api, saved, selected)
			if rerunErr != nil {
				return result, rerunErr
			}
			if again != nil {
				selected = again
			}
		}
		row.Result, err = json.Marshal(executionRun{ID: selected.GetID(), Attempt: selected.GetRunAttempt(), URL: selected.GetHTMLURL()})
		if err != nil {
			return result, err
		}
		row.State, row.Occupied = record.ExecutionAdmitted, false
		if err := e.Put(ctx, row); err != nil {
			return result, err
		}
		return admitted(row)
	}
	// Repeating a confirmed push is a no-op. Never dispatch an uncorrelated second run.
	push := "push confirmed"
	if err := p.Repo.Push(ctx, git.Push{Remote: d.PushURL, Branch: spec.PushBranch(), Commit: string(spec.Source.Commit), ExpectedRemote: saved.Expected}); err != nil {
		if ctx.Err() != nil || !errors.Is(err, git.ErrRefConflict) {
			return result, err
		}
		push = "remote branch differs from the accepted push precondition; no replacement push was made; reconcile it with Git before requesting verification of new contents"
	}
	result.Detail, err = missingRunDetail(ctx, api, row, saved, push)
	return result, err
}

// rerun asks for another attempt of a finished run and returns it once GitHub
// reports one.
//
// One request asks at most once, and nothing needs to be written down for
// that: the caller admits a run either way in the same pass, so this is not
// reached again for the request, and a run that did accept a rerun is no
// longer completed, which is the condition that brings anything here. A
// refusal is not fatal: a run GitHub will not repeat, because its logs have
// expired or it has no unsuccessful job to repeat, still carries the
// conclusion it reached, and that conclusion is observed.
func (p *Provider) rerun(ctx context.Context, api actionsAPI, saved payload, selected *gh.WorkflowRun) (*gh.WorkflowRun, error) {
	if err := api.Rerun(ctx, selected.GetID()); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		progress.Report(ctx, "GitHub would not run %s again (%s); observing the attempt it already has", selected.GetHTMLURL(), err)
		return nil, nil
	}
	current, err := api.Run(ctx, selected.GetID(), 0)
	if err != nil {
		return nil, err
	}
	if current.GetRunAttempt() <= selected.GetRunAttempt() {
		// The attempt is not visible yet; the next observation finds it, and
		// Reran keeps this from asking twice.
		return nil, nil
	}
	if !matches(saved, current) {
		return nil, fmt.Errorf("github verification: the rerun attempt does not match this contribution")
	}
	progress.Report(ctx, "Asked GitHub to run %s again; observing attempt %d", selected.GetHTMLURL(), current.GetRunAttempt())
	return current, nil
}

func admitted(row record.ProviderExecution) (verify.Submission, error) {
	var run executionRun
	if err := json.Unmarshal(row.Result, &run); err != nil {
		return verify.Submission{}, err
	}
	if run.ID <= 0 || run.Attempt <= 0 {
		return verify.Submission{}, fmt.Errorf("github verification: invalid stored run identity")
	}
	var saved payload
	if err := json.Unmarshal(row.Payload, &saved); err != nil {
		return verify.Submission{}, err
	}
	return verify.Submission{State: verify.Admitted, Run: record.ProviderRun{Provider: verify.ProviderGitHub, RequestID: row.ID, RunID: fmt.Sprintf("%d:%d", run.ID, run.Attempt)}, Detail: runDetail(saved, run, "tracking")}, nil
}

func (p *Provider) Reconcile(ctx context.Context, id record.RequestID, options verify.ReconcileOptions) (verify.Reconciliation, error) {
	result := verify.Reconciliation{State: verify.RunUnknown}
	err := p.locked(ctx, id, func(ctx context.Context, e *ledger.Entry) error {
		row, err := e.Read(ctx)
		if errors.Is(err, state.ErrNotFound) {
			result.State = verify.RequestClosed
			return e.CloseUnknown(ctx, time.Now())
		}
		if err != nil {
			return err
		}
		if row.State == record.ExecutionClosed {
			result.Submission, err = rejectedSubmission(row)
			if err != nil {
				return err
			}
			result.State = verify.RequestClosed
			if len(row.Payload) > 0 {
				result.Submission.Detail = "Stopped GitHub submission tracking; any push already sent may still run in Actions"
			}
			return nil
		}
		if options.CancelRequested && row.State == record.ExecutionReserved {
			// The request lock fences both in-flight and stale Submit calls. A push
			// already sent may still run remotely; closing tracking cannot undo it.
			row.State, row.Occupied = record.ExecutionClosed, false
			if err := e.Put(ctx, row); err != nil {
				return err
			}
			result.State = verify.RequestClosed
			result.Submission.Detail = "Stopped GitHub submission tracking; any push already sent may still run in Actions"
			return nil
		}
		result.Submission, err = p.advance(ctx, e, row)
		if err == nil && result.Submission.State == verify.Admitted {
			result.State = verify.RunFound
		}
		return err
	})
	return result, err
}

func rejectedSubmission(row record.ProviderExecution) (verify.Submission, error) {
	if row.State != record.ExecutionClosed || len(row.Result) == 0 {
		return verify.Submission{}, nil
	}
	var saved rejection
	if err := json.Unmarshal(row.Result, &saved); err != nil {
		return verify.Submission{}, err
	}
	if saved.Detail == "" {
		return verify.Submission{}, fmt.Errorf("github verification: missing rejection reason")
	}
	return verify.Submission{State: verify.Unsupported, Detail: saved.Detail}, nil
}

func runDetail(saved payload, run executionRun, status string) string {
	detail := fmt.Sprintf("GitHub Actions %s: %s:%s at %s; run %d attempt %d", status, saved.Config.Destination.HeadRepository, saved.Request.Spec.PushBranch(), saved.Request.Spec.Source.Commit, run.ID, run.Attempt)
	if run.URL != "" {
		detail += "; " + run.URL
	}
	return detail
}
