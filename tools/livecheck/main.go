package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// A bucket is how a port's replay ended, exactly one each
// (the proposal's "What counts as a failure").
type bucket string

const (
	answered bucket = "answered"
	// ownTagMissed is the port's own tag in the recorded catalog and its
	// livecheck filter admitting no tag, with no page answering in its
	// place: pinact before 31fca1db.
	ownTagMissed bucket = "own tag missed"
	internalErr  bucket = "internal error"
	refused      bucket = "refused"
	replayMiss   bucket = "replay miss"
	unrelated    bucket = "not a forge port"
)

// failing are the buckets that are dockhand's failures.
var failing = []bucket{ownTagMissed, internalErr}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "check" {
		fmt.Fprintln(os.Stderr, "usage: livecheck check -db SURVEY.db [-tclsh PATH] [-port NAME]...")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("check", flag.ExitOnError)
	db := flags.String("db", "", "a survey record (tools/survey) of the tree")
	tclsh := flags.String("tclsh", "/opt/local/bin/port-tclsh", "MacPorts' port-tclsh")
	var only list
	flags.Var(&only, "port", "a port to replay; every port where none is named")
	_ = flags.Parse(os.Args[2:])
	if *db == "" {
		fmt.Fprintln(os.Stderr, "livecheck check: -db names the survey record")
		os.Exit(2)
	}
	ports, err := readSurvey(*db, only)
	if err != nil {
		fmt.Fprintln(os.Stderr, "livecheck:", err)
		os.Exit(1)
	}
	versions := &eval.Evaluator{Executable: *tclsh, Env: os.Environ()}
	failed := check(context.Background(), os.Stdout, ports, versions)
	if failed > 0 {
		os.Exit(1)
	}
}

// list is a repeatable string flag.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// check replays each port with only its own tag recorded, prints each
// port that didn't answer with its bucket, then the counts, and returns
// how many failed.
func check(ctx context.Context, out io.Writer, ports []macports.PortInfo, versions *eval.Evaluator) int {
	counts := map[bucket]int{}
	failed := 0
	for _, port := range ports {
		recorded, ok := ownTag(port)
		if !ok {
			counts[unrelated]++
			continue
		}
		got, detail := replay(ctx, port, recorded, versions)
		counts[got]++
		if got != answered {
			fmt.Fprintf(out, "%s\t%s\t%s\n", port.Name, got, detail)
		}
		if slices.Contains(failing, got) {
			failed++
		}
	}
	for _, b := range []bucket{answered, ownTagMissed, internalErr, refused, replayMiss, unrelated} {
		fmt.Fprintf(out, "%s: %d\n", b, counts[b])
	}
	return failed
}

// ownTag is a GitHub port's recording before any capture: its own tag
// alone, as its PortGroup spells it from its version, a fact of the
// Portfile.
func ownTag(port macports.PortInfo) (recording, bool) {
	spec, err := portsource.Interpret(port, portsource.Discovery)
	if err != nil || spec.Forge != portsource.GitHub || spec.Repository == "" || spec.SourceVersion == "" {
		return recording{}, false
	}
	return recording{Repository: spec.Repository, Tags: []string{spec.Pattern.Tag(spec.SourceVersion)}}, true
}

// replay runs dockhand's discovery of a port against a recording, and
// says which bucket it ended in, with what it said.
func replay(ctx context.Context, port macports.PortInfo, recorded recording, versions *eval.Evaluator) (got bucket, detail string) {
	defer func() {
		if p := recover(); p != nil {
			got, detail = internalErr, fmt.Sprint(p)
		}
	}()
	service := &upstream.Service{
		HTTP:            &http.Client{Transport: pages(recorded.Pages)},
		Catalogs:        map[portsource.Forge]upstream.Catalog{portsource.GitHub: catalog{recorded}},
		Versions:        versions,
		EvaluateVersion: func(_ context.Context, version string) (string, error) { return version, nil },
		// A port tracking a branch isn't replayed yet: its git refuses.
		Git: git.Program{Executable: "/nonexistent/git", Env: []string{}},
	}
	result, err := service.DiscoverPort(ctx, port)
	switch {
	case err == nil:
		return answered, result.CandidateVersion
	case errors.Is(err, errNotRecorded):
		return replayMiss, err.Error()
	case errors.Is(err, upstream.ErrAutomaticUnsupported):
		return refused, err.Error()
	case strings.Contains(err.Error(), "nothing upstream lists matches"):
		return ownTagMissed, err.Error()
	}
	return internalErr, err.Error()
}

// readSurvey reads each port's options as the record's newest run
// evaluated them on the Mac's own platform.
func readSurvey(path string, only []string) ([]macports.PortInfo, error) {
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT p.name, f.name, f.value FROM field f
		JOIN evaluation e ON e.id = f.evaluation_id JOIN port p ON p.id = e.port_id
		WHERE e.run_id = (SELECT max(id) FROM run) AND e.modeled = 0
		ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byName := map[string]*macports.PortInfo{}
	var order []string
	for rows.Next() {
		var name, field string
		var value sql.NullString
		if err := rows.Scan(&name, &field, &value); err != nil {
			return nil, err
		}
		if len(only) > 0 && !slices.Contains(only, name) {
			continue
		}
		port, ok := byName[name]
		if !ok {
			port = &macports.PortInfo{Name: name, Options: map[string]string{}}
			byName[name] = port
			order = append(order, name)
		}
		port.Options[field] = value.String
		switch field {
		case "version":
			port.Version = value.String
		case "revision":
			port.Revision, _ = strconv.Atoi(value.String)
		case "epoch":
			port.Epoch, _ = strconv.Atoi(value.String)
		}
	}
	ports := make([]macports.PortInfo, 0, len(order))
	for _, name := range order {
		ports = append(ports, *byName[name])
	}
	return ports, rows.Err()
}
