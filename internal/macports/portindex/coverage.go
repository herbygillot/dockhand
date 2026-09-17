package portindex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// An incremental pass retries ports omitted from its seed, even when those
// Portfiles have not changed. Accept parse failures only when index coverage
// proves that changed directories are complete and unchanged entries survived.
func validateIncrementalCoverage(seed, candidate, root string, changed []string) error {
	if requiresFullIndex(changed) {
		return fmt.Errorf("portindex: shared resource changes require a complete index")
	}
	before, err := indexEntries(seed)
	if err != nil {
		return err
	}
	after, err := indexEntries(candidate)
	if err != nil {
		return err
	}
	directories := map[string]bool{}
	for _, name := range changed {
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) >= 3 {
			directories[parts[0]+"/"+parts[1]] = true
		}
	}
	for name, entry := range before {
		if directories[entry.Portdir] {
			continue
		}
		if next, exists := after[name]; !exists || next.Portdir != entry.Portdir {
			return fmt.Errorf("portindex: previously indexed unchanged port %s is missing", entry.Name)
		}
	}
	indexed := map[string]bool{}
	for _, entry := range after {
		if !directories[entry.Portdir] {
			continue
		}
		indexed[entry.Portdir] = true
		subports, failures := syntax.ListValues(entry.Fields["subports"])
		if len(failures) != 0 {
			return fmt.Errorf("portindex: invalid subport list for %s", entry.Name)
		}
		for _, subport := range subports {
			if child, exists := after[strings.ToLower(subport)]; !exists || child.Portdir != entry.Portdir {
				return fmt.Errorf("portindex: changed port %s has unindexed subport %s", entry.Name, subport)
			}
		}
	}
	for directory := range directories {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(directory), "Portfile"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !indexed[directory] {
			return fmt.Errorf("portindex: changed directory %s has no indexed ports", directory)
		}
	}
	return nil
}

func indexEntries(root string) (map[string]Entry, error) {
	index, err := Open(root)
	if err != nil {
		return nil, err
	}
	entries := map[string]Entry{}
	var invalid error
	err = index.Each(func(entry Entry) bool {
		key := strings.ToLower(entry.Name)
		if _, exists := entries[key]; exists {
			invalid = fmt.Errorf("%w: duplicate port %s", errMalformed, entry.Name)
			return false
		}
		entries[key] = entry
		return true
	})
	if err != nil {
		return nil, err
	}
	return entries, invalid
}
