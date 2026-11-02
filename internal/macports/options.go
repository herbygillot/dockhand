package macports

import (
	"slices"
	"strings"
)

// The Portfile options dockhand reads, and the subsets its rules name, in
// one place: the evaluator is given ReadOptions, so the Tcl side never
// restates the list; the fidelity check, discovery, and source
// interpretation take their lists from here; and a test holds each subset
// to the options that are actually read, mportinfo's keys and the
// evaluator's computed options among them.

// ReadOptions are the options the evaluator reads from a port beyond what
// mportinfo reports (name, version, revision, epoch, homepage, description,
// categories, license, dependencies, variants, subports, and platforms).
var ReadOptions = []string{
	"checksums", "distfiles", "distname", "dist_subdir", "extract.only", "extract.rename", "extract.mkdir", "worksrcdir", "filespath", "master_sites", "fetch.type",
	"fetch.user_agent", "fetch.ignore_sslcert",
	"patchfiles", "patch.pre_args", "patch.args", "patch.dir",
	"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version", "livecheck.branch", "livecheck.ignore_sslcert", "livecheck.compression", "livecheck.curloptions",
	"go.vendors", "go.version", "go.package", "go.domain", "go.offline_build", "go.toolchain_min", "go.bin",
	"cargo.crates", "cargo.crates_github", "cargo.update", "cargo.dir", "cargo.offline_cmd", "build.cmd", "build.pre_args", "build.dir",
	"cmake.source_dir", "configure.dir",
	"github.author", "github.project", "github.version", "github.tag_prefix", "github.tag_suffix", "github.tarball_from",
	"gitlab.author", "gitlab.project", "gitlab.version", "gitlab.tag_prefix", "gitlab.tag_suffix", "gitlab.instance",
	"git.url", "git.branch",
	"python.versions", "python.version", "python.default_version", "python.rootname", "python.pep517_backend",
	"use_xcode", "replaced_by", "known_fail", "supported_archs", "use_configure", "configure.cmd", "macosx_deployment_target",
	"compiler.cxx_standard", "build.args", "conflicts", "use_parallel_build", "python.pep517",
	// What check --plan -v prints of a target's evaluation (field
	// testing's curl +sectrust notes, note 8b), which the fidelity check
	// leaves out (fidelity.ComparablePort).
	"configure.args", "configure.env",
}

// EvaluationOnlyOptions are the options read for check --plan -v to print
// alone, which no comparison of two evaluations reads: their values name
// ${workpath}, which is named for where the Portfile was evaluated.
var EvaluationOnlyOptions = []string{"configure.args", "configure.env"}

// LivecheckOptions are the livecheck options a forge source's discovery
// reads; LivecheckListingOptions are those an archive source's listing
// discovery reads, curl behavior included.
var (
	LivecheckOptions        = []string{"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version"}
	LivecheckListingOptions = []string{"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version", "livecheck.ignore_sslcert", "livecheck.compression", "livecheck.curloptions", "dockhand.livecheck_standard"}
)

// InertOptions are the options whose values no build reads, so a change to
// them builds nothing differently, and livecheck's (InertOption): the
// metadata MacPorts' revision rule asks no bump for. Not license, which
// decides whether a dependent's check installs a kept archive
// (archivefetch only for a distributable one), nor platforms or notes,
// which PortGroups and the receipt read.
var InertOptions = []string{"maintainers", "categories", "description", "long_description", "homepage"}

// inertPrefix is livecheck's options, livecheck.type, livecheck.url, and
// the rest, which a check never runs.
const inertPrefix = "livecheck."

// InertOption reports whether an option, by its name, is one whose value
// no build reads (InertOptions), or livecheck's.
func InertOption(name string) bool {
	return slices.Contains(InertOptions, name) || strings.HasPrefix(name, inertPrefix) && len(name) > len(inertPrefix)
}

// ForgeOptions are the options a forge PortGroup's source interpretation
// reads, for the github or gitlab prefix.
func ForgeOptions(prefix string) []string {
	return []string{prefix + ".author", prefix + ".project", prefix + ".version", prefix + ".tag_prefix", prefix + ".tag_suffix", "git.branch"}
}

