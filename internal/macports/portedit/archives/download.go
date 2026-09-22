package archives

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"golang.org/x/crypto/ripemd160"
)

// Download is one fetched archive: its hashes, and the path of its kept
// bytes when the store kept them.
type Download struct {
	// Path is the kept file, empty when only the hashes were wanted.
	Path                      string `json:"-"`
	Name, URL, SHA256, RMD160 string
	// MD5 and SHA1 serve only a legacy group kept on request.
	MD5, SHA1 string `json:",omitempty"`
	Size      int64
}

// Source is one declared distfile and the single direct location it is
// fetched from.
type Source struct{ Name, URL string }

// Client fetches archives with bounded size and time; the zero value uses
// the default HTTP client, 512 MiB, and two minutes.
type Client struct {
	HTTP *http.Client
	// MaxBytes bounds each archive; 512 MiB when unset.
	MaxBytes int64
	// Timeout bounds each archive download; two minutes when unset.
	Timeout time.Duration
}

// LocalPatches checks that every declared patch file is a frozen regular
// file inside the port directory.
func LocalPatches(info macports.PortInfo, portdir string) error {
	files, errs := syntax.ListValues(info.Options["patchfiles"])
	if len(errs) > 0 {
		return fmt.Errorf("%w: invalid patchfiles", portfile.ErrUnsupported)
	}
	if len(files) == 0 {
		return nil
	}
	if portdir == "" {
		return fmt.Errorf("%w: patchfiles require a frozen local files directory", portfile.ErrUnsupported)
	}
	root := info.Options["filespath"]
	if root == "" {
		root = filepath.Join(portdir, "files")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("%w: local patch directory is unavailable", portfile.ErrUnsupported)
	}
	base, err := filepath.EvalSymlinks(portdir)
	if err != nil {
		return fmt.Errorf("%w: frozen port directory is unavailable", portfile.ErrUnsupported)
	}
	relative, err := filepath.Rel(base, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: patches must be inside the frozen port directory", portfile.ErrUnsupported)
	}
	for _, name := range files {
		if !portfile.Literal(name) || name == "." || name == ".." {
			return fmt.Errorf("%w: remote or ambiguous patchfile %s", portfile.ErrUnsupported, name)
		}
		stat, err := os.Lstat(filepath.Join(resolved, name))
		if err != nil || !stat.Mode().IsRegular() {
			return fmt.Errorf("%w: patchfile %s is not a frozen regular file", portfile.ErrUnsupported, name)
		}
	}
	return nil
}

// Sources maps the evaluated distfiles to their direct locations after
// the archive policy checks.
func Sources(info macports.PortInfo, portdir string) ([]Source, error) {
	if err := CheckPolicy(info, portdir); err != nil {
		return nil, err
	}
	files, errs := syntax.ListValues(info.Options["distfiles"])
	if len(errs) > 0 || len(files) == 0 {
		return nil, fmt.Errorf("%w: source distfiles are required", portfile.ErrUnsupported)
	}
	sites, errs := syntax.ListValues(info.Options["master_sites"])
	if len(errs) > 0 || len(sites) == 0 {
		return nil, fmt.Errorf("%w: direct master sites are required", portfile.ErrUnsupported)
	}
	locations := map[string][]string{}
	for _, raw := range sites {
		site, err := url.Parse(raw)
		if err != nil || site.Host == "" || site.User != nil || site.Fragment != "" || !fetch.Scheme(site.Scheme) {
			return nil, fmt.Errorf("%w: only direct HTTP(S) or FTP master sites are supported", portfile.ErrUnsupported)
		}
		tags := []string{""}
		authority := strings.Index(raw, "://") + 3
		pathStart := strings.Index(raw[authority:], "/")
		if cut := strings.LastIndex(raw, ":"); pathStart >= 0 && cut > authority+pathStart {
			tags = strings.Split(raw[cut+1:], ",")
			for _, tag := range tags {
				if tag == "" || !portfile.Literal(tag) {
					return nil, fmt.Errorf("%w: invalid master-site tag", portfile.ErrUnsupported)
				}
			}
			raw = raw[:cut]
		}
		for _, tag := range tags {
			locations[tag] = append(locations[tag], raw)
		}
	}
	seen := map[string]bool{}
	var result []Source
	for _, file := range files {
		name, tag, _ := strings.Cut(file, ":")
		if name == "" || !portfile.Literal(name) || name == "." || name == ".." || seen[name] || (tag != "" && !portfile.Literal(tag)) {
			return nil, fmt.Errorf("%w: ambiguous distfile %s", portfile.ErrUnsupported, file)
		}
		choices := locations[tag]
		if len(choices) != 1 {
			return nil, fmt.Errorf("%w: distfile %s must select exactly one direct master site", portfile.ErrUnsupported, file)
		}
		seen[name] = true
		result = append(result, Source{Name: name, URL: strings.TrimRight(choices[0], "/") + "/" + url.PathEscape(name)})
	}
	return result, nil
}

