package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Every ask the change request keeps has a row in the flag table, so a
// field added without one fails, and every field a row names is one of the
// request's (#195, "The flag check").
func TestEveryAskTheRequestKeepsHasARow(t *testing.T) {
	t.Parallel()
	asks := map[string]bool{}
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, prefix string) {
		for i := range typ.NumField() {
			field := typ.Field(i)
			path := strings.TrimPrefix(prefix+"."+field.Name, ".")
			switch {
			case strings.HasPrefix(field.Type.Name(), "Flag["):
				asks[path] = true
			case field.Type.Kind() == reflect.Struct && field.Type.PkgPath() == reflect.TypeFor[model.ChangeRequest]().PkgPath():
				walk(field.Type, path)
			}
		}
	}
	walk(reflect.TypeFor[model.ChangeRequest](), "")
	asks["Targets[].Named"] = true
	named := map[string]bool{}
	for _, row := range FlagTable {
		for _, field := range row.FlagFields() {
			named[field] = true
			require.True(t, asks[field], "%v names %s, which the request doesn't keep", row.Flags, field)
		}
	}
	for ask := range asks {
		require.True(t, named[ask], "the request keeps %s, which no row of FlagTable names", ask)
	}
}

// The flags page is made from the flag table and never edited by hand: it
// and the table say the same, and DOCKHAND_WRITE_FLAGS_PAGE=1 writes it.
// It waits in testdata until #195's step 5 makes it true of the binary,
// and moves to docs/usage then.
func TestTheFlagsPageIsTheFlagTable(t *testing.T) {
	t.Parallel()
	page := FlagsPage()
	path := filepath.Join("testdata", "flags.md")
	if os.Getenv("DOCKHAND_WRITE_FLAGS_PAGE") == "1" {
		require.NoError(t, os.WriteFile(path, []byte(page), 0o644))
		return
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, page, string(data), "testdata/flags.md is the table's; write it with DOCKHAND_WRITE_FLAGS_PAGE=1 go test ./internal/engine -run TestTheFlagsPageIsTheFlagTable")
}
