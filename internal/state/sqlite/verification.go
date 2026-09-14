package sqlite

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

func (t *transaction) VerificationCandidates(ctx context.Context, query state.VerificationQuery) ([]record.Attempt, error) {
	if err := t.check(ctx, false); err != nil {
		return nil, err
	}
	if (query.Target.Name == "" && query.Tree == "") || query.Target.Portfile == "" || query.Limit < 1 || query.Limit > 32 || (query.Tree != "" && !objectID(query.Tree)) {
		return nil, state.ErrInvalid
	}
	var sql string
	var args []any
	if query.Target.Name == "" {
		sql = verificationPortfileSQL
		args = []any{t.repo, query.Tree, query.Target.Portfile, query.Limit}
	} else if query.Tree != "" {
		sql = verificationTreeSQL
		args = []any{t.repo, query.Tree, query.Target.Name, query.Target.Portfile, query.Limit}
	} else {
		sql = verificationTargetSQL
		args = []any{t.repo, query.Target.Name, query.Target.Portfile, query.Limit}
	}
	ids, err := t.ids(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	result := make([]record.Attempt, 0, len(ids))
	for _, id := range ids {
		attempt, err := t.Attempt(ctx, record.AttemptID(id))
		if err != nil {
			return nil, err
		}
		result = append(result, attempt)
	}
	return result, nil
}

const verificationTreeBaseSQL = `SELECT a.id FROM sources AS s INDEXED BY sources_reuse_tree CROSS JOIN attempts AS a INDEXED BY attempts_reuse_source
   JOIN attempt_evidence e ON e.repository_id=a.repository_id AND e.attempt_id=a.id
   WHERE s.repository_id=? AND s.tree_id=? AND a.repository_id=s.repository_id AND a.source_id=s.id AND a.state IN ('finished','canceled')
   `

const verificationTreeSQL = verificationTreeBaseSQL + `AND json_extract(a.build,'$.Target.Name')=? AND json_extract(a.build,'$.Target.Portfile')=?
   ORDER BY a.created_at DESC,a.id DESC LIMIT ?`

const verificationPortfileSQL = verificationTreeBaseSQL + `AND json_extract(a.build,'$.Target.Portfile')=?
   ORDER BY a.created_at DESC,a.id DESC LIMIT ?`

const verificationTargetSQL = `SELECT a.id FROM attempts AS a INDEXED BY attempts_reuse_target
   JOIN attempt_evidence e ON e.repository_id=a.repository_id AND e.attempt_id=a.id
   WHERE a.repository_id=? AND a.state IN ('finished','canceled') AND json_extract(a.build,'$.Target.Name')=? AND json_extract(a.build,'$.Target.Portfile')=?
   ORDER BY a.created_at DESC,a.id DESC LIMIT ?`
