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

const branchColumns = "id, name, base, worktree, managed, title, state, pr_repository, pr_number, pr_head, pr_pushed, pr_body, pr_draft, pr_observed, pr_adopted, created_at, origin, note, ended_at, ports"

func (t *tx) scanBranch(row interface{ Scan(...any) error }) (model.Branch, error) {
	var b model.Branch
	var managed int
	var prRepository, prHead sql.NullString
	var prNumber sql.NullInt64
	var prPushed, prBody, prObserved string
	var prDraft, prAdopted int
	var created int64
	var ended sql.NullInt64
	var ports string
	if err := row.Scan(&b.ID, &b.Name, &b.Base, &b.Worktree, &managed, &b.Title, &b.State, &prRepository, &prNumber, &prHead, &prPushed, &prBody, &prDraft, &prObserved, &prAdopted, &created, &b.Origin, &b.Note, &ended, &ports); err != nil {
		return model.Branch{}, storageError(err)
	}
	b.Ports = strings.Fields(ports)
	b.Repository, b.Managed, b.CreatedAt = t.repo, managed == 1, fromMillis(created)
	if ended.Valid {
		b.EndedAt = fromMillis(ended.Int64)
	}
	if prNumber.Valid {
		b.PullRequest = &model.PullRequest{Repository: prRepository.String, Number: int(prNumber.Int64), Head: prHead.String,
			Pushed: model.ObjectID(prPushed), Body: prBody, Draft: prDraft == 1, Adopted: prAdopted == 1}
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

func (t *tx) MergedBranchNamed(name string) (model.Branch, error) {
	return t.scanBranch(t.conn.QueryRowContext(t.ctx, "SELECT "+branchColumns+" FROM branches WHERE repository_id=? AND name=? AND state='merged' ORDER BY created_at DESC, id DESC LIMIT 1", t.repo, name))
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

func pullRequestColumns(pr *model.PullRequest) (any, any, any, string, string, int, string, int) {
	if pr == nil {
		return nil, nil, nil, "", "", 0, "", 0
	}
	observed := ""
	if pr.Observed != nil {
		data, _ := json.Marshal(pr.Observed)
		observed = string(data)
	}
	return pr.Repository, pr.Number, pr.Head, string(pr.Pushed), pr.Body, boolInt(pr.Draft), observed, boolInt(pr.Adopted)
}

func (t *tx) AddBranch(b model.Branch) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := t.checkRepository(b.Repository); err != nil {
		return err
	}
	if b.Origin == "" {
		b.Origin = model.OriginPerson
	}
	repository, number, head, pushed, body, draft, observed, adopted := pullRequestColumns(b.PullRequest)
	var ended any
	if b.State != model.BranchOpen {
		if b.EndedAt.IsZero() {
			return fmt.Errorf("branch %s is %s, so it says when it ended", b.Name, b.State)
		}
		ended = millis(b.EndedAt)
	}
	_, err := t.exec("INSERT INTO branches(repository_id, "+branchColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, b.ID, b.Name, b.Base, b.Worktree, boolInt(b.Managed), b.Title, b.State, repository, number, head, pushed, body, draft, observed, adopted, millis(b.CreatedAt), b.Origin, b.Note, ended, strings.Join(b.Ports, " "))
	return err
}

// endedAt is when a branch going from one state to another ended: none
// once it's open, the time given as it leaves open, which it must give,
// since the store invents none (the architecture review's X5), and else
// when it ended before, unless a time is given.
func endedAt(current, next model.Branch) (any, error) {
	ended := current.EndedAt
	switch {
	case next.State == model.BranchOpen:
		return nil, nil
	case current.State == model.BranchOpen && next.EndedAt.IsZero():
		return nil, fmt.Errorf("branch %s leaves open, so it says when", next.Name)
	case !next.EndedAt.IsZero():
		ended = next.EndedAt
	}
	if ended.IsZero() {
		return nil, nil
	}
	return millis(ended), nil
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
	ended, err := endedAt(current, b)
	if err != nil {
		return err
	}
	repository, number, head, pushed, body, draft, observed, adopted := pullRequestColumns(b.PullRequest)
	return t.update("branch "+string(b.ID), "UPDATE branches SET name=?, base=?, worktree=?, managed=?, title=?, state=?, pr_repository=?, pr_number=?, pr_head=?, pr_pushed=?, pr_body=?, pr_draft=?, pr_observed=?, pr_adopted=?, note=?, ended_at=?, ports=? WHERE repository_id=? AND id=?",
		b.Name, b.Base, b.Worktree, boolInt(b.Managed), b.Title, b.State, repository, number, head, pushed, body, draft, observed, adopted, b.Note, ended, strings.Join(b.Ports, " "), t.repo, b.ID)
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

// revisionsQuery reads a branch's revisions through revision_branch.
const revisionsQuery = "SELECT " + revisionColumns + " FROM revisions WHERE repository_id=? AND branch_id=? ORDER BY created_at, rowid"

func (t *tx) Revisions(branch model.BranchID) ([]model.Revision, error) {
	rows, err := t.conn.QueryContext(t.ctx, revisionsQuery, t.repo, branch)
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
	p, err := decodePlan([]byte(body))
	if err != nil {
		return model.Plan{}, fmt.Errorf("%w: plan %s: %w", store.ErrUnavailable, id, err)
	}
	return p, nil
}

// decodePlan reads a plan's body. One recorded before each environment
// had a plan of its own (2026-09-27, before v0.3.0) has no Builds, and
// isn't read.
func decodePlan(body []byte) (model.Plan, error) {
	var p model.Plan
	if err := json.Unmarshal(body, &p); err != nil {
		return model.Plan{}, err
	}
	if p.Builds == nil {
		return model.Plan{}, errors.New("recorded before each environment had a plan of its own, which this dockhand doesn't read")
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

const runColumns = "id, number, branch_id, revision_id, plan_id, origin, state, detail, created_at, finished_at, cancel_requested_at, baseline_of"

func scanRun(row interface{ Scan(...any) error }) (model.Run, error) {
	var r model.Run
	var created int64
	var finished, cancel sql.NullInt64
	if err := row.Scan(&r.ID, &r.Number, &r.Branch, &r.Revision, &r.Plan, &r.Origin, &r.State, &r.Detail, &created, &finished, &cancel, &r.BaselineOf); err != nil {
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

// runsQuery is the query that selects a repository's runs by a filter.
func runsQuery(repository model.RepositoryID, filter store.RunFilter) (string, []any) {
	query, args := "SELECT "+runColumns+" FROM runs WHERE repository_id=?", []any{repository}
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
	return query, args
}

func (t *tx) Runs(filter store.RunFilter) ([]model.Run, error) {
	query, args := runsQuery(t.repo, filter)
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
	_, err = t.exec("INSERT INTO runs(repository_id, "+runColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, r.ID, r.Number, r.Branch, r.Revision, r.Plan, r.Origin, r.State, r.Detail, millis(r.CreatedAt), nullableMillis(r.FinishedAt), nullableMillis(r.CancelRequested), r.BaselineOf)
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
	if current.Number != r.Number || current.Branch != r.Branch || current.Revision != r.Revision || current.Plan != r.Plan || current.Origin != r.Origin || current.BaselineOf != r.BaselineOf {
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

const executionColumns = "id, run_id, provider, platform_os, platform_version, platform_architecture, developer_tools, attempt, state, detail, provider_ref, observed, identity, reused, created_at, finished_at"

// executionFields are where a row's executionColumns are scanned into e,
// and what completes e once they are.
func executionFields(e *model.GuestExecution) ([]any, func() error) {
	var created int64
	var finished sql.NullInt64
	var observed string
	p := &e.Environment.Platform
	fields := []any{&e.ID, &e.Run, &e.Environment.Provider, &p.OS, &p.Version, &p.Architecture, &e.Environment.DeveloperTools, &e.Attempt, &e.State, &e.Detail, &e.ProviderRef, &observed, &e.Identity, &e.Reused, &created, &finished}
	return fields, func() error {
		if observed != "" {
			if err := json.Unmarshal([]byte(observed), &e.Observed); err != nil {
				return fmt.Errorf("%w: execution %s's observed environment: %w", store.ErrUnavailable, e.ID, err)
			}
		}
		e.CreatedAt, e.FinishedAt = fromMillis(created), fromNullable(finished)
		return nil
	}
}

func scanExecution(row interface{ Scan(...any) error }) (model.GuestExecution, error) {
	var e model.GuestExecution
	fields, complete := executionFields(&e)
	if err := row.Scan(fields...); err != nil {
		return model.GuestExecution{}, storageError(err)
	}
	if err := complete(); err != nil {
		return model.GuestExecution{}, err
	}
	return e, nil
}

func (t *tx) execution(id model.ExecutionID) (model.GuestExecution, error) {
	return scanExecution(t.conn.QueryRowContext(t.ctx, "SELECT "+executionColumns+" FROM executions WHERE repository_id=? AND id=?", t.repo, id))
}

func (t *tx) Execution(id model.ExecutionID) (model.GuestExecution, error) { return t.execution(id) }

// referredQuery reads the executions a provider knows by a reference,
// through execution_ref.
const referredQuery = "SELECT " + executionColumns + " FROM executions WHERE repository_id=? AND provider_ref=? ORDER BY created_at, rowid"

func (t *tx) ExecutionsReferred(ref string) ([]model.GuestExecution, error) {
	if ref == "" {
		return nil, nil
	}
	return t.executions(referredQuery, t.repo, ref)
}

func (t *tx) Executions(run model.RunID) ([]model.GuestExecution, error) {
	return t.executions("SELECT "+executionColumns+" FROM executions WHERE repository_id=? AND run_id=? ORDER BY created_at, rowid", t.repo, run)
}

func (t *tx) executions(query string, args ...any) ([]model.GuestExecution, error) {
	rows, err := t.conn.QueryContext(t.ctx, query, args...)
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
	observed, err := observedJSON(e.Observed)
	if err != nil {
		return err
	}
	_, err = t.exec("INSERT INTO executions(repository_id, "+executionColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, e.ID, e.Run, e.Environment.Provider, p.OS, p.Version, p.Architecture, e.Environment.DeveloperTools, e.Attempt, e.State, e.Detail, e.ProviderRef, observed, e.Identity, boolInt(e.Reused), millis(e.CreatedAt), nullableMillis(e.FinishedAt))
	return err
}

// observedJSON is an observed environment as stored: empty when nothing
// was reported.
func observedJSON(observed model.Observed) (string, error) {
	if observed.IsZero() {
		return "", nil
	}
	data, err := json.Marshal(observed)
	return string(data), err
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
	// What the environment reported stays, whatever a later update of the
	// execution carries.
	observed, err := observedJSON(e.Observed)
	if err != nil {
		return err
	}
	return t.update("execution "+string(e.ID), "UPDATE executions SET state=?, detail=?, provider_ref=?, observed=CASE WHEN ?='' THEN observed ELSE ? END, finished_at=? WHERE repository_id=? AND id=?",
		e.State, e.Detail, e.ProviderRef, observed, observed, nullableMillis(e.FinishedAt), t.repo, e.ID)
}

const resultColumns = "r.execution_id, r.target_id, r.outcome, r.phase, r.tests, r.log, r.steps, r.detail, r.builders, r.inputs, r.archive, r.reused_from, r.recorded_at"

func (t *tx) Check(id model.RunID) (store.Check, error) {
	check := store.Check{Results: map[model.ExecutionID][]model.TargetResult{}, Origins: map[model.ExecutionID]store.Origin{}, Inputs: map[string]model.TargetInputs{}}
	var err error
	if check.Run, err = t.Run(id); err != nil {
		return store.Check{}, err
	}
	if check.Plan, err = t.Plan(check.Run.Plan); err != nil {
		return store.Check{}, err
	}
	if check.Executions, err = t.Executions(id); err != nil {
		return store.Check{}, err
	}
	// Every execution's results in one query, where each was read alone.
	results, err := t.results("SELECT "+resultColumns+" FROM results r JOIN executions e ON e.repository_id=r.repository_id AND e.id=r.execution_id WHERE r.repository_id=? AND e.run_id=? ORDER BY r.recorded_at, r.rowid", t.repo, id)
	if err != nil {
		return store.Check{}, err
	}
	for _, result := range results {
		check.Results[result.Execution] = append(check.Results[result.Execution], result)
		if result.ReusedFrom != "" {
			if _, ok := check.Origins[result.ReusedFrom]; !ok {
				var origin store.Origin
				if origin.Execution, err = t.execution(result.ReusedFrom); err != nil {
					return store.Check{}, err
				}
				if origin.Run, err = t.Run(origin.Execution.Run); err != nil {
					return store.Check{}, err
				}
				check.Origins[result.ReusedFrom] = origin
			}
		}
		if result.Inputs != "" {
			if _, ok := check.Inputs[result.Inputs]; !ok {
				if check.Inputs[result.Inputs], err = t.Inputs(result.Inputs); err != nil {
					return store.Check{}, err
				}
			}
		}
	}
	return check, nil
}

func (t *tx) Results(execution model.ExecutionID) ([]model.TargetResult, error) {
	return t.results("SELECT "+resultColumns+" FROM results r WHERE r.repository_id=? AND r.execution_id=? ORDER BY r.recorded_at, r.rowid", t.repo, execution)
}

// reusableQuery reads a target's passed results in an environment, each
// with the execution that built it, newest first, walking result_target
// back from the newest.
var reusableQuery = "SELECT " + resultColumns + ", " + qualified("e", executionColumns) + " FROM results r JOIN executions e ON e.repository_id=r.repository_id AND e.id=r.execution_id " +
	"WHERE r.repository_id=? AND r.target_id=? AND r.outcome='passed' AND r.inputs<>'' AND r.reused_from='' " +
	"AND e.provider=? AND e.platform_os=? AND e.platform_version=? AND e.platform_architecture=? AND e.developer_tools=? " +
	"ORDER BY r.recorded_at DESC, r.rowid DESC LIMIT ?"

func (t *tx) Reusable(target model.TargetID, environment model.Environment, limit int) ([]store.Build, error) {
	p := environment.Platform
	rows, err := t.conn.QueryContext(t.ctx, reusableQuery, t.repo, target, environment.Provider, p.OS, p.Version, p.Architecture, environment.DeveloperTools, limit)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var builds []store.Build
	for rows.Next() {
		var build store.Build
		result, completeResult := resultFields(&build.Result)
		execution, completeExecution := executionFields(&build.Execution)
		if err := rows.Scan(append(result, execution...)...); err != nil {
			return nil, storageError(err)
		}
		if err := errors.Join(completeResult(), completeExecution()); err != nil {
			return nil, err
		}
		builds = append(builds, build)
	}
	return builds, storageError(rows.Err())
}

// resultFields are where a row's resultColumns are scanned into r, and
// what completes r once they are.
func resultFields(r *model.TargetResult) ([]any, func() error) {
	var recorded int64
	var steps, builders string
	fields := []any{&r.Execution, &r.Target, &r.Outcome, &r.Phase, &r.Tests, &r.Log, &steps, &r.Detail, &builders, &r.Inputs, &r.Archive, &r.ReusedFrom, &recorded}
	return fields, func() error {
		if steps != "" {
			if err := json.Unmarshal([]byte(steps), &r.Steps); err != nil {
				return fmt.Errorf("%w: %s's log steps in execution %s: %w", store.ErrUnavailable, r.Target, r.Execution, err)
			}
		}
		if builders != "" {
			if err := json.Unmarshal([]byte(builders), &r.Builders); err != nil {
				return fmt.Errorf("%w: %s's builders in execution %s: %w", store.ErrUnavailable, r.Target, r.Execution, err)
			}
		}
		r.RecordedAt = fromMillis(recorded)
		return nil
	}
}

// jsonColumn is one of a result's lists as its column keeps it: JSON, or
// empty where the list is.
func jsonColumn[T any](list []T) (string, error) {
	if len(list) == 0 {
		return "", nil
	}
	data, err := json.Marshal(list)
	return string(data), err
}

func (t *tx) results(query string, args ...any) ([]model.TargetResult, error) {
	rows, err := t.conn.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var results []model.TargetResult
	for rows.Next() {
		var r model.TargetResult
		fields, complete := resultFields(&r)
		if err := rows.Scan(fields...); err != nil {
			return nil, storageError(err)
		}
		if err := complete(); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, storageError(rows.Err())
}

// qualified is columns, a comma-separated list, each named by a table's
// alias.
func qualified(alias, columns string) string {
	return alias + "." + strings.ReplaceAll(columns, ", ", ", "+alias+".")
}

func (t *tx) RecordResult(r model.TargetResult) error {
	if err := r.Validate(); err != nil {
		return err
	}
	var run model.RunID
	var planID model.PlanID
	if err := t.conn.QueryRowContext(t.ctx, "SELECT u.id, u.plan_id FROM executions e JOIN runs u ON u.repository_id=e.repository_id AND u.id=e.run_id WHERE e.repository_id=? AND e.id=?",
		t.repo, r.Execution).Scan(&run, &planID); err != nil {
		return fmt.Errorf("%s's result in execution %s: %w", r.Target, r.Execution, storageError(err))
	}
	plan, err := t.Plan(planID)
	if err != nil {
		return err
	}
	if _, ok := plan.Target(r.Target); !ok {
		return fmt.Errorf("%w: target %s is not in run %s's plan", model.ErrInvalid, r.Target, run)
	}
	if r.Inputs != "" {
		if err := t.exists("SELECT 1 FROM inputs WHERE repository_id=? AND key=?", r.Inputs); err != nil {
			return fmt.Errorf("%s's result names inputs %s: %w", r.Target, r.Inputs, err)
		}
	}
	if r.ReusedFrom != "" {
		if err := t.exists("SELECT 1 FROM executions WHERE repository_id=? AND id=?", r.ReusedFrom); err != nil {
			return fmt.Errorf("%s's result is reused from execution %s: %w", r.Target, r.ReusedFrom, err)
		}
	}
	builders, err := jsonColumn(r.Builders)
	if err != nil {
		return err
	}
	steps, err := jsonColumn(r.Steps)
	if err != nil {
		return err
	}
	var existing model.TargetResult
	err = t.conn.QueryRowContext(t.ctx, "SELECT outcome FROM results WHERE repository_id=? AND execution_id=? AND target_id=?", t.repo, r.Execution, r.Target).Scan(&existing.Outcome)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = t.exec("INSERT INTO results(repository_id, execution_id, target_id, outcome, phase, tests, log, steps, detail, builders, inputs, archive, reused_from, recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			t.repo, r.Execution, r.Target, r.Outcome, r.Phase, r.Tests, r.Log, steps, r.Detail, builders, r.Inputs, r.Archive, r.ReusedFrom, millis(r.RecordedAt))
		return err
	case err != nil:
		return storageError(err)
	}
	existing.Execution, existing.Target = r.Execution, r.Target
	if !existing.ReplacedBy(r) {
		return fmt.Errorf("%w: %s's result in execution %s is %s and cannot become %s", store.ErrConflict, r.Target, r.Execution, existing.Outcome, r.Outcome)
	}
	_, err = t.exec("UPDATE results SET outcome=?, phase=?, tests=?, log=?, steps=?, detail=?, builders=?, inputs=?, archive=?, reused_from=?, recorded_at=? WHERE repository_id=? AND execution_id=? AND target_id=?",
		r.Outcome, r.Phase, r.Tests, r.Log, steps, r.Detail, builders, r.Inputs, r.Archive, r.ReusedFrom, millis(r.RecordedAt), t.repo, r.Execution, r.Target)
	return err
}

func (t *tx) Inputs(key string) (model.TargetInputs, error) {
	var record string
	if err := t.conn.QueryRowContext(t.ctx, "SELECT record FROM inputs WHERE repository_id=? AND key=?", t.repo, key).Scan(&record); err != nil {
		return model.TargetInputs{}, storageError(err)
	}
	var inputs model.TargetInputs
	if err := json.Unmarshal([]byte(record), &inputs); err != nil {
		return model.TargetInputs{}, fmt.Errorf("%w: inputs %s: %w", store.ErrUnavailable, key, err)
	}
	return inputs, nil
}

func (t *tx) RecordInputs(inputs model.TargetInputs) (string, error) {
	record, err := json.Marshal(inputs)
	if err != nil {
		return "", err
	}
	key := inputs.Key()
	_, err = t.exec("INSERT INTO inputs(repository_id, key, record) VALUES(?,?,?) ON CONFLICT DO NOTHING", t.repo, key, string(record))
	return key, err
}

func (t *tx) Archive(digest string) (model.Archive, error) {
	archive := model.Archive{Digest: digest}
	var kept int64
	if err := t.conn.QueryRowContext(t.ctx, "SELECT name, size, kept_at FROM archives WHERE repository_id=? AND digest=?", t.repo, digest).Scan(&archive.Name, &archive.Size, &kept); err != nil {
		return model.Archive{}, storageError(err)
	}
	archive.KeptAt = fromMillis(kept)
	return archive, nil
}

func (t *tx) Archives() ([]model.Archive, error) {
	return t.archives("SELECT digest, name, size, kept_at FROM archives WHERE repository_id=? ORDER BY digest", t.repo)
}

func (t *tx) KeepArchive(archive model.Archive) error {
	if !strings.HasPrefix(archive.Digest, "sha256:") || !model.ValidArchiveName(archive.Name) || archive.Size <= 0 {
		return fmt.Errorf("%w: archive %q named %q", store.ErrConflict, archive.Digest, archive.Name)
	}
	_, err := t.exec("INSERT INTO archives(repository_id, digest, name, size, kept_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING",
		t.repo, archive.Digest, archive.Name, archive.Size, millis(archive.KeptAt))
	return err
}

func (t *tx) KeepDependencyArchive(archive model.DependencyArchive) error {
	if archive.Port == "" {
		return fmt.Errorf("%w: archive %q kept for no port", store.ErrConflict, archive.Digest)
	}
	p := archive.Environment.Platform
	_, err := t.exec("INSERT INTO dependency_archives(repository_id, digest, port, provider, platform_os, platform_version, platform_architecture, developer_tools, kept_at) "+
		"VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT DO UPDATE SET kept_at=excluded.kept_at",
		t.repo, archive.Digest, archive.Port, archive.Environment.Provider, p.OS, p.Version, p.Architecture, archive.Environment.DeveloperTools, millis(archive.KeptAt))
	return err
}

// ForgetDependencyArchive drops the record that a guest in an environment
// installed a port from a kept archive; the archive's own record stays, for
// a result that may name it.
func (t *tx) ForgetDependencyArchive(archive model.DependencyArchive) error {
	p := archive.Environment.Platform
	_, err := t.exec("DELETE FROM dependency_archives WHERE repository_id=? AND digest=? AND port=? AND provider=? AND platform_os=? AND platform_version=? AND platform_architecture=? AND developer_tools=?",
		t.repo, archive.Digest, archive.Port, archive.Environment.Provider, p.OS, p.Version, p.Architecture, archive.Environment.DeveloperTools)
	return err
}

// dependencyArchivesPerPort is how many of a port's kept dependency
// archives DependencyArchives gives, newest first: the one a guest's
// MacPorts wants is most likely the newest, and the one before it covers
// a branch whose base is behind.
const dependencyArchivesPerPort = 2

func (t *tx) DependencyArchives(environment model.Environment, ports []string) ([]model.DependencyArchive, error) {
	if len(ports) == 0 {
		return nil, nil
	}
	p := environment.Platform
	args := []any{t.repo, environment.Provider, p.OS, p.Version, p.Architecture, environment.DeveloperTools}
	for _, port := range ports {
		args = append(args, port)
	}
	args = append(args, dependencyArchivesPerPort)
	rows, err := t.conn.QueryContext(t.ctx, "SELECT digest, name, size, archive_kept, port, kept_at FROM (SELECT d.digest, a.name, a.size, a.kept_at AS archive_kept, d.port, d.kept_at, "+
		"ROW_NUMBER() OVER (PARTITION BY d.port ORDER BY d.kept_at DESC, d.digest) AS newest FROM dependency_archives d JOIN archives a ON a.repository_id=d.repository_id AND a.digest=d.digest "+
		"WHERE d.repository_id=? AND d.provider=? AND d.platform_os=? AND d.platform_version=? AND d.platform_architecture=? AND d.developer_tools=? AND d.port IN ("+placeholders(len(ports))+")) "+
		"WHERE newest<=? ORDER BY port, kept_at DESC, digest", args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var found []model.DependencyArchive
	for rows.Next() {
		archive := model.DependencyArchive{Environment: environment}
		var archiveKept, kept int64
		if err := rows.Scan(&archive.Digest, &archive.Name, &archive.Size, &archiveKept, &archive.Port, &kept); err != nil {
			return nil, storageError(err)
		}
		archive.KeptAt = fromMillis(archiveKept)
		found = append(found, archive)
	}
	return found, storageError(rows.Err())
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
