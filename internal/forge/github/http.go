package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

type HTTPError struct {
	StatusCode int
	Resource   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github: GET %s returned HTTP %d", e.Resource, e.StatusCode)
}

func (c *Client) get(ctx context.Context, resource string, result any) error {
	err := c.getJSON(ctx, resource, result, 1<<20)
	var failure *HTTPError
	if errors.As(err, &failure) && failure.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", upstream.ErrTagMissing, resource)
	}
	return err
}

func (c *Client) getJSON(ctx context.Context, resource string, result any, limit int64) error {
	base := c.Config.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	origin, err := url.Parse(base)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Scheme != "http" && origin.Scheme != "https" {
		return fmt.Errorf("github: invalid API base URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+resource, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "dockhand/2")
	if c.Config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Config.Token)
	}
	client := http.DefaultClient
	if c.HTTP != nil {
		client = c.HTTP
	}
	configured := *client
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host || req.URL.User != nil {
			return fmt.Errorf("github: redirect left configured API origin")
		}
		if len(via) >= 5 {
			return fmt.Errorf("github: too many redirects")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	response, err := configured.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: response.StatusCode, Resource: resource}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("github: response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("github: invalid response: %w", err)
	}
	return nil
}
