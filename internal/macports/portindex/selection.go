package portindex

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Filter matches exact metadata values: alternatives within a field, intersection
// across fields. Maintainers accept MacPorts handles/emails and Repology handles,
// and the keywords openmaintainer and nomaintainer, which the index carries
// as maintainers in their own right. NotMaintainers leaves out any port one of
// its values maintains, whichever way the rest was selected, which is how
// "open, but not mine" is asked. All selects every indexed entry while
// retaining coverage problems.
type Filter struct {
	All            bool
	Maintainers    []string
	Categories     []string
	NotMaintainers []string
}

func (f Filter) Validate() error {
	if f.All {
		if len(f.Maintainers)+len(f.Categories) > 0 {
			return fmt.Errorf("portindex: choose all ports or metadata filters")
		}
	} else if len(f.Maintainers)+len(f.Categories) == 0 {
		return fmt.Errorf("portindex: a maintainer or category is required")
	}
	for _, values := range [][]string{f.Maintainers, f.Categories, f.NotMaintainers} {
		for _, value := range values {
			if value == "" || strings.ContainsAny(value, "/\\*?[]{}") || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				return fmt.Errorf("portindex: selector %q must be a nonempty exact value", value)
			}
		}
	}
	return nil
}

type SelectionProblem struct {
	Port   string
	Detail string
}

type Selection struct {
	Entries  []Entry
	Problems []SelectionProblem
}

// Select preserves uncertainty about membership rather than silently excluding
// malformed metadata or Portfiles omitted by the indexer. The index must have
// been staged alongside the immutable source it describes.
func (i *Index) Select(ctx context.Context, filter Filter) (Selection, error) {
	var result Selection
	if err := filter.Validate(); err != nil {
		return result, err
	}
	entries := map[string]Entry{}
	directories := map[string]bool{}
	var failure error
	err := i.Each(func(entry Entry) bool {
		if failure = ctx.Err(); failure != nil {
			return false
		}
		key := strings.ToLower(entry.Name)
		if _, exists := entries[key]; exists {
			failure = fmt.Errorf("%w: duplicate port %s", errMalformed, entry.Name)
			return false
		}
		entries[key] = entry
		directories[entry.Portdir] = true
		match, err := filter.matches(entry)
		if err != nil {
			result.Problems = append(result.Problems, SelectionProblem{Port: entry.Name, Detail: err.Error()})
		} else if match {
			result.Entries = append(result.Entries, entry)
		}
		return true
	})
	if err != nil {
		return result, err
	}
	if failure != nil {
		return result, failure
	}
	for _, entry := range entries {
		names, failures := syntax.ListValues(entry.Fields["subports"])
		if len(failures) > 0 {
			result.Problems = append(result.Problems, SelectionProblem{Port: entry.Name, Detail: "invalid indexed subport list; selection coverage is incomplete"})
			continue
		}
		for _, name := range names {
			child, ok := entries[strings.ToLower(name)]
			if !ok || child.Portdir != entry.Portdir {
				result.Problems = append(result.Problems, SelectionProblem{Port: name, Detail: "subport missing from index; selector membership is unknown"})
			}
		}
	}
	root := filepath.Dir(i.path)
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if entry.IsDir() && (len(parts) > 2 || strings.HasPrefix(parts[0], "_") || strings.HasPrefix(parts[0], ".")) {
			return filepath.SkipDir
		}
		if len(parts) == 3 && parts[2] == "Portfile" {
			directory := strings.Join(parts[:2], "/")
			if !directories[directory] {
				result.Problems = append(result.Problems, SelectionProblem{Port: directory + "/Portfile", Detail: "Portfile missing from index; selector membership is unknown"})
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	slices.SortFunc(result.Entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(result.Problems, func(a, b SelectionProblem) int {
		if n := strings.Compare(a.Port, b.Port); n != 0 {
			return n
		}
		return strings.Compare(a.Detail, b.Detail)
	})
	result.Problems = slices.Compact(result.Problems)
	return result, nil
}

func (f Filter) matches(entry Entry) (bool, error) {
	matched := true
	for _, field := range []struct {
		name   string
		wanted []string
	}{{"maintainers", f.Maintainers}, {"categories", f.Categories}} {
		if len(field.wanted) == 0 {
			continue
		}
		indexed := entry.Fields[field.name]
		if indexed == "" && field.name == "categories" {
			// A port indexed with no categories, an obsolete port or an R
			// port, is placed by its directory: the first category is the
			// directory's by convention, and it is the one a person names.
			if category, _, ok := strings.Cut(entry.Portdir, "/"); ok && category != "" {
				indexed = category
			}
		}
		if indexed == "" {
			return false, fmt.Errorf("missing indexed %s; selector membership is unknown", field.name)
		}
		values, failures := syntax.ListValues(indexed)
		if len(failures) > 0 {
			return false, fmt.Errorf("invalid indexed %s; selector membership is unknown", field.name)
		}
		if field.name == "maintainers" {
			var err error
			if values, err = maintainerIdentities(indexed); err != nil {
				return false, err
			}
		}
		found := false
		for _, wanted := range field.wanted {
			if field.name == "maintainers" {
				wanted = macports.MaintainerIdentity(wanted)
			}
			for _, value := range values {
				if strings.EqualFold(wanted, value) {
					found = true
				}
			}
		}
		matched = matched && found
	}
	if matched && len(f.NotMaintainers) > 0 {
		if entry.Fields["maintainers"] == "" {
			return false, fmt.Errorf("missing indexed maintainers; selector membership is unknown")
		}
		identities, err := maintainerIdentities(entry.Fields["maintainers"])
		if err != nil {
			return false, err
		}
		for _, excluded := range f.NotMaintainers {
			if slices.ContainsFunc(identities, func(value string) bool { return strings.EqualFold(macports.MaintainerIdentity(excluded), value) }) {
				return false, nil
			}
		}
	}
	return matched, nil
}

// maintainerIdentities are who the index's maintainers field names, one
// identity for each spelling of each maintainer, {{a b} c} naming three.
func maintainerIdentities(indexed string) ([]string, error) {
	maintainers, err := macports.ReadMaintainers(indexed)
	if err != nil {
		return nil, fmt.Errorf("invalid indexed maintainers; selector membership is unknown")
	}
	var identities []string
	for _, maintainer := range maintainers {
		for _, spelling := range maintainer {
			identities = append(identities, macports.MaintainerIdentity(spelling))
		}
	}
	return identities, nil
}
