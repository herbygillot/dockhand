package sqlite

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

func (t *transaction) ids(ctx context.Context, query string, args ...any) ([]string, error) {
	if err := t.check(ctx, false); err != nil {
		return nil, err
	}
	rows, err := t.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, storageError(err)
		}
		values = append(values, id)
	}
	return values, storageError(rows.Err())
}
func jobFilter(column string, ids []record.JobID) (string, []any) {
	if ids == nil {
		return "", nil
	}
	if len(ids) == 0 {
		return " AND 0", nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return " AND " + column + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}
func queryLimit(q state.Query) (int, error) {
	if q.Limit < 0 || q.Limit > 10000 || len(q.Jobs) > 10000 {
		return 0, state.ErrInvalid
	}
	if q.Limit == 0 {
		return 256, nil
	}
	return q.Limit, nil
}
func fetch[T any](ctx context.Context, ids []string, err error, get func(context.Context, string) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	values := make([]T, 0, len(ids))
	for _, id := range ids {
		v, e := get(ctx, id)
		if e != nil {
			return nil, e
		}
		values = append(values, v)
	}
	return values, nil
}
func (t *transaction) Jobs(ctx context.Context, q state.Query) ([]record.Job, error) {
	limit, err := queryLimit(q)
	if err != nil {
		return nil, err
	}
	if q.Branch != "" && q.ChangeID != "" || q.Newest && q.After != "" {
		return nil, state.ErrInvalid
	}
	clause, args := jobFilter("j.id", q.Jobs)
	args = append([]any{t.repo, q.After}, args...)
	from := "jobs j"
	if q.ChangeID != "" {
		from += " INDEXED BY jobs_change"
	}
	sql := "SELECT j.id FROM " + from + " WHERE j.repository_id=? AND j.id>?" + clause
	if q.Pending {
		sql += " AND j.state IN ('queued','active')"
	}
	if q.CleanupBefore != nil {
		sql += " AND j.state NOT IN ('queued','active') AND j.finished_at IS NOT NULL AND j.finished_at<=?"
		args = append(args, q.CleanupBefore.UnixMilli())
	}
	if q.Branch != "" {
		sql += " AND j.change_id IN (SELECT id FROM changes WHERE repository_id=? AND branch=?)"
		args = append(args, t.repo, q.Branch)
	}
	if q.Target != "" {
		sql += " AND j.change_id IN (SELECT id FROM changes WHERE repository_id=? AND initiating_target=? COLLATE NOCASE)"
		args = append(args, t.repo, q.Target)
	}
	if q.ChangeID != "" {
		sql += " AND j.change_id=?"
		args = append(args, q.ChangeID)
	}
	if q.WithBuild {
		sql += " AND j.action!='publish' AND json_type(j.options,'$.Build')='object'"
	}
	if q.Action != "" {
		sql += " AND j.action=?"
		args = append(args, q.Action)
	}
	order := "j.id"
	if q.Newest {
		order = "j.accepted_at DESC,j.rowid DESC"
	}
	if q.DueBefore != nil {
		sql += " AND j.next_action_at IS NOT NULL AND j.next_action_at<=?"
		args = append(args, q.DueBefore.UnixMilli())
		order = "j.next_action_at,j.id"
	}
	sql += " ORDER BY " + order + " LIMIT ?"
	args = append(args, limit)
	ids, err := t.ids(ctx, sql, args...)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Job, error) { return t.Job(ctx, record.JobID(id)) })
}
func (t *transaction) Changes(ctx context.Context, q state.Query) ([]record.Change, error) {
	limit, err := queryLimit(q)
	if err != nil {
		return nil, err
	}
	query := "SELECT id FROM changes WHERE repository_id=? AND id>?"
	args := []any{t.repo, q.After}
	if q.Jobs != nil {
		filter, selected := jobFilter("id", q.Jobs)
		query = "SELECT DISTINCT change_id AS id FROM jobs WHERE repository_id=? AND change_id>?" + filter
		args = append(args, selected...)
	}
	if q.Target != "" {
		if q.Jobs != nil {
			return nil, state.ErrInvalid
		}
		query += " AND initiating_target=? COLLATE NOCASE"
		args = append(args, q.Target)
	}
	if q.Pending {
		if q.Jobs != nil {
			return nil, state.ErrInvalid
		}
		query += " AND disposition='open'"
	}
	ids, err := t.ids(ctx, query+" ORDER BY id LIMIT ?", append(args, limit)...)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Change, error) { return t.Change(ctx, record.ChangeID(id)) })
}
func (t *transaction) Revisions(ctx context.Context, q state.Query) ([]record.Revision, error) {
	limit, err := queryLimit(q)
	if err != nil {
		return nil, err
	}
	query := "SELECT id FROM revisions WHERE repository_id=? AND id>?"
	args := []any{t.repo, q.After}
	if q.Jobs != nil {
		filter, selected := jobFilter("j.id", q.Jobs)
		clauses := []string{}
		args = nil
		for _, column := range []string{"j.input_revision", "j.result_revision", "c.current_revision"} {
			join := ""
			if column == "c.current_revision" {
				join = " JOIN changes c ON c.repository_id=j.repository_id AND c.id=j.change_id"
			}
			clauses = append(clauses, "SELECT "+column+" AS id FROM jobs j"+join+" WHERE j.repository_id=?"+filter)
			args = append(args, t.repo)
			args = append(args, selected...)
		}
		query = "SELECT id FROM (" + strings.Join(clauses, " UNION ") + ") WHERE id>?"
		args = append(args, q.After)
	}
	ids, err := t.ids(ctx, query+" ORDER BY id LIMIT ?", append(args, limit)...)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Revision, error) {
		return t.Revision(ctx, record.RevisionID(id))
	})
}

