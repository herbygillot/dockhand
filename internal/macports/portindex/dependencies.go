package portindex

import (
	"errors"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

const (
	DependsFetch   = "depends_fetch"
	DependsExtract = "depends_extract"
	DependsPatch   = "depends_patch"
	DependsBuild   = "depends_build"
	DependsLib     = "depends_lib"
	DependsRun     = "depends_run"
	DependsTest    = "depends_test"
)

var reverseDependencyFields = []string{DependsLib, DependsBuild, DependsRun}
var closureDependencyFields = []string{DependsFetch, DependsExtract, DependsPatch, DependsBuild, DependsLib, DependsRun, DependsTest}

// Dependent describes one reverse dependency edge from an indexed port.
type Dependent struct {
	Name     string
	Portdir  string
	Fields   []string
	Requires []string
}

func (d Dependent) BuildOnly() bool {
	return len(d.Fields) == 1 && d.Fields[0] == DependsBuild
}

// Unread identifies an indexed dependency field that could not be parsed.
type Unread struct {
	Port    string
	Portdir string
	Field   string
}

// Reverse maps lowercased dependency names to their direct dependents.
type Reverse struct {
	ByPort map[string][]Dependent
	Unread []Unread
}

// Closure contains the transitive dependencies of selected roots.
type Closure struct {
	Dependencies []string
	Missing      []string
	Unread       []Unread
}

// DependencyName extracts the provider port from a MacPorts dependency token.
func DependencyName(value string) string {
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		return value[index+1:]
	}
	return value
}

func (e Entry) dependencyEdges(fields []string) (map[string][]string, []Unread) {
	edges := map[string][]string{}
	var unread []Unread
	for _, field := range fields {
		value := e.Fields[field]
		if value == "" {
			continue
		}
		items, failures := syntax.ListValues(value)
		if len(failures) != 0 {
			unread = append(unread, Unread{Port: e.Name, Portdir: e.Portdir, Field: field})
			continue
		}
		for _, item := range items {
			name := strings.ToLower(DependencyName(item))
			if name == "" {
				continue
			}
			values := edges[name]
			if len(values) == 0 || values[len(values)-1] != field {
				edges[name] = append(values, field)
			}
		}
	}
	return edges, unread
}

// ReverseDependencies builds the direct reverse index in one sequential pass.
func (i *Index) ReverseDependencies() (Reverse, error) {
	result := Reverse{ByPort: map[string][]Dependent{}}
	err := i.Each(func(entry Entry) bool {
		edges, unread := entry.dependencyEdges(reverseDependencyFields)
		result.Unread = append(result.Unread, unread...)
		if len(edges) == 0 {
			return true
		}
		requires := make([]string, 0, len(edges))
		for name := range edges {
			requires = append(requires, name)
		}
		sort.Strings(requires)
		for name, fields := range edges {
			result.ByPort[name] = append(result.ByPort[name], Dependent{Name: entry.Name, Portdir: entry.Portdir, Fields: fields, Requires: requires})
		}
		return true
	})
	if err != nil {
		return Reverse{}, err
	}
	for name := range result.ByPort {
		sort.Slice(result.ByPort[name], func(a, b int) bool {
			left, right := result.ByPort[name][a], result.ByPort[name][b]
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return left.Portdir < right.Portdir
		})
	}
	sortUnread(result.Unread)
	return result, nil
}

// DependencyClosure resolves every dependency phase transitively. Names are
// lowercased so the result follows PortIndex's case-insensitive lookup rules.
func (i *Index) DependencyClosure(roots []string) (Closure, error) {
	rootSet := map[string]bool{}
	queue := make([]string, 0, len(roots))
	for _, value := range roots {
		name := strings.ToLower(value)
		if name != "" && !rootSet[name] {
			rootSet[name] = true
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)
	seen := map[string]bool{}
	dependencies := map[string]bool{}
	missing := map[string]bool{}
	var unread []Unread
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		entry, err := i.Lookup(name)
		if errors.Is(err, ErrNotIndexed) {
			missing[name] = true
			continue
		}
		if err != nil {
			return Closure{}, err
		}
		edges, bad := entry.dependencyEdges(closureDependencyFields)
		unread = append(unread, bad...)
		next := make([]string, 0, len(edges))
		for dependency := range edges {
			if !rootSet[dependency] {
				dependencies[dependency] = true
			}
			if !seen[dependency] {
				next = append(next, dependency)
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}
	result := Closure{Dependencies: sortedKeys(dependencies), Missing: sortedKeys(missing), Unread: unread}
	sortUnread(result.Unread)
	return result, nil
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortUnread(values []Unread) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Port != values[j].Port {
			return values[i].Port < values[j].Port
		}
		if values[i].Field != values[j].Field {
			return values[i].Field < values[j].Field
		}
		return values[i].Portdir < values[j].Portdir
	})
}
