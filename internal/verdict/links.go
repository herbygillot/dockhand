package verdict

import (
	"sort"

	"github.com/herbygillot/dockhand/internal/verify"
)

// The link proof: whether a dependent this change proposed to revbump
// actually binds to the library that moved.
//
// It is the one thing that tells a declared dependency from a real one.
// The index says gdal declares port:libwidget; only the linker says
// whether anything gdal installed recorded libwidget's install name. A
// proposal rests on the declaration, because that is all there is
// before the build; the pull request rests on this, because by then the
// dependent has been rebuilt against the new library and the answer is
// a fact rather than an inference.
//
// A dependent that installed and links nothing that MOVED is build-only
// in fact as far as this change is concerned, whatever its depends_*
// fields said. That is worth saying in the body rather than quietly
// dropping: the revbump was still spent, and a reviewer deciding
// whether it was needed is exactly who the line is for.
//
// What moved is the question and not what is published. A headline that
// publishes libwidget.3.dylib and an untouched libwidgetx.1.dylib has
// dependents of both, and a proof taken over everything it publishes
// would print "gdal links against libwidgetx.1.dylib" under a heading
// that says gdal was revbumped because a library moved. The caller
// narrows the names with ABI.Broke before asking.

// installNames lists the install names one installation publishes, once
// each and sorted.
//
// It reads install names and never paths, for the reason ABIDelta does:
// three files can announce one name, and one file — p11-kit-proxy —
// announces a name that is not its own. What a dependent recorded is
// the name.
//
// It is unexported, and that is the fix rather than an accident of
// scope: it was the argument LinkProof used to be called with, and
// asking a dependent about everything a headline publishes is the
// question that made a link proof unearned. ABI.Broke is what a caller
// wants; this stays as the reader those tests are written against.
func installNames(m *verify.Manifest) []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range m.Dylibs {
		if d.InstallName == "" || seen[d.InstallName] {
			continue
		}
		seen[d.InstallName] = true
		out = append(out, d.InstallName)
	}
	sort.Strings(out)
	return out
}

// Binding is one dependent's answer to "did it actually link".
type Binding struct {
	// Port is the dependent the answer is about.
	Port string
	// Linked says at least one file this dependent installed records one
	// of the headline's install names.
	Linked bool
	// Lines are the bindings in the words a reader can check, sorted.
	//
	// Empty is an answer and not an absence: it says the sweep ran and
	// found nothing, which is what makes the build-only claim a
	// measurement. The note's own Links field is written without
	// omitempty for the same reason — a missing key would mean nobody
	// looked, and these two absences must stay tellable apart.
	Lines []string
}

// LinkProof asks whether one dependent's installation binds to any of
// the install names it is handed.
//
// The names are the headline's broken ones and the map is this
// dependent's own: the provider gathers every install name an
// installation records, because the whole installation is only present
// at once inside the environment that built it, and the filtering
// happens here because which library this change is about is the
// judgment. A dependent links against libSystem too, and a proof that
// said so would be answering a question nobody asked.
func LinkProof(port string, names []string, links map[string][]string) Binding {
	b := Binding{Port: port}
	for _, name := range names {
		for _, file := range links[name] {
			b.Lines = append(b.Lines, file+" links against "+name)
		}
	}
	sort.Strings(b.Lines)
	b.Linked = len(b.Lines) > 0
	return b
}
