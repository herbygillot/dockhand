package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

func (t *tx) AddEdit(e model.Edit) error {
	if err := e.Validate(); err != nil {
		return err
	}
	files, err := json.Marshal(e.Files)
	if err != nil {
		return err
	}
	_, err = t.exec("INSERT INTO edits(repository_id, id, branch_id, kind, port, directory, subject, files, at) VALUES(?,?,?,?,?,?,?,?,?)",
		t.repo, e.ID, e.Branch, e.Kind, e.Port, e.Directory, e.Subject, string(files), millis(e.At))
	return err
}

func (t *tx) Edits(branch model.BranchID) ([]model.Edit, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT id, branch_id, kind, port, directory, subject, files, at FROM edits WHERE repository_id=? AND branch_id=? ORDER BY at, rowid", t.repo, branch)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var edits []model.Edit
	for rows.Next() {
		var e model.Edit
		var files string
		var at int64
		if err := rows.Scan(&e.ID, &e.Branch, &e.Kind, &e.Port, &e.Directory, &e.Subject, &files, &at); err != nil {
			return nil, storageError(err)
		}
		if err := json.Unmarshal([]byte(files), &e.Files); err != nil {
			return nil, fmt.Errorf("%w: edit %s: %w", store.ErrUnavailable, e.ID, err)
		}
		e.At = fromMillis(at)
		edits = append(edits, e)
	}
	return edits, storageError(rows.Err())
}

const checkpointColumns = "number, kind, branch_id, before_head, after_head, at, restored_at"

func scanCheckpoint(row interface{ Scan(...any) error }) (model.Checkpoint, error) {
	var c model.Checkpoint
	var at int64
	var restored sql.NullInt64
	if err := row.Scan(&c.Number, &c.Kind, &c.Branch, &c.Before, &c.After, &at, &restored); err != nil {
		return model.Checkpoint{}, storageError(err)
	}
	c.At, c.RestoredAt = fromMillis(at), fromNullable(restored)
	return c, nil
}

func (t *tx) NextCheckpointNumber() (int, error) {
	var n int
	err := t.conn.QueryRowContext(t.ctx, "SELECT coalesce(max(number), 0) + 1 FROM checkpoints WHERE repository_id=?", t.repo).Scan(&n)
	return n, storageError(err)
}

func (t *tx) AddCheckpoint(c model.Checkpoint) error {
	if err := c.Validate(); err != nil {
		return err
	}
	next, err := t.NextCheckpointNumber()
	if err != nil {
		return err
	}
	if c.Number != next {
		return fmt.Errorf("%w: checkpoint %d is not the next, %d", store.ErrConflict, c.Number, next)
	}
	_, err = t.exec("INSERT INTO checkpoints(repository_id, "+checkpointColumns+") VALUES(?,?,?,?,?,?,?,?)",
		t.repo, c.Number, c.Kind, c.Branch, c.Before, c.After, millis(c.At), nullableMillis(c.RestoredAt))
	return err
}

func (t *tx) Checkpoint(number int) (model.Checkpoint, error) {
	return scanCheckpoint(t.conn.QueryRowContext(t.ctx, "SELECT "+checkpointColumns+" FROM checkpoints WHERE repository_id=? AND number=?", t.repo, number))
}

func (t *tx) Checkpoints(branch model.BranchID) ([]model.Checkpoint, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT "+checkpointColumns+" FROM checkpoints WHERE repository_id=? AND branch_id=? ORDER BY number", t.repo, branch)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var checkpoints []model.Checkpoint
	for rows.Next() {
		c, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		checkpoints = append(checkpoints, c)
	}
	return checkpoints, storageError(rows.Err())
}

func (t *tx) MarkRestored(c model.Checkpoint) error {
	if c.RestoredAt == nil {
		return fmt.Errorf("%w: checkpoint %s has no restore time", model.ErrInvalid, c.Name())
	}
	return t.update("checkpoint "+c.Name(), "UPDATE checkpoints SET restored_at=? WHERE repository_id=? AND number=? AND restored_at IS NULL",
		millis(*c.RestoredAt), t.repo, c.Number)
}

func (t *tx) AddAcceptance(a model.Acceptance) error {
	if err := a.Validate(); err != nil {
		return err
	}
	_, err := t.exec("INSERT INTO acceptances(repository_id, branch_id, commit_id, port, at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING",
		t.repo, a.Branch, a.Commit, a.Port, millis(a.At))
	return err
}

func (t *tx) Acceptances(branch model.BranchID, commit model.ObjectID) ([]model.Acceptance, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT branch_id, commit_id, port, at FROM acceptances WHERE repository_id=? AND branch_id=? AND commit_id=? ORDER BY port", t.repo, branch, commit)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var accepted []model.Acceptance
	for rows.Next() {
		var a model.Acceptance
		var at int64
		if err := rows.Scan(&a.Branch, &a.Commit, &a.Port, &at); err != nil {
			return nil, storageError(err)
		}
		a.At = fromMillis(at)
		accepted = append(accepted, a)
	}
	return accepted, storageError(rows.Err())
}

func (t *tx) AddReview(r model.Review) error {
	if err := r.Validate(); err != nil {
		return err
	}
	findings := r.Findings
	if findings == nil {
		findings = []model.ReviewFinding{}
	}
	data, err := json.Marshal(findings)
	if err != nil {
		return err
	}
	_, err = t.exec("INSERT INTO reviews(repository_id, pr_repository, number, head, findings, posted, at) VALUES(?,?,?,?,?,?,?)",
		t.repo, r.Repository, r.Number, r.Head, string(data), r.Posted, millis(r.At))
	return err
}

func (t *tx) LastReview(repository string, number int) (model.Review, error) {
	r := model.Review{Repository: repository, Number: number}
	var findings string
	var at int64
	err := t.conn.QueryRowContext(t.ctx, "SELECT head, findings, posted, at FROM reviews WHERE repository_id=? AND pr_repository=? AND number=? ORDER BY at DESC, rowid DESC LIMIT 1",
		t.repo, repository, number).Scan(&r.Head, &findings, &r.Posted, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("%w: no review of %s#%d", store.ErrNotFound, repository, number)
	}
	if err != nil {
		return r, storageError(err)
	}
	r.At = fromMillis(at)
	if err := json.Unmarshal([]byte(findings), &r.Findings); err != nil {
		return r, fmt.Errorf("review of %s#%d: %w", repository, number, err)
	}
	return r, nil
}
