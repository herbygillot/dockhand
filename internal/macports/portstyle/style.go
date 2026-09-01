package portstyle

// Type identifies the style that produced a location. The set is closed
// and empirically grounded: each entry's argument position was verified
// against real Portfiles before being trusted.
type Type int

const (
	// None is the zero value: no style. It never appears in the
	// style table; it exists so an unset Type field cannot be mistaken
	// for a real style (VersionLine held the zero before).
	None Type = iota
	// VersionLine is a literal version command: version at word 1.
	VersionLine
	// RevisionLine is a literal revision command: revision at word 1.
	RevisionLine
	// GoSetup is go.setup <package> <version> [tag-prefix] [tag-suffix].
	GoSetup
	// GithubSetup is github.setup <author> <project> <version> [tag-prefix] [tag-suffix].
	GithubSetup
	// GitlabSetup is gitlab.setup <author> <project> <version> [tag-prefix] [tag-suffix].
	GitlabSetup
	// BitbucketSetup is bitbucket.setup <author> <project> <version> [tag-prefix].
	BitbucketSetup
	// Perl5Setup is perl5.setup <module> <version> [path].
	Perl5Setup
	// RubySetup is ruby.setup <module> <version> [type ...].
	RubySetup
	// RSetup is R.setup <domain> <author> <package> <version> [prefix].
	RSetup
	// PureSetup is pure.setup <module> <version>.
	PureSetup
	// AspellDictSetup is aspelldict.setup <lang> <version> <description> <index>.
	AspellDictSetup
	// The following were mined from the proc signatures of every PortGroup
	// in the tree (macports/testdata/portgroups); each word index is the
	// version parameter's position in the proc's argument spec.

	// CgitSetup is cgit.setup <url> <project> <version> [tag-prefix] [tag-suffix].
	CgitSetup
	// CodebergSetup is codeberg.setup <author> <project> <version> [tag-prefix] [tag-suffix].
	CodebergSetup
	// CrossBinutilsSetup is crossbinutils.setup <target> <version>.
	CrossBinutilsSetup
	// CrossGccSetup is crossgcc.setup <target> <version>.
	CrossGccSetup
	// CrossGdbSetup is crossgdb.setup <target> <version>.
	CrossGdbSetup
	// ElpaSetup is elpa.setup <name> <version> [repo].
	ElpaSetup
	// GiteaSetup is gitea.setup <author> <project> <version> [tag-prefix] [tag-suffix].
	GiteaSetup
	// GoToolchainSetup is go_toolchain.setup <version> [label].
	GoToolchainSetup
	// HunspellDictSetup is hunspelldict.setup <locale> <version> <lang> [source].
	HunspellDictSetup
	// LuarocksSetup is luarocks.setup <module> <version> [type] [docs] [source] [implementation].
	LuarocksSetup
	// NotabugSetup is notabug.setup <author> <project> <version> [tag-prefix] [tag-suffix].
	NotabugSetup
	// OctaveSetup is octave.setup <repo> <author> [module] [version] [tag-prefix] [tag-suffix].
	OctaveSetup
	// SourcehutSetup is sourcehut.setup <author> <project> <version> [tag-prefix] [tag-suffix].
	SourcehutSetup
	// X11FontSetup is x11font.setup <portname> <version> <fontsubdir>.
	X11FontSetup
	// ZigToolchainSetup is zig_toolchain.setup <version>.
	ZigToolchainSetup
	// SetVariable is a Tcl variable carrying the version: set <name>
	// <version>, with version (or a setup command) reading it back. The
	// weakest style in the table — any set whose value happens to equal
	// the version corroborates — so it is held to two extra rules in
	// Locate: a corroborated non-set carrier always outranks it, and it
	// never enters a decline's candidate list, where the counterfactual
	// probe would otherwise chase coincidences.
	SetVariable
)

