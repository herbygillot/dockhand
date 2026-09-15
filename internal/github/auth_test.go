package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/forge"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type savedCredential struct {
	secret string
	err    error
}

func (s savedCredential) Get(context.Context, credential.Key) (string, error) { return s.secret, s.err }
func (savedCredential) Put(context.Context, credential.Key, string) error     { return nil }

func TestSystemCredentialsPreferEnvironmentAndReuseGitHubCLI(t *testing.T) {
	t.Run("GH_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "first")
		t.Setenv("GITHUB_TOKEN", "second")
		token, err := (github.SystemCredentials{}).Token(t.Context())
		require.NoError(t, err)
		require.Equal(t, "first", token.Secret)
	})
	t.Run("GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "second")
		token, err := (github.SystemCredentials{}).Token(t.Context())
		require.NoError(t, err)
		require.Equal(t, "second", token.Secret)
	})
	t.Run("gh", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		dir := t.TempDir()
		executable := filepath.Join(dir, "gh")
		require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\n[ \"$1 $2 $3 $4\" = \"auth token --hostname github.com\" ] || exit 2\nprintf fixture-from-gh\n"), 0700))
		t.Setenv("PATH", dir)
		token, err := (github.SystemCredentials{}).Token(t.Context())
		require.NoError(t, err)
		require.Equal(t, "fixture-from-gh", token.Secret)
	})
	t.Run("saved credential before gh", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		dir := t.TempDir()
		executable := filepath.Join(dir, "gh")
		require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nprintf credential-from-gh\n"), 0700))
		t.Setenv("PATH", dir)
		source := github.SystemCredentials{Store: savedCredential{secret: "credential-from-keychain"}, Key: credential.Key{Service: "fixture", Account: "github.com"}}
		token, err := source.Token(t.Context())
		require.NoError(t, err)
		require.Equal(t, "credential-from-keychain", token.Secret)
	})
	t.Run("missing", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("PATH", t.TempDir())
		_, err := (github.SystemCredentials{}).Token(t.Context())
		require.ErrorIs(t, err, github.ErrAuthentication)
		require.Contains(t, err.Error(), "GH_TOKEN")
	})
}

func TestAuthenticationChecksIdentityAndCachesOnlyTheCredential(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/user", r.URL.Path)
		assert.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"login":"fixture"}`)
	}))
	defer server.Close()
	sourceCalls := 0
	client := &github.Client{Config: github.Config{BaseURL: server.URL}, Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) {
		sourceCalls++
		return github.Token{Secret: "fixture-token"}, nil
	})}
	require.NoError(t, client.Authenticate(t.Context()))
	require.NoError(t, client.Authenticate(t.Context()))
	require.Equal(t, 1, sourceCalls)
	require.Equal(t, 2, requests)
}

func TestExplicitCredentialTakesPriority(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer explicit", r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"login":"fixture"}`)
	}))
	defer server.Close()
	client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "explicit"}, Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) {
		return github.Token{}, assert.AnError
	})}
	require.NoError(t, client.Authenticate(t.Context()))
}

