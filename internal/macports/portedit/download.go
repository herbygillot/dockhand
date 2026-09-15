package portedit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"golang.org/x/crypto/ripemd160"
)

type Download struct {
	path                      string
	Name, URL, SHA256, RMD160 string
	Size                      int64
}

type archiveSource struct{ Name, URL string }

func downloadSource(info macports.PortInfo) (string, string, error) {
	files, err := downloadSources(info, "")
	if err != nil {
		return "", "", err
	}
	if len(files) != 1 {
		return "", "", fmt.Errorf("%w: expected one distfile", ErrUnsupported)
	}
	return files[0].Name, files[0].URL, nil
}

func localPatches(info macports.PortInfo, portdir string) error {
	files, errs := syntax.ListValues(info.Options["patchfiles"])
	if len(errs) > 0 {
		return fmt.Errorf("%w: invalid patchfiles", ErrUnsupported)
	}
	if len(files) == 0 {
		return nil
	}
	if portdir == "" {
		return fmt.Errorf("%w: patchfiles require a frozen local files directory", ErrUnsupported)
	}
	root := info.Options["filespath"]
	if root == "" {
		root = filepath.Join(portdir, "files")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("%w: local patch directory is unavailable", ErrUnsupported)
	}
	base, err := filepath.EvalSymlinks(portdir)
	if err != nil {
		return fmt.Errorf("%w: frozen port directory is unavailable", ErrUnsupported)
	}
	relative, err := filepath.Rel(base, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: patches must be inside the frozen port directory", ErrUnsupported)
	}
	for _, name := range files {
		if !portfile.Literal(name) || name == "." || name == ".." {
			return fmt.Errorf("%w: remote or ambiguous patchfile %s", ErrUnsupported, name)
		}
		stat, err := os.Lstat(filepath.Join(resolved, name))
		if err != nil || !stat.Mode().IsRegular() {
			return fmt.Errorf("%w: patchfile %s is not a frozen regular file", ErrUnsupported, name)
		}
	}
	return nil
}

func downloadSources(info macports.PortInfo, portdir string) ([]archiveSource, error) {
	if err := checkFetchCredentials(info); err != nil {
		return nil, err
	}
	for _, key := range []string{"distfiles", "master_sites", "checksums", "fetch.type", "fetch.archive_compatible", "patchfiles", "filespath", "fetch.ignore_sslcert", "go.vendors", "cargo.crates", "cargo.crates_github"} {
		if info.OptionErrors[key] != "" {
			return nil, fmt.Errorf("%w: cannot evaluate %s", ErrUnsupported, key)
		}
	}
	if info.Options["fetch.type"] != "standard" || info.Options["fetch.archive_compatible"] != "1" || info.Options["fetch.ignore_sslcert"] != "no" || info.Options["go.vendors"] != "" || info.Options["cargo.crates"] != "" || info.Options["cargo.crates_github"] != "" {
		return nil, fmt.Errorf("%w: fetch customization or vendored source requires a dedicated preparer", ErrUnsupported)
	}
	if err := localPatches(info, portdir); err != nil {
		return nil, err
	}
	files, errs := syntax.ListValues(info.Options["distfiles"])
	if len(errs) > 0 || len(files) == 0 {
		return nil, fmt.Errorf("%w: source distfiles are required", ErrUnsupported)
	}
	sites, errs := syntax.ListValues(info.Options["master_sites"])
	if len(errs) > 0 || len(sites) == 0 {
		return nil, fmt.Errorf("%w: direct master sites are required", ErrUnsupported)
	}
	locations := map[string][]string{}
	for _, raw := range sites {
		site, err := url.Parse(raw)
		if err != nil || site.Host == "" || site.User != nil || site.Fragment != "" || (site.Scheme != "https" && site.Scheme != "http") {
			return nil, fmt.Errorf("%w: only direct HTTP(S) master sites are supported", ErrUnsupported)
		}
		tags := []string{""}
		authority := strings.Index(raw, "://") + 3
		pathStart := strings.Index(raw[authority:], "/")
		if cut := strings.LastIndex(raw, ":"); pathStart >= 0 && cut > authority+pathStart {
			tags = strings.Split(raw[cut+1:], ",")
			for _, tag := range tags {
				if tag == "" || !portfile.Literal(tag) {
					return nil, fmt.Errorf("%w: invalid master-site tag", ErrUnsupported)
				}
			}
			raw = raw[:cut]
		}
		for _, tag := range tags {
			locations[tag] = append(locations[tag], raw)
		}
	}
	seen := map[string]bool{}
	var result []archiveSource
	for _, file := range files {
		name, tag, _ := strings.Cut(file, ":")
		if name == "" || !portfile.Literal(name) || name == "." || name == ".." || seen[name] || (tag != "" && !portfile.Literal(tag)) {
			return nil, fmt.Errorf("%w: ambiguous distfile %s", ErrUnsupported, file)
		}
		choices := locations[tag]
		if len(choices) != 1 {
			return nil, fmt.Errorf("%w: distfile %s must select exactly one direct master site", ErrUnsupported, file)
		}
		seen[name] = true
		result = append(result, archiveSource{Name: name, URL: strings.TrimRight(choices[0], "/") + "/" + url.PathEscape(name)})
	}
	return result, nil
}

