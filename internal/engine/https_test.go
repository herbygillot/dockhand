package engine

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type roundTrips func(*http.Request) (*http.Response, error)

func (f roundTrips) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The probe create and update ask with doesn't count an HTTPS URL that
// redirects to plain HTTP as answering over HTTPS: fetching refuses that
// downgrade, and so does the probe now (the helper-ownership review's
// finding 2, its probe as a regression test).
func TestTheHTTPSProbeRejectsADowngrade(t *testing.T) {
	probe := requestProbe{client: &http.Client{Transport: roundTrips(func(r *http.Request) (*http.Response, error) {
		status, header := http.StatusOK, http.Header{}
		if r.URL.Scheme == "https" && r.URL.Path == "/" {
			status = http.StatusFound
			header.Set("Location", "http://example.invalid/")
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	require.False(t, probe.Answers(t.Context(), "https://example.invalid/"))
	require.True(t, probe.Answers(t.Context(), "https://example.invalid/secure"))
}
