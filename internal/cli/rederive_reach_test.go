package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// TestRederiveReachSurvey asks whether the rule-D population needs a
// second evaluation FRAME at all. It ships nothing.
//
// Rule D is the terraform shape: one checksums command naming several
// files, of which this evaluation fetches one. Re-deriving the rest
// means fetching them, and the question is where their URLs come from.
//
// A file's site is chosen by the tag bound to it in `distfiles`, and a
// file that is not in distfiles at all has no binding — so it would be
// fetched from the UNTAGGED site group, the default. If the port has
// one, the unfetched file's URL is the fetched file's URL with the name
// swapped: no second frame, no second evaluation, just a fetch. If every
// site is tagged there is no default, and re-derivation cannot assemble
// the URL without being told more.
//
// The count decides how much machinery rule-D re-derivation needs, and
// it is separate from rule T, where the untaken block's FILENAMES are
// unknown until another frame evaluates.
//
//	DOCKHAND_REACH_SURVEY=/path/to/ports DOCKHAND_REACH_PORTS=<file> \
//	  go test ./internal/cli/ -run TestRederiveReachSurvey -timeout 120m -v
func TestRederiveReachSurvey(t *testing.T) {
	root := os.Getenv("DOCKHAND_REACH_SURVEY")
	list := os.Getenv("DOCKHAND_REACH_PORTS")
	if root == "" || list == "" {
		t.Skip("set DOCKHAND_REACH_SURVEY and DOCKHAND_REACH_PORTS to run")
	}
	b, err := os.ReadFile(list)
	require.NoError(t, err)
	names := strings.Fields(string(b))

	s := &Services{TreeRoot: root, Tools: testFinder(), Err: os.Stderr, Out: os.Stderr}
	ctx := context.Background()
	require.NoError(t, s.Acquire(ctx, app.Needs{Tree: true, Evaluator: true}))
	defer s.Close()
	ev, err := s.Eval()
	require.NoError(t, err)
	tr, err := s.Tree()
	require.NoError(t, err)

	var ruleD, untagged, tagged, noSites, evalErr int
	for _, name := range names {
		target, terr := tr.Resolve(name)
		if terr != nil {
			evalErr++
			continue
		}
		h := portHandle(target, ev, s)
		vals, err := h.Values(ctx)
		if err != nil {
			evalErr++
			continue
		}
		opts, err := h.Options(ctx, "master_sites", "distfiles", "patchfiles")
		if err != nil {
			evalErr++
			continue
		}
		// What this evaluation fetches is distfiles AND patchfiles —
		// base's checkfiles is checkpatchfiles followed by
		// checkdistfiles, and both kinds are checksummed. Counting only
		// distfiles reported a port's own patches as unreachable
		// distfiles: apple-gcc42's gcc-4.2.1-4.2.4-v2.patch, lookup's
		// texinfo.tex.
		//
		// Both options carry `filename:tag` suffixes, stripped here the
		// way base strips them. Reading the assembled fetch surface
		// instead would be more faithful and is unusable at this scale:
		// FetchInfo builds a URL per site per file through the rpc, and
		// a port like cargo — fifty distfiles across every mirror —
		// spends minutes on answers this survey throws away.
		fetched := append(strings.Fields(opts["distfiles"]), strings.Fields(opts["patchfiles"])...)
		unfetched := unfetchedFiles(vals, fetched)
		if len(unfetched) == 0 {
			continue
		}
		ruleD++
		sites := strings.Fields(opts["master_sites"])
		switch {
		case len(sites) == 0:
			noSites++
			fmt.Fprintf(os.Stderr, "no-sites  %-28s unfetched=%s\n", name, few(unfetched))
		case hasDefaultSite(sites):
			untagged++
			fmt.Fprintf(os.Stderr, "default   %-28s unfetched=%s\n", name, few(unfetched))
		default:
			tagged++
			fmt.Fprintf(os.Stderr, "ALLTAGGED %-28s unfetched=%s sites=%v\n", name, few(unfetched), sites)
		}
	}
	fmt.Fprintf(os.Stderr, "\n== %d ports | %d rule D ==\n untagged %4d (a fetch is enough)\n TAGGED   %4d (needs MacPorts to say which site)\n no-sites %4d\n error    %4d\n",
		len(names), ruleD, untagged, tagged, noSites, evalErr)
}

// few renders a file list short enough to read: cargo names fifty.
func few(files []string) string {
	if len(files) <= 3 {
		return strings.Join(files, " ")
	}
	return fmt.Sprintf("%s … (%d more)", strings.Join(files[:3], " "), len(files)-3)
}

// unfetchedFiles is the files the evaluated checksums name that this
// evaluation's distfiles do not: rule D's population, by name.
func unfetchedFiles(vals info.Values, distfiles []string) []string {
	if len(distfiles) == 0 {
		return nil
	}
	fetched := map[string]bool{}
	for _, f := range distfiles {
		fetched[distName(f)] = true
	}
	var out []string
	for _, g := range intent.ChecksumGroups(vals.Checksums) {
		if g.File != "" && !fetched[g.File] {
			out = append(out, g.File)
		}
	}
	return out
}

// distTag is base's getdistname rule: a distfiles entry may end in
// `:tagname`, binding that file to a tagged master_sites entry, and the
// name is what precedes it. The character class is base's own, and it
// excludes `/` so that a URL in the list keeps its path.
var distTag = regexp.MustCompile(`^(.+):[0-9A-Za-z_-]+$`)

func distName(entry string) string {
	if m := distTag.FindStringSubmatch(entry); m != nil {
		return m[1]
	}
	return entry
}

// hasDefaultSite reports whether a master_sites list has an entry bound
// to no tag — the group a file with no tag binding of its own is fetched
// from.
func hasDefaultSite(sites []string) bool {
	for _, s := range sites {
		if !tagged(s) {
			return true
		}
	}
	return false
}

// tagged reports whether one master_sites entry binds itself to a tag,
// by base's own two rules in fetch_common.tcl's checksites.
//
// A full URL — anything matching a scheme — is tagged only when it ends
// in `:tagname` after that scheme. Anything else is a MIRROR MACRO, and
// base splits it on colons into at most mirrors, subdir and tag: so
// `sourceforge:libpng` is a macro with a subdirectory and NO tag, while
// `gnu:/gcc/gcc-4.2.1:gnu` is a macro with both. Reading the last colon
// of either shape as a tag counts every macro-with-subdir in the tree
// as tagged, which is most of them.
func tagged(site string) bool {
	if schemed.MatchString(site) {
		return taggedURL.MatchString(site)
	}
	return strings.Count(site, ":") >= 2
}

var (
	schemed   = regexp.MustCompile(`^[a-zA-Z]+://`)
	taggedURL = regexp.MustCompile(`^[a-zA-Z]+://.+/?:[0-9A-Za-z_-]+$`)
)