func (s *Service) download(ctx context.Context, info macports.PortInfo) (Download, error) {
	name, address, err := downloadSource(info)
	if err != nil {
		return Download{}, err
	}
	return s.downloadArchive(ctx, info, archiveSource{name, address}, nil)
}

func (s *Service) downloadArchive(ctx context.Context, info macports.PortInfo, source archiveSource, output io.Writer) (Download, error) {
	name, address := source.Name, source.URL
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
			return fmt.Errorf("portedit: unsupported download redirect")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("portedit: download redirect downgraded HTTPS")
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
		return Download{}, fmt.Errorf("portedit: downloading %s returned HTTP %d", name, response.StatusCode)
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return Download{}, fmt.Errorf("portedit: download returned encoded content")
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return Download{}, fmt.Errorf("portedit: download returned HTML for %s", name)
	}
	limit := s.MaxDownloadBytes
	if limit <= 0 {
		limit = 512 << 20
	}
	if response.ContentLength > limit {
		return Download{}, fmt.Errorf("portedit: distfile exceeds download limit (%d bytes)", limit)
	}
	reader := io.LimitReader(response.Body, limit+1)
	prefix := make([]byte, 512)
	n, err := io.ReadFull(reader, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return Download{}, err
	}
	prefix = prefix[:n]
	if strings.Contains(http.DetectContentType(prefix), "text/html") {
		return Download{}, fmt.Errorf("portedit: download body is HTML for %s", name)
	}
	sha, rmd := sha256.New(), ripemd160.New()
	writers := []io.Writer{sha, rmd}
	if output != nil {
		writers = append(writers, output)
	}
	hashes := io.MultiWriter(writers...)
	if _, err = hashes.Write(prefix); err != nil {
		return Download{}, err
	}
	remaining, err := io.Copy(hashes, reader)
	if err != nil {
		return Download{}, err
	}
	size := int64(n) + remaining
	if size == 0 || size > limit {
		return Download{}, fmt.Errorf("portedit: empty or oversized distfile %s", name)
	}
	return Download{Name: name, URL: address, SHA256: fmt.Sprintf("%x", sha.Sum(nil)), RMD160: fmt.Sprintf("%x", rmd.Sum(nil)), Size: size}, nil
}

func checkFetchCredentials(info macports.PortInfo) error {
	if info.OptionErrors["fetch.has_credentials"] != "" || info.Options["fetch.has_credentials"] == "" {
		return fmt.Errorf("%w: cannot determine applicable MacPorts fetch credentials; prepare this update manually with MacPorts", ErrUnsupported)
	}
	if info.Options["fetch.has_credentials"] != "0" {
		return fmt.Errorf("%w: MacPorts credentials apply to the selected source downloads; authenticated fetching is not supported by the direct downloader; prepare this update manually with MacPorts", ErrUnsupported)
	}
	return nil
}
