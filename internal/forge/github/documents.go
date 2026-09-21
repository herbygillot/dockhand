package github

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	githubapi "github.com/herbygillot/dockhand/internal/github"
)

const apiHost = "api.github.com"

// Document fetches a livecheck URL on the GitHub API through the client, so
// the user's credentials and the rate-limit handling apply, sending the
// caller's headers, which are Base's curl fetch's, in place of the SDK's
// user agent, Accept, and API version. The API lays its JSON out by those
// headers, and the maintainers' expressions are written against the
// indented form MacPorts receives; the SDK's own headers get the compact
// form. A URL off the API is not served. The body is bounded like a plain
// listing.
func (c *Client) Document(ctx context.Context, address string, headers http.Header) ([]byte, bool, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host != apiHost || parsed.User != nil {
		return nil, false, nil
	}
	api, err := c.API(ctx)
	if err != nil {
		return nil, true, githubapi.RateLimitError(err)
	}
	relative := strings.TrimPrefix(parsed.Path, "/")
	if parsed.RawQuery != "" {
		relative += "?" + parsed.RawQuery
	}
	request, err := api.NewRequest(ctx, http.MethodGet, relative, nil)
	if err != nil {
		return nil, true, err
	}
	request.Header.Del("X-GitHub-Api-Version")
	for key, values := range headers {
		request.Header[key] = values
	}
	var body bytes.Buffer
	if _, err := api.Do(request, &boundedWriter{Writer: &body, remaining: 16 << 20}); err != nil {
		return nil, true, githubapi.RateLimitError(err)
	}
	return body.Bytes(), true, nil
}

type boundedWriter struct {
	io.Writer
	remaining int
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("github: document exceeds the 16 MiB listing limit")
	}
	w.remaining -= len(p)
	return w.Writer.Write(p)
}