func (t *transaction) AttemptsForJob(ctx context.Context, id record.JobID) ([]record.Attempt, error) {
	ids, err := t.ids(ctx, "SELECT id FROM attempts WHERE repository_id=? AND job_id=? ORDER BY id", t.repo, id)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Attempt, error) {
		return t.Attempt(ctx, record.AttemptID(id))
	})
}
func (t *transaction) SubmissionsForAttempt(ctx context.Context, id record.AttemptID) ([]record.Submission, error) {
	ids, err := t.ids(ctx, "SELECT id FROM submissions WHERE repository_id=? AND attempt_id=? ORDER BY sequence", t.repo, id)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Submission, error) {
		return t.Submission(ctx, record.RequestID(id))
	})
}
func (t *transaction) ResourcesForAttempt(ctx context.Context, id record.AttemptID) ([]record.Resource, error) {
	ids, err := t.ids(ctx, "SELECT id FROM resources WHERE repository_id=? AND attempt_id=? ORDER BY id", t.repo, id)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Resource, error) {
		return t.Resource(ctx, record.ResourceID(id))
	})
}
func (t *transaction) Resources(ctx context.Context, q state.Query) ([]record.Resource, error) {
	limit, err := queryLimit(q)
	if err != nil {
		return nil, err
	}
	clause, args := jobFilter("a.job_id", q.Jobs)
	base := append([]any{t.repo, q.After}, args...)
	sql := "SELECT r.id FROM resources r JOIN attempts a ON a.repository_id=r.repository_id AND a.id=r.attempt_id WHERE r.repository_id=? AND r.id>?" + clause
	if q.Jobs != nil {
		sql = "SELECT r.id FROM attempts a INDEXED BY attempts_job CROSS JOIN resources r INDEXED BY resources_attempt ON r.repository_id=a.repository_id AND r.attempt_id=a.id WHERE a.repository_id=? AND r.id>?" + clause
	}
	order := "r.id"
	if q.DueBefore != nil {
		sql += " AND r.next_action_at IS NOT NULL AND r.next_action_at<=? AND a.state IN ('finished','canceled')"
		base = append(base, q.DueBefore.UnixMilli())
		order = "r.next_action_at,r.id"
	}
	if q.CleanupBefore != nil {
		sql += " AND r.artifacts_pruned_at IS NULL AND a.state IN ('finished','canceled') AND EXISTS(SELECT 1 FROM jobs j WHERE j.repository_id=a.repository_id AND j.id=a.job_id AND j.state IN ('completed','failed','needs-attention','canceled','superseded') AND j.finished_at<=?) AND ((r.state='released' AND r.released_at<=?) OR r.state IN ('retained','release-requested','uncertain'))"
		base = append(base, q.CleanupBefore.UnixMilli(), q.CleanupBefore.UnixMilli())
	}
	if q.Pending {
		sql += " AND r.state IN ('retained','uncertain','release-requested')"
	}
	ids, err := t.ids(ctx, sql+" ORDER BY "+order+" LIMIT ?", append(base, limit)...)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.Resource, error) {
		return t.Resource(ctx, record.ResourceID(id))
	})
}
func (t *transaction) Controls(ctx context.Context, q state.Query) ([]record.ControlRequest, error) {
	limit, err := queryLimit(q)
	if err != nil {
		return nil, err
	}
	clause, args := jobFilter("c.job_id", q.Jobs)
	base := append([]any{t.repo, q.After}, args...)
	sql := "SELECT r.id FROM requests r WHERE r.repository_id=? AND r.id>? AND r.kind='cancel' AND EXISTS(SELECT 1 FROM control_jobs c WHERE c.repository_id=r.repository_id AND c.request_id=r.id" + clause
	if q.Pending {
		sql += " AND c.applied_at IS NULL"
	}
	sql += ")"
	if q.Pending {
		sql += " AND r.completed_at IS NULL"
	}
	ids, err := t.ids(ctx, sql+" ORDER BY r.id LIMIT ?", append(base, limit)...)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.ControlRequest, error) {
		return t.Control(ctx, record.RequestID(id))
	})
}
