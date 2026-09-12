// ledgerperf measures existing ledger and workflow behavior in disposable repositories.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/lock"
	"github.com/herbygillot/dockhand/v2/internal/perftest"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

var (
	records    = flag.String("records", "10,100,1000", "completed jobs, comma separated")
	sources    = flag.String("sources", "shared,distinct", "shared, distinct, or an explicit source count")
	ops        = flag.String("ops", "read,noop,edit,new-revision,status,submit,observe,complete", "operations, comma separated; also cycle-all, codec-encode, codec-decode")
	samples    = flag.Int("samples", 5, "samples per case")
	active     = flag.Int("active", 1, "active builds in addition to completed jobs")
	history    = flag.Int("history", 0, "additional synthetic commits with identical state trees")
	packed     = flag.Bool("pack", false, "run git gc on the disposable fixture before measurement")
	budget     = flag.Duration("budget", 30*time.Second, "deadline for each measured operation")
	workers    = flag.Int("writers", 0, "run this many writer subprocesses instead of operation cases")
	readers    = flag.Int("readers", 0, "reader subprocesses to run alongside writers")
	growth     = flag.Int("growth", 0, "perform this many real updates without resetting history")
	workerRoot = flag.String("worker-root", "", "internal worker repository")
	workerOp   = flag.String("worker-op", "edit", "writer subprocess operation: edit or cycle-all; read is reserved for reader workers")
)

