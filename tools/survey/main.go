package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/assess"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/scratch"
)

// list is a repeatable string flag.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// run is how a survey was made and what it cost, written beside its
// journal as <journal>.run.json.
type run struct {
	Tree          string    `json:"tree"`
	Commit        string    `json:"commit"`
	Tclsh         string    `json:"tclsh"`
	Portindex     string    `json:"portindex"`
	Base          string    `json:"base"`
	Parallel      int       `json:"parallel"`
	Started       time.Time `json:"started"`
	Finished      time.Time `json:"finished"`
	WallSeconds   float64   `json:"wall_seconds"`
	UserSeconds   float64   `json:"user_seconds"`
	SystemSeconds float64   `json:"system_seconds"`
	Cores         float64   `json:"cores"`
	LoadAtStart   string    `json:"load_at_start"`
	Assessed      int       `json:"assessed"`
	Skipped       int       `json:"skipped"`
	Outcomes      any       `json:"outcomes"`
}

func main() {
	if err := survey(); err != nil {
		fmt.Fprintln(os.Stderr, "survey:", err)
		os.Exit(1)
	}
}

func survey() (err error) {
	var ports, maintainers, notMaintainers, categories list
	tree := flag.String("tree", firstOf(os.Getenv("MACPORTS_TREE"), "."), "ports tree to survey, at its committed HEAD")
	journalPath := flag.String("journal", "", "journal to write, one JSON line per port; a rerun with the same file continues it")
	prefix := flag.String("prefix", "", "MacPorts prefix whose port-tclsh and portindex evaluate; the ones on PATH when empty")
	gitBin := flag.String("git", "", "git executable; git on PATH when empty")
	parallel := flag.Int("parallel", assess.Concurrency, "Portfiles assessed at once")
	indexCache := flag.String("index-cache", "", "port index cache; $DOCKHAND_INDEX_CACHE, else dockhand/indexes in the user cache directory")
	go2port := flag.String("go2port", firstOf(os.Getenv("GO2PORT_BIN"), lookPath("go2port")), "go2port, for Go dependency blocks")
	cargo2port := flag.String("cargo2port", firstOf(os.Getenv("CARGO2PORT_BIN"), lookPath("cargo2port")), "cargo2port, for Cargo dependency blocks")
	cpuProfile := flag.String("cpuprofile", "", "write a CPU profile of the survey itself")
	verbose := flag.Bool("v", false, "report the work behind the scenes")
	compare := flag.Bool("compare", false, "compare two journals, the baseline and the new one, given as arguments")
	top := flag.Int("top", 15, "with -compare, how many of each kind of change to list")
	flag.Var(&ports, "port", "a port to assess (repeatable); every port when no selection is given")
	flag.Var(&maintainers, "maintainer", "an exact maintainer to select (repeatable)")
	flag.Var(&notMaintainers, "not-maintainer", "a maintainer to leave out (repeatable)")
	flag.Var(&categories, "category", "an exact category to select (repeatable)")
	flag.Parse()

	if *compare {
		if flag.NArg() != 2 {
			return errors.New("-compare takes the baseline journal and the new one")
		}
		return compareJournals(os.Stdout, flag.Arg(0), flag.Arg(1), *top)
	}
	if *journalPath == "" {
		return errors.New("-journal is required, so a long survey can be continued")
	}
	request := assess.Request{}
	request.Selection.Ports, request.Selection.Maintainers, request.Selection.NotMaintainers, request.Selection.Categories = ports, maintainers, notMaintainers, categories
	request.Selection.All = len(ports)+len(maintainers)+len(categories) == 0
	if err := request.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() { err = errors.Join(err, scratch.Close()) }()
	threshold := progress.Info
	if *verbose {
		threshold = progress.Verbose
	}
	ctx = progress.WithReporter(ctx, func(u progress.Update) {
		if u.Level <= threshold {
			fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().Format("15:04:05"), u.Message)
		}
	})
	if *cpuProfile != "" {
		file, err := os.Create(*cpuProfile)
		if err != nil {
			return err
		}
		if err := pprof.StartCPUProfile(file); err != nil {
			return err
		}
		defer func() { pprof.StopCPUProfile(); err = errors.Join(err, file.Close()) }()
	}

	root, err := filepath.Abs(*tree)
	if err != nil {
		return err
	}
	repo, err := git.Open(ctx, root, *gitBin)
	if err != nil {
		return err
	}
	if err := macports.ValidatePortsTree(repo.Root, root); err != nil {
		return err
	}
	tclsh, portindexBin := lookPath("port-tclsh"), lookPath("portindex")
	if *prefix != "" {
		tclsh, portindexBin = filepath.Join(*prefix, "bin", "port-tclsh"), filepath.Join(*prefix, "bin", "portindex")
	}
	if tclsh == "" {
		return errors.New("no port-tclsh on PATH; give -prefix")
	}
	cache := *indexCache
	if cache == "" {
		if cache, err = defaultIndexCache(); err != nil {
			return err
		}
	}
	native := &eval.Evaluator{Executable: tclsh}
	index := portindex.Config{CacheDirectory: cache, Executable: portindexBin}
	service := assess.Service{
		Repo:            repo,
		Ports:           &selection.Reader{Evaluator: native, Index: &portindex.Stager{Repo: repo, Config: index, NativePlatform: native.NativePlatform, WithoutBase: true}},
		Index:           &portindex.Stager{Repo: repo, Config: index},
		DependencyTools: dependency.Tools{Go2Port: *go2port, Cargo2Port: *cargo2port},
	}
	assess.Concurrency = max(*parallel, 1)
	journal, err := assess.OpenJournal(*journalPath)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, journal.Close()) }()
	request.Journal = journal

	record := run{Tree: root, Tclsh: tclsh, Portindex: portindexBin, Base: baseVersion(tclsh), Parallel: assess.Concurrency, Started: time.Now(), LoadAtStart: loadAverage()}
	fmt.Fprintf(os.Stderr, "Surveying %s with %s (%s), %d Portfiles at a time; load %s\n", root, tclsh, record.Base, assess.Concurrency, record.LoadAtStart)
	result, err := service.Assess(ctx, request)
	if err != nil {
		return err
	}
	record.Finished = time.Now()
	record.Commit = string(result.Source.Commit)
	record.WallSeconds = record.Finished.Sub(record.Started).Seconds()
	record.UserSeconds, record.SystemSeconds = cpuSeconds()
	if record.WallSeconds > 0 {
		record.Cores = (record.UserSeconds + record.SystemSeconds) / record.WallSeconds
	}
	record.Assessed, record.Skipped = len(result.Ports), result.Skipped
	outcomes := map[string]int{}
	for _, port := range result.Ports {
		outcomes[port.Outcome]++
	}
	record.Outcomes = outcomes
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*journalPath+".run.json", append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Assessed %d ports (%d already in the journal) at %s in %s: %v; %.1f cores\n",
		len(result.Ports), result.Skipped, shortID(record.Commit), time.Duration(record.WallSeconds*float64(time.Second)).Round(time.Second), outcomes, record.Cores)
	return nil
}

// cpuSeconds is the user and system time of this process and the
// evaluators it ran and waited for.
func cpuSeconds() (user, system float64) {
	for _, who := range []int{syscall.RUSAGE_SELF, syscall.RUSAGE_CHILDREN} {
		var usage syscall.Rusage
		if syscall.Getrusage(who, &usage) == nil {
			user += time.Duration(usage.Utime.Nano()).Seconds()
			system += time.Duration(usage.Stime.Nano()).Seconds()
		}
	}
	return user, system
}

func baseVersion(tclsh string) string {
	query := exec.Command(tclsh)
	query.Stdin = strings.NewReader("package require macports; puts [macports::version]\n")
	out, err := query.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func loadAverage() string {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return "unknown"
	}
	return strings.Trim(strings.TrimSpace(string(out)), "{} ")
}

func defaultIndexCache() (string, error) {
	if chosen := os.Getenv("DOCKHAND_INDEX_CACHE"); chosen != "" {
		return filepath.Abs(chosen)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "dockhand", "indexes"), nil
}

func lookPath(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
