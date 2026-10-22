package cmdtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
)

type memoryStore map[credential.Key]string

func (m memoryStore) Get(_ context.Context, key credential.Key) (string, error) {
	value, ok := m[key]
	if !ok {
		return "", credential.ErrNotFound
	}
	return value, nil
}

func (m memoryStore) Put(_ context.Context, key credential.Key, value string) error {
	m[key] = value
	return nil
}

func (m memoryStore) Delete(_ context.Context, key credential.Key) error {
	if _, ok := m[key]; !ok {
		return credential.ErrNotFound
	}
	delete(m, key)
	return nil
}

func WithAuth(t *testing.T, flow credential.DeviceFlow) memoryStore {
	t.Helper()
	store := memoryStore{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"login": "ada"})
	}))
	t.Cleanup(server.Close)
	Hosted(t).AuthFlow, Hosted(t).AuthStore = flow, store
	Hosted(t).AuthAPI = func(signin github.Signin) *github.Client {
		return &github.Client{HTTP: server.Client(), Config: github.Config{BaseURL: server.URL}, Credentials: github.SystemCredentials{Store: signin.Store, Key: github.CredentialKey, Override: signin.Token, CLI: signin.CLI}}
	}
	Setenv(t, "GH_TOKEN", "")
	Setenv(t, "GITHUB_TOKEN", "")
	Setenv(t, "PATH", t.TempDir())
	return store
}