type result struct {
	Advanced        int     `json:"advanced,omitempty"`
	PID             int     `json:"pid,omitempty"`
	StartedNS       int64   `json:"started_ns,omitempty"`
	Kind            string  `json:"kind"`
	Operation       string  `json:"operation,omitempty"`
	Completed       int     `json:"completed"`
	Sources         int     `json:"sources"`
	Active          int     `json:"active"`
	History         int     `json:"history"`
	Packed          bool    `json:"packed"`
	Sample          int     `json:"sample,omitempty"`
	Worker          int     `json:"worker,omitempty"`
	Writers         int     `json:"writers,omitempty"`
	Readers         int     `json:"readers,omitempty"`
	Milliseconds    float64 `json:"ms,omitempty"`
	Bytes           uint64  `json:"allocated_bytes,omitempty"`
	Allocations     uint64  `json:"allocations,omitempty"`
	SnapshotBytes   int     `json:"snapshot_bytes,omitempty"`
	RepositoryBytes int64   `json:"repository_bytes,omitempty"`
	Objects         string  `json:"objects,omitempty"`
	Pins            int     `json:"pins,omitempty"`
	Error           string  `json:"error,omitempty"`
}

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if *samples < 1 || *budget <= 0 || *active < 0 || *history < 0 || *growth < 0 || *workers < 0 || *readers < 0 {
		return fmt.Errorf("invalid experiment options")
	}
	// Isolate repository settings without changing the production command wrapper.
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(key, "GIT_") {
			os.Unsetenv(key)
		}
	}
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if *workerOp != "edit" && *workerOp != "cycle-all" && !(*workerRoot != "" && *workerOp == "read") {
		return fmt.Errorf("invalid worker operation %q", *workerOp)
	}
	if *workerRoot != "" {
		return worker()
	}
	gitVersion, _ := exec.Command("git", "--version").Output()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"kind": "environment", "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "git": strings.TrimSpace(string(gitVersion)), "args": os.Args[1:], "time": time.Now().UTC()}); err != nil {
		return err
	}
	for _, size := range strings.Split(*records, ",") {
		n, err := strconv.Atoi(size)
		if err != nil {
			return err
		}
		for _, mode := range strings.Split(*sources, ",") {
			d := 1
			switch mode {
			case "shared":
			case "distinct":
				d = n
			default:
				d, err = strconv.Atoi(mode)
				if err != nil {
					return err
				}
			}
			if err := runCase(n, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func runCase(n, d int) error {
	root, err := os.MkdirTemp("", "dockhand-ledgerperf-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	f, err := perftest.New(ctx, root, n, d, *active)
	if err != nil {
		return err
	}
	if err := f.AddHistory(ctx, *history); err != nil {
		return err
	}
	if *packed {
		if _, err := perftest.Git(ctx, root, nil, "gc", "--quiet"); err != nil {
			return err
		}
	}
	base := result{Completed: n, Sources: d, Active: *active, History: *history, Packed: *packed, SnapshotBytes: f.SnapshotBytes, Writers: *workers, Readers: *readers}
	if err := storage(ctx, f, base, "fixture"); err != nil {
		return err
	}
	if *workers+*readers > 0 {
		return contend(ctx, f, base)
	}
	if *growth > 0 {
		for i := 1; i <= *growth; i++ {
			r := measure(base, "growth-edit", i, func(ctx context.Context) error { return f.Edit(ctx, fmt.Sprintf("growth %08d", i)) })
			emit(r)
			if r.Error != "" {
				break
			}
			if i == 1 || i == 10 || i == 100 || i == *growth {
				at := base
				at.Sample = i
				if err := storage(ctx, f, at, "growth-storage"); err != nil {
					return err
				}
			}
		}
		if _, err := perftest.Git(ctx, root, nil, "gc", "--quiet"); err != nil {
			return err
		}
		return storage(ctx, f, base, "growth-after-gc")
	}
	for _, op := range strings.Split(*ops, ",") {
		for i := 1; i <= *samples; i++ {
			if err := f.Reset(ctx); err != nil {
				return err
			}
			// Warm the current snapshot consistently. Setup, reset, and GC are untimed.
			if _, err := f.Store.Read(ctx); err != nil {
				return err
			}
			call, err := operation(f, op, i)
			if err != nil {
				return err
			}
			runtime.GC()
			r := measure(base, op, i, call)
			emit(r)
			if r.Error != "" {
				break
			}
		}
	}
	return nil
}

func operation(f *perftest.Fixture, op string, sample int) (func(context.Context) error, error) {
	e := &workflow.Engine{Ledger: f.Store, Provider: &perftest.Provider{Finish: op == "complete"}, Owner: "performance-driver"}
	switch op {
	case "read":
		return func(ctx context.Context) error { _, err := f.Store.Read(ctx); return err }, nil
	case "noop":
		return func(ctx context.Context) error {
			return f.Store.Update(ctx, func(context.Context, *ledger.Transaction) error { return nil })
		}, nil
	case "edit":
		return func(ctx context.Context) error { return f.Edit(ctx, fmt.Sprintf("sample %08d", sample)) }, nil
	case "new-revision":
		return f.AddRevision, nil
	case "status":
		return func(ctx context.Context) error { _, err := e.Status(ctx, workflow.Scope{All: true}); return err }, nil
	case "submit":
		return func(ctx context.Context) error {
			_, err := e.Submit(ctx, workflow.Request{ID: "perf_request", Spec: record.JobSpec{Action: record.Verify, InputRevision: "revision_000000", Targets: []record.Target{perftest.Target}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &perftest.Build}})
			return err
		}, nil
	case "observe", "complete", "cycle-all":
		scope := workflow.Scope{Jobs: f.Active}
		if op == "cycle-all" {
			scope = workflow.Scope{All: true}
		} else if len(f.Active) == 0 {
			return nil, fmt.Errorf("%s requires active builds", op)
		}
		return func(ctx context.Context) error {
			r, err := e.Cycle(ctx, scope)
			if err != nil {
				return err
			}
			if len(r.Problems) > 0 {
				return fmt.Errorf("cycle problems: %+v", r.Problems)
			}
			return nil
		}, nil
	case "codec-encode":
		return func(context.Context) error { _, err := ledger.Encode(f.State); return err }, nil
	case "codec-decode":
		data, err := ledger.Encode(f.State)
		if err != nil {
			return nil, err
		}
		return func(context.Context) error { _, err := ledger.Decode(data); return err }, nil
	default:
		return nil, fmt.Errorf("unknown operation %q", op)
	}
}

func measure(base result, op string, sample int, call func(context.Context) error) result {
	ctx, cancel := context.WithTimeout(context.Background(), *budget)
	defer cancel()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	os.Setenv("DOCKHAND_PERF_ACTIVE", "1")
	start := time.Now()
	err := call(ctx)
	elapsed := time.Since(start)
	os.Unsetenv("DOCKHAND_PERF_ACTIVE")
	runtime.ReadMemStats(&after)
	base.PID, base.StartedNS = os.Getpid(), start.UnixNano()
	base.Kind, base.Operation, base.Sample = "sample", op, sample
	base.Milliseconds = float64(elapsed) / float64(time.Millisecond)
	base.Bytes, base.Allocations = after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs
	if err != nil {
		base.Error = err.Error()
	}
	return base
}

func emit(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		panic(err)
	}
}

func storage(ctx context.Context, f *perftest.Fixture, r result, kind string) error {
	r.Kind = kind
	if err := filepath.WalkDir(f.Repo.CommonDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			s, err := d.Info()
			if err != nil {
				return err
			}
			r.RepositoryBytes += s.Size()
		}
		return nil
	}); err != nil {
		return err
	}
	objects, err := perftest.Git(ctx, f.Repo.Root, nil, "count-objects", "-v")
	if err != nil {
		return err
	}
	r.Objects = string(objects)
	pins, err := f.Repo.ReadRefs(ctx, ledger.PinsPrefix)
	if err != nil {
		return err
	}
	r.Pins = len(pins)
	emit(r)
	return nil
}

func worker() error {
	ctx := context.Background()
	repo, err := git.Open(ctx, *workerRoot, "git")
	if err != nil {
		return err
	}
	locks, err := lock.NewDirectory(filepath.Join(*workerRoot, "locks"))
	if err != nil {
		return err
	}
	writer, err := locks.File("repositories", repo.CommonDir, "ledger")
	if err != nil {
		return err
	}
	s, err := ledger.New(repo, ledger.Options{WriterLock: writer})
	if err != nil {
		return err
	}
	f := &perftest.Fixture{Repo: repo, Store: s}
	if _, err = s.Read(ctx); err != nil {
		return err
	}
	runtime.GC()
	fmt.Println("ready")
	if _, err = bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return err
	}
	engine := workflow.Engine{Ledger: s, Provider: &perftest.Provider{}}
	for i := 1; i <= *samples; i++ {
		advanced := 0
		var call func(context.Context) error
		if *workerOp == "cycle-all" {
			call = func(ctx context.Context) error {
				cycle, err := engine.Cycle(ctx, workflow.Scope{All: true})
				advanced = len(cycle.Advanced)
				if err != nil {
					return err
				}
				if len(cycle.Problems) != 0 {
					return fmt.Errorf("cycle problems: %+v", cycle.Problems)
				}
				return nil
			}
		} else if *workerOp == "read" {
			call = func(ctx context.Context) error { _, err := s.Read(ctx); return err }
		} else {
			call = func(ctx context.Context) error {
				return f.Edit(ctx, fmt.Sprintf("worker %d sample %08d", os.Getpid(), i))
			}
		}
		sample := measure(result{}, *workerOp, i, call)
		sample.Advanced = advanced
		emit(sample)
	}
	return nil
}