func TestAuthenticationFailsBeforeWritesAndDoesNotLeakCredentials(t *testing.T) {
	t.Run("custom API requires explicit source", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
		defer server.Close()
		client := &github.Client{Config: github.Config{BaseURL: server.URL}}
		err := client.Authenticate(t.Context())
		require.ErrorIs(t, err, github.ErrAuthentication)
		require.Zero(t, requests)
		_, err = (&forgegithub.Client{Client: client}).Create(t.Context(), forge.PullRequestInput{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", Desired: record.PublicationContent{Title: "update"}})
		require.ErrorIs(t, err, github.ErrAuthentication)
		require.Zero(t, requests)
	})
	t.Run("rejected token", func(t *testing.T) {
		const token = "secret-never-print-this"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"message":%q}`, token)
		}))
		defer server.Close()
		client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: token}}
		err := client.Authenticate(t.Context())
		require.ErrorIs(t, err, github.ErrAuthentication)
		require.NotContains(t, err.Error(), token)
	})
	t.Run("transient API failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "fixture"}}
		err := client.Authenticate(t.Context())
		require.Error(t, err)
		require.NotErrorIs(t, err, github.ErrAuthentication)
	})
}

func TestRejectedCredentialsIdentifySourceWithoutFallback(t *testing.T) {
	for _, source := range []github.CredentialSource{github.SourceGHEnvironment, github.SourceGitHubEnvironment, github.SourceKeychain, github.SourceGitHubCLI} {
		t.Run(string(source), func(t *testing.T) {
			t.Setenv("GH_TOKEN", "")
			t.Setenv("GITHUB_TOKEN", "")
			const secret = "private-token"
			dir := t.TempDir()
			marker := filepath.Join(dir, "gh-called")
			t.Setenv("GH_MARKER", marker)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf called > \"$GH_MARKER\"\nprintf private-token\n"), 0700))
			t.Setenv("PATH", dir)
			saved := savedCredential{secret: secret}
			switch source {
			case github.SourceGHEnvironment, github.SourceGitHubEnvironment:
				t.Setenv(string(source), secret)
			case github.SourceGitHubCLI:
				saved.err = credential.ErrNotFound
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, "Bearer "+secret, r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprintf(w, `{"message":%q}`, secret)
			}))
			defer server.Close()
			client := &github.Client{Config: github.Config{BaseURL: server.URL}, Credentials: github.SystemCredentials{Store: saved}}
			for i := 0; i < 2; i++ {
				err := client.Authenticate(t.Context())
				require.ErrorIs(t, err, forge.ErrAuthentication)
				require.Contains(t, err.Error(), string(source))
				require.Contains(t, err.Error(), "no alternative credential was tried")
				require.NotContains(t, err.Error(), secret)
			}
			require.Equal(t, source, client.CredentialSource())
			require.Equal(t, 2, requests)
			if source == github.SourceKeychain {
				require.NoFileExists(t, marker)
			}
			_, err := (&forgegithub.Client{Client: client}).Create(t.Context(), forge.PullRequestInput{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main", Desired: record.PublicationContent{Title: "update"}})
			require.ErrorIs(t, err, forge.ErrRejected)
			require.ErrorIs(t, err, forge.ErrAuthentication)
			require.NotContains(t, err.Error(), secret)
		})
	}
}

func TestPublicReadsResolveCredentialsWithoutPriorAuthentication(t *testing.T) {
	for _, mode := range []string{"saved", "missing", "locked", "malformed", "rejected", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				want := "Bearer fixture-token"
				if mode == "missing" {
					want = ""
				}
				assert.Equal(t, want, r.Header.Get("Authorization"))
				if mode == "rejected" {
					w.WriteHeader(401)
					return
				}
				fmt.Fprint(w, `[]`)
			}))
			defer server.Close()
			client := &github.Client{Config: github.Config{BaseURL: server.URL}, Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) {
				switch mode {
				case "missing":
					return github.Token{}, github.ErrNoCredentials
				case "locked":
					return github.Token{}, fmt.Errorf("keychain locked")
				case "malformed":
					return github.Token{Secret: "bad token"}, nil
				case "canceled":
					return github.Token{}, context.Canceled
				}
				return github.Token{Secret: "fixture-token", Source: github.SourceKeychain}, nil
			})}
			_, err := (&forgegithub.Client{Client: client}).Find(t.Context(), forge.PullRequestQuery{Repository: "upstream/ports", HeadRepository: "author/ports", HeadBranch: "candidate", BaseBranch: "main"})
			switch mode {
			case "saved", "missing":
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			case "rejected":
				require.ErrorIs(t, err, github.ErrAuthentication)
				require.Equal(t, 1, calls)
			default:
				require.Error(t, err)
				require.Zero(t, calls)
			}
		})
	}
}
