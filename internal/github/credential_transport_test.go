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
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
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
			req, _ := http.NewRequest(method, "https://example.test/", nil)
			response, err := transport.RoundTrip(req)
			if method == http.MethodGet {
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
