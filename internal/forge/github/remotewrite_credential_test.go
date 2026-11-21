package github_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/remotewrite"
)

type renewingCredential struct{}

func (renewingCredential) Token(context.Context) (githubapi.Token, error) {
	return githubapi.Token{Secret: "old", Source: githubapi.SourceKeychain}, nil
}
func (renewingCredential) Renew(context.Context, githubapi.Token) (githubapi.Token, error) {
	return githubapi.Token{Secret: "new", Source: githubapi.SourceKeychain}, nil
}

type changingAccount struct {
	accounts []string
	reads    int
}

func (a *changingAccount) Current(context.Context) (githubapi.Identity, error) {
	index := min(a.reads, len(a.accounts)-1)
	a.reads++
	return githubapi.Identity{Account: a.accounts[index]}, nil
}

func TestReadyMutationAfterRenewalIsNotReplayed(t *testing.T) {
	var mutations int
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/graphql") {
			mutations++
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		body := `{"number":42,"node_id":"PR_node","state":"open","draft":true,"title":"title","html_url":"https://github.com/macports/macports-ports/pull/42","head":{"ref":"feature","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repo":{"full_name":"ada/macports-ports"}},"base":{"ref":"master","repo":{"full_name":"macports/macports-ports"}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	client := &githubapi.Client{HTTP: &http.Client{Transport: transport}, Config: githubapi.Config{BaseURL: "https://api.github.test/"}, Credentials: renewingCredential{}}
	forgeClient := &forgegithub.Client{Client: client}
	ctx := remotewrite.ForAccount(t.Context(), "ada", &changingAccount{accounts: []string{"ada", "bea"}})
	_, err := remotewrite.Ready(ctx, forgeClient, remotewrite.Made, forge.PullRequestRef{Forge: forge.GitHub, Repository: "macports/macports-ports", Number: 42})
	require.EqualError(t, err, "the account changed; run it again")
	require.Equal(t, 1, mutations, "the GraphQL mutation was not replayed with the renewed credential")
}
