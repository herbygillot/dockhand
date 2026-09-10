package bump

import (
	"slices"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// namedNotFetched is the files a context's checksums NAME that it does
// not fetch and no vendored block supplies: the port's own recorded
// distfiles that this evaluation never retrieves.
//
// It is the terraform shape. distname is set twice, arm64 last, so
// distfiles offers one file while the checksums command still records
// both architectures. Those digests describe the release the port is
// leaving, and a bump that moved the version and left them would ship a
// Portfile whose amd64 entry is a lie — which is the refusal this
// exists to stop having to make.
//
// Order is the checksums command's own, which is what makes the before
// and after lists pair up: MacPorts evaluates the same command in both
// contexts, so the nth file named there before the edit is the nth
// after it.
func namedNotFetched(checksumTokens, fetched, supplied []string) []string {
	var out []string
	for _, g := range intent.ChecksumGroups(checksumTokens) {
		if g.File == "" || slices.Contains(fetched, g.File) ||
			slices.Contains(supplied, g.File) || slices.Contains(out, g.File) {
			continue
		}
		out = append(out, g.File)
	}
	return out
}

// rederivable pairs the unfetched names before an edit with the ones
// after it, and reports whether every one of them can actually be
// reached.
//
// THREE THINGS HAVE TO HOLD and each failure keeps the existing refusal
// rather than inventing an answer:
//
//   - The two lists are the same length. A checksums command that names
//     a different number of files after the edit is a command whose
//     shape the edit changed, and pairing by position across a change of
//     shape would attach a digest to the wrong file.
//   - Something actually moved. A file whose name is identical on both
//     sides is a pin — cliclick's legacy branch, LyX's older release —
//     and its digests are correct as they stand.
//   - Every new name has somewhere to be fetched from. A port whose
//     every master_sites entry is tagged binds its files to sites
//     through distfiles, and a file absent from distfiles has no
//     binding; measured over the tree, that is 8 ports of 76.
func rederivable(before, after []string, named map[string][]string) ([]string, []string, bool) {
	if len(before) == 0 || len(before) != len(after) {
		return nil, nil, false
	}
	var oldNames, newNames []string
	for i := range before {
		if before[i] == after[i] {
			continue // a pin, correct as it stands
		}
		if len(named[after[i]]) == 0 {
			return nil, nil, false
		}
		oldNames = append(oldNames, before[i])
		newNames = append(newNames, after[i])
	}
	return oldNames, newNames, len(oldNames) > 0
}

// namedURLs is the fetch surface for one of those files, in the shape
// the fetcher takes.
func namedURLs(fi info.FetchInfo, file string) []string { return fi.Named[file] }
