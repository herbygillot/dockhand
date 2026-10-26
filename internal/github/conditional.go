package github

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/herbygillot/dockhand/internal/atomicfile"
)

// ConditionalCache keeps GitHub's answers to reads with their ETags, so a
// read asked again goes with If-None-Match, and GitHub's 304 for one that
// hasn't moved, which it doesn't count against the hour's allowance, is
// answered with what was kept (Then's item 5): the M1's run at 1da4fdbf
// spent 2,166 requests on one outdated --mine of 835 ports, and a second
// look spent as many. It's kept for the process, and on disk where a
// directory is given, so the next command's look is nearly free too. Each
// answer is kept by the request's address, what it accepts, and a digest
// of its credential, so one login's answers are never another's.
type ConditionalCache struct {
	directory string
	mu        sync.Mutex
	kept      map[string]keptAnswer
}

// NewConditionalCache keeps answers in the process, and in directory where
// it isn't empty.
func NewConditionalCache(directory string) *ConditionalCache {
	return &ConditionalCache{directory: directory, kept: map[string]keptAnswer{}}
}

// keptAnswer is one read's answer as GitHub gave it, with its ETag.
type keptAnswer struct {
	ETag   string
	Status int
	Header http.Header
	Body   []byte
}

// keptLimit is the largest answer kept: a page of tags or releases is
// well under it.
const keptLimit = 4 << 20

// conditionalHits counts the reads this process answered from the cache
// on GitHub's 304, which spend nothing of the hour's allowance.
var conditionalHits atomic.Int64

func (c *ConditionalCache) load(key string) (keptAnswer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.kept[key]; ok {
		return kept, true
	}
	if c.directory == "" {
		return keptAnswer{}, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return keptAnswer{}, false
	}
	var kept keptAnswer
	if json.Unmarshal(data, &kept) != nil || kept.ETag == "" {
		return keptAnswer{}, false
	}
	c.kept[key] = kept
	return kept, true
}

// store keeps an answer; one the disk won't take is kept for the process.
func (c *ConditionalCache) store(key string, kept keptAnswer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kept[key] = kept
	if c.directory == "" {
		return
	}
	data, err := json.Marshal(kept)
	if err != nil {
		return
	}
	path := c.path(key)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = atomicfile.Write(path, data, 0o600)
	}
}

func (c *ConditionalCache) path(key string) string {
	return filepath.Join(c.directory, key[:2], key+".json")
}

// conditionalKey is what a read's answer is kept by.
func conditionalKey(req *http.Request) string {
	sum := sha256.New()
	for _, part := range []string{req.URL.String(), req.Header.Get("Accept"), req.Header.Get("Authorization")} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// conditional sends a read GitHub answered before with its ETag, and
// answers GitHub's 304 with what was kept, GitHub's rate-limit headers
// from the 304 laid over it, so the limits read are the hour's now.
type conditional struct {
	next  http.RoundTripper
	cache *ConditionalCache
}

func (t conditional) RoundTrip(req *http.Request) (*http.Response, error) {
	response, hit, err := t.roundTrip(req)
	if err == nil && !hit && !freeOfTheAllowance(req) {
		recordCharged(req, response.StatusCode)
	}
	return response, err
}

// roundTrip is RoundTrip, saying whether the cache answered it.
func (t conditional) roundTrip(req *http.Request) (*http.Response, bool, error) {
	if t.cache == nil || req.Method != http.MethodGet || req.Header.Get("If-None-Match") != "" || req.Header.Get("Range") != "" {
		response, err := t.next.RoundTrip(req)
		return response, false, err
	}
	key := conditionalKey(req)
	kept, ok := t.cache.load(key)
	if ok {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", kept.ETag)
	}
	response, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, false, err
	}
	if ok && response.StatusCode == http.StatusNotModified {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		conditionalHits.Add(1)
		header := kept.Header.Clone()
		for name, values := range response.Header {
			if strings.HasPrefix(strings.ToLower(name), "x-ratelimit") || name == "Date" {
				header[name] = values
			}
		}
		return &http.Response{Status: strconv.Itoa(kept.Status) + " " + http.StatusText(kept.Status), StatusCode: kept.Status, Proto: response.Proto, ProtoMajor: response.ProtoMajor, ProtoMinor: response.ProtoMinor,
			Header: header, Body: io.NopCloser(bytes.NewReader(kept.Body)), ContentLength: int64(len(kept.Body)), Request: response.Request}, true, nil
	}
	etag := response.Header.Get("ETag")
	if response.StatusCode != http.StatusOK || etag == "" {
		return response, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, keptLimit+1))
	response.Body.Close()
	if err != nil {
		return nil, false, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) <= keptLimit {
		t.cache.store(key, keptAnswer{ETag: etag, Status: response.StatusCode, Header: response.Header.Clone(), Body: body})
	}
	return response, false, nil
}

// charged are the requests this process sent GitHub that the cache didn't
// answer, each as "GET /repos/x/y/tags 200", for a command's spend line to
// name: D-N6's second look on rc4 counted one request, and nothing said
// which.
var charged struct {
	mu       sync.Mutex
	requests []string
}

// chargedLimit bounds what's kept of them; a spend names its first few.
const chargedLimit = 10000

func recordCharged(req *http.Request, status int) {
	charged.mu.Lock()
	defer charged.mu.Unlock()
	if len(charged.requests) < chargedLimit {
		charged.requests = append(charged.requests, fmt.Sprintf("%s %s %d", req.Method, req.URL.Path, status))
	}
}

// chargedSince are the requests recorded since the nth.
func chargedSince(n int) (int, []string) {
	charged.mu.Lock()
	defer charged.mu.Unlock()
	return len(charged.requests), slices.Clone(charged.requests[min(n, len(charged.requests)):])
}
