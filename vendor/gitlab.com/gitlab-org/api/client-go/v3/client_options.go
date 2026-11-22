//
// Copyright 2021, Sander van Harmelen
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package gitlab

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

// ClientOptionFunc can be used to customize a new GitLab API client.
type ClientOptionFunc func(*Client) error

// WithBaseURL sets the base URL for API requests to a custom endpoint.
func WithBaseURL(urlStr string) ClientOptionFunc {
	return func(c *Client) error {
		return c.setBaseURL(urlStr)
	}
}

// WithAuthSourceStrategy set strategy for authSource inject auth header
func WithAuthSourceStrategy(strategy AuthTokenStrategy) ClientOptionFunc {
	return func(c *Client) error {
		c.authSourceStrategy = strategy
		return nil
	}
}

// WithCustomBackoff can be used to configure a custom backoff policy.
func WithCustomBackoff(backoff retryablehttp.Backoff) ClientOptionFunc {
	return func(c *Client) error {
		c.client.Backoff = backoff
		return nil
	}
}

// WithCustomLeveledLogger can be used to configure a custom retryablehttp
// leveled logger.
func WithCustomLeveledLogger(leveledLogger retryablehttp.LeveledLogger) ClientOptionFunc {
	return func(c *Client) error {
		c.client.Logger = leveledLogger
		return nil
	}
}

// WithCustomLimiter injects a custom rate limiter to the client.
func WithCustomLimiter(limiter RateLimiter) ClientOptionFunc {
	return func(c *Client) error {
		c.configureLimiterOnce.Do(func() {})
		c.limiter = limiter
		return nil
	}
}

// WithCustomLogger can be used to configure a custom retryablehttp logger.
func WithCustomLogger(logger retryablehttp.Logger) ClientOptionFunc {
	return func(c *Client) error {
		c.client.Logger = logger
		return nil
	}
}

// WithCustomRetry can be used to configure a custom retry policy.
func WithCustomRetry(checkRetry retryablehttp.CheckRetry) ClientOptionFunc {
	return func(c *Client) error {
		c.client.CheckRetry = checkRetry
		return nil
	}
}

// WithCustomRetryMax can be used to configure a custom maximum number of retries.
func WithCustomRetryMax(retryMax int) ClientOptionFunc {
	return func(c *Client) error {
		c.client.RetryMax = retryMax
		return nil
	}
}

// WithCustomRetryWaitMinMax can be used to configure a custom minimum and
// maximum time to wait between retries.
func WithCustomRetryWaitMinMax(waitMin, waitMax time.Duration) ClientOptionFunc {
	return func(c *Client) error {
		c.client.RetryWaitMin = waitMin
		c.client.RetryWaitMax = waitMax
		return nil
	}
}

// WithDetailedNotFoundErrors makes a 404 Not Found response return a
// per-request *ErrorResponse, as every other error status does, instead of
// the shared ErrNotFound. The error then names the method and URL of the
// request, and carries the response and its body.
//
// The error still matches errors.Is(err, ErrNotFound) and
// HasStatusCode(err, http.StatusNotFound), but no longer err == ErrNotFound,
// so switch such comparisons to errors.Is before enabling this option.
//
// This is expected to become the default behavior in 4.0.
func WithDetailedNotFoundErrors() ClientOptionFunc {
	return func(c *Client) error {
		c.detailedNotFoundErrors = true
		return nil
	}
}

// WithErrorHandler can be used to configure a custom error handler.
func WithErrorHandler(handler retryablehttp.ErrorHandler) ClientOptionFunc {
	return func(c *Client) error {
		c.client.ErrorHandler = handler
		return nil
	}
}

// WithHTTPClient can be used to configure a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOptionFunc {
	return func(c *Client) error {
		c.client.HTTPClient = httpClient
		return nil
	}
}

// WithRequestLogHook can be used to configure a custom request log hook.
func WithRequestLogHook(hook retryablehttp.RequestLogHook) ClientOptionFunc {
	return func(c *Client) error {
		c.client.RequestLogHook = hook
		return nil
	}
}

// WithResponseLogHook can be used to configure a custom response log hook.
func WithResponseLogHook(hook retryablehttp.ResponseLogHook) ClientOptionFunc {
	return func(c *Client) error {
		c.client.ResponseLogHook = hook
		return nil
	}
}

