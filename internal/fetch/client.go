package fetch

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// ResponseWait is how long a request waits for a response's headers once
// it's sent.
const ResponseWait = time.Minute

// BodyStall is how long a response's body may go without a byte before
// it's given up, as long as a download may (Stall).
const BodyStall = 3 * time.Minute

// Transport is how dockhand's requests reach a host: Go's default, which
// bounds connecting at 30 s and a TLS handshake at 10 s, and a response's
// headers at ResponseWait, which it doesn't. A host that accepted a
// request and never answered held outdated, and serve's daily look, as
// long as the command ran (the limits sweep, 2026-10-01). A body that
// stops after its headers is given up once BodyStall passes without a
// byte: GitHub's API, a job's log, PyPI, and the mirror's index were held
// by one as long as the command ran (batch 26).
var Transport http.RoundTripper = &stalling{next: newTransport(), bound: BodyStall}

// stalling gives up a response's body that goes bound without a byte.
type stalling struct {
	next  http.RoundTripper
	bound time.Duration
}

func (t *stalling) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil || response.Body == nil || response.Body == http.NoBody {
		return response, err
	}
	body := &stallingBody{body: response.Body, bound: t.bound}
	body.timer = time.AfterFunc(t.bound, func() {
		body.stalled.Store(true)
		_ = body.body.Close()
	})
	response.Body = body
	return response, nil
}

// stallingBody is a response's body, closed under its reader once bound
// passes without a byte, which ends a read waiting on it.
type stallingBody struct {
	body    io.ReadCloser
	bound   time.Duration
	timer   *time.Timer
	stalled atomic.Bool
}

func (b *stallingBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	switch {
	case b.stalled.Load():
		return n, fmt.Errorf("%w for %s after the response began, so dockhand gave up on it", ErrStalled, b.bound)
	case err != nil:
		b.timer.Stop()
	case n > 0:
		b.timer.Reset(b.bound)
	}
	return n, err
}

func (b *stallingBody) Close() error {
	b.timer.Stop()
	return b.body.Close()
}

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
