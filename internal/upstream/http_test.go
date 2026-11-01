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
	t.Parallel()
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
		require.True(t, result.Release.NoUpdate)
		require.NoError(t, service.Check(t.Context(), port, *result.Release), "an already-current archive release is coherent; preparation reports no update")
	}
}

func TestHTTPDiscoveryRejectsIncompleteOrUnsupportedObservations(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"empty", "malformed", "HTTP failure", "not found", "custom hook", "curl option", "unmatched version", "invalid capture", "ambiguous"} {
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
			case "HTTP failure":
				status = 503
			case "not found":
				status = 404
			case "custom hook":
				port.Options["dockhand.livecheck_standard"] = "0"
			case "curl option":
				port.Options["livecheck.curloptions"] = "--user secret"
			case "unmatched version":
				// livecheck.version is the source spelling; one no literal
				// maps to fails when the candidate is evaluated.
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
			if scenario == "unmatched version" {
				service.EvaluateVersion = func(_ context.Context, value string) (string, error) {
					return "", fmt.Errorf("no version input can select %s", value)
				}
			}
			result, err := service.DiscoverPort(t.Context(), port)
			require.Error(t, err)
			require.Equal(t, upstream.Unknown, result.Assessment)
			require.Nil(t, result.Release)
			if scenario == "custom hook" || scenario == "curl option" {
				require.Zero(t, calls)
			}
			if scenario == "not found" {
				// tao's livecheck reads a GitHub release's download folder,
				// as port livecheck does, and both find nothing there
				// (field testing's tao update, 2026-10-28).
				require.ErrorContains(t, err, "'s livecheck reads "+server.URL+", which answered 404, as port livecheck finds too, so the newest release can't be read from it; name the version: dockhand update ")
			}
		})
	}
}

// The perl5 PortGroup checks CPAN with a multiline regex against the module
// version and derives the port version from it: discovery compares the
// listing against that spelling, records the evaluated port version, and
// keeps the spelling beside it for the edit.
func TestHTTPDiscoveryFollowsADerivedVersionSpelling(t *testing.T) {
	t.Parallel()
	page := "{\n  \"name\" : \"App-cpanminus-1.7050\",\n  \"name\" : \"App-cpanminus-1.7049\"\n}"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, page) }))
	defer server.Close()
	service := automaticService(t, &catalog{})
	service.HTTP = server.Client()
	convert := map[string]string{"1.7049": "1.704.900", "1.7050": "1.705.0"}
	service.EvaluateVersion = func(_ context.Context, value string) (string, error) {
		if converted, ok := convert[value]; ok {
			return converted, nil
		}
		return "", fmt.Errorf("no version input can select %s", value)
	}
	port := macports.PortInfo{Name: "p5.34-app-cpanminus", Version: "1.704.900", Options: map[string]string{
		"livecheck.type": "regexm", "livecheck.url": server.URL, "livecheck.regex": `{"name"} : {"App\-cpanminus-([^"]+?)"}`, "livecheck.version": "1.7049",
		"livecheck.ignore_sslcert": "no", "livecheck.compression": "yes", "livecheck.curloptions": "", "dockhand.livecheck_standard": "1",
	}}
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment)
	require.Equal(t, "1.705.0", result.Release.Version, "the release is the evaluated port version")
	require.Equal(t, "1.7050", result.Release.SourceVersion, "the module version is what the input is edited to")
	require.Equal(t, "1.7050", result.Release.SourceSpelling())
	require.Equal(t, "1.704.900", result.Release.CurrentVersion)
	require.NoError(t, service.Check(t.Context(), port, *result.Release))
	explicit, err := service.Resolve(t.Context(), port, "1.7050")
	require.NoError(t, err)
	require.Equal(t, "1.705.0", explicit.Version)
	require.Equal(t, "1.7050", explicit.SourceVersion)
	require.NoError(t, service.Check(t.Context(), port, explicit))
	page = "{ \"name\" : \"App-cpanminus-1.7049\" }"
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.Current, result.Assessment)
	require.Equal(t, "1.704.900", result.Release.Version, "already current keeps the port version")
	require.Equal(t, "1.7049", result.Release.SourceVersion, "beside the spelling the listing was compared against")
	require.NoError(t, service.Check(t.Context(), port, *result.Release))
}