func (c Client) download(ctx context.Context, info macports.PortInfo, source Source, output io.Writer) (Download, error) {
	name, address := source.Name, source.URL
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	limit := c.MaxBytes
	if limit <= 0 {
		limit = 512 << 20
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// fail words a failure with the file, the URL it was asked from, and the
	// cause, so the job's detail says where and why without the log.
	fail := func(err error) error { return downloadError(parent, name, address, limit, timeout, err) }
	var reader io.ReadCloser
	if strings.HasPrefix(address, "ftp://") {
		// An anonymous FTP fetch, which about a hundred ports' only master
		// sites offer; the body is hashed and sniffed like an HTTP one.
		body, err := fetch.OpenFTP(ctx, address, limit)
		if err != nil {
			return Download{}, fail(err)
		}
		reader = body
	} else {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return Download{}, fail(err)
		}
		request.Header.Set("Accept-Encoding", "identity")
		agent := info.Options["fetch.user_agent"]
		if agent == "" {
			agent = fetch.UserAgent
		}
		request.Header.Set("User-Agent", agent)
		response, err := fetch.Open(c.HTTP, request, limit)
		if err != nil {
			return Download{}, fail(err)
		}
		if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
			response.Body.Close()
			return Download{}, fail(fmt.Errorf("the server sent %s-encoded content instead of the file", encoding))
		}
		if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
			response.Body.Close()
			return Download{}, fail(errHTMLPage)
		}
		reader = response.Body
	}
	defer reader.Close()
	prefix := make([]byte, 512)
	n, err := io.ReadFull(reader, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return Download{}, fail(fmt.Errorf("transfer stopped after %d bytes: %w", n, err))
	}
	prefix = prefix[:n]
	if strings.Contains(http.DetectContentType(prefix), "text/html") {
		return Download{}, fail(errHTMLPage)
	}
	sha, rmd, md, sha1sum := sha256.New(), ripemd160.New(), md5.New(), sha1.New()
	writers := []io.Writer{sha, rmd, md, sha1sum}
	if output != nil {
		writers = append(writers, output)
	}
	hashes := io.MultiWriter(writers...)
	if _, err = hashes.Write(prefix); err != nil {
		return Download{}, err
	}
	remaining, err := io.Copy(hashes, reader)
	if err != nil {
		return Download{}, fail(fmt.Errorf("transfer stopped after %d bytes: %w", int64(n)+remaining, err))
	}
	size := int64(n) + remaining
	if size == 0 {
		return Download{}, fail(errors.New("the server sent an empty file"))
	}
	return Download{Name: name, URL: address, SHA256: fmt.Sprintf("%x", sha.Sum(nil)), RMD160: fmt.Sprintf("%x", rmd.Sum(nil)), MD5: fmt.Sprintf("%x", md.Sum(nil)), SHA1: fmt.Sprintf("%x", sha1sum.Sum(nil)), Size: size}, nil
}

var errHTMLPage = errors.New("the server sent an HTML page instead of the file")

