package distfile

// The in-process fetch engine. http and https URLs are fetched with
// Go's client; any other scheme — ftp still appears in master_sites —
// is handed to the curl binary when the machine has one.
//
// The transfer behavior mirrors what base's own libcurl use has
// learned (pextlib1.0/curl.c, portfetch.tcl): transparent compression
// stays OFF, because the recorded checksums are of the on-the-wire
// bytes; connections time out at 30s and stalled transfers abort, so a
// dead mirror cannot hang the chain; redirect chains may run to 50; and
// the cookie engine runs with a throwaway jar, because some mirror
// redirect chains require it.

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/checksums"
)

// ErrUnavailable reports that none of a distfile's URLs could be
// fetched.
var ErrUnavailable = errors.New("distfile: no url could be fetched")

// defaultUserAgent identifies dockhand when the port declares no UA of
// its own.
const defaultUserAgent = "dockhand (+https://github.com/herbygillot/dockhand)"

// Transfer limits, mirroring base's.
const (
	connectTimeout = 30 * time.Second
	stallTimeout   = 60 * time.Second // base: <1KB/s for 60s; ours: no bytes for 60s
	maxRedirects   = 50
)

// transport is the shared base transport: connect timeout, and no
// transparent compression — checksums are of the wire bytes.
var transport = &http.Transport{
	Proxy:               http.ProxyFromEnvironment,
	DialContext:         (&net.Dialer{Timeout: connectTimeout}).DialContext,
	DisableCompression:  true,
	TLSHandshakeTimeout: connectTimeout,
}

var insecureTransport = func() *http.Transport {
	t := transport.Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // fetch.ignore_sslcert is the port's own declaration
	return t
}()

// curlPath resolves curl once; empty when the machine has none.
var curlPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("curl")
	if err != nil {
		return ""
	}
	return p
})

// Direct is the in-process fetcher, for contexts with no MacPorts
// installation in play — upstream probes, tests. It identifies as
// dockhand, which is why it must never compute recorded checksums:
// those fetches have to be indistinguishable from the ones port will
// later perform, and belong to portfetch.
type Direct struct{}

// Fetch implements the planner's fetcher over the direct client.
func (Direct) Fetch(ctx context.Context, urls []string, opts Options, dest string) (checksums.Sums, error) {
	return Fetch(ctx, urls, opts, dest)
}

// Fetch downloads one distfile to dest, trying urls in order — the
// order MacPorts' own machinery proposed, upstream before fallback
// mirrors — and returns its checksums. A URL that fails is skipped;
// when every URL fails, the error carries the last failure.
//
// The bytes are kept rather than discarded so that whatever is read out
// of them later — a lockfile, a manifest — provably came from the same
// artifact the returned checksums describe. The caller owns dest and
// removes it.
func Fetch(ctx context.Context, urls []string, opts Options, dest string) (checksums.Sums, error) {
	var lastErr error
	for _, url := range urls {
		sums, err := fetchOne(ctx, url, opts, dest)
		if err == nil {
			return sums, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return checksums.Sums{}, fmt.Errorf("%w (%d urls): %w", ErrUnavailable, len(urls), lastErr)
}

// fetchOne writes one attempt to dest, hashing as it goes. dest is
// truncated per attempt, so a failed URL leaves nothing for the next one
// to append to.
func fetchOne(ctx context.Context, url string, opts Options, dest string) (checksums.Sums, error) {
	f, err := os.Create(dest)
	if err != nil {
		return checksums.Sums{}, err
	}
	defer f.Close() //nolint:errcheck // writes are unbuffered; close only releases the fd
	h := checksums.New()
	w := io.MultiWriter(h, f)
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		err = httpFetch(ctx, url, opts, w)
	} else {
		err = curlFetch(ctx, url, opts, w)
	}
	if err != nil {
		return checksums.Sums{}, err
	}
	return h.Sums(), nil
}

func userAgent(opts Options) string {
	if opts.UserAgent != "" {
		return opts.UserAgent
	}
	return defaultUserAgent
}

func httpFetch(ctx context.Context, url string, opts Options, w io.Writer) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	var rt http.RoundTripper = transport
	if opts.IgnoreSSLCert {
		rt = insecureTransport
	}
	client := &http.Client{
		Transport: rt,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("more than %d redirects", maxRedirects)
			}
			return nil
		},
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent(opts))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // read-path close
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	if _, err := io.Copy(w, newStallReader(resp.Body, cancel)); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	return nil
}

// stallReader aborts a transfer that delivers nothing for stallTimeout:
// each successful read rearms the timer; expiry cancels the request.
// Base aborts below 1KB/s sustained for the same window; zero-progress
// is the conservative approximation without a rate meter.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func newStallReader(r io.Reader, cancel context.CancelFunc) *stallReader {
	return &stallReader{r: r, timer: time.AfterFunc(stallTimeout, cancel)}
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(stallTimeout)
	}
	if err != nil {
		s.timer.Stop()
	}
	return n, err
}

// curlFetch streams a URL through curl — the reach for every scheme Go's
// http client does not speak — with the same learned flags portfetch
// passes to base's libcurl.
func curlFetch(ctx context.Context, url string, opts Options, w io.Writer) error {
	curl := curlPath()
	if curl == "" {
		return fmt.Errorf("%s: scheme needs curl, which is not on PATH", url)
	}
	args := []string{
		"--fail", "--silent", "--show-error", "--location",
		"--max-redirs", fmt.Sprint(maxRedirects),
		"--connect-timeout", fmt.Sprint(int(connectTimeout.Seconds())),
		"--speed-limit", "1024", "--speed-time", "60",
		"--cookie-jar", "/dev/null",
		"--user-agent", userAgent(opts),
		"--output", "-",
	}
	if opts.DisableEPSV {
		args = append(args, "--disable-epsv")
	}
	if opts.IgnoreSSLCert {
		args = append(args, "--insecure")
	}
	args = append(args, url)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, curl, args...)
	cmd.Stdout = w
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: curl: %s", url, msg)
	}
	return nil
}
