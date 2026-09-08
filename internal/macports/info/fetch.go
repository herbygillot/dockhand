package info

// FetchInfo is one evaluation context's fetch surface: each distfile
// with the full URLs it may be fetched from, plus the port's own fetch
// exceptions — the fetch.* options portfetch itself threads through to
// curl. Ports that fetch from a repository rather than distfiles have
// no Files.
//
// It lives here for the same reason Values does: it is a fact read off
// a port, not the machinery that reads one. Assembling the surface does
// take a Tcl session — mirror macros expand inside MacPorts' own
// portfetch, and nothing outside base knows how — so the method that
// produces a FetchInfo stays on the evaluator, which owns the session.
// Only the vocabulary moves. That split is what it buys: a scripted
// oracle can answer the fetch question, and a planner can be handed a
// surface built by hand, without either importing the evaluator it is
// standing in for.
type FetchInfo struct {
	// Files maps a distfile's name to the ordered list of URLs MacPorts
	// would try for it, mirror macros already expanded. The order is
	// portfetch's own preference and is load-bearing: a fetcher walks it.
	Files map[string][]string
	// DisableEPSV, IgnoreSSLCert and UserAgent are the port's fetch.*
	// exceptions, stated in the negative form a fetcher acts on rather
	// than the option's own polarity (fetch.use_epsv is a yes/no).
	// They ride with the URLs because they are part of the same fact: a
	// site reached without the exception the port declares is not the
	// site the port meant, and a fetch that ignores them can report a
	// failure the maintainer would never see.
	DisableEPSV   bool
	IgnoreSSLCert bool
	UserAgent     string
}
