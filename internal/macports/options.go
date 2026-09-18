package macports

import "slices"

// The Portfile options dockhand reads, and the subsets its rules name, in
// one place: the evaluator is given ReadOptions, so the Tcl side never
// restates the list; the fidelity check, discovery, and source
// interpretation take their lists from here; and a test holds each subset
// to the options that are actually read.

// ReadOptions are the options the evaluator reads from a port beyond what
// mportinfo reports (name, version, revision, epoch, homepage, description,
// categories, license, dependencies, variants, subports, and platforms).
var ReadOptions = []string{
	"checksums", "distfiles", "distname", "dist_subdir", "extract.only", "extract.rename", "worksrcdir", "filespath", "master_sites", "fetch.type",
	"fetch.user_agent", "fetch.ignore_sslcert",
	"patchfiles", "patch.pre_args", "patch.dir",
	"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version", "livecheck.ignore_sslcert", "livecheck.compression", "livecheck.curloptions",
	"go.vendors", "go.version", "go.package", "go.domain", "go.offline_build", "go.toolchain_min",
	"cargo.crates", "cargo.crates_github", "cargo.update", "cargo.dir", "cargo.offline_cmd",
	"github.author", "github.project", "github.version", "github.tag_prefix", "github.tag_suffix", "github.tarball_from",
	"gitlab.author", "gitlab.project", "gitlab.version", "gitlab.tag_prefix", "gitlab.tag_suffix", "gitlab.instance",
	"git.url", "git.branch",
	"use_xcode", "replaced_by",
}

// InfoOptions are the mportinfo keys dockhand's rules name; the evaluator
// reports them without being asked.
var InfoOptions = []string{"name", "version", "revision", "epoch", "homepage"}

// ComputedOptions are the options the evaluator computes itself, with the
// dockhand prefix or from Base's fetch machinery.
var ComputedOptions = []string{"fetch.has_credentials", "fetch.archive_compatible", "dockhand.livecheck_standard", "dockhand.metadata_only", "dockhand.base_version", "dockhand.livecheck_declared", "livecheck.name"}

// VersionFollowers are the options a version bump may change on the
// selected port: those the source is named by, and a homepage that spells
// the version. Anything else that moves is an unexpected change.
var VersionFollowers = []string{"fetch.has_credentials", "version", "github.version", "gitlab.version", "go.version", "git.branch", "distname", "dist_subdir", "distfiles", "extract.only", "master_sites", "worksrcdir", "livecheck.version", "homepage"}

// LivecheckOptions are the livecheck options a forge source's discovery
// reads; LivecheckListingOptions are those an archive source's listing
// discovery reads, curl behavior included.
var (
	LivecheckOptions        = []string{"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version"}
	LivecheckListingOptions = []string{"livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version", "livecheck.ignore_sslcert", "livecheck.compression", "livecheck.curloptions", "dockhand.livecheck_standard"}
)

// ForgeOptions are the options a forge PortGroup's source interpretation
// reads, for the github or gitlab prefix.
func ForgeOptions(prefix string) []string {
	return []string{prefix + ".author", prefix + ".project", prefix + ".version", prefix + ".tag_prefix", prefix + ".tag_suffix", "git.branch"}
}

// KnownOption reports whether an option name is one the evaluator reports.
func KnownOption(name string) bool {
	return slices.Contains(ReadOptions, name) || slices.Contains(InfoOptions, name) || slices.Contains(ComputedOptions, name)
}