// PortGroupAssumption is what dockhand relies on a PortGroup's version to
// define, by name: the options it reads (ReadOptions) and the procedures
// it calls or matches. The worker checks each after the port is
// evaluated, and an absence is an option error, so a rule that reads it is
// inconclusive rather than wrong; an entry with neither says the version
// was checked and nothing of it is assumed. Feeds are the facts the
// evaluator reads through a procedure, by its name: an absent procedure's
// option error is each of its facts' as well, so a rule reading the fact
// is inconclusive rather than reading no value as the answer.
type PortGroupAssumption struct {
	Name, Version string
	Options       []string
	Procs         []string
	Feeds         map[string][]string
}

// PortGroupAssumptions are the PortGroups dockhand's code assumes
// something of (architecture's PortGroup assumptions inventory,
// 2026-10-30), checked against macports-ports' _resources at the corpus's
// pinned ee76ef2c and at master's 5a58a7e6f26 of 2026-10-31: an option
// counted here is one a PortGroup declares itself or by a PortGroup it
// loads. A PortGroup of a name here at a version not here is one the
// manifest wasn't checked against (UncheckedPortGroups).
var PortGroupAssumptions = []PortGroupAssumption{
	{Name: "github", Version: "1.0", Options: []string{"github.author", "github.project", "github.version", "github.tag_prefix", "github.tag_suffix", "github.tarball_from"}, Procs: []string{"github.setup"}},
	{Name: "gitlab", Version: "1.0", Options: []string{"gitlab.author", "gitlab.project", "gitlab.version", "gitlab.tag_prefix", "gitlab.tag_suffix", "gitlab.instance"}, Procs: []string{"gitlab.setup"}},
	{Name: "golang", Version: "1.0", Options: []string{"go.vendors", "go.version", "go.package", "go.domain", "go.offline_build", "go.toolchain_min", "go.bin"}, Procs: []string{"go.setup", "go_toolchain.ceiling"}},
	{Name: "rust", Version: "1.0", Options: []string{"cargo.crates", "cargo.crates_github", "cargo.update", "cargo.offline_cmd"}, Procs: []string{"cargo.rust_platform"}},
	{Name: "cargo", Version: "1.0", Options: []string{"cargo.crates", "cargo.crates_github", "cargo.update", "cargo.offline_cmd"}, Procs: []string{"cargo.rust_platform"}},
	{Name: "cargo_fetch", Version: "1.0", Options: []string{"cargo.crates", "cargo.crates_github", "cargo.update", "cargo.dir", "cargo.offline_cmd"}},
	{Name: "python", Version: "1.0", Options: []string{"python.versions", "python.version", "python.default_version", "python.rootname", "python.pep517", "python.pep517_backend"}, Procs: []string{"python_get_default_version"},
		Feeds: map[string][]string{"python_get_default_version": {"dockhand.python_default", "dockhand.python_group_default"}}},
	{Name: "cmake", Version: "1.0"},
	{Name: "cmake", Version: "1.1", Options: []string{"cmake.source_dir"}},
	{Name: "xcodeversion", Version: "1.0", Options: []string{"minimum_xcodeversions"}},
	{Name: "compilers", Version: "1.0", Procs: []string{"fortran_variant_name"}},
	{Name: "mpi", Version: "1.0", Procs: []string{"mpi_variant_name"}},
}

// TclPortGroupAssumptions renders assumptions as the Tcl list the
// evaluator's worker check reads: name, version, options, and procedures,
// one list each, and the facts each procedure feeds, a dictionary of
// procedure to list.
func TclPortGroupAssumptions(assumptions []PortGroupAssumption) string {
	var entries []string
	for _, a := range assumptions {
		var feeds []string
		for _, proc := range a.Procs {
			if facts := a.Feeds[proc]; len(facts) > 0 {
				feeds = append(feeds, proc, tclList(facts...))
			}
		}
		entries = append(entries, tclList(a.Name, a.Version, tclList(a.Options...), tclList(a.Procs...), tclList(feeds...)))
	}
	return tclList(entries...)
}

// UncheckedPortGroups are the PortGroups of groups whose name the manifest
// has and whose version it doesn't: what it assumes of the name wasn't
// checked against that version, which is said, and evaluation goes on.
func UncheckedPortGroups(groups []LoadedPortGroup) []LoadedPortGroup {
	var unchecked []LoadedPortGroup
	for _, group := range groups {
		named, checked := false, false
		for _, a := range PortGroupAssumptions {
			named = named || a.Name == group.Name
			checked = checked || a.Name == group.Name && a.Version == group.Version
		}
		if named && !checked {
			unchecked = append(unchecked, group)
		}
	}
	return unchecked
}
