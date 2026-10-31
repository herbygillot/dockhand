package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A GraphQL query is counted in GraphQL's points, as its rateLimit { cost }
// says, or one where it doesn't ask, and never as one of the hour's
// requests; its data and errors are its caller's.
func TestGraphQLCountsPointsNotRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Query string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		if strings.Contains(request.Query, "rateLimit") {
			fmt.Fprint(w, `{"data":{"rateLimit":{"cost":3},"viewer":{"login":"ada"}}}`)
			return
		}
		fmt.Fprint(w, `{"data":null,"errors":[{"type":"FORBIDDEN","path":["mutate"],"message":"no"}]}`)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Config: Config{BaseURL: server.URL, Token: "t"}}
	api, err := client.AuthenticatedAPI(t.Context())
	require.NoError(t, err)
	spend := StartSpend()
	var data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	errs, err := GraphQL(t.Context(), api, "query { rateLimit { cost } viewer { login } }", nil, &data)
	require.NoError(t, err)
	require.Empty(t, errs)
	require.Equal(t, "ada", data.Viewer.Login)
	errs, err = GraphQL(t.Context(), api, "mutation { mutate }", nil, nil)
	require.NoError(t, err)
	require.Equal(t, []GraphQLError{{Type: "FORBIDDEN", Path: []any{"mutate"}, Message: "no"}}, errs)
	require.EqualValues(t, 4, spend.Points())
	require.Zero(t, spend.Requests())
}
