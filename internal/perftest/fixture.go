// Package perftest supplies disposable repositories for benchmarks and performance experiments.
// Application packages must not depend on it.
package perftest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/lock"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

type Fixture struct {
	Repo          *git.Repository
	Store         *ledger.Store
	State         ledger.State
	Sources       []record.Source
	Active        []record.JobID
	Version       string
	SnapshotBytes int
	pins          map[string]bool
}

var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
var Platform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
var Build = record.BuildConfig{Provider: "perf", Platform: Platform, EnvironmentDigest: "sha256:performance-fixture", FromSource: true, Tests: record.TestDeclared}
var Target = record.Target{Name: "fixture", Portfile: "Portfile", Variants: map[string]bool{"debug": false}}

// New seeds a full snapshot directly, outside the measured path. Source objects
// are generated in one fast-import process; ordinary operations still use Store.
// The last source is reserved for measuring addition of a previously unpinned source.
func New(ctx context.Context, root string, completed, sources, active int) (*Fixture, error) {
	if completed < 1 || sources < 1 || sources > completed || active < 0 {
		return nil, fmt.Errorf("invalid fixture sizes: completed=%d sources=%d active=%d", completed, sources, active)
	}
	if _, err := Git(ctx, root, nil, "init", "--quiet", "--object-format=sha1"); err != nil {
		return nil, err
	}
	if _, err := Git(ctx, root, nil, "config", "gc.auto", "0"); err != nil {
		return nil, err
	}
	repo, err := git.Open(ctx, root, "git")
	if err != nil {
		return nil, err
	}
	locks, err := lock.NewDirectory(filepath.Join(root, "locks"))
	if err != nil {
		return nil, err
	}
	writer, err := locks.File("repositories", repo.CommonDir, "ledger")
	if err != nil {
		return nil, err
	}
	store, err := ledger.New(repo, ledger.Options{WriterLock: writer})
	if err != nil {
		return nil, err
	}
	ss, err := makeSources(ctx, root, sources+1)
	if err != nil {
		return nil, err
	}
	f := &Fixture{Repo: repo, Store: store, State: ledger.NewState(), Sources: ss, pins: map[string]bool{}}
	for i := 0; i < completed+active; i++ {
		source := ss[i%sources]
		changeID := record.ChangeID(fmt.Sprintf("change_%06d", i))
		revisionID := record.RevisionID(fmt.Sprintf("revision_%06d", i))
		jobID := record.JobID(fmt.Sprintf("job_%06d", i))
		requestID := record.RequestID(fmt.Sprintf("request_%06d", i))
		attemptID := record.AttemptID(fmt.Sprintf("attempt_%06d", i))
		targetID := record.TargetID(fmt.Sprintf("target_%06d", i))
		resourceID := record.ResourceID(fmt.Sprintf("resource_%06d", i))
		submissionID := record.RequestID(fmt.Sprintf("submission_%06d", i))
		f.State.Changes[changeID] = record.Change{ID: changeID, Branch: fmt.Sprintf("dockhand/fixture-%06d", i), Targets: []record.Target{Target}, CurrentRevision: revisionID, Disposition: record.ChangeOpen, CreatedAt: Epoch}
		f.State.Revisions[revisionID] = record.Revision{ID: revisionID, ChangeID: changeID, Source: source, CreatedAt: Epoch}
		spec := record.JobSpec{Action: record.Verify, ChangeID: changeID, InputRevision: revisionID, Source: source, Targets: []record.Target{Target}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: &Build}
		job := record.Job{ID: jobID, RequestID: requestID, Spec: spec, ChangeID: changeID, State: record.JobCompleted, AcceptedAt: Epoch, AdmittedAt: &Epoch, FinishedAt: &Epoch, Detail: "Verification completed"}
		attempt := record.Attempt{ID: attemptID, JobID: jobID, TargetID: targetID, Spec: record.BuildSpec{RevisionID: revisionID, Source: source, Target: Target, Config: Build}, State: record.AttemptFinished, SubmissionID: submissionID, ClaimGeneration: 3, CreatedAt: Epoch, Run: record.ProviderRun{Provider: "perf", RequestID: submissionID, RunID: string(attemptID)}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: Epoch, Steps: []record.StepResult{{Package: "fixture", Phase: "build", Verdict: record.VerdictPassed}, {Package: "fixture", Phase: "test", Verdict: record.VerdictPassed}}, Logs: []record.Artifact{{Name: "build.log", Digest: "sha256:fixture-log", Location: "file:///external/fixture/build.log", MediaType: "text/plain"}}}}
		resource := record.Resource{ID: resourceID, AttemptID: attemptID, Handle: record.ResourceHandle{Provider: "perf", ID: string(resourceID)}, State: record.ResourceReleased, ReleasedAt: &Epoch, ClaimGeneration: 1}
		if i >= completed {
			job.State, job.FinishedAt = record.JobActive, nil
			attempt.State = record.AttemptRunning
			attempt.Evidence = &record.Evidence{Verdict: record.VerdictUnknown, ObservedAt: Epoch}
			resource.State, resource.ReleasedAt = record.ResourceActive, nil
			f.Active = append(f.Active, jobID)
		}
		f.State.Jobs[jobID], f.State.Requests[requestID] = job, jobID
		f.State.Attempts[attemptID], f.State.Resources[resourceID] = attempt, resource
		f.State.Plans[jobID] = record.VerificationPlan{JobID: jobID, RevisionID: revisionID, Targets: []record.VerificationTarget{{ID: targetID, Port: Target, Platform: Platform}}}
	}
	data, err := ledger.Encode(f.State)
	if err != nil {
		return nil, err
	}
	f.SnapshotBytes = len(data)
	blob, err := repo.WriteBlob(ctx, data)
	if err != nil {
		return nil, err
	}
	tree, err := repo.WriteTree(ctx, []git.TreeEntry{{Name: "state.json", Mode: 0100644, Type: "blob", Object: blob}})
	if err != nil {
		return nil, err
	}
	sig := git.Signature{Name: "Performance fixture", Email: "perf@example.invalid", When: Epoch}
	f.Version, err = repo.WriteCommit(ctx, git.Commit{Tree: tree, Message: "performance fixture\n", Author: sig, Committer: sig})
	if err != nil {
		return nil, err
	}
	refs := []git.RefChange{{Name: ledger.StateRef, Desired: git.RefValue{Exists: true, Object: f.Version}}, {Name: "refs/perftest/extra", Desired: git.RefValue{Exists: true, Object: string(ss[len(ss)-1].Commit)}}}
	for _, source := range ss[:sources] {
		for _, id := range []record.ObjectID{source.Commit, source.Tree, source.Base} {
			name := ledger.PinsPrefix + string(id)
			if !f.pins[name] {
				refs = append(refs, git.RefChange{Name: name, Desired: git.RefValue{Exists: true, Object: string(id)}})
				f.pins[name] = true
			}
		}
	}
	if err := repo.UpdateRefs(ctx, refs); err != nil {
		return nil, err
	}
	if _, err := Git(ctx, root, nil, "update-ref", "-d", "refs/heads/perf-source"); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Fixture) Reset(ctx context.Context) error {
	if _, err := Git(ctx, f.Repo.Root, nil, "update-ref", ledger.StateRef, f.Version); err != nil {
		return err
	}
	pins, err := f.Repo.ReadRefs(ctx, ledger.PinsPrefix)
	if err != nil {
		return err
	}
	var changes []git.RefChange
	for name, value := range pins {
		if !f.pins[name] {
			changes = append(changes, git.RefChange{Name: name, Expected: value})
		}
	}
	return f.Repo.UpdateRefs(ctx, changes)
}

