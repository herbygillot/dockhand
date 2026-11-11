// Command livecheck replays dockhand's upstream discovery over the ports
// tree with no network, from recorded catalogs and pages, and checks it
// against what needs no oracle: where a port's own tag exists upstream,
// the port's own livecheck filter must admit it (#200, the offline
// livecheck sweep's first step). pinact's livecheck broke because
// dockhand's model of GitHub's tags page didn't match the page
// (31fca1db), and nothing checked that model across the tree before
// release.
//
// The replay sits at upstream.Service's seam: its forge Catalogs and its
// HTTP client. Before any capture exists, each port's catalog holds only
// its own tag, spelled from its PortGroup's prefix and its version, a
// fact of the Portfile; a page discovery asks for that wasn't recorded
// is a replay miss, not a failure. Versions stays MacPorts' own tclsh,
// whose regex and vercmp are what's tested against.
//
//	livecheck check -db SURVEY.db [-tclsh PATH] [-port NAME]...
//
// reads each port's evaluated options from a survey record (tools/survey)
// and prints each port's bucket, then the counts. Fixtures for ports
// that found a bug are in testdata, replayed by the package's test.
package main
