package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// AddEdit records an edit whose files are written; one with Written zero
// is refused, so a writer nobody moved to the preparation record's order
// fails in its own test rather than leave an edit every reader hides
// (#194).
func (t *tx) AddEdit(e model.Edit) error {
	if e.Written.IsZero() {
		return fmt.Errorf("%w: edit %s has no written time; an edit recorded before its files goes through AddUnwritten", store.ErrConflict, e.ID)
	}
	return t.addEdit(e)
}

// AddUnwritten records an edit before its files are written (#194, step
// 3): every reader but the settler's two passes over it until SetWritten.
func (t *tx) AddUnwritten(e model.Edit) error {
	if !e.Written.IsZero() {
		return fmt.Errorf("%w: edit %s is already written", store.ErrConflict, e.ID)
	}
	return t.addEdit(e)
}

// SetWritten records that an unwritten edit's files are written.
func (t *tx) SetWritten(edit model.EditID, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("%w: edit %s: a written time is required", store.ErrConflict, edit)
	}
	return t.update("unwritten edit "+string(edit), "UPDATE edits SET written=? WHERE repository_id=? AND id=? AND written=0", millis(at), t.repo, edit)
}

// RemoveUnwritten removes an edit whose files were never written; a
// written edit is never removed by it.
func (t *tx) RemoveUnwritten(edit model.EditID) error {
	return t.update("unwritten edit "+string(edit), "DELETE FROM edits WHERE repository_id=? AND id=? AND written=0", t.repo, edit)
}

