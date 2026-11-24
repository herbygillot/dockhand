package engine

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// askRow is one ask of the flag table: how to give it, a value to keep
// first, and another, false or empty where the ask takes one, given after.
type askRow struct {
	ask         string
	give        func(*model.ChangeRequest, any)
	kept, given any
}

func boolAsk(field func(*model.ChangeRequest) *model.Flag[bool]) func(*model.ChangeRequest, any) {
	return func(r *model.ChangeRequest, v any) { *field(r) = model.Set(v.(bool)) }
}

func stringAsk(field func(*model.ChangeRequest) *model.Flag[string]) func(*model.ChangeRequest, any) {
	return func(r *model.ChangeRequest, v any) { *field(r) = model.Set(v.(string)) }
}

func listAsk(field func(*model.ChangeRequest) *model.Flag[[]string]) func(*model.ChangeRequest, any) {
	return func(r *model.ChangeRequest, v any) { *field(r) = model.Set(v.([]string)) }
}

// askRows are every ask the request keeps, E3's four (--draft, --no-check,
// --type, --skip-notification) among them by name.
var askRows = []askRow{
	{"Shared", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Shared }), true, false},
	{"Source.Source", stringAsk(func(r *model.ChangeRequest) *model.Flag[string] { return &r.Source.Source }), "releases", "keep"},
	{"Source.KeepHTTP", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Source.KeepHTTP }), true, false},
	{"Source.KeepOldChecksums", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Source.KeepOldChecksums }), true, false},
	{"Source.KeepModeline", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Source.KeepModeline }), true, false},
	{"Source.FollowMoves", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Source.FollowMoves }), true, false},
	{"Actions.WithObsolete", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Actions.WithObsolete }), true, false},
	{"Actions.RevbumpDependents", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Actions.RevbumpDependents }), true, false},
	{"Actions.Except", listAsk(func(r *model.ChangeRequest) *model.Flag[[]string] { return &r.Actions.Except }), []string{"gojq"}, []string{}},
	{"Actions.Closes", listAsk(func(r *model.ChangeRequest) *model.Flag[[]string] { return &r.Actions.Closes }), []string{"71234"}, []string{}},
	{"To", stringAsk(func(r *model.ChangeRequest) *model.Flag[string] { return &r.To }), "check", "commit"},
	{"Check.On", listAsk(func(r *model.ChangeRequest) *model.Flag[[]string] { return &r.Check.On }), []string{"sequoia"}, []string{"command"}},
	{"Check.Build", stringAsk(func(r *model.ChangeRequest) *model.Flag[string] { return &r.Check.Build }), "minmax", "host"},
	{"Check.Tests", stringAsk(func(r *model.ChangeRequest) *model.Flag[string] { return &r.Check.Tests }), "required", "skip"},
	{"Publish.Draft", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Publish.Draft }), true, false},
	{"Publish.NoCheck", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Publish.NoCheck }), true, false},
	{"Publish.Type", listAsk(func(r *model.ChangeRequest) *model.Flag[[]string] { return &r.Publish.Type }), []string{"bugfix"}, []string{}},
	{"Publish.SkipNotification", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Publish.SkipNotification }), true, false},
	{"Publish.TestedBinaries", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Publish.TestedBinaries }), true, false},
	{"Publish.TestedVariants", boolAsk(func(r *model.ChangeRequest) *model.Flag[bool] { return &r.Publish.TestedVariants }), true, false},
}

// askOf is an ask's Flag in a request, read by its path.
func askOf(request model.ChangeRequest, ask string) reflect.Value {
	value := reflect.ValueOf(request)
	for _, part := range strings.Split(ask, ".") {
		value = value.FieldByName(part)
	}
	return value
}

// Each row of the flag table behaves alike: an ask kept is left by a later
// command that leaves its flag out, replaced by one that gives it, false
// or empty included, and survives the store opened again (#195, "The flag
// check").
func TestEveryAskIsKeptLeftOutAndReplaced(t *testing.T) {
	t.Parallel()
	asked := map[string]bool{}
	for _, row := range askRows {
		asked[row.ask] = true
	}
	for _, row := range FlagTable {
		for _, field := range row.FlagFields() {
			if field != "Targets[].Named" {
				require.True(t, asked[field], "%s has no row here", field)
			}
		}
	}
	f := setup(t)
	e, _ := f.withPreparer(t)
	for _, row := range askRows {
		branch, err := e.Start(t.Context(), StartRequest{Name: "jq-" + strings.ToLower(strings.ReplaceAll(row.ask, ".", "-"))})
		require.NoError(t, err)
		var first model.ChangeRequest
		row.give(&first, row.kept)
		if row.ask == "Actions.Except" {
			first.Actions.RevbumpDependents = model.Set(true)
		}
		_, branch, err = e.keepRequest(t.Context(), branch, Asking{Origin: "ship", Ports: []string{"jq"}, Asked: first})
		require.NoError(t, err, row.ask)

		_, branch, err = e.keepRequest(t.Context(), branch, Asking{Origin: "ship"})
		require.NoError(t, err, row.ask)
		require.Equal(t, fmt.Sprint(row.kept), fmt.Sprint(askOf(*branch.Request, row.ask).FieldByName("Value").Interface()), "%s left out keeps the kept value", row.ask)

		var then model.ChangeRequest
		row.give(&then, row.given)
		if row.ask == "Actions.Except" {
			then.Actions.RevbumpDependents = model.Set(true)
		}
		_, branch, err = e.keepRequest(t.Context(), branch, Asking{Origin: "ship", Asked: then})
		require.NoError(t, err, row.ask)
		replaced := askOf(*branch.Request, row.ask)
		require.True(t, replaced.FieldByName("Given").Bool(), "%s given", row.ask)
		require.Equal(t, fmt.Sprint(row.given), fmt.Sprint(replaced.FieldByName("Value").Interface()), "%s given replaces it, false or empty too", row.ask)

		reopened, err := f.open(t).Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		again := askOf(*reopened.Request, row.ask)
		require.True(t, again.FieldByName("Given").Bool(), "%s survives the store opened again", row.ask)
		require.Equal(t, fmt.Sprint(row.given), fmt.Sprint(again.FieldByName("Value").Interface()), "%s survives the store opened again", row.ask)
	}
}
