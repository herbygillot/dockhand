package project

import (
	"bytes"
	"strings"
)

// RDependencyFields are the fields of an R package's DESCRIPTION its build
// reads for what it needs, as Writing R Extensions §1.1.1 names them.
var RDependencyFields = []string{"Depends", "Imports", "LinkingTo"}

// RDependencies are an R package's DESCRIPTION's dependency fields, by
// name, each value with its whitespace collapsed. DESCRIPTION is in Debian
// Control File format: a field is "Name: value", and a line that starts
// with whitespace continues the one before it. A field the file doesn't
// have is absent.
func RDependencies(data []byte) map[string]string {
	fields := map[string]string{}
	current := ""
	for _, line := range strings.Split(string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))), "\n") {
		if line != "" && (line[0] == ' ' || line[0] == '\t') {
			if current != "" {
				fields[current] += " " + strings.TrimSpace(line)
			}
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		current = ""
		if !ok {
			continue
		}
		for _, wanted := range RDependencyFields {
			if name == wanted {
				current = name
				fields[name] = strings.TrimSpace(value)
			}
		}
	}
	for name, value := range fields {
		fields[name] = strings.Join(strings.Fields(value), " ")
	}
	return fields
}
