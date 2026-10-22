package main

import (
	"bufio"
	"debug/macho"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
)

// allowedList is imports.txt: a symbol on each line, # starting a comment.
//
//go:embed imports.txt
var allowedList string

const usage = "usage: legacymacos BINARY..."

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "legacymacos:", err)
		os.Exit(1)
	}
}

func run(binaries []string) error {
	if len(binaries) == 0 {
		return errors.New(usage)
	}
	allowed := parseAllowed(allowedList)
	failed := false
	for _, binary := range binaries {
		extra, err := unlisted(binary, allowed)
		if err != nil {
			return err
		}
		if len(extra) > 0 {
			failed = true
			fmt.Fprintf(os.Stderr, "%s imports what imports.txt doesn't list, and may not load on macOS 10.13 to 11:\n", binary)
			for _, symbol := range extra {
				fmt.Fprintln(os.Stderr, "  "+symbol)
			}
		}
	}
	if failed {
		return errors.New("check each symbol against the macOS 10.13 SDK, as tools/legacymacos/README.md says, before adding it to imports.txt")
	}
	return nil
}

// parseAllowed reads imports.txt's symbols.
func parseAllowed(list string) map[string]bool {
	allowed := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(list))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowed[line] = true
	}
	return allowed
}

// unlisted returns the symbols binary imports that allowed doesn't have, in
// the order the binary lists them.
func unlisted(binary string, allowed map[string]bool) ([]string, error) {
	file, err := macho.Open(binary)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	symbols, err := file.ImportedSymbols()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", binary, err)
	}
	var extra []string
	for _, symbol := range symbols {
		if !allowed[symbol] {
			extra = append(extra, symbol)
		}
	}
	return extra, nil
}
