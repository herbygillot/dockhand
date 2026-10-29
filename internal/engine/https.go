package engine

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
)

// HTTPSProbe says whether a URL answers over HTTPS. It is asked about
// several URLs at once.
type HTTPSProbe interface {
	Answers(ctx context.Context, url string) bool
}

// httpsAsks is how many URLs are asked over HTTPS at once, each within
// httpsWait: a port names a few, and a long list of mirrors waits on
// eight hosts at a time, not on each in turn; a slow host keeps its link
// (the architecture thread, 2026-10-29).
const (
	httpsAsks = 8
	httpsWait = 5 * time.Second
)

// httpsKept is how long an answer stands within a run: serve's runs for
// days, and a host that answers now may not tomorrow.
const httpsKept = time.Hour

// PlainURL is a URL a port names over plain HTTP, its https form, and
// whether that answers.
type PlainURL struct {
	macports.PlainURL
	HTTPS   string
	Answers bool
}

// httpsCache is one run's answers of URLs over HTTPS, by URL, each asked
// once, eight at once: the engine's, which every update, create, bump,
// ship, serve, and submit in it shares (the architecture thread,
// 2026-10-29), where each command kept its own.
type httpsCache struct {
	mu    sync.Mutex
	asked map[string]*httpsAsk
	slots chan struct{}
}

// httpsAsk is one URL's ask, done once answered.
type httpsAsk struct {
	done    chan struct{}
	answers bool
	at      time.Time
}

// httpsAnswers is the engine's answers, as an edit asks them
// (portedit.HTTPSAnswers).
type httpsAnswers struct{ e *Engine }

// Ask starts asking a URL over HTTPS, where it hasn't been, returning at
// once.
func (h httpsAnswers) Ask(_ context.Context, url string) { h.e.askHTTPS(url) }

// Answers waits for a URL's answer, false where ctx ends first.
func (h httpsAnswers) Answers(ctx context.Context, url string) bool {
	ask := h.e.askHTTPS(url)
	select {
	case <-ask.done:
		return ask.answers
	case <-ctx.Done():
		return false
	}
}

// askHTTPS is a URL's ask, started where it hasn't been, or its answer has
// stood its time. It runs on its own, so whoever asked first can go on,
// and whoever waits later has it.
func (e *Engine) askHTTPS(url string) *httpsAsk {
	c := &e.https
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked == nil {
		c.asked = map[string]*httpsAsk{}
		c.slots = make(chan struct{}, httpsAsks)
	}
	if ask, ok := c.asked[url]; ok && (ask.at.IsZero() || time.Since(ask.at) < httpsKept) {
		return ask
	}
	ask := &httpsAsk{done: make(chan struct{})}
	c.asked[url] = ask
	probe := e.httpsProbe()
	go func() {
		c.slots <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), httpsWait)
		answers := probe.Answers(ctx, url)
		cancel()
		<-c.slots
		c.mu.Lock()
		ask.answers, ask.at = answers, time.Now()
		c.mu.Unlock()
		close(ask.done)
	}()
	return ask
}

// plainHTTP are a port's plain-HTTP URLs, each with whether its https form
// answers, asked as macports.PlainURL.Ask says, in the order the port names
// them: the run's answers, so a port waits about as long as its slowest
// host, up to httpsWait, and a URL asked once isn't asked again.
func (e *Engine) plainHTTP(ctx context.Context, info macports.PortInfo) []PlainURL {
	urls := info.PlainHTTP()
	answers := httpsAnswers{e}
	for _, url := range urls {
		if url.Ask != "" {
			answers.Ask(ctx, url.Ask)
		}
	}
	var plain []PlainURL
	for _, url := range urls {
		found := PlainURL{PlainURL: url, HTTPS: macports.HTTPS(url.URL)}
		if url.Ask != "" {
			found.Answers = answers.Answers(ctx, url.Ask)
		}
		plain = append(plain, found)
	}
	return plain
}

// switchedLinks are the links an edit switched to https, each of which
// answered there.
func switchedLinks(urls []macports.PlainURL) []PlainURL {
	var switched []PlainURL
	for _, url := range urls {
		switched = append(switched, PlainURL{PlainURL: url, HTTPS: macports.HTTPS(url.URL), Answers: true})
	}
	return switched
}

func (e *Engine) httpsProbe() HTTPSProbe {
	if e.HTTPS != nil {
		return e.HTTPS
	}
	return requestProbe{}
}

// requestProbe asks a URL as fetch asks one (fetch.Ask), within its
// context: a redirect back to plain HTTP is no answer over HTTPS, as
// fetching refuses it (the helper-ownership review's finding 2).
type requestProbe struct{ client *http.Client }

func (p requestProbe) Answers(ctx context.Context, url string) bool {
	return fetch.Ask(ctx, p.client, url).Answered
}
