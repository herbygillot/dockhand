package fetchguard

import "strings"

// AffectsFetch reports whether writing the named option changes what the
// fetch phase does: which archives it fetches and where from, what it
// checks them against, which patches it applies, and how the source is
// named and laid out. These are the fetch phase's inputs and the
// archive-shape options the editor tracks, so a hook that writes none of
// them may fail the fetch but cannot change it, which is the property the
// pre-fetch guard grammar protects. A name with an option suffix, -append
// or -delete, is judged by its option.
func AffectsFetch(option string) bool {
	option = OptionName(option)
	if fetchOptions[option] {
		return true
	}
	for _, prefix := range fetchOptionPrefixes {
		if strings.HasPrefix(option, prefix) {
			return true
		}
	}
	return false
}

// OptionName is the option an option command names: the command with its
// -append, -prepend, -delete, -replace, or -strsed suffix removed.
func OptionName(command string) string {
	if i := strings.LastIndexByte(command, '-'); i > 0 {
		switch command[i:] {
		case "-append", "-prepend", "-delete", "-replace", "-strsed":
			return command[:i]
		}
	}
	return command
}

// fetchOptions are the options whose value the fetch reads, by name; the
// port's identity is among them because the archive's default name is
// composed from it.
var fetchOptions = map[string]bool{
	"name": true, "version": true, "epoch": true, "distname": true, "distfiles": true, "dist_subdir": true, "distpath": true,
	"master_sites": true, "master_sites.mirror_subdir": true, "checksums": true, "patchfiles": true, "patch_sites": true, "filespath": true,
	"worksrcdir": true, "extract.suffix": true, "extract.mkdir": true, "go.vendors": true, "cargo.crates": true, "cargo.crates_github": true,
	"mirror_sites": true, "worksrcpath": true,
}

// fetchOptionPrefixes are the option families the fetch reads whole: the
// fetch and extract settings, the archive types, and the version control
// and forge sources.
var fetchOptionPrefixes = []string{"fetch.", "extract.", "use_", "git.", "svn.", "hg.", "bzr.", "cvs.", "github.", "gitlab.", "bitbucket.", "sourceforge."}