func (f *Fixture) Edit(ctx context.Context, detail string) error {
	return f.Store.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		job := tx.State.Jobs["job_000000"]
		job.Detail = detail
		tx.State.Jobs[job.ID] = job
		return nil
	})
}

func (f *Fixture) AddRevision(ctx context.Context) error {
	return f.Store.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		tx.State.Revisions["revision_new"] = record.Revision{ID: "revision_new", ChangeID: "change_000000", Previous: "revision_000000", Source: f.Sources[len(f.Sources)-1], CreatedAt: Epoch.Add(time.Hour)}
		change := tx.State.Changes["change_000000"]
		change.CurrentRevision = "revision_new"
		tx.State.Changes[change.ID] = change
		return nil
	})
}

// AddHistory varies commit-chain length without changing the current snapshot.
// These synthetic commits intentionally share a tree; Growth uses real updates.
func (f *Fixture) AddHistory(ctx context.Context, count int) error {
	if count == 0 {
		return nil
	}
	var input bytes.Buffer
	for i := 0; i < count; i++ {
		message := fmt.Sprintf("history %d\n", i)
		fmt.Fprintf(&input, "commit %s\ncommitter Fixture <perf@example.invalid> 1767225600 +0000\ndata %d\n%s", ledger.StateRef, len(message), message)
		if i == 0 {
			fmt.Fprintf(&input, "from %s\n", f.Version)
		}
		input.WriteByte('\n')
	}
	if _, err := Git(ctx, f.Repo.Root, input.Bytes(), "fast-import", "--quiet", "--force"); err != nil {
		return err
	}
	ref, err := f.Repo.ReadRef(ctx, ledger.StateRef)
	if err != nil {
		return err
	}
	f.Version = ref.Object
	return nil
}

// Git is fixture plumbing, never the timed production Git wrapper.
func Git(ctx context.Context, root string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Stdin = root, bytes.NewReader(input)
	cmd.Env = Environment()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("fixture git %v: %w: %s", args, err, out)
	}
	return out, nil
}

func Environment() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func makeSources(ctx context.Context, root string, count int) ([]record.Source, error) {
	var input bytes.Buffer
	for i := 0; i < count; i++ {
		contents := fmt.Sprintf("PortSystem 1.0\nname fixture\nversion 1.%d\n", i)
		fmt.Fprintf(&input, "blob\nmark :%d\ndata %d\n%s\ncommit refs/heads/perf-source\nmark :%d\ncommitter Fixture <perf@example.invalid> 1767225600 +0000\ndata 8\nfixture\nM 100644 :%d Portfile\n\n", 2*i+1, len(contents), contents, 2*i+2, 2*i+1)
	}
	marks := filepath.Join(root, "marks")
	if _, err := Git(ctx, root, input.Bytes(), "fast-import", "--quiet", "--export-marks="+marks); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(marks)
	if err != nil {
		return nil, err
	}
	commits := make([]string, count)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.Fields(line)
		mark, err := strconv.Atoi(strings.TrimPrefix(parts[0], ":"))
		if err != nil {
			return nil, err
		}
		if mark%2 == 0 {
			commits[mark/2-1] = parts[1]
		}
	}
	data, err = Git(ctx, root, []byte(strings.Join(commits, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(bytes.NewReader(data))
	ss := make([]record.Source, count)
	for i, commit := range commits {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != commit || fields[1] != "commit" {
			return nil, fmt.Errorf("unexpected commit header %q", header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, err
		}
		body := make([]byte, size+1)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		tree := strings.Fields(string(body))[1]
		ss[i] = record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree), Base: record.ObjectID(commits[0])}
	}
	return ss, nil
}
