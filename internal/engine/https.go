package engine

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
)

// HTTPSProbe says whether a URL answers over HTTPS.
type HTTPSProbe interface {
	Answers(ctx context.Context, url string) bool
}

// PlainURL is a URL a port names over plain HTTP, its https form, and
// whether that answers.
type PlainURL struct {
	macports.PlainURL
	HTTPS   string
	Answers bool
}

// plainHTTP are a port's plain-HTTP URLs, each asked over HTTPS.
func (e *Engine) plainHTTP(ctx context.Context, info macports.PortInfo) []PlainURL {
	var plain []PlainURL
	for _, url := range info.PlainHTTP() {
		secure := "https://" + strings.TrimPrefix(url.URL, "http://")
		plain = append(plain, PlainURL{PlainURL: url, HTTPS: secure, Answers: e.httpsProbe().Answers(ctx, secure)})
	}
	return plain
}

func (e *Engine) httpsProbe() HTTPSProbe {
	if e.HTTPS != nil {
		return e.HTTPS
	}
	return requestProbe{}
}

// requestProbe asks a URL for its head, and failing that its first byte,
// as a server that refuses HEAD answers GET: an answer below 400, after
// redirects, within ten seconds, is one.
type requestProbe struct{}

func (requestProbe) Answers(ctx context.Context, url string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		request, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return false
		}
		request.Header.Set("Range", "bytes=0-0")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return false
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			return true
		}
	}
	return false
}
