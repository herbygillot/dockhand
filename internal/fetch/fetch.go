// Package fetch performs bounded HTTP transfers using caller-owned requests and clients.
package fetch

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrTooLarge = errors.New("fetch: response exceeds size limit")

// Open requires a successful HTTP response and bounds bytes read from its body.
// The caller closes the body and decides content validation, hashing, and storage.
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
		return nil, fmt.Errorf("fetch: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		response.Body.Close()
		return nil, ErrTooLarge
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
			return 0, ErrTooLarge
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
