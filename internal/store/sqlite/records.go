package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Times are stored in milliseconds, so a record read back carries its times
// truncated to the millisecond.

const branchColumns = "id, name, base, worktree, managed, title, state, pr_repository, pr_number, pr_head, pr_pushed, pr_body, pr_draft, pr_observed, created_at"

func (t *tx) scanBranch(row interface{ Scan(...any) error }) (model.Branch, error) {
	var b model.Branch
	var managed int
	var prRepository, prHead sql.NullString
	var prNumber sql.NullInt64
	var prPushed, prBody, prObserved string
	var prDraft int
	var created int64
	if err := row.Scan(&b.ID, &b.Name, &b.Base, &b.Worktree, &managed, &b.Title, &b.State, &prRepository, &prNumber, &prHead, &prPushed, &prBody, &prDraft, &prObserved, &created); err != nil {
		return model.Branch{}, storageError(err)
	}
	b.Repository, b.Managed, b.CreatedAt = t.repo, managed == 1, fromMillis(created)
	if prNumber.Valid {
		b.PullRequest = &model.PullRequest{Repository: prRepository.String, Number: int(prNumber.Int64), Head: prHead.String,
			Pushed: model.ObjectID(prPushed), Body: prBody, Draft: prDraft == 1}
		if prObserved != "" {
			b.PullRequest.Observed = &model.PullRequestObservation{}
			if err := json.Unmarshal([]byte(prObserved), b.PullRequest.Observed); err != nil {
				return model.Branch{}, fmt.Errorf("%w: branch %s's pull request observation: %w", store.ErrUnavailable, b.ID, err)
			}
		}
	}
	return b, nil
}

func (t *tx) Branch(id model.BranchID) (model.Branch, error) {
	return t.scanBranch(t.conn.QueryRowContext(t.ctx, "SELECT "+branchColumns+" FROM branches WHERE repository_id=? AND id=?", t.repo, id))
}

func (t *tx) BranchNamed(name string) (model.Branch, error) {
	return t.scanBranch(t.conn.QueryRowContext(t.ctx, "SELECT "+branchColumns+" FROM branches WHERE repository_id=? AND name=? AND state<>'merged'", t.repo, name))
}

func (t *tx) Branches(filter store.BranchFilter) ([]model.Branch, error) {
	query, args := "SELECT "+branchColumns+" FROM branches WHERE repository_id=?", []any{t.repo}
	if len(filter.States) > 0 {
		query += " AND state IN (" + placeholders(len(filter.States)) + ")"
		for _, s := range filter.States {
			args = append(args, s)
		}
	}
	rows, err := t.conn.QueryContext(t.ctx, query+" ORDER BY created_at, id", args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var branches []model.Branch
	for rows.Next() {
		b, err := t.scanBranch(rows)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	return branches, storageError(rows.Err())
}

func (t *tx) checkRepository(repository model.RepositoryID) error {
	if repository != t.repo {
		return fmt.Errorf("%w: record belongs to repository %s, not %s", model.ErrInvalid, repository, t.repo)
	}
	return nil
}

func pullRequestColumns(pr *model.PullRequest) (any, any, any, string, string, int, string) {
	if pr == nil {
		return nil, nil, nil, "", "", 0, ""
	}
	observed := ""
	if pr.Observed != nil {
		data, _ := json.Marshal(pr.Observed)
		observed = string(data)
	}
	return pr.Repository, pr.Number, pr.Head, string(pr.Pushed), pr.Body, boolInt(pr.Draft), observed
}

func (t *tx) AddBranch(b model.Branch) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := t.checkRepository(b.Repository); err != nil {
		return err
	}
	repository, number, head, pushed, body, draft, observed := pullRequestColumns(b.PullRequest)
	_, err := t.exec("INSERT INTO branches(repository_id, "+branchColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, b.ID, b.Name, b.Base, b.Worktree, boolInt(b.Managed), b.Title, b.State, repository, number, head, pushed, body, draft, observed, millis(b.CreatedAt))
	return err
}

func (t *tx) UpdateBranch(b model.Branch) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := t.checkRepository(b.Repository); err != nil {
		return err
	}
	current, err := t.Branch(b.ID)
	if err != nil {
		return err
	}
	if current.State != b.State && !current.State.CanBecome(b.State) {
		return fmt.Errorf("%w: branch %s cannot go from %s to %s", store.ErrConflict, b.ID, current.State, b.State)
	}
	if !current.CreatedAt.Equal(b.CreatedAt) {
		return fmt.Errorf("%w: branch %s's creation time is fixed", store.ErrConflict, b.ID)
	}
	repository, number, head, pushed, body, draft, observed := pullRequestColumns(b.PullRequest)
	return t.update("branch "+string(b.ID), "UPDATE branches SET name=?, base=?, worktree=?, managed=?, title=?, state=?, pr_repository=?, pr_number=?, pr_head=?, pr_pushed=?, pr_body=?, pr_draft=?, pr_observed=? WHERE repository_id=? AND id=?",
		b.Name, b.Base, b.Worktree, boolInt(b.Managed), b.Title, b.State, repository, number, head, pushed, body, draft, observed, t.repo, b.ID)
}

