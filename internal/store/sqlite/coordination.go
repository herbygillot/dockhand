package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

const sessionColumns = "id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at"

func (t *tx) scanSession(row interface{ Scan(...any) error }) (model.Session, error) {
	var s model.Session
	var started, heartbeat int64
	var ended sql.NullInt64
	if err := row.Scan(&s.ID, &s.Kind, &s.PID, &s.ProcessStart, &s.Version, &started, &heartbeat, &ended); err != nil {
		return model.Session{}, storageError(err)
	}
	s.Repository, s.StartedAt, s.HeartbeatAt, s.EndedAt = t.repo, fromMillis(started), fromMillis(heartbeat), fromNullable(ended)
	return s, nil
}

func (t *tx) Session(id model.SessionID) (model.Session, error) {
	return t.scanSession(t.conn.QueryRowContext(t.ctx, "SELECT "+sessionColumns+" FROM sessions WHERE repository_id=? AND id=?", t.repo, id))
}

func (t *tx) Sessions() ([]model.Session, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT "+sessionColumns+" FROM sessions WHERE repository_id=? AND ended_at IS NULL ORDER BY started_at, id", t.repo)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var sessions []model.Session
	for rows.Next() {
		s, err := t.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, storageError(rows.Err())
}

func (t *tx) AddSession(s model.Session) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := t.checkRepository(s.Repository); err != nil {
		return err
	}
	_, err := t.exec("INSERT INTO sessions(repository_id, "+sessionColumns+") VALUES(?,?,?,?,?,?,?,?,?)",
		t.repo, s.ID, s.Kind, s.PID, s.ProcessStart, s.Version, millis(s.StartedAt), millis(s.HeartbeatAt), nullableMillis(s.EndedAt))
	return err
}

func (t *tx) UpdateSession(s model.Session) error {
	if err := s.Validate(); err != nil {
		return err
	}
	current, err := t.Session(s.ID)
	if err != nil {
		return err
	}
	if current.Kind != s.Kind || current.PID != s.PID || current.ProcessStart != s.ProcessStart || !current.StartedAt.Equal(fromMillis(millis(s.StartedAt))) {
		return fmt.Errorf("%w: session %s's process is fixed", store.ErrConflict, s.ID)
	}
	if current.EndedAt != nil {
		return fmt.Errorf("%w: session %s has ended", store.ErrConflict, s.ID)
	}
	return t.update("session "+string(s.ID), "UPDATE sessions SET version=?, heartbeat_at=?, ended_at=? WHERE repository_id=? AND id=?",
		s.Version, millis(s.HeartbeatAt), nullableMillis(s.EndedAt), t.repo, s.ID)
}

func (t *tx) Lease(resource string) (model.Lease, error) {
	l := model.Lease{Resource: resource}
	var holder sql.NullString
	var acquired int64
	err := t.conn.QueryRowContext(t.ctx, "SELECT holder, generation, acquired_at FROM leases WHERE repository_id=? AND resource=?", t.repo, resource).Scan(&holder, &l.Generation, &acquired)
	if err != nil {
		return model.Lease{}, storageError(err)
	}
	l.Holder, l.AcquiredAt = model.SessionID(holder.String), fromMillis(acquired)
	return l, nil
}

func (t *tx) AcquireLease(resource string, session model.SessionID) (model.Lease, error) {
	if resource == "" || session == "" {
		return model.Lease{}, fmt.Errorf("%w: a lease needs a resource and a session", model.ErrInvalid)
	}
	holder, err := t.Session(session)
	if err != nil {
		return model.Lease{}, err
	}
	if holder.EndedAt != nil {
		return model.Lease{}, fmt.Errorf("%w: session %s has ended", store.ErrConflict, session)
	}
	now := time.Now()
	current, err := t.Lease(resource)
	switch {
	case errors.Is(err, store.ErrNotFound):
		_, err = t.exec("INSERT INTO leases(repository_id, resource, holder, generation, acquired_at) VALUES(?,?,?,?,?)", t.repo, resource, session, 1, millis(now))
		return model.Lease{Resource: resource, Holder: session, Generation: 1, AcquiredAt: fromMillis(millis(now))}, err
	case err != nil:
		return model.Lease{}, err
	}
	next := current.Generation + 1
	if _, err = t.exec("UPDATE leases SET holder=?, generation=?, acquired_at=? WHERE repository_id=? AND resource=?", session, next, millis(now), t.repo, resource); err != nil {
		return model.Lease{}, err
	}
	return model.Lease{Resource: resource, Holder: session, Generation: next, AcquiredAt: fromMillis(millis(now))}, nil
}

func (t *tx) CheckLease(l model.Lease) error {
	current, err := t.Lease(l.Resource)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: no lease on %s", store.ErrStale, l.Resource)
	}
	if err != nil {
		return err
	}
	if l.Holder == "" || current.Holder != l.Holder || current.Generation != l.Generation {
		return fmt.Errorf("%w: %s is held under generation %d, not %d by %s", store.ErrStale, l.Resource, current.Generation, l.Generation, l.Holder)
	}
	return nil
}

func (t *tx) ReleaseLease(l model.Lease) error {
	if err := t.CheckLease(l); err != nil {
		return err
	}
	_, err := t.exec("UPDATE leases SET holder=NULL WHERE repository_id=? AND resource=?", t.repo, l.Resource)
	return err
}

func (t *tx) AppendEvent(e model.Event) (int64, error) {
	switch {
	case e.Kind == "":
		return 0, fmt.Errorf("%w: an event needs a kind", model.ErrInvalid)
	case e.At.IsZero():
		return 0, fmt.Errorf("%w: an event needs a time", model.ErrInvalid)
	}
	if e.Level == "" {
		e.Level = model.LevelInfo
	}
	result, err := t.exec("INSERT INTO events(repository_id, at, session_id, branch_id, run_id, target_id, kind, level, message) VALUES(?,?,?,?,?,?,?,?,?)",
		t.repo, millis(e.At), e.Session, e.Branch, e.Run, e.Target, e.Kind, e.Level, e.Message)
	if err != nil {
		return 0, err
	}
	sequence, err := result.LastInsertId()
	return sequence, storageError(err)
}

func (t *tx) Events(after int64, limit int) ([]model.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := t.conn.QueryContext(t.ctx, "SELECT sequence, at, session_id, branch_id, run_id, target_id, kind, level, message FROM events WHERE repository_id=? AND sequence>? ORDER BY sequence LIMIT ?", t.repo, after, limit)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var events []model.Event
	for rows.Next() {
		var e model.Event
		var at int64
		if err := rows.Scan(&e.Sequence, &at, &e.Session, &e.Branch, &e.Run, &e.Target, &e.Kind, &e.Level, &e.Message); err != nil {
			return nil, storageError(err)
		}
		e.At = fromMillis(at)
		events = append(events, e)
	}
	return events, storageError(rows.Err())
}
