package portindex

import (
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

const (
	DependsBuild = "depends_build"
	DependsLib   = "depends_lib"
	DependsRun   = "depends_run"
)

var reverseDependencyFields = []string{DependsLib, DependsBuild, DependsRun}

// Dependent describes one reverse dependency edge from an indexed port.
type Dependent struct {
	Name     string
	Portdir  string
	Fields   []string
	Requires []string
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

// dependencyName extracts the provider port from a MacPorts dependency token.
func dependencyName(value string) string {
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
			name := strings.ToLower(dependencyName(item))
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
