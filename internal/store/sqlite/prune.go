package sqlite

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// The store's pruning: what cleanup forgets of archives and history, its
// keep-sets, and the deletes in foreign-key order, apart from the records
// they prune (the SQL study's split, 2026-10-13).

// unnamedArchive selects the archives kept before a time that no live
// result names: neither reuse's newest builds nor an open branch's newest
// results.
// reusableBuilds are, for each target in each environment, its newest
// passed build that recorded what it read and kept an archive, from the
// build itself rather than a reuse of it: the one result reuse may still
// choose, which keeps its archive and its record (D6). Its columns are
// the result's execution, target, and archive.
const reusableBuilds = "SELECT execution_id, target_id, archive FROM (SELECT r.execution_id, r.target_id, r.archive, ROW_NUMBER() OVER (" +
	"PARTITION BY r.target_id, e.provider, e.platform_os, e.platform_version, e.platform_architecture, e.developer_tools, e.prefix " +
	"ORDER BY r.recorded_at DESC, r.rowid DESC) AS newest FROM results r JOIN executions e ON e.repository_id=r.repository_id AND e.id=r.execution_id " +
	"WHERE r.repository_id=? AND r.outcome='passed' AND r.inputs<>'' AND r.reused_from='' AND r.archive<>'') WHERE newest=1"

// openBranchArchives are the archives an open branch's newest passed
// result of each target in each environment names, a reuse's included:
// what its evidence may name.
const openBranchArchives = "SELECT archive FROM (SELECT r.archive, ROW_NUMBER() OVER (" +
	"PARTITION BY u.branch_id, r.target_id, e.provider, e.platform_os, e.platform_version, e.platform_architecture, e.developer_tools, e.prefix " +
	"ORDER BY r.recorded_at DESC, r.rowid DESC) AS newest FROM results r JOIN executions e ON e.repository_id=r.repository_id AND e.id=r.execution_id " +
	"JOIN runs u ON u.repository_id=e.repository_id AND u.id=e.run_id JOIN branches b ON b.repository_id=u.repository_id AND b.id=u.branch_id " +
	"WHERE r.repository_id=? AND r.outcome='passed' AND r.archive<>'' AND b.state='open') WHERE newest=1"

const unnamedArchive = "a.repository_id=? AND a.kept_at<? AND a.digest NOT IN (SELECT archive FROM (" + reusableBuilds + ")) " +
	"AND a.digest NOT IN (" + openBranchArchives + ") AND a.digest NOT IN (" + newestDependencies + ")"

// newestDependencies are the archives a guest installed each port from, in
// each environment, the newest of each, kept since a time: a dependency's
// archive is named by no result, so each day's cleanup forgot it once it
// was an hour old, and rust, built from source where MacPorts had no
// archive for the release, was built again the next day (the rc9 full
// stage). It lasts until cleanup.after, as a result's record does.
const newestDependencies = "SELECT digest FROM (SELECT digest, kept_at, ROW_NUMBER() OVER (" +
	"PARTITION BY port, provider, platform_os, platform_version, platform_architecture, developer_tools ORDER BY kept_at DESC, rowid DESC) AS newest " +
	"FROM dependency_archives WHERE repository_id=?) WHERE newest=1 AND kept_at>=?"

// PruneArchives forgets the unnamed archives and reads them back in the one
// statement, which returns them in no order of its own.
func (t *tx) PruneArchives(before, dependenciesSince time.Time) ([]model.Archive, error) {
	if err := t.write(); err != nil {
		return nil, err
	}
	pruned, err := t.archives("DELETE FROM archives AS a WHERE "+unnamedArchive+" RETURNING digest, name, size, kept_at", t.repo, millis(before), t.repo, t.repo, t.repo, millis(dependenciesSince))
	slices.SortFunc(pruned, func(a, b model.Archive) int { return strings.Compare(a.Digest, b.Digest) })
	return pruned, err
}

func (t *tx) archives(query string, args ...any) ([]model.Archive, error) {
	rows, err := t.conn.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	var archives []model.Archive
	for rows.Next() {
		var archive model.Archive
		var kept int64
		if err := rows.Scan(&archive.Digest, &archive.Name, &archive.Size, &kept); err != nil {
			return nil, storageError(err)
		}
		archive.KeptAt = fromMillis(kept)
		archives = append(archives, archive)
	}
	return archives, storageError(rows.Err())
}

