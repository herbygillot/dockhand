package engine

import (
	"context"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
)

// HTTPSProbe says whether a URL answers over HTTPS. It is asked about
// several URLs at once.
type HTTPSProbe interface {
	Answers(ctx context.Context, url string) bool
}

// httpsAsks is how many of a port's URLs are asked over HTTPS at once: a
// port names a few, and a long list of mirrors waits on four hosts at a
// time, not on each in turn.
const httpsAsks = 4

// PlainURL is a URL a port names over plain HTTP, its https form, and
// whether that answers.
type PlainURL struct {
	macports.PlainURL
	HTTPS   string
	Answers bool
}

// plainHTTP are a port's plain-HTTP URLs, each asked over HTTPS, in the
// order the port names them. They're asked a few at once (httpsAsks), so
// a port waits about as long as its slowest host, up to ten seconds, not
// the sum of them; PlainHTTP names each URL once, so each is asked once.
func (e *Engine) plainHTTP(ctx context.Context, info macports.PortInfo) []PlainURL {
	urls := info.PlainHTTP()
	if len(urls) == 0 {
		return nil
	}
	probe := e.httpsProbe()
	plain := make([]PlainURL, len(urls))
	var asks errgroup.Group
	asks.SetLimit(httpsAsks)
	for i, url := range urls {
		plain[i] = PlainURL{PlainURL: url, HTTPS: "https://" + strings.TrimPrefix(url.URL, "http://")}
		asks.Go(func() error {
			plain[i].Answers = probe.Answers(ctx, plain[i].HTTPS)
			return nil
		})
	}
	_ = asks.Wait()
	return plain
}

func (e *Engine) httpsProbe() HTTPSProbe {
	if e.HTTPS != nil {
		return e.HTTPS
	}
	return requestProbe{}
}

// requestProbe asks a URL as fetch asks one (fetch.Ask), within ten
// seconds: a redirect back to plain HTTP is no answer over HTTPS, as
// fetching refuses it (the helper-ownership review's finding 2).
type requestProbe struct{ client *http.Client }

func (p requestProbe) Answers(ctx context.Context, url string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return fetch.Ask(ctx, p.client, url).Answered
}
