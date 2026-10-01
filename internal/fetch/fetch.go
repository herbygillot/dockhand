package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
)

// ErrTooLarge reports a body that exceeds the caller's size limit, whether the
// server declared the size or the limit was reached while reading.
var ErrTooLarge = errors.New("fetch: response exceeds size limit")

// StatusError reports an unsuccessful response with the URL that produced it.
// Requested is the URL asked for when a redirect led somewhere else, and
// Reason is the first line of a plain-text or JSON body, which is where a
// server usually says why it refused.
type StatusError struct {
	Status    int
	URL       string
	Requested string `json:",omitempty"`
	Reason    string `json:",omitempty"`
}

func (e *StatusError) Error() string {
	text := fmt.Sprintf("fetch: HTTP %d for %s", e.Status, e.URL)
	if e.Requested != "" {
		text += ", redirected from " + e.Requested
	}
	if e.Reason != "" {
		text += ": " + e.Reason
	}
	return text
}

// reasonFrom is the first line of a short plain-text or JSON error body. An
// HTML page is left out; its text is markup around a generic message.
func reasonFrom(response *http.Response) string {
	kind := strings.ToLower(response.Header.Get("Content-Type"))
	if !strings.HasPrefix(kind, "text/plain") && !strings.HasPrefix(kind, "application/json") {
		return ""
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	line = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, line)
	if len(line) > 200 {
		line = line[:200]
	}
	return strings.TrimSpace(line)
}

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
	response, err := redirecting(client).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		failure := &StatusError{Status: response.StatusCode, URL: response.Request.URL.Redacted(), Reason: reasonFrom(response)}
		response.Body.Close()
		if requested := request.URL.Redacted(); requested != failure.URL {
			failure.Requested = requested
		}
		return nil, failure
	}
	if response.ContentLength > limit {
		response.Body.Close()
		return nil, ErrTooLarge
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: limit}
	return response, nil
}

// redirecting is the client as Open and Ask use it: following at most ten
// redirects, to http and https alone, never from HTTPS to plain HTTP, and
// then as the client's own rule says.
func redirecting(client *http.Client) *http.Client {
	if client == nil {
		client = Client
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
	return &configured
}

// Answer is what asking a URL found: whether it answered, below 400, and
// the URL that did after redirects; Err is why it didn't.
type Answer struct {
	Answered bool
	Final    string
	Err      error
}

// Ask asks a URL for its head, and, where that isn't answered, as some
// servers refuse HEAD, its first byte, following redirects as Open does:
// one from HTTPS to plain HTTP is no answer. Unlike Open's, its success is
// any status below 400, since a probe needs no body.
func Ask(ctx context.Context, client *http.Client, url string) Answer {
	var answer Answer
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		request, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return Answer{Err: err}
		}
		if request.URL.User != nil || request.URL.Host == "" || request.URL.Scheme != "http" && request.URL.Scheme != "https" {
			return Answer{Err: fmt.Errorf("fetch: unsupported URL")}
		}
		request.Header.Set("User-Agent", UserAgent)
		request.Header.Set("Range", "bytes=0-0")
		response, err := redirecting(client).Do(request)
		if err != nil {
			return Answer{Err: err}
		}
		response.Body.Close()
		answer = Answer{Final: response.Request.URL.Redacted()}
		if response.StatusCode < 400 {
			answer.Answered = true
			return answer
		}
		answer.Err = &StatusError{Status: response.StatusCode, URL: answer.Final}
	}
	return answer
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

// Transient reports a failure another try may not meet: the network's, a
// timeout, or a server that was busy or failing (429, or 500 and above). A
// refusal, as a 404 is, says what the server has, and isn't one. Of
// failures joined, as upstream's and a mirror's are, any one that is makes
// it one.
func Transient(err error) bool {
	switch failure := err.(type) {
	case nil:
		return false
	case *StatusError:
		return failure.Status == http.StatusTooManyRequests || failure.Status >= http.StatusInternalServerError
	case net.Error:
		// A timeout is one: context.DeadlineExceeded is a net.Error too.
		return true
	case interface{ Unwrap() []error }:
		return slices.ContainsFunc(failure.Unwrap(), Transient)
	}
	return Transient(errors.Unwrap(err))
}
