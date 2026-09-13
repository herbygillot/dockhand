package prepare

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
	"golang.org/x/crypto/ripemd160"
)

type Download struct {
	Name, URL, SHA256, RMD160 string
	Size                      int64
}

func downloadSource(info macports.PortInfo) (string, string, error) {
	for _, key := range []string{"distfiles", "master_sites", "checksums", "fetch.type", "fetch.has_credentials", "fetch.customized", "patchfiles", "fetch.ignore_sslcert", "go.vendors", "cargo.crates", "cargo.crates_github"} {
		if info.OptionErrors[key] != "" {
			return "", "", fmt.Errorf("%w: cannot evaluate %s", ErrUnsupported, key)
		}
	}
	if info.Options["fetch.type"] != "standard" || info.Options["fetch.has_credentials"] != "0" || info.Options["fetch.customized"] != "0" || info.Options["patchfiles"] != "" || info.Options["fetch.ignore_sslcert"] != "no" || info.Options["go.vendors"] != "" || info.Options["cargo.crates"] != "" || info.Options["cargo.crates_github"] != "" {
		return "", "", fmt.Errorf("%w: fetch customization or vendored source requires a dedicated preparer", ErrUnsupported)
	}
	files, errs := syntax.ListValues(info.Options["distfiles"])
	if len(errs) > 0 || len(files) != 1 || files[0] == "" || strings.ContainsAny(files[0], "/:\\") || files[0] == "." || files[0] == ".." || !literalVersion(files[0]) {
		return "", "", fmt.Errorf("%w: one untagged distfile is required", ErrUnsupported)
	}
	sites, errs := syntax.ListValues(info.Options["master_sites"])
	if len(errs) > 0 || len(sites) != 1 {
		return "", "", fmt.Errorf("%w: one direct master site is required", ErrUnsupported)
	}
	site, err := url.Parse(sites[0])
	if err != nil || site.Host == "" || site.User != nil || site.Fragment != "" || site.Scheme != "https" && site.Scheme != "http" || strings.Contains(site.Path, ":") {
		return "", "", fmt.Errorf("%w: only untagged HTTP(S) master sites are supported", ErrUnsupported)
	}
	// MacPorts appends the encoded filename to the site, including query-style sites.
	return files[0], strings.TrimRight(sites[0], "/") + "/" + url.PathEscape(files[0]), nil
}

func (s *Service) download(ctx context.Context, info macports.PortInfo) (Download, error) {
	name, address, err := downloadSource(info)
	if err != nil {
		return Download{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return Download{}, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	agent := info.Options["fetch.user_agent"]
	if agent == "" {
		agent = "dockhand/2"
	}
	request.Header.Set("User-Agent", agent)
	client := http.DefaultClient
	if s.HTTP != nil {
		client = s.HTTP
	}
	configured := *client
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || req.URL.Scheme != "https" && req.URL.Scheme != "http" || len(via) >= 10 {
			return fmt.Errorf("prepare: unsupported download redirect")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("prepare: download redirect downgraded HTTPS")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	response, err := configured.Do(request)
	if err != nil {
		return Download{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Download{}, fmt.Errorf("prepare: downloading %s returned HTTP %d", name, response.StatusCode)
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return Download{}, fmt.Errorf("prepare: download returned encoded content")
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return Download{}, fmt.Errorf("prepare: download returned HTML for %s", name)
	}
	limit := s.MaxDownloadBytes
	if limit <= 0 {
		limit = 512 << 20
	}
	if response.ContentLength > limit {
		return Download{}, fmt.Errorf("prepare: distfile exceeds download limit (%d bytes)", limit)
	}
	reader := io.LimitReader(response.Body, limit+1)
	prefix := make([]byte, 512)
	n, err := io.ReadFull(reader, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return Download{}, err
	}
	prefix = prefix[:n]
	if strings.Contains(http.DetectContentType(prefix), "text/html") {
		return Download{}, fmt.Errorf("prepare: download body is HTML for %s", name)
	}
	sha, rmd := sha256.New(), ripemd160.New()
	hashes := io.MultiWriter(sha, rmd)
	if _, err = hashes.Write(prefix); err != nil {
		return Download{}, err
	}
	remaining, err := io.Copy(hashes, reader)
	if err != nil {
		return Download{}, err
	}
	size := int64(n) + remaining
	if size == 0 || size > limit {
		return Download{}, fmt.Errorf("prepare: empty or oversized distfile %s", name)
	}
	return Download{Name: name, URL: address, SHA256: fmt.Sprintf("%x", sha.Sum(nil)), RMD160: fmt.Sprintf("%x", rmd.Sum(nil)), Size: size}, nil
}