func contend(ctx context.Context, f *perftest.Fixture, base result) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	type child struct {
		cmd     *exec.Cmd
		scanner *bufio.Scanner
		start   func() error
	}
	var children []child
	defer func() {
		for _, c := range children {
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
		}
	}()
	for i := 0; i < *workers+*readers; i++ {
		op := *workerOp
		if i >= *workers {
			op = "read"
		}
		cmd := exec.CommandContext(ctx, executable, "-worker-root", f.Repo.Root, "-worker-op", op, "-samples", strconv.Itoa(*samples), "-budget", budget.String())
		cmd.Env = perftest.Environment()
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		in, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		if err = cmd.Start(); err != nil {
			return err
		}
		scanner := bufio.NewScanner(out)
		children = append(children, child{cmd, scanner, func() error {
			_, err := fmt.Fprintln(in, "start")
			closeErr := in.Close()
			if err != nil {
				return err
			}
			return closeErr
		}})
		if !scanner.Scan() || scanner.Text() != "ready" {
			return fmt.Errorf("worker failed before barrier")
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := make(chan error, len(children))
	for i, c := range children {
		wg.Add(1)
		go func(i int, c child) {
			defer wg.Done()
			for c.scanner.Scan() {
				var r result
				if err := json.Unmarshal(c.scanner.Bytes(), &r); err != nil {
					errs <- err
					return
				}
				r.Completed, r.Sources, r.Active, r.History, r.Packed = base.Completed, base.Sources, base.Active, base.History, base.Packed
				r.Worker, r.Writers, r.Readers = i+1, *workers, *readers
				r.SnapshotBytes = base.SnapshotBytes
				mu.Lock()
				emit(r)
				mu.Unlock()
			}
			if err := c.scanner.Err(); err != nil {
				errs <- err
				return
			}
			if err := c.cmd.Wait(); err != nil {
				errs <- err
			}
		}(i, c)
	}
	for _, c := range children {
		if err := c.start(); err != nil {
			return err
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
