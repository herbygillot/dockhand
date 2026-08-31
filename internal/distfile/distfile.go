// Package distfile is the vocabulary of distribution files: the fetch
// options a port declares, the error a failed fetch reports, and
// reading a named file out of distfiles already fetched.
//
// It once carried an in-process fetch engine as well — an http client
// and a curl driver of its own — deleted when macports/portfetch won
// outright: every real fetch goes through MacPorts' own machinery,
// which brings the proxies, the learned exceptions and the
// configuration that engine would have had to reimplement. The history
// has it if a no-installation context ever becomes real.
package distfile

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/checksums"
)

// ErrUnavailable reports that no url could serve a distfile. It is the
// branchable outcome every fetcher reports, whatever machinery it
// drives.
var ErrUnavailable = errors.New("distfile: no url could be fetched")

// Fetcher downloads one distfile to dest and reports its checksums; a
// planner asks it once per distfile. portfetch implements it over
// MacPorts' own curl, which is the engine, singular: an in-process
// alternative existed and was deleted for want of a caller.
//
// The bytes stay at dest rather than being hashed and dropped, so a
// planner that must read inside a distfile — a lockfile for a vendored
// block — reads the same artifact whose checksums it just recorded,
// with no second download to disagree with the first. The caller owns
// dest.
type Fetcher interface {
	Fetch(ctx context.Context, urls []string, opts Options, dest string) (checksums.Sums, error)
}

// Options carries a port's own fetch exceptions, read from its
// fetch.* options — ports declare what their upstreams need.
type Options struct {
	DisableEPSV   bool   // fetch.use_epsv no (ftp behind broken firewalls)
	IgnoreSSLCert bool   // fetch.ignore_sslcert yes
	UserAgent     string // fetch.user_agent, for hosts that gate on UA
}