// A candidate newer only by a component written with a leading zero the
// port's version hasn't is uncertain: aften's listing kept 0.07, an older
// scheme than 0.0.8 that compares newer, and outdated said it was ready to
// ship (field testing's outdated-ports session).
func TestAnOlderSchemesCandidateIsUncertain(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `>aften-0.0.8< >aften-0.07<`)
	}))
	defer server.Close()
	service := automaticService(t, &catalog{})
	service.HTTP = server.Client()
	port := macports.PortInfo{Name: "aften", Version: "0.0.8", Options: map[string]string{
		"livecheck.type": "regex", "livecheck.url": server.URL, "livecheck.regex": `{>aften-([0-9.]+)<}`, "livecheck.version": "0.0.8",
		"livecheck.ignore_sslcert": "no", "livecheck.compression": "yes", "livecheck.curloptions": "", "dockhand.livecheck_standard": "1",
	}}
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.Uncertain, result.Assessment, result.Detail)
	require.Equal(t, "0.07", result.CandidateVersion)
	require.Equal(t, "0.07 may be an older scheme than 0.0.8", result.SetAside[0].Why)
	_, err = service.Resolve(t.Context(), port, "")
	require.ErrorContains(t, err, "can't tell whether aften is current: 0.07 may be an older scheme than 0.0.8; name the version to update to")
}

// A version a livecheck captured from the start of a prerelease's name is
// that prerelease: ap-utils's regex stopped before "pre1" and took 1.4.2
// from 1.4.2pre1, its newest stable 1.4.1, so a port on a stable release
// passes it over, and one on a prerelease keeps the name it was read from
// (field testing, the outdated-ports session's note 19).
func TestAVersionCapturedFromAPrereleasesNameIsOne(t *testing.T) {
	t.Parallel()
	page := `<link>https://sourceforge.net/projects/ap-utils/files/ap-utils/1.4.2pre1/ap-utils-1.4.2pre1.tar.bz2</link> <link>https://sourceforge.net/projects/ap-utils/files/ap-utils/1.4.1/ap-utils-1.4.1.tar.bz2</link>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	}))
	defer server.Close()
	service := automaticService(t, &catalog{})
	service.HTTP = server.Client()
	port := listingPort(server.URL)
	port.Name, port.Version = "ap-utils", "1.4.1"
	port.Options["livecheck.regex"], port.Options["livecheck.version"] = `{/ap-utils-([0-9]+(?:\.[0-9]+)*)}`, "1.4.1"
	result, err := service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.Current, result.Assessment, result.Detail)

	port.Version, port.Options["livecheck.version"] = "1.4.1rc1", "1.4.1rc1"
	service.EvaluateVersion = func(_ context.Context, version string) (string, error) { return version, nil }
	result, err = service.DiscoverPort(t.Context(), port)
	require.NoError(t, err)
	require.Equal(t, upstream.UpdateAvailable, result.Assessment, result.Detail)
	require.Equal(t, "1.4.2", result.Release.Version)
	require.Equal(t, "1.4.2pre1", result.Release.CapturedFrom)
	require.Equal(t, "1.4.2 was read from 1.4.2pre1, a prerelease's name, by the port's livecheck", result.Release.CapturedWords())
}

// A listing whose only match is a prerelease, which a port on a stable
// release doesn't take, reads the port current and says why, where it
// read "nothing upstream lists matches": php-igbinary's PECL page lists
// 3.2.17RC1 first (prioritization's, 2026-11-01). The same holds of a
// version read from a prerelease's name.
func TestAListingOfPrereleasesAloneReadsCurrent(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ page, newest string }{
		{`>terraform_1.16.3-rc1<`, "1.16.3-rc1"},
		{`>terraform_1.16.3rc1.tar.gz<`, "1.16.3rc1"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, c.page) }))
		port := listingPort(server.URL)
		if c.newest == "1.16.3rc1" {
			port.Options["livecheck.regex"] = `{terraform_([0-9.]+[0-9])}`
		}
		service := automaticService(t, &catalog{})
		service.HTTP = server.Client()
		result, err := service.DiscoverPort(t.Context(), port)
		server.Close()
		require.NoError(t, err, c.page)
		require.Equal(t, upstream.Current, result.Assessment, result.Detail)
		require.Equal(t, "upstream's newest, "+c.newest+", is a prerelease, which a port on a stable release doesn't take", result.Detail)
		require.True(t, result.Release.NoUpdate)
	}
}
