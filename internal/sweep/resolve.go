package sweep

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// The grammar's prefixed forms. A prefix is tested before a path,
// so a directory literally named "maintainer:foo" cannot shadow the
// grammar — a token with a colon in it is the grammar's, and a user who
// really has such a directory can still name it "./maintainer:foo".
const (
	categoryPrefix   = "category:"
	maintainerPrefix = "maintainer:"
	// allToken sweeps the whole tree, and only when it is the sole
	// argument. Verified on the maintainer's tree: "all" is neither an
	// indexed port name nor a category directory. The sole-argument
	// rule is the cheap mitigation for the day it becomes one — a port
	// named "all" stays reachable by portdir, by category/dir, and
	// alongside any second argument.
	allToken = "all"
	// meToken is the maintainer handle that has to be looked up rather
	// than read.
	meToken = "me"
)

// ErrNoSelector reports a resolution asked for nothing. A caller with a
// usage voice of its own should say so in that voice instead of
// surfacing this.
var ErrNoSelector = errors.New("sweep: no selector given")

// ErrNoMaintainer reports a maintainer token that matches no key in the
// tree's index. It is deliberately an error and not an empty sweep: a
// typo'd handle that resolved to nothing would exit 0 having done
// nothing, which reads as success.
var ErrNoMaintainer = errors.New("sweep: no ports under that maintainer")

// ErrNoIdentity reports that "me" could not be resolved to a maintainer
// key. The remedy is always a named one — authenticate gh, set
// git config user.email, or spell the handle out — never a silent
// fallback to an empty set.
var ErrNoIdentity = errors.New("sweep: cannot tell who you are")

// Sources are the lookups the grammar needs, each supplied by the
// caller that owns it.
//
// Tree is a func and not a *tree.Tree because half the grammar needs no
// tree at all: a portdir path says where the port is without being
// looked up, so `dockhand bump ./devel/foo` must keep working outside a
// ports tree. Opening one is deferred to the first form that needs it,
// and the caller's own error — which knows about --tree and
// DOCKHAND_TREE — is what a user sees.
//
// Login and Email answer "me". They are separate because they are
// different identities: Login is the forge handle behind gh:, Email is
// the git identity behind mail:, and on the maintainer's own tree the
// mail key is the more complete of the two. Either may be nil, which
// makes "me" fail with ErrNoIdentity rather than resolve to half of a
// person.
type Sources struct {
	Tree  func() (*tree.Tree, error)
	Login func(context.Context) (string, error)
	Email func(context.Context) (string, error)
}

// Ambiguity is a bare token that names both a category and a port.
//
// Thirteen tokens do on the maintainer's tree — gnome, kde, lua,
// ocaml, php, ruby and the rest — and the resolution order below reads
// every one of them as the category, which is what `dockhand classify
// php` has always meant. Resolve reports the collision instead of
// deciding what a verb should do about it, because the answer differs
// by verb: a census of 108 ports is a survey, and a bump of 494 is not
// what anybody typing `bump ruby` meant. It also depends on where the
// user is standing: inside lang/, "ruby" is a path and this never
// arises.
type Ambiguity struct {
	// Token is the argument as the user wrote it.
	Token string
	// Category is what the token resolved to: the portdirs of the
	// category, which are what Targets carries.
	Category int
	// Port is what the same token would have named through the index,
	// had the category not won.
	Port tree.Target
}

// Resolution is what a selector expanded to, and what the expansion
// noticed on the way.
type Resolution struct {
	// Targets are the resolved ports. Order is deterministic: the
	// arguments in the order they were written, and within any
	// argument that expands to many ports, lexical by portdir. Literal
	// repeats dedupe; two subports of one Portfile do not — collapsing
	// them is CollapseByPortdir's job, and whether to is the verb's.
	Targets []tree.Target
	// Notes are what the grammar decided and a user should be told:
	// which maintainer keys were used and how many ports each names,
	// near-miss spellings that were NOT folded in, index entries that
	// resolved to no portdir. They are prose for stderr, not output.
	Notes []string
	// Ambiguous holds the bare tokens that named both a category and a
	// port. Empty for every unambiguous invocation.
	Ambiguous []Ambiguity
}

