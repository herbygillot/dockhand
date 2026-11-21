package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type swappingCredentials struct{}

func (swappingCredentials) Token(context.Context) (Token, error) {
	return Token{Secret: "first", Source: SourceKeychain}, nil
}
func (swappingCredentials) Renew(context.Context, Token) (Token, error) {
	return Token{Secret: "second", Source: SourceKeychain}, nil
}

func TestRenewedCredentialReplaysReadButHandsWriteBackToItsCaller(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		write        bool
	}{
		{name: "GraphQL read POST", method: http.MethodPost},
		{name: "mutation", method: http.MethodPost, write: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var auth []string
			next := roundTrip(func(req *http.Request) (*http.Response, error) {
				auth = append(auth, req.Header.Get("Authorization"))
				status := http.StatusUnauthorized
				if req.Header.Get("Authorization") == "Bearer second" {
					status = http.StatusOK
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			})
			transport := credentialTransport{next: next, credentials: &credentials{source: swappingCredentials{}}}
			req, _ := http.NewRequest(tc.method, "https://example.test/", nil)
			if tc.write {
				req = req.WithContext(AsWrite(req.Context()))
			}
			response, err := transport.RoundTrip(req)
			if !tc.write {
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Equal(t, []string{"Bearer first", "Bearer second"}, auth)
				response.Body.Close()
			} else {
				require.ErrorIs(t, err, ErrCredentialRenewed)
				require.Nil(t, response)
				require.Equal(t, []string{"Bearer first"}, auth)
			}
		})
	}
}
