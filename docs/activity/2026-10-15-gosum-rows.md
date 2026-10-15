# create and update name each go.sum-only row to add (v0.4.0 part 1, c)

The rc9 full stage's B8: countdown 1.5.0, at go 1.14, is drafted with
checksums 0 and go.vendors empty, and `checksums_problem` names
rivo/uniseg, which only go.sum has. The roadmap's better way was to write
the row, its version from go.sum and its checksums from proxy.golang.org's
zip, "as go2port takes them", or failing that, the note naming the row to
add.

**Pushed back, with evidence: the proxy's zip isn't what MacPorts
fetches.** The golang PortGroup (`_resources/port1.0/group/golang-1.0.tcl`,
`handle_set_go_vendors`) makes each go.vendors row a distfile from the
module's host: a GitHub module's from
`codeload.github.com/<author>/<project>/legacy.tar.gz/<lock>`, GitLab's,
Bitbucket's, Sourcehut's, and Gitea's from theirs, and refuses any other
domain. Checksums taken from the module proxy's zip would never match what
the fetch phase downloads. Writing the row right means fetching the
archive the PortGroup names for the module's lock, which is
distfetch's job on an evaluated Portfile; that's left for when this is
next opened, rather than guessed at.

**Built: the note names each row to add.** `unprunedGraph` reads go.sum's
newest pin of each module it has the source of that go.mod doesn't
require, and the lock go2port would write for it (a pseudo-version's
commit), so the refusal reads: "…which go.vendors needs by hand: add
github.com/rivo/uniseg lock v0.1.0, as go.sum pins it, with the checksums
port -v checksum reports for its archive". The checksums refresh sets
go.vendors aside, so `dockhand checksums` doesn't fill a row added by
hand, and the note doesn't say it does. update's refusal of the same port
reads the same.

Test: TestGoGeneratorRefusesAnUnprunedGraphGoModLeavesShort.
