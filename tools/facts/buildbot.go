package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const buildbotURL = "https://build.macports.org"

var errNotFound = errors.New("not found")

// builderName is a ports builder: ports-26_x86_64-builder, whose macOS
// product and architecture it names.
var builderName = regexp.MustCompile(`^ports-([0-9.]+)_(x86_64|arm64|i386)-builder$`)

// A buildbot harvest is one record for each builder whose recent logs
// carry the header Base prints when it runs with -d.
type builderFacts struct {
	Builder      string `json:"builder"`
	Build        int    `json:"build"`
	Started      string `json:"started"`
	Architecture string `json:"architecture"`
	Header       header `json:"header"`
}

// header is Base's debug header in a port -d log:
//
//	DEBUG: macOS 26.7 (darwin/25.6.0) arch i386
//	DEBUG: MacPorts 2.12.6
//	DEBUG: Xcode 26.6, CLT 26.6.0.0.1781586589
//	DEBUG: SDK 26
type header struct {
	MacOS    string `json:"macos"`
	Darwin   int    `json:"darwin"`
	MacPorts string `json:"macports"`
	Xcode    string `json:"xcode"`
	Tools    string `json:"tools"`
	SDK      string `json:"sdk"`
}

var (
	// Base names the release as Apple did when it shipped: Mac OS X 10.6,
	// OS X 10.11, macOS 10.12 and on.
	headerMacOS    = regexp.MustCompile(`^DEBUG: (?:macOS|OS X|Mac OS X) (\S+) \(darwin/(\d+)\.`)
	headerMacPorts = regexp.MustCompile(`^DEBUG: MacPorts (\S+)$`)
	headerXcode    = regexp.MustCompile(`^DEBUG: Xcode (\S+), CLT (\S+)$`)
	headerSDK      = regexp.MustCompile(`^DEBUG: SDK (\S+)$`)
)

// readHeader reads a log up to its first complete header, and reports
// whether it found one.
func readHeader(log io.Reader) (header, bool) {
	var h header
	scanner := bufio.NewScanner(log)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if m := headerMacOS.FindStringSubmatch(line); m != nil {
			h.MacOS = m[1]
			h.Darwin, _ = strconv.Atoi(m[2])
		} else if m := headerMacPorts.FindStringSubmatch(line); m != nil {
			h.MacPorts = m[1]
		} else if m := headerXcode.FindStringSubmatch(line); m != nil {
			h.Xcode, h.Tools = m[1], m[2]
		} else if m := headerSDK.FindStringSubmatch(line); m != nil {
			h.SDK = m[1]
		}
		if h.Darwin != 0 && h.MacPorts != "" && h.Xcode != "" && h.SDK != "" {
			return h, true
		}
	}
	return h, false
}

func harvestBuildbot(ctx context.Context, base string, builds int, out string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	var builders map[string]json.RawMessage
	if err := getJSON(ctx, client, base+"/json/builders", &builders); err != nil {
		return err
	}
	var names []string
	for name := range builders {
		if builderName.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var found []builderFacts
	for _, name := range names {
		facts, err := harvestBuilder(ctx, client, base, name, builds)
		if err != nil {
			return err
		}
		if facts == nil {
			fmt.Fprintf(os.Stderr, "%s: no header in its last %d builds\n", name, builds)
			continue
		}
		fmt.Fprintf(os.Stderr, "%s: build %d, Darwin %d, Xcode %s, CLT %s\n", name, facts.Build, facts.Header.Darwin, facts.Header.Xcode, facts.Header.Tools)
		found = append(found, *facts)
	}
	data, err := json.MarshalIndent(found, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0o644)
}

// harvestBuilder tries a builder's recent builds, newest first, for an
// install-port log with the header.
func harvestBuilder(ctx context.Context, client *http.Client, base, name string, builds int) (*builderFacts, error) {
	architecture := builderName.FindStringSubmatch(name)[2]
	for i := 1; i <= builds; i++ {
		var build struct {
			Number int
			Times  []float64
			Steps  []struct{ Name string }
		}
		if err := getJSON(ctx, client, fmt.Sprintf("%s/json/builders/%s/builds/-%d", base, url.PathEscape(name), i), &build); err != nil {
			return nil, err
		}
		installs := false
		for _, step := range build.Steps {
			installs = installs || step.Name == "install-port"
		}
		if !installs {
			continue
		}
		log, err := get(ctx, client, fmt.Sprintf("%s/builders/%s/builds/%d/steps/install-port/logs/stdio/text", base, url.PathEscape(name), build.Number))
		if errors.Is(err, errNotFound) {
			// The step didn't run, so it kept no log.
			continue
		}
		if err != nil {
			return nil, err
		}
		h, ok := readHeader(log)
		log.Close()
		if !ok {
			continue
		}
		started := ""
		if len(build.Times) > 0 {
			started = time.Unix(int64(build.Times[0]), 0).UTC().Format(time.DateOnly)
		}
		return &builderFacts{Builder: name, Build: build.Number, Started: started, Architecture: architecture, Header: h}, nil
	}
	return nil, nil
}

func get(ctx context.Context, client *http.Client, address string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%s: %w", address, errNotFound)
		}
		return nil, fmt.Errorf("%s: %s", address, response.Status)
	}
	return response.Body, nil
}

func getJSON(ctx context.Context, client *http.Client, address string, value any) error {
	body, err := get(ctx, client, address)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(body).Decode(value)
}
