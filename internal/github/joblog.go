package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/herbygillot/dockhand/internal/fetch"
)

// JobLog fetches GitHub's job-log link without API credentials. The caller
// supplies its own size limit and keeps its own log processing and errors.
func JobLog(ctx context.Context, client *http.Client, link *url.URL, limit int64) (*http.Response, error) {
	if link == nil || link.Scheme != "https" || link.Host == "" || link.User != nil {
		return nil, fmt.Errorf("github: invalid job log download URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	return fetch.Open(client, request, limit)
}