const revisionColumns = "id, branch_id, kind, snapshot, commit_id, tree_id, base_id, head_id, created_at"

func scanRevision(row interface{ Scan(...any) error }) (model.Revision, error) {
	var r model.Revision
	var created int64
	if err := row.Scan(&r.ID, &r.Branch, &r.Kind, &r.Snapshot, &r.Source.Commit, &r.Source.Tree, &r.Source.Base, &r.Head, &created); err != nil {
		return model.Revision{}, storageError(err)
	}
	r.CreatedAt = fromMillis(created)
	return r, nil
}

func (t *tx) Revision(id model.RevisionID) (model.Revision, error) {
	return scanRevision(t.conn.QueryRowContext(t.ctx, "SELECT "+revisionColumns+" FROM revisions WHERE repository_id=? AND id=?", t.repo, id))
}

func (t *tx) Revisions(branch model.BranchID) ([]model.Revision, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT "+revisionColumns+" FROM revisions WHERE repository_id=? AND branch_id=? ORDER BY created_at, rowid", t.repo, branch)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var revisions []model.Revision
	for rows.Next() {
		r, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, r)
	}
	return revisions, storageError(rows.Err())
}

func (t *tx) NextSnapshot(branch model.BranchID) (int, error) {
	var last int
	err := t.conn.QueryRowContext(t.ctx, "SELECT coalesce(max(snapshot), 0) FROM revisions WHERE repository_id=? AND branch_id=? AND kind='snapshot'", t.repo, branch).Scan(&last)
	return last + 1, storageError(err)
}

func (t *tx) AddRevision(r model.Revision) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Kind == model.RevisionSnapshot {
		next, err := t.NextSnapshot(r.Branch)
		if err != nil {
			return err
		}
		if r.Snapshot != next {
			return fmt.Errorf("%w: branch %s's next snapshot is %d, not %d", store.ErrConflict, r.Branch, next, r.Snapshot)
		}
	}
	_, err := t.exec("INSERT INTO revisions(repository_id, "+revisionColumns+") VALUES(?,?,?,?,?,?,?,?,?,?)",
		t.repo, r.ID, r.Branch, r.Kind, r.Snapshot, r.Source.Commit, r.Source.Tree, r.Source.Base, r.Head, millis(r.CreatedAt))
	return err
}

func (t *tx) Plan(id model.PlanID) (model.Plan, error) {
	var body string
	if err := t.conn.QueryRowContext(t.ctx, "SELECT body FROM plans WHERE repository_id=? AND id=?", t.repo, id).Scan(&body); err != nil {
		return model.Plan{}, storageError(err)
	}
	var p model.Plan
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return model.Plan{}, fmt.Errorf("%w: plan %s: %w", store.ErrUnavailable, id, err)
	}
	return p, nil
}

func (t *tx) AddPlan(p model.Plan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	p.CreatedAt = fromMillis(millis(p.CreatedAt))
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = t.exec("INSERT INTO plans(repository_id, id, revision_id, body, created_at) VALUES(?,?,?,?,?)", t.repo, p.ID, p.Revision, string(body), millis(p.CreatedAt))
	return err
}

const runColumns = "id, number, branch_id, revision_id, plan_id, origin, state, detail, created_at, finished_at, cancel_requested_at"

func scanRun(row interface{ Scan(...any) error }) (model.Run, error) {
	var r model.Run
	var created int64
	var finished, cancel sql.NullInt64
	if err := row.Scan(&r.ID, &r.Number, &r.Branch, &r.Revision, &r.Plan, &r.Origin, &r.State, &r.Detail, &created, &finished, &cancel); err != nil {
		return model.Run{}, storageError(err)
	}
	r.CreatedAt, r.FinishedAt, r.CancelRequested = fromMillis(created), fromNullable(finished), fromNullable(cancel)
	return r, nil
}

func (t *tx) Run(id model.RunID) (model.Run, error) {
	return scanRun(t.conn.QueryRowContext(t.ctx, "SELECT "+runColumns+" FROM runs WHERE repository_id=? AND id=?", t.repo, id))
}

