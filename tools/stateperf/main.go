package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
)

type sample struct {
	History     int
	Distinct    bool
	Operation   string
	Nanoseconds []int64
	Advanced    int
	Errors      []string
}

func (s sample) emit() {
	if err := json.NewEncoder(os.Stdout).Encode(s); err != nil {
		panic(err)
	}
}
func measure(ctx context.Context, n int, distinct bool, name string, iterations int, fn func() (int, error)) sample {
	s := sample{History: n, Distinct: distinct, Operation: name, Errors: []string{}}
	for range iterations {
		start := time.Now()
		advanced, err := fn()
		s.Nanoseconds = append(s.Nanoseconds, time.Since(start).Nanoseconds())
		s.Advanced += advanced
		if err != nil {
			s.Errors = append(s.Errors, err.Error())
		}
		if ctx.Err() != nil {
			break
		}
	}
	return s
}

type capacityProvider struct{ verify.Provider }

func (capacityProvider) Capabilities(context.Context) (verify.Capabilities, error) {
	return verify.Capabilities{Name: "perf", Platforms: []record.Platform{{OS: "darwin", Version: "25", Architecture: "arm64"}}, Capacity: 1}, nil
}
func (capacityProvider) Submit(context.Context, verify.Request) (verify.Submission, error) {
	return verify.Submission{State: verify.AtCapacity}, nil
}
func main() {
	sizes := flag.String("sizes", "100,1000,10000", "Completed jobs per fixture")
	iterations := flag.Int("iterations", 20, "Samples per operation")
	worker := flag.String("worker", "", "Internal subprocess database path")
	repository := flag.String("repository", "", "Internal subprocess repository ID")
	history := flag.Int("history", 0, "Internal subprocess history size")
	distinct := flag.Bool("distinct", false, "Internal subprocess source mode")
	workerID := flag.String("worker-id", "", "Internal worker identity")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if *iterations < 1 {
		panic("iterations must be positive")
	}
	if *worker != "" {
		s, err := sqlite.Open(ctx, *worker, sqlite.Options{})
		must(err)
		defer s.Close()
		e := workflow.Engine{State: state.Bind(s, record.Repository{ID: record.RepositoryID(*repository)}), Provider: capacityProvider{}, Owner: record.ProcessID(*workerID), Now: func() time.Time { return time.Now().Add(2 * time.Second) }, RetryDelay: time.Nanosecond}
		measure(ctx, *history, *distinct, "concurrent-cycle", *iterations, func() (int, error) {
			r, err := e.Cycle(ctx, workflow.Scope{Jobs: []record.JobID{"active"}})
			return len(r.Advanced), err
		}).emit()
		return
	}
	for _, size := range strings.Split(*sizes, ",") {
		n, err := strconv.Atoi(size)
		must(err)
		if n < 1 {
			panic("history must be positive")
		}
		for _, distinct := range []bool{false, true} {
			run(ctx, n, distinct, *iterations)
		}
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func run(ctx context.Context, n int, distinct bool, iterations int) {
	root, err := os.MkdirTemp("", "dockhand-stateperf-")
	must(err)
	defer os.RemoveAll(root)
	s, err := sqlite.Open(ctx, filepath.Join(root, "state.db"), sqlite.Options{})
	must(err)
	defer s.Close()
	repo, err := s.RegisterRepository(ctx, filepath.Join(root, "repo"))
	must(err)
	now := time.Now().UTC().Truncate(time.Millisecond)
	spec := record.JobSpec{Action: record.Verify, ChangeID: "change", InputRevision: "revision", Targets: []record.Target{{Name: "fixture", Portfile: "Portfile"}}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &record.BuildConfig{Provider: "perf", Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, EnvironmentDigest: "fixture", Tests: record.TestDeclared}}
	source := record.Source{Tree: record.ObjectID(fmt.Sprintf("%040x", 1)), Commit: record.ObjectID(fmt.Sprintf("%040x", 2))}
	spec.Source = source
	must(s.Update(ctx, repo.ID, func(ctx context.Context, tx state.Tx) error {
		if err := tx.PutChange(ctx, record.Change{ID: "change", CurrentRevision: "revision", Disposition: record.ChangeOpen, CreatedAt: now}); err != nil {
			return err
		}
		if err := tx.PutRevision(ctx, record.Revision{ID: "revision", ChangeID: "change", Source: source, CreatedAt: now}); err != nil {
			return err
		}
		for i := range n {
			id := fmt.Sprintf("history_%08d", i)
			question := spec
			if distinct {
				question.InputRevision = record.RevisionID(id)
				question.Source.Tree = record.ObjectID(fmt.Sprintf("%040x", i+100))
				question.Source.Commit = record.ObjectID(fmt.Sprintf("%040x", i+20000))
				if err := tx.PutRevision(ctx, record.Revision{ID: question.InputRevision, ChangeID: "change", Source: question.Source, CreatedAt: now}); err != nil {
					return err
				}
			}
			payload, err := json.Marshal(question)
			if err != nil {
				return err
			}
			if err = tx.PutRequest(ctx, record.AcceptedRequest{ID: record.RequestID(id), Kind: record.JobRequest, Payload: payload, AcceptedAt: now}); err != nil {
				return err
			}
			if err = tx.PutJob(ctx, record.Job{ID: record.JobID(id), RequestID: record.RequestID(id), Spec: question, ChangeID: "change", State: record.JobCompleted, Phase: record.PhaseVerification, AcceptedAt: now, FinishedAt: &now}); err != nil {
				return err
			}
			build := record.BuildSpec{RevisionID: question.InputRevision, Source: question.Source, Target: question.Targets[0], Config: *question.Build}
			if err = tx.PutAttempt(ctx, record.Attempt{ID: record.AttemptID(id), JobID: record.JobID(id), TargetID: "target", Spec: build, State: record.AttemptFinished, CreatedAt: now, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: now}}); err != nil {
				return err
			}
			if err = tx.PutSubmission(ctx, record.Submission{ID: record.RequestID("submit_" + id), AttemptID: record.AttemptID(id), Sequence: 1, Provider: "perf", RunID: id, CreatedAt: now, AdmittedAt: &now}); err != nil {
				return err
			}
			if err = tx.PutResource(ctx, record.Resource{ID: record.ResourceID(id), AttemptID: record.AttemptID(id), SubmissionID: record.RequestID("submit_" + id), Handle: record.ResourceHandle{Provider: "perf", ID: id}, State: record.ResourceReleased, ReleasedAt: &now}); err != nil {
				return err
			}
		}
		return nil
	}))
	counter := 0
	measure(ctx, n, distinct, "job-write", iterations, func() (int, error) {
		counter++
		return 1, s.Update(ctx, repo.ID, func(ctx context.Context, tx state.Tx) error {
			job, err := tx.Job(ctx, "history_00000000")
			if err != nil {
				return err
			}
			job.Detail = strconv.Itoa(counter)
			return tx.PutJob(ctx, job)
		})
	}).emit()
	e := workflow.Engine{State: state.Bind(s, repo), Provider: capacityProvider{}}
	measure(ctx, n, distinct, "idle-cycle", iterations, func() (int, error) { r, err := e.Cycle(ctx, workflow.Scope{All: true}); return len(r.Advanced), err }).emit()
	measure(ctx, n, distinct, "selected-status", iterations, func() (int, error) {
		_, err := e.Status(ctx, workflow.Scope{Jobs: []record.JobID{"history_00000000"}})
		return 0, err
	}).emit()
	must(s.Update(ctx, repo.ID, func(ctx context.Context, tx state.Tx) error {
		payload, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		if err = tx.PutRequest(ctx, record.AcceptedRequest{ID: "active", Kind: record.JobRequest, Payload: payload, AcceptedAt: now}); err != nil {
			return err
		}
		return tx.PutJob(ctx, record.Job{ID: "active", RequestID: "active", Spec: spec, ChangeID: "change", State: record.JobQueued, Phase: record.PhaseVerification, AcceptedAt: now})
	}))
	e.Now = func() time.Time { now = now.Add(2 * time.Second); return now }
	measure(ctx, n, distinct, "active-cycle", iterations, func() (int, error) {
		r, err := e.Cycle(ctx, workflow.Scope{Jobs: []record.JobID{"active"}})
		return len(r.Advanced), err
	}).emit()
	// Release the synthetic retry delay before subprocesses use a real clock.
	must(s.Update(ctx, repo.ID, func(ctx context.Context, tx state.Tx) error {
		as, err := tx.AttemptsForJob(ctx, "active")
		if err != nil {
			return err
		}
		a := as[0]
		a.RetryAt = nil
		return tx.PutAttempt(ctx, a)
	}))
	exe, err := os.Executable()
	must(err)
	commands := []*exec.Cmd{}
	outputs := []*bytes.Buffer{}
	for i := range 4 {
		cmd := exec.CommandContext(ctx, exe, "-worker", s.Path(), "-repository", string(repo.ID), "-history", strconv.Itoa(n), "-distinct="+strconv.FormatBool(distinct), "-iterations", strconv.Itoa(iterations), "-worker-id", fmt.Sprint(i))
		output := &bytes.Buffer{}
		cmd.Stdout = output
		cmd.Stderr = output
		must(cmd.Start())
		commands = append(commands, cmd)
		outputs = append(outputs, output)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			panic(fmt.Sprintf("%v: %s", err, outputs[i].String()))
		}
		_, err = os.Stdout.Write(outputs[i].Bytes())
		must(err)
	}
}