func (t Type) String() string {
	switch t {
	case None:
		return "no style"
	case VersionLine:
		return "version"
	case RevisionLine:
		return "revision"
	case GoSetup:
		return "go.setup"
	case GithubSetup:
		return "github.setup"
	case GitlabSetup:
		return "gitlab.setup"
	case BitbucketSetup:
		return "bitbucket.setup"
	case Perl5Setup:
		return "perl5.setup"
	case RubySetup:
		return "ruby.setup"
	case RSetup:
		return "R.setup"
	case PureSetup:
		return "pure.setup"
	case AspellDictSetup:
		return "aspelldict.setup"
	case CgitSetup:
		return "cgit.setup"
	case CodebergSetup:
		return "codeberg.setup"
	case CrossBinutilsSetup:
		return "crossbinutils.setup"
	case CrossGccSetup:
		return "crossgcc.setup"
	case CrossGdbSetup:
		return "crossgdb.setup"
	case ElpaSetup:
		return "elpa.setup"
	case GiteaSetup:
		return "gitea.setup"
	case GoToolchainSetup:
		return "go_toolchain.setup"
	case HunspellDictSetup:
		return "hunspelldict.setup"
	case LuarocksSetup:
		return "luarocks.setup"
	case NotabugSetup:
		return "notabug.setup"
	case OctaveSetup:
		return "octave.setup"
	case SourcehutSetup:
		return "sourcehut.setup"
	case X11FontSetup:
		return "x11font.setup"
	case ZigToolchainSetup:
		return "zig_toolchain.setup"
	case SetVariable:
		return "set variable"
	}
	return "unknown style"
}

// styleSpec is one row of a field's style table: the command carrying
// the field, the word index holding the value (word 0 is the command
// name itself), and the transform the style's PortGroup applies when
// the literal is not the evaluated value verbatim — corroboration
// compares through it, identity when nil.
type styleSpec struct {
	style     Type
	command   string
	word      int
	transform func(string) string
}

// revisionStyles is FieldRevision's table: the literal revision line is
// the only style.
var revisionStyles = []styleSpec{
	{RevisionLine, "revision", 1, nil},
}

// versionStyles is FieldVersion's table.
var versionStyles = []styleSpec{
	{VersionLine, "version", 1, nil},
	{GoSetup, "go.setup", 2, nil},
	{GithubSetup, "github.setup", 3, nil},
	{GitlabSetup, "gitlab.setup", 3, nil},
	{BitbucketSetup, "bitbucket.setup", 3, nil},
	{Perl5Setup, "perl5.setup", 2, perl5ConvertVersion},
	{RubySetup, "ruby.setup", 2, nil},
	{RSetup, "R.setup", 4, nil},
	{PureSetup, "pure.setup", 2, nil},
	{AspellDictSetup, "aspelldict.setup", 2, nil},
	{CgitSetup, "cgit.setup", 3, nil},
	{CodebergSetup, "codeberg.setup", 3, nil},
	{CrossBinutilsSetup, "crossbinutils.setup", 2, nil},
	{CrossGccSetup, "crossgcc.setup", 2, nil},
	{CrossGdbSetup, "crossgdb.setup", 2, nil},
	{ElpaSetup, "elpa.setup", 2, nil},
	{GiteaSetup, "gitea.setup", 3, nil},
	{GoToolchainSetup, "go_toolchain.setup", 1, nil},
	{HunspellDictSetup, "hunspelldict.setup", 2, nil},
	{LuarocksSetup, "luarocks.setup", 2, nil},
	{NotabugSetup, "notabug.setup", 3, nil},
	{OctaveSetup, "octave.setup", 4, nil},
	{SourcehutSetup, "sourcehut.setup", 3, nil},
	{X11FontSetup, "x11font.setup", 2, nil},
	{ZigToolchainSetup, "zig_toolchain.setup", 1, nil},
	{SetVariable, "set", 2, nil},
}

// Transformed reports whether the style writes its literal in a form
// other than the evaluated value — a PortGroup transform sits between,
// so the evaluated value cannot simply be written back into the span.
func (t Type) Transformed() bool {
	for _, s := range versionStyles {
		if s.style == t {
			return s.transform != nil
		}
	}
	return false
}
