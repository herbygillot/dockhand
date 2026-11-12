package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
)

// Serve's count by its rows (schema 61, docs/designs/publication-record.md,
// "serve's count"): a row is one pull request serve opened, may have
// opened, or holds a place to open, and outlives its branch.

const openingColumns = "id, base, head, after, holding, tried, unanswered, number, opened, gone"

func (t *tx) ServeOpenings() ([]model.ServeOpening, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT "+openingColumns+" FROM serve_openings WHERE repository_id=? ORDER BY id", t.repo)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var openings []model.ServeOpening
	for rows.Next() {
		var o model.ServeOpening
		var holding, unanswered int
		var tried, opened, gone sql.NullInt64
		if err := rows.Scan(&o.ID, &o.Base, &o.Head, &o.After, &holding, &tried, &unanswered, &o.Number, &opened, &gone); err != nil {
			return nil, storageError(err)
		}
		o.Holding, o.Unanswered = holding == 1, unanswered == 1
		o.Tried, o.Opened, o.Gone = nullTime(tried), nullTime(opened), nullTime(gone)
		openings = append(openings, o)
	}
	return openings, storageError(rows.Err())
}

func (t *tx) ServeUpgrade() (time.Time, bool, bool, error) {
	var at int64
	var done int
	err := t.conn.QueryRowContext(t.ctx, "SELECT at, first_pass_done FROM serve_openings_upgrade WHERE repository_id=?", t.repo).Scan(&at, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, true, false, nil
	}
	if err != nil {
		return time.Time{}, false, false, storageError(err)
	}
	return fromMillis(at), done == 1, true, nil
}

func (t *tx) AddServeOpening(o model.ServeOpening) (int64, error) {
	if o.Base == "" || o.Head == "" {
		return 0, fmt.Errorf("%w: a row of serve's count names its base and head", model.ErrInvalid)
	}
	result, err := t.exec("INSERT INTO serve_openings(repository_id, base, head, after, holding, tried, unanswered, number, opened, gone) VALUES(?,?,?,?,?,?,?,?,?,?)",
		t.repo, o.Base, o.Head, o.After, boolInt(o.Holding), timeOrNull(o.Tried), boolInt(o.Unanswered), o.Number, timeOrNull(o.Opened), timeOrNull(o.Gone))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (t *tx) UpdateServeOpening(o model.ServeOpening) error {
	return t.update(fmt.Sprintf("serve opening %d", o.ID), "UPDATE serve_openings SET base=?, head=?, after=?, holding=?, tried=?, unanswered=?, number=?, opened=?, gone=? WHERE repository_id=? AND id=?",
		o.Base, o.Head, o.After, boolInt(o.Holding), timeOrNull(o.Tried), boolInt(o.Unanswered), o.Number, timeOrNull(o.Opened), timeOrNull(o.Gone), t.repo, o.ID)
}

func (t *tx) ReleaseServeOpening(id int64) error {
	return t.update(fmt.Sprintf("serve opening %d", id), "DELETE FROM serve_openings WHERE repository_id=? AND id=?", t.repo, id)
}

func (t *tx) SetServeFirstPassDone() error {
	_, err := t.exec("UPDATE serve_openings_upgrade SET first_pass_done=1 WHERE repository_id=?", t.repo)
	return err
}

// PruneServeOpenings removes the openings older than before: an opening
// counts on its own day alone, and a row tried or holding a place is
// never pruned.
func (t *tx) PruneServeOpenings(before time.Time) error {
	_, err := t.exec("DELETE FROM serve_openings WHERE repository_id=? AND opened IS NOT NULL AND opened<?", t.repo, millis(before))
	return err
}

func timeOrNull(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return millis(at)
}

func nullTime(at sql.NullInt64) time.Time {
	if !at.Valid {
		return time.Time{}
	}
	return fromMillis(at.Int64)
}