// Resolve expands a selector into targets.
//
// The grammar, in the order a token is tested against it:
//
//	all              the whole tree, and only as the sole argument
//	category:x       every portdir of category x
//	maintainer:h     every port under maintainer h; h may be "me"
//	<portdir path>   a directory holding a Portfile — needs no tree
//	<category>       the bare form of category:x
//	<name>           category/dir, then the PortIndex (subports
//	                 included), then a portdir's directory name
//
// The last three are the order cmd.resolveTargets has always used and
// the prefixed forms are added ahead of them, so nothing a user could
// type before means something else now. Path before category is what
// makes a token resolve to the single port when you are standing in
// its category, and category before name is what makes `classify php`
// sweep 108 ports — an existing behaviour, reported through Ambiguous
// rather than changed here.
func Resolve(ctx context.Context, src Sources, args []string) (Resolution, error) {
	var res Resolution
	if len(args) == 0 {
		return res, ErrNoSelector
	}

	// The tree is opened at most once, on the first form that needs
	// one, and the caller's error is returned unwrapped so its remedy
	// survives.
	var tr *tree.Tree
	needTree := func() (*tree.Tree, error) {
		if tr != nil {
			return tr, nil
		}
		if src.Tree == nil {
			// Not a user's mistake: this selector form needs a tree
			// and the caller wired none. Saying so plainly beats
			// reporting the port as missing.
			return nil, fmt.Errorf("%w: this selector needs a ports tree and none was supplied",
				tree.ErrNoTreeAbove)
		}
		got, err := src.Tree()
		if err != nil {
			return nil, err
		}
		tr = got
		return tr, nil
	}

	seen := make(map[tree.Target]bool)
	add := func(targets ...tree.Target) {
		for _, tgt := range targets {
			if !seen[tgt] {
				seen[tgt] = true
				res.Targets = append(res.Targets, tgt)
			}
		}
	}

	if len(args) == 1 && args[0] == allToken {
		t, err := needTree()
		if err != nil {
			return res, err
		}
		dirs, err := t.Portdirs()
		if err != nil {
			return res, err
		}
		for _, d := range dirs {
			add(tree.Target{Portdir: d})
		}
		return res, nil
	}

	for _, a := range args {
		switch {
		case strings.HasPrefix(a, categoryPrefix):
			t, err := needTree()
			if err != nil {
				return res, err
			}
			dirs, err := categoryPortdirs(t, strings.TrimPrefix(a, categoryPrefix))
			if err != nil {
				return res, err
			}
			for _, d := range dirs {
				add(tree.Target{Portdir: d})
			}

		case strings.HasPrefix(a, maintainerPrefix):
			t, err := needTree()
			if err != nil {
				return res, err
			}
			targets, notes, err := maintained(ctx, src, t, strings.TrimPrefix(a, maintainerPrefix))
			if err != nil {
				return res, err
			}
			res.Notes = append(res.Notes, notes...)
			add(targets...)

		default:
			if tgt, ok := tree.PathTarget(a); ok {
				add(tgt)
				continue
			}
			t, err := needTree()
			if err != nil {
				return res, err
			}
			if cat, ok := categoryName(t, a); ok {
				dirs, err := t.CategoryPortdirs(cat)
				if err != nil {
					return res, err
				}
				// The same token may also be an indexed port. The
				// category wins, as it always has; the collision is
				// reported so a verb that should not silently sweep
				// hundreds of ports can refuse.
				if alt, err := t.Resolve(a); err == nil {
					res.Ambiguous = append(res.Ambiguous, Ambiguity{
						Token: a, Category: len(dirs), Port: alt})
				}
				for _, d := range dirs {
					add(tree.Target{Portdir: d})
				}
				continue
			}
			tgt, err := t.Resolve(a)
			if err != nil {
				return res, err
			}
			add(tgt)
		}
	}
	return res, nil
}

// categoryPortdirs expands an explicit category:x form, which must name
// a category — an unknown one is an error, never an empty sweep.
func categoryPortdirs(t *tree.Tree, name string) ([]string, error) {
	cat, ok := categoryName(t, name)
	if !ok {
		return nil, fmt.Errorf("%q: %w (no such category in %s)", name, tree.ErrPortNotFound, t.Root())
	}
	return t.CategoryPortdirs(cat)
}

