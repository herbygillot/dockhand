package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestDeviceFlowUsesOAuthPollingAndValidatesTheAuthenticatedUser(t *testing.T) {
	presented := false
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device/code":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "fixture-client", r.Form.Get("client_id"))
			assert.Equal(t, "public_repo", r.Form.Get("scope"))
			fmt.Fprint(w, `{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":10,"interval":1}`)
		case "/access_token":
			tokenRequests++
			assert.True(t, presented)
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "fixture-client", r.Form.Get("client_id"))
			assert.Equal(t, "device-secret", r.Form.Get("device_code"))
			assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
			fmt.Fprint(w, `{"access_token":"oauth-secret","token_type":"bearer","scope":"public_repo"}`)
		case "/user":
			assert.Equal(t, "Bearer oauth-secret", r.Header.Get("Authorization"))
			fmt.Fprint(w, `{"login":"fixture-user"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{DeviceAuthURL: server.URL + "/device/code", TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}, APIBaseURL: server.URL}
	value, err := flow.Authorize(ctx, "fixture-client", func(authorization credential.DeviceAuthorization) error {
		presented = true
		require.Equal(t, "ABCD-EFGH", authorization.UserCode)
		require.Equal(t, "https://github.com/login/device", authorization.VerificationURL)
		require.False(t, authorization.ExpiresAt.IsZero())
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, credential.Value{Secret: "oauth-secret", Account: "fixture-user"}, value)
	require.Equal(t, 1, tokenRequests)
}

func TestDeviceFlowStopsWhenThePromptCannotBePresented(t *testing.T) {
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/device/code" {
			fmt.Fprint(w, `{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":10,"interval":1}`)
			return
		}
		tokenRequests++
	}))
	defer server.Close()
	flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{DeviceAuthURL: server.URL + "/device/code", TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}}
	_, err := flow.Authorize(t.Context(), "fixture-client", func(credential.DeviceAuthorization) error { return assert.AnError })
	require.ErrorIs(t, err, assert.AnError)
	require.Zero(t, tokenRequests)
	_, err = flow.Authorize(t.Context(), "", func(credential.DeviceAuthorization) error { return nil })
	require.Error(t, err)
}
