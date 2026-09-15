package github

import (
	"errors"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
)

func RateLimitError(err error) error {
	var known *forge.RateLimitError
	if errors.As(err, &known) {
		return err
	}
	var primary *gh.RateLimitError
	if errors.As(err, &primary) {
		return &forge.RateLimitError{RetryAt: primary.Rate.Reset.Time, Err: err}
	}
	var secondary *gh.AbuseRateLimitError
	if errors.As(err, &secondary) {
		delay := time.Minute
		if secondary.RetryAfter != nil {
			delay = *secondary.RetryAfter
		}
		return &forge.RateLimitError{RetryAt: time.Now().Add(delay), Err: err}
	}
	return err
}
