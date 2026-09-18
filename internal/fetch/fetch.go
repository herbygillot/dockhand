package fetch

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

var errTooLarge = errors.New("fetch: response exceeds size limit")

// StatusError reports an unsuccessful response with the URL that produced it.
type StatusError struct {
	Status int
	URL    string
}

func (e *StatusError) Error() string { return fmt.Sprintf("fetch: HTTP %d for %s", e.Status, e.URL) }

// Open requires a successful HTTP response and bounds bytes read from its body.
// The caller closes the body and decides content validation, hashing, and storage.
// UserAgent identifies dockhand's own HTTP requests. Some listings, MetaCPAN
// among them, refuse a client library's default agent.
const UserAgent = "dockhand/2"

func Open(client *http.Client, request *http.Request, limit int64) (*http.Response, error) {
	if request == nil || request.URL == nil || limit <= 0 {
		return nil, fmt.Errorf("fetch: request and positive size limit required")
	}
	if request.URL.User != nil || request.URL.Host == "" || request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return nil, fmt.Errorf("fetch: unsupported URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	configured := *client
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || req.URL.Scheme != "http" && req.URL.Scheme != "https" || len(via) >= 10 {
			return fmt.Errorf("fetch: unsupported redirect")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("fetch: redirect downgraded HTTPS")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	response, err := configured.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, &StatusError{Status: response.StatusCode, URL: response.Request.URL.Redacted()}
	}
	if response.ContentLength > limit {
		response.Body.Close()
		return nil, errTooLarge
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: limit}
	return response, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n > 0 {
			return 0, errTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
