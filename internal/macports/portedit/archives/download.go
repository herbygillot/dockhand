package archives

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // MacPorts checksums are rmd160, among others.
)

// Download is one fetched archive: what its bytes hashed to, under the
// archive's name, and the path of the kept bytes when the store kept them.
type Download struct {
	// Path is the kept file, empty when only the hashes were wanted.
	Path string `json:"-"`
	URL  string
	portfile.Checksum
}

// Source is one declared distfile and the single direct location it is
// fetched from.
type Source struct{ Name, URL string }

// Client fetches archives, each of any size, as long as data keeps
// arriving; the zero value uses dockhand's download client and a stall of
// three minutes, the person's choice (2026-10-01). A download's size is unbounded at the person's word (2026-10-01):
// rustc's 566 MB source passed the 512 MiB it had, and the two minutes.
type Client struct {
	HTTP *http.Client
	// MaxBytes bounds each archive; none when unset.
	MaxBytes int64
	// Stall bounds how long a download may go without a byte, the wait
	// for a response included; three minutes when unset.
	Stall time.Duration
	// Mirror is where Shipped looks for an archive upstream no longer
	// serves as declared, MacPortsMirror in use; none when empty, as in
	// tests.
	Mirror string
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
		return fmt.Errorf("%w: patches must be inside the frozen port directory; filespath %s is outside %s", portfile.ErrUnsupported, resolved, base)
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
	bound := c.Stall
	if bound <= 0 {
		bound = 3 * time.Minute
	}
	limit := c.MaxBytes
	if limit <= 0 {
		limit = math.MaxInt64
	}
	parent := ctx
	ctx, stall := fetch.NewStall(ctx, bound)
	defer stall.Stop()
	// fail words a failure with the file, the URL it was asked from, and the
	// cause, so the job's detail says where and why without the log.
	fail := func(err error) error {
		if errors.Is(context.Cause(ctx), fetch.ErrStalled) {
			err = fmt.Errorf("%w for %s, so dockhand gave up on it: %w", fetch.ErrStalled, bound, err)
		}
		return downloadError(parent, name, address, limit, err)
	}
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
		client := c.HTTP
		if client == nil {
			client = fetch.DownloadClient
		}
		response, err := fetch.Open(client, request, limit)
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
	body := stall.Reader(reader)
	prefix := make([]byte, 512)
	n, err := io.ReadFull(body, prefix)
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
	remaining, err := io.Copy(hashes, body)
	if err != nil {
		return Download{}, fail(fmt.Errorf("transfer stopped after %d bytes: %w", int64(n)+remaining, err))
	}
	size := int64(n) + remaining
	if size == 0 {
		return Download{}, fail(errors.New("the server sent an empty file"))
	}
	return Download{URL: address, Checksum: portfile.Checksum{Name: name, SHA256: fmt.Sprintf("%x", sha.Sum(nil)), RMD160: fmt.Sprintf("%x", rmd.Sum(nil)), MD5: fmt.Sprintf("%x", md.Sum(nil)), SHA1: fmt.Sprintf("%x", sha1sum.Sum(nil)), Size: size}}, nil
}

var errHTMLPage = errors.New("the server sent an HTML page instead of the file")

// downloadError words one archive's failure: the file, the URL it was asked
// from, and the cause. A refusal keeps its status, the redirect that led to
// it, and what the server said; a size limit is named as such, since the
// bare error says neither the number nor whose limit it was, and a stall
// says itself; a transport error drops the URL it repeats. The parent
// context's own cancellation passes through unworded, and every cause stays
// reachable with errors.Is and errors.As.
func downloadError(parent context.Context, name, address string, limit int64, err error) error {
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
	case errors.Is(err, fetch.ErrStalled):
		return fmt.Errorf("archives: downloading %s from %s: %w", name, address, err)
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
	credentials, err := info.FetchCredentials()
	if err != nil {
		return fmt.Errorf("%w: cannot determine applicable MacPorts fetch credentials; prepare this update manually with MacPorts", portfile.ErrUnsupported)
	}
	if credentials {
		return fmt.Errorf("%w: MacPorts credentials apply to the selected source downloads; authenticated fetching is not supported by the direct downloader; prepare this update manually with MacPorts", portfile.ErrUnsupported)
	}
	return nil
}

// OwnArchives is a port's Portfile with the crates or Go modules it
// declares set aside, as a checksum refresh sets them aside: evaluated, its
// fetch plan names the port's own archives, which CheckPolicy may admit
// where it refuses the port as it is. The declarations are its lock
// file's, read in those archives, and MacPorts' fetch of them isn't one
// the direct downloader makes. False, with the Portfile as it is, for a
// port that declares none.
func OwnArchives(contents []byte, info macports.PortInfo) ([]byte, bool, error) {
	declared, err := dependency.Declared(contents, info)
	switch {
	case err != nil:
		return nil, false, fmt.Errorf("%w: %s: %w", portfile.ErrUnsupported, info.Name, err)
	case declared == nil:
		return contents, false, nil
	}
	stripped, err := declared.Strip(contents)
	return stripped, err == nil, err
}

// CheckPolicy refuses a port whose archives the direct downloader cannot
// fetch as MacPorts would: credentials, an incompatible fetch, customized
// fetching, vendored sources, or patches outside the port directory.
func CheckPolicy(info macports.PortInfo, portdir string) error {
	if err := CheckFetchCredentials(info); err != nil {
		return err
	}
	compatible, problem, err := info.ArchiveCompatible()
	switch {
	case err != nil:
		return fmt.Errorf("%w: %v", portfile.ErrUnsupported, err)
	case !compatible:
		return fmt.Errorf("%w: %s; prepare this port manually", portfile.ErrUnsupported, problem)
	}
	for _, key := range []string{"distfiles", "master_sites", "checksums", "fetch.type", "fetch.archive_compatible", "patchfiles", "filespath", "fetch.ignore_sslcert", "go.vendors", "cargo.crates", "cargo.crates_github"} {
		if info.OptionErrors[key] != "" {
			return fmt.Errorf("%w: cannot evaluate %s", portfile.ErrUnsupported, key)
		}
	}
	// fetch.ignore_sslcert is read as Tcl reads a boolean, and must have
	// been evaluated and be false.
	_, evaluated := info.Options["fetch.ignore_sslcert"]
	ignoreCertificate, err := info.Bool("fetch.ignore_sslcert")
	if info.Options["fetch.type"] != "standard" || !evaluated || err != nil || ignoreCertificate || info.Options["go.vendors"] != "" || info.Options["cargo.crates"] != "" || info.Options["cargo.crates_github"] != "" {
		return fmt.Errorf("%w: fetch customization or vendored source requires a dedicated preparer", portfile.ErrUnsupported)
	}
	if err := LocalPatches(info, portdir); err != nil {
		return err
	}

	return nil
}