// categoryName reports whether ref names a category of the tree, and
// returns the category as the tree spells it.
//
// The cleaning is what makes the path forms of a category work.
// PathTarget answers only for a directory holding a Portfile, so a
// category directory resolves as neither a path nor — before this — a
// category: "lang/" was accepted because the trailing slash survives a
// join, while "./lang" was rejected, because the dot test was being
// applied to the argument rather than to the directory's base name. A
// user who tab-completes a category should not be told the port does
// not exist.
func categoryName(t *tree.Tree, ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	clean := filepath.Clean(ref)
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(t.Root(), clean)
		if err != nil {
			return "", false
		}
		clean = rel
	}
	// One segment only: a category is a directory at the root, and
	// "lang/ruby" is a portdir reference for Tree.Resolve to answer.
	if clean == "" || strings.ContainsRune(clean, filepath.Separator) {
		return "", false
	}
	if !t.HasCategory(clean) {
		return "", false
	}
	return clean, true
}

// maintained expands maintainer:<token>.
//
// A token already carrying a key prefix is used as written. A bare one
// resolves to BOTH spellings — gh:<token> and mail:<token>@macports.org
// — because the index keeps them apart on purpose and picking one
// silently loses ports: ryandesign's two keys differ by 81. The keys
// used and their counts are returned as notes, so what was swept is
// stated rather than assumed.
func maintained(ctx context.Context, src Sources, t *tree.Tree, token string) ([]tree.Target, []string, error) {
	byKey, err := t.Maintained()
	if err != nil {
		return nil, nil, err
	}

	var keys []string
	var notes []string
	switch token {
	case meToken:
		keys, notes, err = meKeys(ctx, src)
		if err != nil {
			return nil, nil, err
		}
	case "":
		return nil, nil, fmt.Errorf("%w: maintainer: needs a handle", ErrNoMaintainer)
	default:
		keys = maintainerKeys(token)
	}

	names := make(map[string]bool)
	var matched []string
	for _, k := range keys {
		ports := byKey[k]
		if len(ports) == 0 {
			continue
		}
		matched = append(matched, k)
		notes = append(notes, fmt.Sprintf("%s names %d port(s)", k, len(ports)))
		for _, n := range ports {
			names[n] = true
		}
	}
	if len(matched) == 0 {
		return nil, nil, fmt.Errorf("%w: %s (tried %s)", ErrNoMaintainer, token, strings.Join(keys, ", "))
	}
	if token == meToken {
		if near := nearSpellings(byKey, matched, names); len(near) > 0 {
			notes = append(notes, "near-miss keys NOT included, add them explicitly to sweep them: "+
				strings.Join(near, ", "))
		}
	}

	// The maintainer index answers in names, so each one is resolved
	// back to a portdir. That is one index lookup per name — 1072 for
	// this user, 2325 for the tree's largest maintainer — and every one
	// of them re-reads data the single pass that built the map already
	// had in hand. Carrying the portdir on the map, as the reverse
	// dependency index already does for exactly this reason, would end
	// the cost; until it does, this is where it is paid.
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	var out []tree.Target
	unresolved := 0
	for _, n := range sorted {
		tgt, err := t.Resolve(n)
		if err != nil {
			unresolved++
			continue
		}
		out = append(out, tgt)
	}
	if unresolved > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d indexed name(s) resolved to no portdir and were skipped; the index may be stale", unresolved))
	}
	sortTargets(out)
	return out, notes, nil
}

// maintainerKeys turns a maintainer token into the index keys it could
// mean.
func maintainerKeys(token string) []string {
	lower := strings.ToLower(token)
	if strings.HasPrefix(lower, portindex.MaintainerGitHub) ||
		strings.HasPrefix(lower, portindex.MaintainerMail) {
		return []string{lower}
	}
	if handle, ok := strings.CutPrefix(lower, "@"); ok {
		return []string{portindex.MaintainerGitHub + handle}
	}
	if strings.ContainsRune(lower, '@') {
		return []string{portindex.MaintainerMail + lower}
	}
	return []string{
		portindex.MaintainerGitHub + lower,
		portindex.MaintainerMail + lower + "@macports.org",
	}
}

