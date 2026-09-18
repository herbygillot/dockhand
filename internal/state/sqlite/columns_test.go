package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A table's statements come from its column list, and values travel by
// name: a value for a column the list does not have, or a column the
// values do not name, is refused rather than shifted into the wrong slot.
func TestTableStatementsAndValuesFollowTheColumnList(t *testing.T) {
	t.Parallel()
	fixture := table{name: "fixture", columns: []string{"id", "one", "two"}}
	require.Equal(t, "SELECT id,one,two FROM fixture WHERE repository_id=? AND id=?", fixture.selectByID())
	require.Equal(t, "INSERT INTO fixture(repository_id,id,one,two) VALUES(?,?,?,?)", fixture.insert())
	require.Equal(t, "UPDATE fixture SET two=?,one=? WHERE repository_id=? AND id=?", fixture.update("two", "one"))
	named := map[string]any{"id": "x", "one": 1, "two": 2}
	args, err := fixture.insertArgs("repo", named)
	require.NoError(t, err)
	require.Equal(t, []any{"repo", "x", 1, 2}, args)
	args, err = fixture.updateArgs([]string{"two", "one"}, named, "repo", "x")
	require.NoError(t, err)
	require.Equal(t, []any{2, 1, "repo", "x"}, args)
	_, err = fixture.insertArgs("repo", map[string]any{"id": "x", "one": 1})
	require.Error(t, err, "a missing value")
	_, err = fixture.insertArgs("repo", map[string]any{"id": "x", "one": 1, "two": 2, "three": 3})
	require.Error(t, err, "a value with no column")
	_, err = fixture.updateArgs([]string{"three"}, named, "repo", "x")
	require.Error(t, err, "a column the values do not name")
	for _, list := range []table{changes, pullRequests, jobs} {
		require.Equal(t, "id", list.columns[0], list.name)
	}
}