// PruneHistory removes what up to limit branches that ended before a
// time recorded of their checks (D6), the branches taken in ID order past
// after, so a large backlog commits a chunk at a time and no write holds
// the lock past the busy timeout other writers wait for (the SQL study's
// SQL-S2). It says the last branch it took, which the next call passes as
// after, or "" once none is left; that call also removes the inputs no
// remaining result names, which no one branch's do.
func (t *tx) PruneHistory(before time.Time, after model.BranchID, limit int) (store.Pruned, model.BranchID, error) {
	var pruned store.Pruned
	if err := t.write(); err != nil {
		return pruned, "", err
	}
	rows, err := t.conn.QueryContext(t.ctx, "SELECT id FROM branches WHERE repository_id=? AND state<>'open' AND ended_at IS NOT NULL AND ended_at<? AND id>? ORDER BY id LIMIT ?",
		t.repo, millis(before), string(after), limit)
	if err != nil {
		return pruned, "", storageError(err)
	}
	var ended []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return pruned, "", storageError(err)
		}
		ended = append(ended, id)
	}
	if err := rows.Close(); err != nil {
		return pruned, "", storageError(err)
	}
	if len(ended) > 0 {
		if pruned, err = t.pruneBranchHistory(ended); err != nil {
			return pruned, "", err
		}
	}
	if len(ended) == limit {
		return pruned, model.BranchID(ended[len(ended)-1]), nil
	}
	result, err := t.conn.ExecContext(t.ctx, pruneInputs, t.repo, t.repo)
	if err != nil {
		return pruned, "", storageError(err)
	}
	n, err := result.RowsAffected()
	pruned.Inputs = int(n)
	return pruned, "", storageError(err)
}

// pruneInputs removes the inputs records no result names.
const pruneInputs = "DELETE FROM inputs WHERE repository_id=? AND key NOT IN (SELECT inputs FROM results WHERE repository_id=? AND inputs<>'')"

// pruneBranchHistory removes what the branches recorded of their checks,
// in the order foreign keys allow: the results of their checks but those
// reuse may still choose (reusableBuilds); the executions left with no
// result, but one a remaining result reused; the runs left with no
// execution, but each branch's newest, which status shows; the plans and
// revisions no remaining run needs; and the branches' assessments and
// change records. It's one delete over a set of branches, which forgetting
// an archived branch is to reuse for one.
//
// Each statement reaches its rows through an index (schema 31): a
// correlated subquery that scans made it quadratic in history, and it
// passed its timeout at a year's scale (the SQL study's SQL-S1). The keep
// set is computed once, not probed per result.
func (t *tx) pruneBranchHistory(branches []string) (store.Pruned, error) {
	var pruned store.Pruned
	steps := pruneBranchSteps(len(branches))
	counts := []*int{&pruned.Results, &pruned.Executions, &pruned.Runs, &pruned.Plans, &pruned.Revisions, &pruned.Assessments, &pruned.Changes}
	for i, step := range steps {
		result, err := t.conn.ExecContext(t.ctx, step.query, step.args(t.repo, branches)...)
		if err != nil {
			return pruned, storageError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return pruned, storageError(err)
		}
		*counts[i] = int(n)
	}
	return pruned, nil
}

// pruneStep is one of pruneBranchHistory's statements, over n branches,
// with how its arguments are laid out: the repository at each repo
// position, and the branches at the set.
type pruneStep struct {
	query string
	// layout is the statement's arguments in order: 'r' the repository,
	// 'b' the branch set.
	layout string
}

func (s pruneStep) args(repo model.RepositoryID, branches []string) []any {
	var args []any
	for _, c := range s.layout {
		if c == 'r' {
			args = append(args, repo)
			continue
		}
		for _, b := range branches {
			args = append(args, b)
		}
	}
	return args
}

// pruneBranchSteps are pruneBranchHistory's statements over n branches,
// in the order it runs them.
func pruneBranchSteps(n int) []pruneStep {
	set := "(" + placeholders(n) + ")"
	runs := "SELECT id FROM runs WHERE repository_id=? AND branch_id IN " + set
	return []pruneStep{
		{"WITH keep AS MATERIALIZED (SELECT execution_id, target_id FROM (" + reusableBuilds + ")) " +
			"DELETE FROM results WHERE repository_id=? AND execution_id IN (SELECT id FROM executions WHERE repository_id=? AND run_id IN (" + runs + ")) " +
			"AND (execution_id, target_id) NOT IN (SELECT execution_id, target_id FROM keep)", "rrrrb"},
		{"DELETE FROM executions AS e WHERE e.repository_id=? AND e.run_id IN (" + runs + ") " +
			"AND NOT EXISTS (SELECT 1 FROM results r WHERE r.repository_id=e.repository_id AND r.execution_id=e.id) " +
			"AND NOT EXISTS (SELECT 1 FROM results r WHERE r.repository_id=e.repository_id AND r.reused_from=e.id)", "rrb"},
		{"DELETE FROM runs AS u WHERE u.repository_id=? AND u.branch_id IN " + set + " " +
			"AND u.number<(SELECT max(v.number) FROM runs v WHERE v.repository_id=u.repository_id AND v.branch_id=u.branch_id) " +
			"AND NOT EXISTS (SELECT 1 FROM executions e WHERE e.repository_id=u.repository_id AND e.run_id=u.id)", "rb"},
		{"DELETE FROM plans AS p WHERE p.repository_id=? AND p.revision_id IN (SELECT id FROM revisions WHERE repository_id=? AND branch_id IN " + set + ") " +
			"AND NOT EXISTS (SELECT 1 FROM runs u WHERE u.repository_id=p.repository_id AND u.plan_id=p.id)", "rrb"},
		{"DELETE FROM revisions AS v WHERE v.repository_id=? AND v.branch_id IN " + set + " " +
			"AND NOT EXISTS (SELECT 1 FROM runs u WHERE u.repository_id=v.repository_id AND u.revision_id=v.id) " +
			"AND NOT EXISTS (SELECT 1 FROM plans p WHERE p.repository_id=v.repository_id AND p.revision_id=v.id)", "rb"},
		{"DELETE FROM assessments WHERE repository_id=? AND branch_id IN " + set, "rb"},
		{"DELETE FROM change_records WHERE repository_id=? AND branch_id IN " + set, "rb"},
	}
}

