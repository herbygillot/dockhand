package buildlog

import (
	"bufio"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// FetchFailure is an archive MacPorts couldn't fetch, as its fetch phase
// says it: "Error: Failed to fetch <file>: <reason>", the reason its last
// site's, and that site, from the "Fetching <url> failed" line before it
// that names the file, where the log is verbose enough to have one.
// garage's CI on macos-15 stopped at jni-sys-macros-0.4.1.crate, which
// mirror.sjtu.edu.cn answered 403 after static.crates.io timed out, where
// a check on the same commit passed (field testing, 2026-10-03).
//
// An exception to decision 32, chosen knowingly, as the "Command failed:"
// reading is: MacPorts documents none of these lines, nor curl the words
// of its errors. When their wording changes, none is found, the failure
// is unclassified, and nothing is judged by it.
type FetchFailure struct {
	File, Reason string
	// URL is the last site tried, empty where the log doesn't say.
	URL string
	// Status is the HTTP status the site answered, as curl words it,
	// "The requested URL returned error: 403"; 0 where it answered none,
	// as a timeout.
	Status int
	// Number is the Error line's, counting from 1.
	Number int
}

var (
	// fetchFailed is MacPorts' fetch error for one file; a port's,
	// "Failed to fetch garage: Failed to fetch distfiles", follows it.
	fetchFailed = regexp.MustCompile(`^Error: Failed to fetch (\S+): (.+)$`)
	siteFailed  = regexp.MustCompile(`^DEBUG: Fetching (\S+) failed: (.+)$`)
	httpStatus  = regexp.MustCompile(`returned error: (\d{3})\b`)
)

// FetchFailed reads a log for the first archive MacPorts' fetch failed;
// none where the log has none, or can't be read.
func FetchFailed(log io.Reader) (FetchFailure, bool) {
	lines := bufio.NewReaderSize(log, maxLine)
	sites := map[string]string{}
	for number := 1; ; number++ {
		line, err := lines.ReadSlice('\n')
		long := false
		for errors.Is(err, bufio.ErrBufferFull) {
			long = true
			_, err = lines.ReadSlice('\n')
		}
		if err != nil && !errors.Is(err, io.EOF) || len(line) == 0 && !long {
			return FetchFailure{}, false
		}
		text := strings.TrimRight(string(line), "\r\n")
		if m := siteFailed.FindStringSubmatch(text); !long && m != nil {
			if site, err := url.Parse(m[1]); err == nil {
				sites[site.Path[strings.LastIndex(site.Path, "/")+1:]] = m[1]
			}
		}
		if m := fetchFailed.FindStringSubmatch(text); !long && m != nil && m[2] != "Failed to fetch distfiles" {
			failure := FetchFailure{File: m[1], Reason: m[2], URL: sites[m[1]], Number: number}
			if s := httpStatus.FindStringSubmatch(m[2]); s != nil {
				failure.Status, _ = strconv.Atoi(s[1])
			}
			return failure, true
		}
		if err != nil {
			return FetchFailure{}, false
		}
	}
}

// Words say the failure as status does, "fetching
// jni-sys-macros-0.4.1.crate (mirror.sjtu.edu.cn answered 403)", the
// reason itself where the site answered no status.
func (f FetchFailure) Words() string {
	host := ""
	if site, err := url.Parse(f.URL); err == nil {
		host = site.Host
	}
	switch {
	case host != "" && f.Status != 0:
		return "fetching " + f.File + " (" + host + " answered " + strconv.Itoa(f.Status) + ")"
	case host != "":
		return "fetching " + f.File + " (" + host + ": " + f.Reason + ")"
	}
	return "fetching " + f.File + " (" + f.Reason + ")"
}