func (t *tx) RunNumbered(number int) (model.Run, error) {
	return scanRun(t.conn.QueryRowContext(t.ctx, "SELECT "+runColumns+" FROM runs WHERE repository_id=? AND number=?", t.repo, number))
}

func (t *tx) Runs(filter store.RunFilter) ([]model.Run, error) {
	query, args := "SELECT "+runColumns+" FROM runs WHERE repository_id=?", []any{t.repo}
	if filter.Branch != "" {
		query += " AND branch_id=?"
		args = append(args, filter.Branch)
	}
	if len(filter.States) > 0 {
		query += " AND state IN (" + placeholders(len(filter.States)) + ")"
		for _, s := range filter.States {
			args = append(args, s)
		}
	}
	query += " ORDER BY number DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	rows, err := t.conn.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var runs []model.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, storageError(rows.Err())
}

func (t *tx) NextRunNumber() (int, error) {
	var last int
	err := t.conn.QueryRowContext(t.ctx, "SELECT coalesce(max(number), 0) FROM runs WHERE repository_id=?", t.repo).Scan(&last)
	return last + 1, storageError(err)
}

func (t *tx) AddRun(r model.Run) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.State != model.RunQueued {
		return fmt.Errorf("%w: run %s must start queued", model.ErrInvalid, r.ID)
	}
	next, err := t.NextRunNumber()
	if err != nil {
		return err
	}
	if r.Number != next {
		return fmt.Errorf("%w: the next run number is %d, not %d", store.ErrConflict, next, r.Number)
	}
	plan, err := t.Plan(r.Plan)
	if err != nil {
		return err
	}
	revision, err := t.Revision(r.Revision)
	if err != nil {
		return err
	}
	switch {
	case plan.Revision != r.Revision:
		return fmt.Errorf("%w: run %s's plan is for revision %s", model.ErrInvalid, r.ID, plan.Revision)
	case revision.Branch != r.Branch:
		return fmt.Errorf("%w: run %s's revision belongs to branch %s", model.ErrInvalid, r.ID, revision.Branch)
	case !plan.Runnable():
		return fmt.Errorf("%w: run %s's plan has unresolved targets or none at all", model.ErrInvalid, r.ID)
	}
	_, err = t.exec("INSERT INTO runs(repository_id, "+runColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, r.ID, r.Number, r.Branch, r.Revision, r.Plan, r.Origin, r.State, r.Detail, millis(r.CreatedAt), nullableMillis(r.FinishedAt), nullableMillis(r.CancelRequested))
	return err
}

func (t *tx) UpdateRun(r model.Run) error {
	if err := r.Validate(); err != nil {
		return err
	}
	current, err := t.Run(r.ID)
	if err != nil {
		return err
	}
	if current.Number != r.Number || current.Branch != r.Branch || current.Revision != r.Revision || current.Plan != r.Plan || current.Origin != r.Origin {
		return fmt.Errorf("%w: run %s's request is immutable", store.ErrConflict, r.ID)
	}
	if current.State != r.State && !current.State.CanBecome(r.State) {
		return fmt.Errorf("%w: run %s cannot go from %s to %s", store.ErrConflict, r.ID, current.State, r.State)
	}
	if current.CancelRequested != nil && (r.CancelRequested == nil || !r.CancelRequested.Equal(*current.CancelRequested)) {
		return fmt.Errorf("%w: run %s's cancel request stands once made", store.ErrConflict, r.ID)
	}
	return t.update("run "+string(r.ID), "UPDATE runs SET state=?, detail=?, finished_at=?, cancel_requested_at=? WHERE repository_id=? AND id=?",
		r.State, r.Detail, nullableMillis(r.FinishedAt), nullableMillis(r.CancelRequested), t.repo, r.ID)
}

const executionColumns = "id, run_id, provider, platform_os, platform_version, platform_architecture, attempt, state, detail, provider_ref, created_at, finished_at"

func scanExecution(row interface{ Scan(...any) error }) (model.GuestExecution, error) {
	var e model.GuestExecution
	var created int64
	var finished sql.NullInt64
	p := &e.Environment.Platform
	if err := row.Scan(&e.ID, &e.Run, &e.Environment.Provider, &p.OS, &p.Version, &p.Architecture, &e.Attempt, &e.State, &e.Detail, &e.ProviderRef, &created, &finished); err != nil {
		return model.GuestExecution{}, storageError(err)
	}
	e.CreatedAt, e.FinishedAt = fromMillis(created), fromNullable(finished)
	return e, nil
}

func (t *tx) execution(id model.ExecutionID) (model.GuestExecution, error) {
	return scanExecution(t.conn.QueryRowContext(t.ctx, "SELECT "+executionColumns+" FROM executions WHERE repository_id=? AND id=?", t.repo, id))
}