func (t *tx) addEdit(e model.Edit) error {
	if err := e.Validate(); err != nil {
		return err
	}
	files, err := json.Marshal(e.Files)
	if err != nil {
		return err
	}
	upstream, release, https, result, source := "", "", "", "", ""
	if e.Upstream != nil {
		data, err := json.Marshal(e.Upstream)
		if err != nil {
			return err
		}
		upstream = string(data)
	}
	if e.Release != nil {
		data, err := json.Marshal(e.Release)
		if err != nil {
			return err
		}
		release = string(data)
	}
	if len(e.HTTPS) > 0 {
		data, err := json.Marshal(e.HTTPS)
		if err != nil {
			return err
		}
		https = string(data)
	}
	if e.Result != nil {
		data, err := json.Marshal(e.Result)
		if err != nil {
			return err
		}
		result = string(data)
	}
	if e.Source != nil {
		data, err := json.Marshal(e.Source)
		if err != nil {
			return err
		}
		source = string(data)
	}
	written := int64(0)
	if !e.Written.IsZero() {
		written = millis(e.Written)
	}
	_, err = t.exec("INSERT INTO edits(repository_id, id, branch_id, kind, port, directory, subject, files, at, upstream, release, excepted, https, source, written, result) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, e.ID, e.Branch, e.Kind, e.Port, e.Directory, e.Subject, string(files), millis(e.At), upstream, release, strings.Join(e.Excepted, " "), https, source, written, result)
	return err
}

func (t *tx) SetExcepted(edit model.EditID, excepted []string) error {
	return t.update("edit "+string(edit), "UPDATE edits SET excepted=? WHERE repository_id=? AND id=?", strings.Join(excepted, " "), t.repo, edit)
}

// Edits lists a branch's written edits, oldest first: an unwritten one is
// the settler's alone (#194).
func (t *tx) Edits(branch model.BranchID) ([]model.Edit, error) {
	return t.queryEdits("WHERE repository_id=? AND branch_id=? AND written!=0 ORDER BY at, rowid", t.repo, branch)
}

// UnwrittenEdits lists a branch's edits recorded before their files and
// not yet seen written, oldest first.
func (t *tx) UnwrittenEdits(branch model.BranchID) ([]model.Edit, error) {
	return t.queryEdits("WHERE repository_id=? AND branch_id=? AND written=0 ORDER BY at, rowid", t.repo, branch)
}

// Edit is one edit by its ID, written or not, which the record's two
// witnesses read; false where there is none.
func (t *tx) Edit(id model.EditID) (model.Edit, bool, error) {
	edits, err := t.queryEdits("WHERE repository_id=? AND id=?", t.repo, id)
	if err != nil || len(edits) == 0 {
		return model.Edit{}, false, err
	}
	return edits[0], true, nil
}

func (t *tx) queryEdits(where string, args ...any) ([]model.Edit, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT id, branch_id, kind, port, directory, subject, files, at, upstream, release, excepted, https, source, written, result FROM edits "+where, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var edits []model.Edit
	for rows.Next() {
		var e model.Edit
		var files, upstream, release, excepted, https, source, result string
		var at, written int64
		if err := rows.Scan(&e.ID, &e.Branch, &e.Kind, &e.Port, &e.Directory, &e.Subject, &files, &at, &upstream, &release, &excepted, &https, &source, &written, &result); err != nil {
			return nil, storageError(err)
		}
		if excepted != "" {
			e.Excepted = strings.Fields(excepted)
		}
		if upstream != "" {
			if err := json.Unmarshal([]byte(upstream), &e.Upstream); err != nil {
				return nil, fmt.Errorf("%w: edit %s: %w", store.ErrUnavailable, e.ID, err)
			}
		}
		if release != "" {
			if err := json.Unmarshal([]byte(release), &e.Release); err != nil {
				return nil, fmt.Errorf("%w: edit %s's release: %w", store.ErrUnavailable, e.ID, err)
			}
		}
		if https != "" {
			if err := json.Unmarshal([]byte(https), &e.HTTPS); err != nil {
				return nil, fmt.Errorf("%w: edit %s's links switched to https: %w", store.ErrUnavailable, e.ID, err)
			}
		}
		if source != "" {
			if err := json.Unmarshal([]byte(source), &e.Source); err != nil {
				return nil, fmt.Errorf("%w: edit %s's source move: %w", store.ErrUnavailable, e.ID, err)
			}
		}
		if result != "" {
			if err := json.Unmarshal([]byte(result), &e.Result); err != nil {
				return nil, fmt.Errorf("%w: edit %s's result: %w", store.ErrUnavailable, e.ID, err)
			}
		}
		if err := json.Unmarshal([]byte(files), &e.Files); err != nil {
			return nil, fmt.Errorf("%w: edit %s: %w", store.ErrUnavailable, e.ID, err)
		}
		e.At = fromMillis(at)
		if written != 0 {
			e.Written = fromMillis(written)
		}
		edits = append(edits, e)
	}
	return edits, storageError(rows.Err())
}

const checkpointColumns = "number, kind, state, branch_id, before_head, after_head, base_before, base_after, index_tree, at, restored_at"

func scanCheckpoint(row interface{ Scan(...any) error }) (model.Checkpoint, error) {
	var c model.Checkpoint
	var at int64
	var restored sql.NullInt64
	if err := row.Scan(&c.Number, &c.Kind, &c.State, &c.Branch, &c.Before, &c.After, &c.BaseBefore, &c.BaseAfter, &c.Index, &at, &restored); err != nil {
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
	if c.Number < next {
		return fmt.Errorf("%w: checkpoint %d is below the next, %d", store.ErrConflict, c.Number, next)
	}
	_, err = t.exec("INSERT INTO checkpoints(repository_id, "+checkpointColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
		t.repo, c.Number, c.Kind, c.State, c.Branch, c.Before, c.After, c.BaseBefore, c.BaseAfter, c.Index, millis(c.At), nullableMillis(c.RestoredAt))
	return err
}

func (t *tx) Checkpoint(number int) (model.Checkpoint, error) {
	return scanCheckpoint(t.conn.QueryRowContext(t.ctx, "SELECT "+checkpointColumns+" FROM checkpoints WHERE repository_id=? AND number=?", t.repo, number))
}

// checkpointsQuery reads a branch's checkpoints through checkpoint_branch.
const checkpointsQuery = "SELECT " + checkpointColumns + " FROM checkpoints WHERE repository_id=? AND branch_id=? ORDER BY number"

func (t *tx) Checkpoints(branch model.BranchID) ([]model.Checkpoint, error) {
	rows, err := t.conn.QueryContext(t.ctx, checkpointsQuery, t.repo, branch)
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

func (t *tx) SettleCheckpoint(c model.Checkpoint) error {
	if c.State != model.CheckpointApplied && c.State != model.CheckpointAbandoned {
		return fmt.Errorf("%w: checkpoint %s can't settle as %s", model.ErrInvalid, c.Name(), c.State)
	}
	return t.update("checkpoint "+c.Name(), "UPDATE checkpoints SET state=? WHERE repository_id=? AND number=? AND state='prepared'",
		c.State, t.repo, c.Number)
}

func (t *tx) MarkRestored(c model.Checkpoint) error {
	if c.RestoredAt == nil {
		return fmt.Errorf("%w: checkpoint %s has no restore time", model.ErrInvalid, c.Name())
	}
	return t.update("checkpoint "+c.Name(), "UPDATE checkpoints SET restored_at=? WHERE repository_id=? AND number=? AND state='applied' AND restored_at IS NULL",
		millis(*c.RestoredAt), t.repo, c.Number)
}

func (t *tx) AddDecision(d model.Decision) error {
	if err := d.Validate(); err != nil {
		return err
	}
	// A standing decision is no branch's, which the column says as null.
	// One made again on the same evidence replaces it, as a person's
	// "awaiting" becomes "acceptable because…".
	var branch any
	if d.Branch != "" {
		branch = d.Branch
	}
	_, err := t.exec("INSERT INTO decisions(repository_id, branch_id, finding, subject, evidence, answer, words, at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(repository_id, ifnull(branch_id, ''), finding, subject, evidence) DO UPDATE SET answer=excluded.answer, words=excluded.words, at=excluded.at",
		t.repo, branch, d.Finding, d.Subject, d.Evidence, d.Answer, d.Words, millis(d.At))
	return err
}

func (t *tx) Decisions(branch model.BranchID, finding model.FindingKind, evidence string) ([]model.Decision, error) {
	return t.decisions("SELECT ifnull(branch_id, ''), finding, subject, evidence, answer, words, at FROM decisions WHERE repository_id=? AND branch_id=? AND finding=? AND evidence=? ORDER BY subject",
		t.repo, branch, finding, evidence)
}

func (t *tx) BranchDecisions(branch model.BranchID, finding model.FindingKind) ([]model.Decision, error) {
	return t.decisions("SELECT ifnull(branch_id, ''), finding, subject, evidence, answer, words, at FROM decisions WHERE repository_id=? AND branch_id=? AND finding=? ORDER BY subject, at",
		t.repo, branch, finding)
}

func (t *tx) StandingDecisions(finding model.FindingKind) ([]model.Decision, error) {
	return t.decisions("SELECT '', finding, subject, evidence, answer, words, at FROM decisions WHERE repository_id=? AND branch_id IS NULL AND finding=? ORDER BY subject, evidence",
		t.repo, finding)
}

func (t *tx) decisions(query string, args ...any) ([]model.Decision, error) {
	rows, err := t.conn.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var decisions []model.Decision
	for rows.Next() {
		var d model.Decision
		var at int64
		if err := rows.Scan(&d.Branch, &d.Finding, &d.Subject, &d.Evidence, &d.Answer, &d.Words, &at); err != nil {
			return nil, storageError(err)
		}
		d.At = fromMillis(at)
		decisions = append(decisions, d)
	}
	return decisions, storageError(rows.Err())
}

func (t *tx) AddCIFailure(f model.CIFailure) error {
	if err := f.Validate(); err != nil {
		return err
	}
	ports := ""
	if len(f.Ports) > 0 {
		data, err := json.Marshal(f.Ports)
		if err != nil {
			return err
		}
		ports = string(data)
	}
	_, err := t.exec("INSERT INTO ci_failures(repository_id, branch_id, head, run, attempt, job, check_name, cause, file, url, status, reason, error, ports, at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING",
		t.repo, f.Branch, f.Head, f.Run, f.Attempt, f.Job, f.Check, f.Cause, f.File, f.URL, f.Status, f.Reason, f.Error, ports, millis(f.At))
	return err
}

func (t *tx) CIFailures(branch model.BranchID, head model.ObjectID) ([]model.CIFailure, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT branch_id, head, run, attempt, job, check_name, cause, file, url, status, reason, error, ports, at FROM ci_failures WHERE repository_id=? AND branch_id=? AND head=? ORDER BY check_name, job, attempt",
		t.repo, branch, head)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var failures []model.CIFailure
	for rows.Next() {
		var f model.CIFailure
		var at int64
		var ports string
		if err := rows.Scan(&f.Branch, &f.Head, &f.Run, &f.Attempt, &f.Job, &f.Check, &f.Cause, &f.File, &f.URL, &f.Status, &f.Reason, &f.Error, &ports, &at); err != nil {
			return nil, storageError(err)
		}
		if ports != "" {
			if err := json.Unmarshal([]byte(ports), &f.Ports); err != nil {
				return nil, fmt.Errorf("%w: CI job %d's ports: %w", store.ErrUnavailable, f.Job, err)
			}
		}
		f.At = fromMillis(at)
		failures = append(failures, f)
	}
	return failures, storageError(rows.Err())
}

func (t *tx) SetNetworkHolds(branch model.BranchID, tree model.ObjectID, at time.Time, holds []model.Concern) error {
	if branch == "" || tree == "" || at.IsZero() {
		return fmt.Errorf("%w: network holds need a branch, a tree, and a time", model.ErrInvalid)
	}
	if _, err := t.exec("DELETE FROM network_holds WHERE repository_id=? AND branch_id=? AND tree=?", t.repo, branch, tree); err != nil {
		return err
	}
	// A plan that found none is kept too, as a row with no origin, so
	// status says none were found rather than nothing being known.
	if len(holds) == 0 {
		_, err := t.exec("INSERT INTO network_holds(repository_id, branch_id, tree, origin, port, rule, path, subject, source, class, detail, at) VALUES(?,?,?,'','','','','','','','',?)", t.repo, branch, tree, millis(at))
		return err
	}
	for _, h := range holds {
		if _, err := t.exec("INSERT INTO network_holds(repository_id, branch_id, tree, origin, port, rule, path, subject, source, class, detail, at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
			t.repo, branch, tree, h.Origin, h.Port, h.Rule, h.Path, h.Subject, h.Source, h.Class, h.Detail, millis(at)); err != nil {
			return err
		}
	}
	return nil
}

func (t *tx) NetworkHolds(branch model.BranchID, tree model.ObjectID) ([]model.Concern, time.Time, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT origin, port, rule, path, subject, source, class, detail, at FROM network_holds WHERE repository_id=? AND branch_id=? AND tree=? ORDER BY rowid",
		t.repo, branch, tree)
	if err != nil {
		return nil, time.Time{}, storageError(err)
	}
	defer rows.Close()
	var holds []model.Concern
	var at time.Time
	for rows.Next() {
		var h model.Concern
		var when int64
		if err := rows.Scan(&h.Origin, &h.Port, &h.Rule, &h.Path, &h.Subject, &h.Source, &h.Class, &h.Detail, &when); err != nil {
			return nil, time.Time{}, storageError(err)
		}
		at = fromMillis(when)
		if h.Origin != "" {
			holds = append(holds, h)
		}
	}
	return holds, at, storageError(rows.Err())
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

func (t *tx) AllowEvaluation(c model.EvaluationConsent) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := t.exec("INSERT INTO evaluation_consents(repository_id, pr_repository, number, head, at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING",
		t.repo, c.Repository, c.Number, c.Head, millis(c.At))
	return err
}

func (t *tx) EvaluationConsents(repository string, number int) ([]model.EvaluationConsent, error) {
	rows, err := t.conn.QueryContext(t.ctx, "SELECT head, at FROM evaluation_consents WHERE repository_id=? AND pr_repository=? AND number=? ORDER BY at DESC, head",
		t.repo, repository, number)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var consents []model.EvaluationConsent
	for rows.Next() {
		c := model.EvaluationConsent{Repository: repository, Number: number}
		var at int64
		if err := rows.Scan(&c.Head, &at); err != nil {
			return nil, storageError(err)
		}
		c.At = fromMillis(at)
		consents = append(consents, c)
	}
	return consents, storageError(rows.Err())
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

func (t *tx) RecordAssessment(a model.Assessment) error {
	if err := a.Validate(); err != nil {
		return err
	}
	comparison, err := json.Marshal(a.Comparison)
	if err != nil {
		return err
	}
	_, err = t.exec("INSERT INTO assessments(repository_id, branch_id, tree, base, port, directory, comparison, policy, build, at) VALUES(?,?,?,?,?,?,?,?,?,?) "+
		"ON CONFLICT(repository_id, branch_id, tree, base, port) DO UPDATE SET directory=excluded.directory, comparison=excluded.comparison, policy=excluded.policy, build=excluded.build, at=excluded.at",
		t.repo, a.Branch, a.Tree, a.Base, a.Port, a.Directory, string(comparison), a.Policy, a.Build, millis(a.At))
	return err
}

func (t *tx) RecordChange(r model.ChangeRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	record, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = t.exec("INSERT INTO change_records(repository_id, branch_id, tree, base, directory, record, policy, at) VALUES(?,?,?,?,?,?,?,?) "+
		"ON CONFLICT(repository_id, branch_id, tree, base, directory) DO UPDATE SET record=excluded.record, policy=excluded.policy, at=excluded.at",
		t.repo, r.Branch, r.Tree, r.Base, r.Directory, string(record), r.Policy, millis(r.At))
	return err
}

func (t *tx) ChangeRecords(filter store.AssessmentFilter) ([]model.ChangeRecord, error) {
	query, args := "SELECT record, at FROM change_records WHERE repository_id=?", []any{any(t.repo)}
	if filter.Branch != "" {
		query += " AND branch_id=?"
		args = append(args, filter.Branch)
	}
	if filter.Tree != "" {
		query += " AND tree=?"
		args = append(args, filter.Tree)
	}
	if filter.Base != "" {
		query += " AND base=?"
		args = append(args, filter.Base)
	}
	rows, err := t.conn.QueryContext(t.ctx, query+" ORDER BY +at DESC, rowid DESC", args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var records []model.ChangeRecord
	for rows.Next() {
		var record string
		var at int64
		if err := rows.Scan(&record, &at); err != nil {
			return nil, storageError(err)
		}
		var r model.ChangeRecord
		if err := json.Unmarshal([]byte(record), &r); err != nil {
			return nil, fmt.Errorf("%w: change record: %w", store.ErrUnavailable, err)
		}
		r.At = fromMillis(at)
		records = append(records, r)
	}
	return records, storageError(rows.Err())
}

// assessmentsQuery reads the assessments a filter selects. A revision's,
// by its branch, tree, and base, are found through the key they're kept
// by, where every check read and decoded all its branch had ever recorded
// to keep its revision's (the SQL review's rescan). They're ordered by
// +at, which no index holds: by at, the planner read them through
// assessment_branch, every one the branch had, to spare itself sorting
// the revision's few.
func assessmentsQuery(repository model.RepositoryID, filter store.AssessmentFilter) (string, []any) {
	query, args := "SELECT branch_id, tree, base, port, directory, comparison, policy, build, at FROM assessments WHERE repository_id=?", []any{repository}
	if filter.Branch != "" {
		query += " AND branch_id=?"
		args = append(args, filter.Branch)
	}
	if filter.Tree != "" {
		query += " AND tree=?"
		args = append(args, filter.Tree)
	}
	if filter.Base != "" {
		query += " AND base=?"
		args = append(args, filter.Base)
	}
	return query + " ORDER BY +at DESC, rowid DESC", args
}

func (t *tx) Assessments(filter store.AssessmentFilter) ([]model.Assessment, error) {
	query, args := assessmentsQuery(t.repo, filter)
	rows, err := t.conn.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var assessments []model.Assessment
	for rows.Next() {
		var a model.Assessment
		var comparison string
		var at int64
		if err := rows.Scan(&a.Branch, &a.Tree, &a.Base, &a.Port, &a.Directory, &comparison, &a.Policy, &a.Build, &at); err != nil {
			return nil, storageError(err)
		}
		if err := json.Unmarshal([]byte(comparison), &a.Comparison); err != nil {
			return nil, fmt.Errorf("%w: assessment of %s: %w", store.ErrUnavailable, a.Port, err)
		}
		a.At = fromMillis(at)
		assessments = append(assessments, a)
	}
	return assessments, storageError(rows.Err())
}
