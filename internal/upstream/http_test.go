package upstream_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/stretchr/testify/require"
)

func listingPort(address string) macports.PortInfo {
	return macports.PortInfo{Name: "terraform-1.16", Version: "1.16.0", Options: map[string]string{
		"livecheck.type": "regex", "livecheck.url": address, "livecheck.regex": `{>terraform_(1\.16\.[^<]+)<}`, "livecheck.version": "1.16.0",
		"livecheck.ignore_sslcert": "no", "livecheck.compression": "yes", "livecheck.curloptions": `--append-http-header {Accept: text/html}`, "dockhand.livecheck_standard": "1",
	}}
}

func TestHTTPDiscoveryUsesNativeFilterAndFreezesRelease(t *testing.T) {
	page := `>terraform_1.16.2< >terraform_1.17.0< >terraform_1.16.10< >terraform_1.16.10< >terraform_1.16.11-rc1<`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "text/html", r.Header.Get("Accept"))
		w.Header().Set("ETag", `"listing-1"`)
		fmt.Fprint(w, page)
	}))
	defer server.Close()
	service := automaticService(t, &catalog{})
	service.HTTP = server.Client()
	port := listingPort(server.URL)
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment)
	require.Equal(t, "1.16.10", result.Release.Version)
	require.Empty(t, result.Release.Forge)
	require.True(t, result.Release.Archive)
	require.Equal(t, `"listing-1"`, result.Release.Listing.ETag)
	require.Len(t, result.Release.Listing.SHA256, 64)
	page = `>terraform_1.16.12<`
	require.NoError(t, service.Check(t.Context(), port, *result.Release))
	require.Equal(t, 1, calls, "checking frozen discovery must not choose a new release")
	explicit, err := service.Resolve(t.Context(), port, "1.16.10")
	require.NoError(t, err)
	require.Equal(t, result.Release.Version, explicit.Version)
	for _, version := range []string{"1.16.12", "1.16.13"} {
		port.Version = version
		port.Options["livecheck.version"] = version
		service.EvaluateVersion = func(context.Context, string) (string, error) {
			t.Fatal("already-current should not probe an old candidate")
			return "", nil
		}
		result, err = service.DiscoverPort(t.Context(), port)
		require.NoError(t, err)
		require.Equal(t, upstream.Current, result.Assessment)
	}
}

func TestHTTPDiscoveryRejectsIncompleteOrUnsupportedObservations(t *testing.T) {
	for _, scenario := range []string{"empty", "malformed", "prerelease", "HTTP failure", "custom hook", "curl option", "unmatched version", "invalid capture", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			page := `>terraform_1.16.2<`
			status := 200
			calls := 0
			port := listingPort("")
			switch scenario {
			case "empty":
				page = "nothing"
			case "malformed":
				port.Options["livecheck.regex"] = `{(}`
			case "prerelease":
				page = `>terraform_1.16.2-rc1<`
			case "HTTP failure":
				status = 503
			case "custom hook":
				port.Options["dockhand.livecheck_standard"] = "0"
			case "curl option":
				port.Options["livecheck.curloptions"] = "--user secret"
			case "unmatched version":
				port.Options["livecheck.version"] = "0.1"
			case "invalid capture":
				port.Options["livecheck.regex"] = "terraform"
			case "ambiguous":
				page = `>terraform_1.16.2< >terraform_1.16.02<`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(status); fmt.Fprint(w, page) }))
			defer server.Close()
			port.Options["livecheck.url"] = server.URL
			service := automaticService(t, &catalog{})
			service.HTTP = server.Client()
			result, err := service.DiscoverPort(t.Context(), port)
			require.Error(t, err)
			require.Equal(t, upstream.Unknown, result.Assessment)
			require.Nil(t, result.Release)
			if scenario == "custom hook" || scenario == "curl option" || scenario == "unmatched version" {
				require.Zero(t, calls)
			}
		})
	}
}
