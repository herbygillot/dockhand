package fetch

import (
	"net/http"
	"time"
)

// ResponseWait is how long a request waits for a response's headers once
// it's sent.
const ResponseWait = time.Minute

// Transport is how dockhand's requests reach a host: Go's default, which
// bounds connecting at 30 s and a TLS handshake at 10 s, and a response's
// headers at ResponseWait, which it doesn't. A host that accepted a
// request and never answered held outdated, and serve's daily look, as
// long as the command ran (the limits sweep, 2026-10-01). A body is the
// caller's to bound, as a download's two minutes are.
var Transport http.RoundTripper = newTransport()

func newTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = ResponseWait
	return transport
}

// Client is the HTTP client dockhand's requests go through, on Transport.
var Client = &http.Client{Transport: Transport}

// DownloadClient is Client for a download, which a Stall bounds, the wait
// for its response included: Transport's minute would cut that wait short
// of a download's own bound.
var DownloadClient = &http.Client{Transport: downloadTransport()}

func downloadTransport() *http.Transport {
	transport := newTransport()
	transport.ResponseHeaderTimeout = 0
	return transport
}