func (t *tx) Executions(run model.RunID) ([]model.GuestExecution, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT "+executionColumns+" FROM executions WHERE repository_id=? AND run_id=? ORDER BY created_at, rowid", t.repo, run)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var executions []model.GuestExecution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, e)
	}
	return executions, storageError(rows.Err())
}

func (t *tx) AddExecution(e model.GuestExecution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	run, err := t.Run(e.Run)
	if err != nil {
		return err
	}
	if run.State.Terminal() {
		return fmt.Errorf("%w: run %s is %s", store.ErrConflict, run.ID, run.State)
	}
	plan, err := t.Plan(run.Plan)
	if err != nil {
		return err
	}
	planned := false
	for _, environment := range plan.Environments {
		planned = planned || environment == e.Environment
	}
	if !planned {
		return fmt.Errorf("%w: execution %s's environment is not in run %s's plan", model.ErrInvalid, e.ID, run.ID)
	}
	p := e.Environment.Platform
	_, err = t.exec("INSERT INTO executions(repository_id, "+executionColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, e.ID, e.Run, e.Environment.Provider, p.OS, p.Version, p.Architecture, e.Attempt, e.State, e.Detail, e.ProviderRef, millis(e.CreatedAt), nullableMillis(e.FinishedAt))
	return err
}

func (t *tx) UpdateExecution(e model.GuestExecution) error {
	if err := e.Validate(); err != nil {
		return err
	}
	current, err := t.execution(e.ID)
	if err != nil {
		return err
	}
	if current.Run != e.Run || current.Environment != e.Environment || current.Attempt != e.Attempt {
		return fmt.Errorf("%w: execution %s's run, environment, and attempt are fixed", store.ErrConflict, e.ID)
	}
	if current.State != e.State && !current.State.CanBecome(e.State) {
		return fmt.Errorf("%w: execution %s cannot go from %s to %s", store.ErrConflict, e.ID, current.State, e.State)
	}
	return t.update("execution "+string(e.ID), "UPDATE executions SET state=?, detail=?, provider_ref=?, finished_at=? WHERE repository_id=? AND id=?",
		e.State, e.Detail, e.ProviderRef, nullableMillis(e.FinishedAt), t.repo, e.ID)
}

func (t *tx) Results(execution model.ExecutionID) ([]model.TargetResult, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at FROM results WHERE repository_id=? AND execution_id=? ORDER BY recorded_at, rowid", t.repo, execution)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var results []model.TargetResult
	for rows.Next() {
		var r model.TargetResult
		var recorded int64
		if err := rows.Scan(&r.Execution, &r.Target, &r.Outcome, &r.Phase, &r.Tests, &r.Log, &r.Inputs, &recorded); err != nil {
			return nil, storageError(err)
		}
		r.RecordedAt = fromMillis(recorded)
		results = append(results, r)
	}
	return results, storageError(rows.Err())
}

func (t *tx) RecordResult(r model.TargetResult) error {
	if err := r.Validate(); err != nil {
		return err
	}
	e, err := t.execution(r.Execution)
	if err != nil {
		return err
	}
	run, err := t.Run(e.Run)
	if err != nil {
		return err
	}
	plan, err := t.Plan(run.Plan)
	if err != nil {
		return err
	}
	if _, ok := plan.Target(r.Target); !ok {
		return fmt.Errorf("%w: target %s is not in run %s's plan", model.ErrInvalid, r.Target, run.ID)
	}
	var existing model.TargetResult
	err = t.conn.QueryRowContext(t.ctx, "SELECT outcome FROM results WHERE repository_id=? AND execution_id=? AND target_id=?", t.repo, r.Execution, r.Target).Scan(&existing.Outcome)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = t.exec("INSERT INTO results(repository_id, execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at) VALUES(?,?,?,?,?,?,?,?,?)",
			t.repo, r.Execution, r.Target, r.Outcome, r.Phase, r.Tests, r.Log, r.Inputs, millis(r.RecordedAt))
		return err
	case err != nil:
		return storageError(err)
	}
	existing.Execution, existing.Target = r.Execution, r.Target
	if !existing.ReplacedBy(r) {
		return fmt.Errorf("%w: %s's result in execution %s is %s and cannot become %s", store.ErrConflict, r.Target, r.Execution, existing.Outcome, r.Outcome)
	}
	_, err = t.exec("UPDATE results SET outcome=?, phase=?, tests=?, log=?, inputs=?, recorded_at=? WHERE repository_id=? AND execution_id=? AND target_id=?",
		r.Outcome, r.Phase, r.Tests, r.Log, r.Inputs, millis(r.RecordedAt), t.repo, r.Execution, r.Target)
	return err
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
