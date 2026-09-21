package sqlite

import (
	"fmt"
	"strings"
)

// table names a record's columns once. The statements that read and write
// the record are built from the list, and values and scan targets travel by
// column name, so a column is spelled in the list and at its use and never
// in a statement or a positional argument list. Adding a column is a list
// entry, a migration, and a named value.
type table struct {
	name    string
	columns []string
}

func (t table) selectByID() string {
	return "SELECT " + strings.Join(t.columns, ",") + " FROM " + t.name + " WHERE repository_id=? AND id=?"
}

func (t table) insert() string {
	return "INSERT INTO " + t.name + "(repository_id," + strings.Join(t.columns, ",") + ") VALUES(?" + strings.Repeat(",?", len(t.columns)) + ")"
}

// update sets the named columns of one row.
func (t table) update(columns ...string) string {
	assignments := make([]string, len(columns))
	for i, column := range columns {
		assignments[i] = column + "=?"
	}
	return "UPDATE " + t.name + " SET " + strings.Join(assignments, ",") + " WHERE repository_id=? AND id=?"
}

// ordered arranges named values in the order of the given columns, refusing
// a column the values do not name or a value the columns do not.
func (t table) ordered(columns []string, named map[string]any) ([]any, error) {
	if len(named) != len(columns) {
		return nil, fmt.Errorf("sqlite: %s: %d values for %d columns", t.name, len(named), len(columns))
	}
	values := make([]any, 0, len(columns))
	for _, column := range columns {
		value, ok := named[column]
		if !ok {
			return nil, fmt.Errorf("sqlite: %s: no value for column %s", t.name, column)
		}
		values = append(values, value)
	}
	return values, nil
}

// insertArgs is the repository followed by every column's value.
func (t table) insertArgs(repo any, named map[string]any) ([]any, error) {
	values, err := t.ordered(t.columns, named)
	if err != nil {
		return nil, err
	}
	return append([]any{repo}, values...), nil
}

// updateArgs is the named columns' values followed by the repository and ID.
func (t table) updateArgs(columns []string, named map[string]any, repo, id any) ([]any, error) {
	subset := make(map[string]any, len(columns))
	for _, column := range columns {
		value, ok := named[column]
		if !ok {
			return nil, fmt.Errorf("sqlite: %s: no value for column %s", t.name, column)
		}
		subset[column] = value
	}
	values, err := t.ordered(columns, subset)
	if err != nil {
		return nil, err
	}
	return append(values, repo, id), nil
}

// scanArgs is every column's scan target in column order.
func (t table) scanArgs(named map[string]any) ([]any, error) {
	return t.ordered(t.columns, named)
}

var (
	changes      = table{name: "changes", columns: []string{"id", "branch", "current_revision", "disposition", "targets", "created_at", "published_revision", "pull_request_id", "generated_commit", "initiating_target", "cleanup", "keep_body"}}
	pullRequests = table{name: "pull_requests", columns: []string{"id", "change_id", "forge", "remote_repository", "number", "observation", "observe_after"}}
	jobs         = table{name: "jobs", columns: []string{"id", "request_id", "change_id", "spec_change_id", "input_revision", "result_revision", "source_id", "action", "phase", "destination", "verification", "options", "state", "accepted_at", "cancel_at", "admitted_at", "finished_at", "detail", "claim_owner", "claim_generation", "claim_until", "retry_at", "prepared", "resolved_release", "reused_attempt", "reuse_detail", "consecutive_failures", "consecutive_waits", "wait_kind"}}
)