// downloadError words one archive's failure: the file, the URL it was asked
// from, and the cause. A refusal keeps its status, the redirect that led to
// it, and what the server said; the size limit and dockhand's own deadline
// are named as such, since the bare errors say neither the number nor whose
// limit it was; a transport error drops the URL it repeats. The parent
// context's own cancellation passes through unworded, and every cause stays
// reachable with errors.Is and errors.As.
func downloadError(parent context.Context, name, address string, limit int64, timeout time.Duration, err error) error {
	if parent.Err() != nil {
		return fmt.Errorf("archives: downloading %s from %s: %w", name, address, parent.Err())
	}
	var status *fetch.StatusError
	var transport *url.Error
	switch {
	case errors.As(err, &status):
		note := ""
		if status.Status == http.StatusNotFound {
			note = "; no archive is published at that location yet, and a release tag alone does not publish its assets"
		}
		return fmt.Errorf("archives: downloading %s: %w%s", name, err, note)
	case errors.Is(err, fetch.ErrTooLarge):
		return fmt.Errorf("archives: downloading %s from %s: larger than the %s limit: %w", name, address, byteLabel(limit), err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("archives: downloading %s from %s: no complete response within dockhand's %s limit: %w", name, address, timeout, err)
	case errors.As(err, &transport):
		return fmt.Errorf("archives: downloading %s from %s: %w", name, address, transport.Err)
	}
	return fmt.Errorf("archives: downloading %s from %s: %w", name, address, err)
}

// byteLabel is a size in the unit that reads naturally for a download limit.
func byteLabel(size int64) string {
	switch {
	case size >= 1<<30 && size%(1<<30) == 0:
		return fmt.Sprintf("%d GiB", size>>30)
	case size >= 1<<20 && size%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", size>>20)
	}
	return fmt.Sprintf("%d bytes", size)
}

// CheckFetchCredentials refuses a port whose downloads need MacPorts
// credentials, which the direct downloader does not carry.
func CheckFetchCredentials(info macports.PortInfo) error {
	if info.OptionErrors["fetch.has_credentials"] != "" || info.Options["fetch.has_credentials"] == "" {
		return fmt.Errorf("%w: cannot determine applicable MacPorts fetch credentials; prepare this update manually with MacPorts", portfile.ErrUnsupported)
	}
	if info.Options["fetch.has_credentials"] != "0" {
		return fmt.Errorf("%w: MacPorts credentials apply to the selected source downloads; authenticated fetching is not supported by the direct downloader; prepare this update manually with MacPorts", portfile.ErrUnsupported)
	}
	return nil
}

// CheckPolicy refuses a port whose archives the direct downloader cannot
// fetch as MacPorts would: credentials, an incompatible fetch, customized
// fetching, vendored sources, or patches outside the port directory.
func CheckPolicy(info macports.PortInfo, portdir string) error {
	if err := CheckFetchCredentials(info); err != nil {
		return err
	}
	if problem := info.OptionErrors["fetch.archive_compatible"]; problem != "" {
		return fmt.Errorf("%w: %s", portfile.ErrUnsupported, problem)
	}
	for _, key := range []string{"distfiles", "master_sites", "checksums", "fetch.type", "fetch.archive_compatible", "patchfiles", "filespath", "fetch.ignore_sslcert", "go.vendors", "cargo.crates", "cargo.crates_github"} {
		if info.OptionErrors[key] != "" {
			return fmt.Errorf("%w: cannot evaluate %s", portfile.ErrUnsupported, key)
		}
	}
	if info.Options["fetch.type"] != "standard" || info.Options["fetch.archive_compatible"] != "1" || info.Options["fetch.ignore_sslcert"] != "no" || info.Options["go.vendors"] != "" || info.Options["cargo.crates"] != "" || info.Options["cargo.crates_github"] != "" {
		return fmt.Errorf("%w: fetch customization or vendored source requires a dedicated preparer", portfile.ErrUnsupported)
	}
	if err := LocalPatches(info, portdir); err != nil {
		return err
	}

	return nil
}