// meKeys resolves "me" to the two keys a MacPorts maintainer is spelled
// under, and says which lookup produced each.
//
// Both are consulted because neither is complete on its own. On the
// maintainer's own tree the forge handle names 1070 ports and the mail
// key names 1072; the two stragglers spell the handle differently in a
// braced maintainers list, and a "me" that resolved to the forge handle
// alone would miss two of this user's own ports invisibly. A lookup
// that fails is reported and the other still counts — but if neither
// answers, that is ErrNoIdentity and not an empty sweep.
func meKeys(ctx context.Context, src Sources) ([]string, []string, error) {
	var keys, notes []string
	var failures []string

	if src.Login == nil {
		failures = append(failures, "no forge lookup is wired")
	} else if login, err := src.Login(ctx); err != nil {
		failures = append(failures, "forge handle: "+err.Error())
	} else if login = strings.TrimSpace(login); login == "" {
		failures = append(failures, "forge handle: gh reported no login")
	} else {
		keys = append(keys, portindex.MaintainerGitHub+strings.ToLower(login))
	}

	if src.Email == nil {
		failures = append(failures, "no git identity lookup is wired")
	} else if mail, err := src.Email(ctx); err != nil {
		failures = append(failures, "git identity: "+err.Error())
	} else if mail = strings.TrimSpace(mail); mail == "" {
		failures = append(failures, "git identity: git config user.email is unset")
	} else {
		keys = append(keys, portindex.MaintainerMail+strings.ToLower(mail))
	}

	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("%w: %s (or spell the handle out: maintainer:gh:<handle>)",
			ErrNoIdentity, strings.Join(failures, "; "))
	}
	for _, f := range failures {
		notes = append(notes, "me is half-resolved — "+f)
	}
	return keys, notes, nil
}

// nearSpellings lists maintainer keys that look like another spelling
// of the same person and were left out.
//
// The test is two conjoined facts, both mechanical: the key co-occurs
// with one of the resolved keys on at least one port, and its handle is
// a near-spelling of one of them once separators are dropped. On this
// user's tree that surfaces gh:herby and gh:herby-gillot, one port
// each, beside gh:herbygillot.
//
// They are listed and never folded in. The index keeps spellings apart
// on purpose — merging them would take a guess about identity the index
// gives no ground for — so this offers the guess to the person who can
// actually make it.
func nearSpellings(byKey map[string][]string, have []string, names map[string]bool) []string {
	mine := make(map[string]bool, len(have))
	for _, k := range have {
		mine[k] = true
	}
	var out []string
	for key, ports := range byKey {
		if mine[key] {
			continue
		}
		if !anyNear(key, have) {
			continue
		}
		for _, p := range ports {
			if names[p] {
				out = append(out, fmt.Sprintf("%s (%d port(s))", key, len(ports)))
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func anyNear(key string, have []string) bool {
	for _, k := range have {
		if nearSpelling(key, k) {
			return true
		}
	}
	return false
}

// nearSpelling compares two maintainer keys by their identifying half —
// the handle, or a mail key's local part — with separators removed. One
// being a prefix of the other counts, which is what catches a handle
// that was shortened rather than punctuated differently; four
// characters is the floor, so a two-letter handle does not claim
// everybody.
func nearSpelling(a, b string) bool {
	x, y := identPart(a), identPart(b)
	if x == "" || y == "" {
		return false
	}
	if x == y {
		return true
	}
	if len(x) > len(y) {
		x, y = y, x
	}
	return len(x) >= 4 && strings.HasPrefix(y, x)
}

// identPart reduces a key to the letters that identify the person:
// the handle for gh:, the local part for mail:, with '-', '.' and '_'
// dropped.
func identPart(key string) string {
	s := key
	if rest, ok := strings.CutPrefix(s, portindex.MaintainerGitHub); ok {
		s = rest
	} else if rest, ok := strings.CutPrefix(s, portindex.MaintainerMail); ok {
		s, _, _ = strings.Cut(rest, "@")
	}
	return strings.NewReplacer("-", "", ".", "", "_", "").Replace(strings.ToLower(s))
}

// CollapseByPortdir reduces targets to one per portdir, returning the
// collapsed set and how many targets went into it.
//
// A branch edits a file, so a portdir is the unit of work for anything
// that writes: two subports of one Portfile are two targets and one
// edit, and a verb that minted a branch per target would meet its own
// change on the second. The reverse dependency index already establishes
// the rule and its arithmetic — gdal's 82 dependents are 39 portdirs.
//
// The count is returned rather than logged because the collapse must be
// reported: a user who selected 1072 ports and sees 1000 rows is owed
// the sentence that says why.
func CollapseByPortdir(targets []tree.Target) ([]tree.Target, int) {
	seen := make(map[string]bool, len(targets))
	out := make([]tree.Target, 0, len(targets))
	for _, t := range targets {
		if seen[t.Portdir] {
			continue
		}
		seen[t.Portdir] = true
		// The subport is dropped with the duplicates: what survives
		// addresses the Portfile, which is what an edit changes.
		out = append(out, tree.Target{Portdir: t.Portdir})
	}
	return out, len(targets)
}
