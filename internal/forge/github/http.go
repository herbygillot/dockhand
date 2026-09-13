package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/forge"
)

const (
	defaultAPIURL   = "https://api.github.com"
	apiVersion      = "2026-03-10"
	acceptMediaType = "application/vnd.github+json"
	userAgent       = "dockhand/2"
)

type HTTPError struct {
	Method     string
	StatusCode int
	Resource   string
}

func (e *HTTPError) Error() string {
	method := e.Method
	if method == "" {
		method = http.MethodGet
	}
	return fmt.Sprintf("github: %s %s returned HTTP %d", method, e.Resource, e.StatusCode)
}

func (c *Client) getJSON(ctx context.Context, resource string, result any, limit int64) error {
	return c.requestJSON(ctx, http.MethodGet, resource, nil, result, limit)
}

func (c *Client) requestJSON(ctx context.Context, method, resource string, input, result any, limit int64) error {
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}

	base := c.Config.BaseURL
	if base == "" {
		base = defaultAPIURL
	}
	origin, err := url.Parse(base)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Scheme != "http" && origin.Scheme != "https" {
		return fmt.Errorf("github: invalid API base URL")
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+"/"+resource, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", acceptMediaType)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	request.Header.Set("User-Agent", userAgent)
	if c.Config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Config.Token)
	}
	client := http.DefaultClient
	if c.HTTP != nil {
		client = c.HTTP
	}
	configured := *client
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if method != http.MethodGet {
			return http.ErrUseLastResponse
		}
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
	if response.StatusCode != http.StatusOK && !(method == http.MethodPost && response.StatusCode == http.StatusCreated) {
		failure := &HTTPError{Method: method, StatusCode: response.StatusCode, Resource: resource}
		if method != http.MethodGet && (response.StatusCode == 400 || response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 || response.StatusCode == 409 || response.StatusCode == 422) {
			return fmt.Errorf("%w: %w", forge.ErrRejected, failure)
		}
		return failure
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("github: response exceeds size limit")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("github: invalid response: %w", err)
	}
	return nil
}