// WithRetryAfterBackoff makes the client wait for the time the Retry-After
// header of a 429 response gives before it retries, when the response has no
// Ratelimit-Reset header. GitLab's application rate limits answer that way,
// with the period of the limit: a minute for most of them, and up to a day
// for some. Without this option such a 429 is retried with the exponential
// backoff, and the caller gets it back within a few seconds once its retries
// are spent. Ratelimit-Reset takes precedence over Retry-After either way.
//
// A wait that would end after the deadline of the request's context takes
// the exponential backoff instead, so a caller whose deadline is too short
// for the wait still gets the 429 back rather than the context error.
//
// Waiting for Retry-After is planned to become the default behavior in
// client-go v5.0, with a call-out in v4.0.
func WithRetryAfterBackoff() ClientOptionFunc {
	return func(c *Client) error {
		c.retryAfterBackoff = true
		return nil
	}
}

// WithRetryAfterMax stops retrying a 429 whose Retry-After exceeds maxWait,
// unless it has a Ratelimit-Reset header. GitLab's application rate limits
// send the limit's period, up to a day. The cap is per retry, so the total
// wait can reach maxWait times the retry count. An unparsable Retry-After
// is ignored.
//
// It is checked by the default retry policy, so WithCustomRetry and
// WithRequestRetry bypass it.
func WithRetryAfterMax(maxWait time.Duration) ClientOptionFunc {
	return func(c *Client) error {
		if maxWait < 0 {
			return fmt.Errorf("retry after max must not be negative, got %s", maxWait)
		}
		c.retryAfterMax = maxWait
		c.hasRetryAfterMax = true
		return nil
	}
}

// WithoutRetries disables the default retry logic.
func WithoutRetries() ClientOptionFunc {
	return func(c *Client) error {
		c.disableRetries = true
		return nil
	}
}

// WithOnlyIdempotentRetries restricts automatic retries to requests using an
// idempotent HTTP method: GET, HEAD, OPTIONS, TRACE, PUT, DELETE or QUERY
// (RFC 9110 §9.2.2, plus QUERY from RFC 10008). Notably, a 5xx response to a
// POST is then not retried, as the server may have already applied its side
// effect, e.g. creating a duplicate comment. A 429 response is still retried
// for any method, as a rate-limited request is rejected before it is
// processed.
//
// This is expected to become the default behavior in the next major release.
func WithOnlyIdempotentRetries() ClientOptionFunc {
	return func(c *Client) error {
		c.onlyIdempotentRetries = true
		return nil
	}
}

// WithNilOptionsOmitted makes the client treat an options argument that holds
// a nil pointer as no options at all, the way it treats an untyped nil. By
// default a nil *SomeOptions is not equal to nil once held in an any, so a
// POST, PUT or PATCH request carries the JSON literal null as its body, and a
// request with any other method has a query already on the URL given to
// NewRequestToURL replaced with an empty one. Methods that delegate to their
// WithOptions sibling with a nil options pointer, such as Jobs.CancelJob and
// DraftNotes.PublishAllDraftNotes, send null that way.
//
// This is expected to become the default behavior in 4.0, where this option
// will have no effect, and the option to be removed in 5.0.
func WithNilOptionsOmitted() ClientOptionFunc {
	return func(c *Client) error {
		c.omitNilOptions = true
		return nil
	}
}

// WithRequestOptions can be used to configure default request options applied to every request.
func WithRequestOptions(options ...RequestOptionFunc) ClientOptionFunc {
	return func(c *Client) error {
		c.defaultRequestOptions = append(c.defaultRequestOptions, options...)
		return nil
	}
}

// WithUserAgent can be used to configure a custom user agent.
func WithUserAgent(userAgent string) ClientOptionFunc {
	return func(c *Client) error {
		c.UserAgent = userAgent
		return nil
	}
}

// WithURLWarningLogger sets a custom logger for URL validation warnings.
// By default, warnings are logged using slog.Default().
// Pass slog.New(slog.DiscardHandler) to disable warnings.
func WithURLWarningLogger(logger *slog.Logger) ClientOptionFunc {
	return func(c *Client) error {
		if logger == nil {
			return errors.New("logger cannot be nil, use slog.New(slog.DiscardHandler) to discard warnings")
		}
		c.urlWarningLogger = logger
		return nil
	}
}

// WithCookieJar can be used to configure a cookie jar.
func WithCookieJar(jar http.CookieJar) ClientOptionFunc {
	return func(c *Client) error {
		c.jar = jar
		return nil
	}
}

// WithInterceptor registers an Interceptor in the client's http request call pipeline.
// It returns a ClientOptionFunc that adds the interceptor to the client.
func WithInterceptor(i Interceptor) ClientOptionFunc {
	return func(c *Client) error {
		if i == nil {
			return errors.New("interceptor cannot be nil")
		}
		c.interceptors = append(c.interceptors, i)
		return nil
	}
}
