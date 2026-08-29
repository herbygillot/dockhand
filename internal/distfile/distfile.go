// Package distfile is the in-process fetch engine for distribution
// files, for contexts with no MacPorts installation in play — the
// planner's normal fetches go through macports/portfetch instead. What
// a checksum is, and the hashing itself, live in internal/checksums;
// this package moves bytes and streams them through those hashes.
package distfile

// Options carries a port's own fetch exceptions, read from its
// fetch.* options — ports declare what their upstreams need.
type Options struct {
	DisableEPSV   bool   // fetch.use_epsv no (ftp behind broken firewalls)
	IgnoreSSLCert bool   // fetch.ignore_sslcert yes
	UserAgent     string // fetch.user_agent, for hosts that gate on UA
}
