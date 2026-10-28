package sourcecompare

// The wording of a CMakeLists.txt's change: its options, the packages it
// finds, and where in it the change is (Codex's review of 8e314b96, which
// split it from compare.go when its matching moved to textedit).

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// cmakeNamed is how many of a CMakeLists.txt's changes are said before
// the rest are counted.
const cmakeNamed = 5

// cmakeWords say what a CMakeLists.txt's change declares: its options and
// the packages it finds. A change to an option the Portfile sets (sets)
// leads, and is never counted away: duckdb 2.0.0 dropped
// ENABLE_EXTENSION_AUTOLOADING, which the Portfile passes as -D…=1, and
// it fell into "and 7 more" behind the options added (field testing,
// 2026-10-21). The rest keep their order, and the count.
func cmakeWords(old, now project.CMakeDocument, sets func(string) bool) string {
	before, after := old.Facts(), now.Facts()
	var lead, said []string
	statements := now.Statements(nil, nil)
	for _, name := range slices.Sorted(maps.Keys(after.Options)) {
		option, was := after.Options[name], before.Options[name]
		switch _, had := before.Options[name]; {
		case !had:
			by := option.Default
			if by == "ON" || by == "OFF" {
				by = strings.ToLower(by)
			}
			words := fmt.Sprintf("option %s added, %s by default", name, by)
			gating, portfiles := cmakeGated(statements, name, sets)
			if gating != "" {
				words += ", gating " + gating
			}
			if portfiles {
				lead = append(lead, words)
			} else {
				said = append(said, words)
			}
		case was.Default != option.Default && sets(name):
			lead = append(lead, fmt.Sprintf("option %s, which the Portfile sets, has its default move from %s to %s", name, was.Default, option.Default))
		case was.Default != option.Default:
			said = append(said, fmt.Sprintf("option %s's default moves from %s to %s", name, was.Default, option.Default))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(before.Options)) {
		if _, has := after.Options[name]; !has {
			if sets(name) {
				lead = append(lead, "option "+name+", which the Portfile sets, was removed")
				continue
			}
			said = append(said, "option "+name+" removed")
		}
	}
	find := func(packages []project.CMakePackage, name string) (project.CMakePackage, bool) {
		for _, pkg := range packages {
			if pkg.Name == name {
				return pkg, true
			}
		}
		return project.CMakePackage{}, false
	}
	under := func(pkg project.CMakePackage) string {
		if len(pkg.Under) == 0 {
			return ""
		}
		return ", under " + strings.Join(pkg.Under, " and ")
	}
	seen := map[string]bool{}
	for _, pkg := range after.Packages {
		if seen[pkg.Name] {
			continue
		}
		seen[pkg.Name] = true
		was, had := find(before.Packages, pkg.Name)
		switch {
		case !had:
			said = append(said, fmt.Sprintf("find_package(%s) added%s", pkg.Name, under(pkg)))
		case was.Version != pkg.Version && pkg.Version != "":
			said = append(said, fmt.Sprintf("find_package(%s) now asks for %s%s", pkg.Name, pkg.Version, under(pkg)))
		case !was.Required && pkg.Required:
			said = append(said, fmt.Sprintf("find_package(%s) is now REQUIRED%s", pkg.Name, under(pkg)))
		case strings.Join(was.Under, " and ") != strings.Join(pkg.Under, " and "):
			said = append(said, fmt.Sprintf("find_package(%s) moves%s", pkg.Name, cmp.Or(under(pkg), ", out of its if()")))
		}
	}
	for _, pkg := range before.Packages {
		if _, has := find(after.Packages, pkg.Name); !has && !seen[pkg.Name] {
			seen[pkg.Name] = true
			said = append(said, fmt.Sprintf("find_package(%s) dropped", pkg.Name))
		}
	}
	switch {
	case len(lead) == 0 && len(said) == 0:
		return ", though no option or find_package did"
	case len(said) > cmakeNamed:
		return fmt.Sprintf(": %s; and %d more", strings.Join(append(lead, said[:cmakeNamed]...), "; "), len(said)-cmakeNamed)
	}
	return ": " + strings.Join(append(lead, said...), "; ")
}

// cmakeGated names the commands an option's if()s gate, by name and
// file, the first cmakeGates of them and a count of the rest: "gating
// duckdb_extension_statically_link in extension/extension_build_tools.cmake".
// It says what the option turns on, and claims nothing of what the build
// lost: duckdb 2.0.0's LINK_CORE_EXTENSIONS, off by default, gates the
// linking its default build had done (field testing, 2026-10-21). set(),
// and list() and string(), which set variables as it does, message(), and
// the blocks' own heads aren't named, nor a find_package,
// said with the packages. portfiles reports a
// gated command in a loop over a list the Portfile sets, as duckdb's
// BUILD_EXTENSIONS, which then leads.
func cmakeGated(statements []project.CMakeStatement, option string, sets func(string) bool) (gating string, portfiles bool) {
	var named []string
	for _, statement := range statements {
		switch statement.Name() {
		case "set", "unset", "list", "string", "message", "if", "elseif", "else", "endif", "foreach", "endforeach", "while", "endwhile",
			"option", "cmake_dependent_option", "return", "continue", "break", "find_package":
			// A find_package is said with the packages, under its option.
			continue
		}
		if !slices.ContainsFunc(statement.Under, func(c project.CMakeCondition) bool { return gates(c, option) }) {
			continue
		}
		for _, loop := range statement.Loops {
			if slices.ContainsFunc(loop.Names, sets) {
				portfiles = true
			}
		}
		where := statement.Name()
		if statement.File != "" {
			where += " in " + statement.File
		}
		if !slices.Contains(named, where) {
			named = append(named, where)
		}
	}
	if len(named) > cmakeGates {
		return fmt.Sprintf("%s, and %d more", strings.Join(named[:cmakeGates], ", "), len(named)-cmakeGates), portfiles
	}
	return strings.Join(named, ", "), portfiles
}

// gates reports a condition that holds where option is on: one testing it
// as itself, not an else() of it, nor NOT it.
func gates(condition project.CMakeCondition, option string) bool {
	if !condition.Tests(option) {
		return false
	}
	fields := strings.Fields(strings.NewReplacer("(", " ", ")", " ").Replace(condition.Text))
	for i, field := range fields {
		if (field == option || field == "${"+option+"}") && i > 0 && fields[i-1] == "NOT" {
			return false
		}
	}
	return true
}

// cmakeGates is how many of the commands an added option gates are named.
const cmakeGates = 2

// cmakeElsewhere says where a CMakeLists.txt changed beside what it
// declares, its options and the packages it finds, among what the default
// build reaches, by the if() each change is under, so a hold names what
// it's for: fluent-bit 5.1.3's read as held for FLB_PROTOBUF_ENCODER,
// which the person had just decided holds nothing, where it held for
// lines under if(FLB_ALL) and a block re-gated on FLB_AVRO_ENCODER OR
// FLB_PROTOBUF_ENCODER (the dogfood run with 11d1df2f). The version it
// names isn't a change, nor are its comments.
func cmakeElsewhere(old, now project.CMakeDocument, versions Versions, named func(string) bool) string {
	return cmakePlaces(cmakeWhere(versioned(old.Rest(old.Off(named)), versions), now.Rest(now.Off(named))))
}

// versioned are statements with the version an update moves from spelled
// as the one it moves to, whole or in parts, so the version they name
// isn't a change.
func versioned(statements []project.CMakeStatement, versions Versions) []project.CMakeStatement {
	spelled := make([]project.CMakeStatement, len(statements))
	for i, statement := range statements {
		statement.Text = withVersion(statement.Text, versions)
		statement.Under = slices.Clone(statement.Under)
		for j := range statement.Under {
			statement.Under[j].Text = withVersion(statement.Under[j].Text, versions)
		}
		spelled[i] = statement
	}
	return spelled
}

// withVersion is a CMakeLists.txt's text with the version an update moves
// from spelled as the one it moves to, whole or in parts, so the version
// it names isn't a change.
func withVersion(text string, versions Versions) string {
	if versions.Old != "" && versions.New != "" {
		return project.VersionPartsAs(strings.ReplaceAll(text, versions.Old, versions.New), versions.Old, versions.New)
	}
	return text
}

// cmakePlace is where a CMakeLists.txt changes: the outermost if() its
// statements are under, empty for none, as a person finds the block, the
// file it's in, empty for the CMakeLists.txt itself, and each if() they're
// within.
type cmakePlace struct {
	Condition string
	File      string
	Within    []project.CMakeCondition
}

// cmakeWhere are where a CMakeLists.txt's changed statements are, each
// place once, in the order the new document has them and then the old.
// Statements are compared with the conditions they're under, as the
// document reads them, so an endif() pairs only with its own block's:
// fluent-bit 5.1.3's blocks added after if(FLB_UTF8_ENCODER)'s read as
// under it, its endif() paired with theirs (the dogfood run with
// 91340a56).
func cmakeWhere(was, rest []project.CMakeStatement) []cmakePlace {
	keys := func(statements []project.CMakeStatement) []string {
		keyed := make([]string, len(statements))
		for i, statement := range statements {
			var under []string
			for _, condition := range statement.Under {
				under = append(under, condition.Text)
			}
			keyed[i] = statement.File + "\x02" + strings.Join(under, "\x00") + "\x01" + statement.Text
		}
		return keyed
	}
	gone, come := changedLines(keys(was), keys(rest))
	var where []cmakePlace
	for _, changed := range []struct {
		statements []project.CMakeStatement
		at         []int
	}{{rest, come}, {was, gone}} {
		for _, i := range changed.at {
			statement := changed.statements[i]
			place := cmakePlace{File: statement.File, Within: statement.Under}
			if len(statement.Under) > 0 {
				place.Condition = statement.Under[0].Text
			}
			at := slices.IndexFunc(where, func(p cmakePlace) bool { return p.Condition == place.Condition && p.File == place.File })
			if at < 0 {
				where = append(where, place)
				continue
			}
			for _, condition := range statement.Under {
				if !slices.ContainsFunc(where[at].Within, func(c project.CMakeCondition) bool { return c.Text == condition.Text }) {
					where[at].Within = append(where[at].Within, condition)
				}
			}
		}
	}
	return where
}

// cmakePlaces says the conditions changes are under, "under if(FLB_ALL)"
// or "outside any if()", with the file a change is in where it's one the
// CMakeLists.txt include()s, the first five, and how many more.
func cmakePlaces(places []cmakePlace) string {
	var where []string
	for _, place := range places {
		words := "outside any if()"
		if place.Condition != "" {
			words = "under if(" + place.Condition + ")"
		}
		if place.File != "" {
			words += " in " + place.File
		}
		where = append(where, words)
	}
	if len(where) > cmakeNamed {
		return fmt.Sprintf("%s, and in %d more places", strings.Join(where[:cmakeNamed], ", "), len(where)-cmakeNamed)
	}
	return strings.Join(where, ", ")
}

// changedLines are the lines of each side a longest common subsequence
// leaves out: those gone from the old, and those come in the new. Past
// what's cheap to compare line by line, the lines between the common
// start and end are all said.
func changedLines(old, now []string) (gone, come []int) {
	from, _ := textedit.Match(len(old), len(now), func(i, j int) bool { return old[i] == now[j] })
	for j, i := range from {
		if i < 0 {
			come = append(come, j)
		}
	}
	return textedit.Unmatched(len(old), from), come
}

// cmakeOptionsOnly says a CMakeLists.txt's change where all it does that
// the default build reads, but the version it names, is add options, each
// built as its default: the person decided an added option holds nothing,
// and that what the default build doesn't reach holds nothing, as a
// branch whose condition is false where each option off by default that
// nothing turns on is OFF (project.CMakeOff): fluent-bit 5.1.3's
// find_package(Protobuf) under FLB_PROTOBUF_ENCODER, and its lines under
// if(FLB_ALL) (D12, revisited 2026-10-01). What an option on by default
// gates holds, as do a default that flips and an option removed; so does
// a change under an option the Portfile names, in any variant, or the
// file sets. False where anything else changed.
func cmakeOptionsOnly(name string, old, now project.CMakeDocument, versions Versions, named func(string) bool) (string, bool) {
	before, after := old.Facts(), now.Facts()
	added := map[string]bool{}
	for option := range after.Options {
		if _, had := before.Options[option]; !had {
			added[option] = true
		}
	}
	offOld, offNew := old.Off(named), now.Off(named)
	rest, was := project.StatementsText(now.Without(added, offNew)), project.StatementsText(old.Without(nil, offOld))
	if rest != was {
		if _, ok := versionOnly(name, []byte(was), []byte(rest), versions); !ok {
			return "", false
		}
	}
	var said []string
	for _, option := range slices.Sorted(maps.Keys(added)) {
		by := after.Options[option].Default
		if by == "ON" || by == "OFF" {
			by = strings.ToLower(by)
		}
		words := fmt.Sprintf("option %s, %s by default", option, by)
		var gated []string
		for _, pkg := range after.Packages {
			if offNew[option] && slices.Contains(pkg.Under, option) {
				gated = append(gated, "find_package("+pkg.Name+")")
			}
		}
		if len(gated) > 0 {
			words += ", which gates " + strings.Join(gated, ", ")
		}
		said = append(said, words)
	}
	// Where else it changed is said, with what leaves it unreached; what
	// an added option's own block gates is said with the option.
	addedOff := map[string]bool{}
	for option := range added {
		if offNew[option] {
			addedOff[option] = true
		}
	}
	places := cmakeWhere(versioned(old.Without(nil, nil), versions), now.Without(added, addedOff))
	var words []string
	if len(said) > 0 {
		words = append(words, "adds "+strings.Join(said, "; "))
	}
	if len(places) > 0 {
		// The options named are those off that an if() over a change
		// tests, an else()'s negated test aside.
		tested := func(option string) bool {
			return slices.ContainsFunc(places, func(place cmakePlace) bool {
				return slices.ContainsFunc(place.Within, func(condition project.CMakeCondition) bool { return condition.Tests(option) })
			})
		}
		var gates []string
		for option := range offNew {
			if tested(option) {
				gates = append(gates, option)
			}
		}
		for option := range offOld {
			if !offNew[option] && tested(option) {
				gates = append(gates, option)
			}
		}
		slices.Sort(gates)
		reach := "changes only what the default build doesn't reach, " + cmakePlaces(places)
		if len(gates) > 0 {
			reach += ": neither it nor the Portfile turns " + orList(gates) + " on"
		}
		if len(words) > 0 {
			reach = "otherwise " + reach
		}
		words = append(words, reach)
	}
	switch {
	case len(words) == 0:
		return "changes only comments and layout, which the build doesn't read", true
	case len(places) == 0:
		words[0] += ", and changes nothing else the default build reads"
	}
	what := strings.Join(words, "; ")
	if len(said) > 0 {
		what += "; each option builds as its default"
	}
	return what, true
}
