package github

import (
	"fmt"
	"net/http"
)

// Leave redirect handling to the HTTP client, but do not replay writes or send
// API credentials outside the original origin.
type redirectTransport struct{ next http.RoundTripper }

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if previous := req.Response; previous != nil {
		if previous.Request.Method != http.MethodGet {
			return nil, fmt.Errorf("github: refusing to redirect an API write")
		}
		origin := previous.Request.URL
		if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host || req.URL.User != nil {
			return nil, fmt.Errorf("github: redirect left configured API origin")
		}
	}
	return t.next.RoundTrip(req)
}