// PruneAssessments removes the assessments and change records of the trees
// an open branch has moved past: each one of a tree its newest revision doesn't have, where
// that revision was recorded before a time (D6). Nothing reads them again;
// a branch restored to such a tree is assessed again.
func (t *tx) PruneAssessments(before time.Time) (int, error) {
	if err := t.write(); err != nil {
		return 0, err
	}
	total := 0
	for _, table := range []string{"assessments", "change_records"} {
		result, err := t.conn.ExecContext(t.ctx, "DELETE FROM "+table+" AS a WHERE a.repository_id=? AND a.branch_id IN (SELECT id FROM branches WHERE repository_id=? AND state='open') "+
			"AND EXISTS (SELECT 1 FROM revisions v WHERE v.repository_id=a.repository_id AND v.branch_id=a.branch_id AND v.created_at<? AND v.tree_id<>a.tree "+
			"AND v.created_at=(SELECT max(w.created_at) FROM revisions w WHERE w.repository_id=v.repository_id AND w.branch_id=v.branch_id))",
			t.repo, t.repo, millis(before))
		if err != nil {
			return total, storageError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return total, storageError(err)
		}
		total += int(n)
	}
	return total, nil
}

// ForgetBranch forgets an ended branch (clean --delete): its events,
// edits, decisions, and checkpoints go, and what it recorded of its
// checks goes as history does (pruneBranchHistory), its latest check too,
// keeping only what reuse may still choose or another result names; its
// record stays, marked forgotten at, which no read of branches returns,
// and its name and pull request are free.
func (t *tx) ForgetBranch(id model.BranchID, at time.Time) (store.Pruned, error) {
	if err := t.write(); err != nil {
		return store.Pruned{}, err
	}
	branch, err := t.Branch(id)
	if err != nil {
		return store.Pruned{}, err
	}
	if branch.State == model.BranchOpen || !branch.ForgottenAt.IsZero() {
		return store.Pruned{}, fmt.Errorf("%w: only an ended branch is forgotten, and once", store.ErrConflict)
	}
	for _, table := range []string{"events", "edits", "decisions", "checkpoints"} {
		if _, err := t.conn.ExecContext(t.ctx, "DELETE FROM "+table+" WHERE repository_id=? AND branch_id=?", t.repo, id); err != nil {
			return store.Pruned{}, storageError(err)
		}
	}
	pruned, err := t.pruneBranchHistory([]string{string(id)})
	if err != nil {
		return pruned, err
	}
	// History keeps a branch's latest check, which a forgotten branch
	// doesn't need; then the plans and revisions it held go too.
	steps := pruneBranchSteps(1)
	latest := pruneStep{"DELETE FROM runs AS u WHERE u.repository_id=? AND u.branch_id=? " +
		"AND NOT EXISTS (SELECT 1 FROM executions e WHERE e.repository_id=u.repository_id AND e.run_id=u.id)", "rb"}
	counts := []*int{&pruned.Runs, &pruned.Plans, &pruned.Revisions}
	for i, step := range []pruneStep{latest, steps[3], steps[4]} {
		result, err := t.conn.ExecContext(t.ctx, step.query, step.args(t.repo, []string{string(id)})...)
		if err != nil {
			return pruned, storageError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return pruned, storageError(err)
		}
		*counts[i] += int(n)
	}
	return pruned, t.update("branch "+string(id), "UPDATE branches SET forgotten_at=? WHERE repository_id=? AND id=?", millis(at), t.repo, id)
}
