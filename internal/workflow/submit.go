package workflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow/policy"
)

// BranchInput adopts a bound branch as a revision of the contribution
// tracked on it, or as a new contribution when a publication binds one;
// acceptance rechecks its expectations inside the transaction.
type BranchInput struct {
	Scope            *record.ReleaseScope `json:",omitempty"`
	Name             string
	ExpectedChange   record.ChangeID
	ExpectedRevision record.RevisionID
	// InferredTarget is the recorded contribution target used for inference.
	// Acceptance rechecks it even if the contribution revision has not changed.
	InferredTarget *record.Target `json:",omitempty"`
	// Shared lists the files under _resources the branch changes beside its
	// port and what loads them, for the revision adoption records.
	Shared []record.SharedFile `json:",omitempty"`
}

func validateBranchInput(branch *BranchInput, spec record.JobSpec) error {
	if branch == nil {
		return nil
	}
	if !git.ValidBranchName(branch.Name) || (spec.Action != record.Verify && spec.Action != record.Publish) || spec.InputRevision != "" || spec.ChangeID != "" || len(spec.Targets) != 1 || (spec.Build == nil) != (spec.Verification == record.VerificationSkipped) || (branch.ExpectedChange == "") != (branch.ExpectedRevision == "") {
		return fmt.Errorf("%w: branch adoption requires one frozen verification input and matching revision preconditions", ErrInvalidRequest)
	}
	if branch.InferredTarget != nil && (spec.Action != record.Verify || branch.ExpectedChange == "" || branch.InferredTarget.Portfile != spec.Targets[0].Portfile) {
		return fmt.Errorf("%w: inferred verification requires a tracked contribution target", ErrInvalidRequest)
	}
	if spec.Action == record.Publish && (spec.Source.Commit == "" || spec.Source.Base == "" || spec.Publication == nil || spec.Publication.SourceBranch() != branch.Name) {
		return ErrInvalidRequest
	}
	if spec.Checkout != nil && spec.Checkout.Branch != branch.Name {
		return fmt.Errorf("%w: checkout branch disagrees with binding", ErrInvalidRequest)
	}
	if branch.ExpectedChange != "" && (!validToken(string(branch.ExpectedChange)) || !validToken(string(branch.ExpectedRevision))) {
		return ErrInvalidRequest
	}
	return nil
}

func adoptBranch(ctx context.Context, tx state.Tx, spec record.JobSpec, input BranchInput, now time.Time) (record.JobSpec, error) {
	change, err := tx.OpenChangeByBranch(ctx, input.Name)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return record.JobSpec{}, err
	}
	if change.ID != input.ExpectedChange || change.CurrentRevision != input.ExpectedRevision {
		return record.JobSpec{}, fmt.Errorf("%w: tracked branch %s changed while binding", ErrStaleRevision, input.Name)
	}
	if input.InferredTarget != nil && (len(change.Targets) != 1 || record.CompareTargets(change.Targets[0], *input.InferredTarget) != 0) {
		return record.JobSpec{}, fmt.Errorf("%w: tracked targets changed while binding; run verify again", ErrStaleRevision)
	}
	if change.ID == "" && spec.Action != record.Publish {
		return spec, nil
	}
	var previous record.Revision
	if change.ID == "" {
		change = record.Change{InitiatingTarget: spec.Targets[0].Name, ID: record.ChangeID("change_" + rand.Text()), Branch: input.Name, Targets: spec.Targets, Disposition: record.ChangeOpen, CreatedAt: now}
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	} else {
		previous, err = tx.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return record.JobSpec{}, err
		}
	}
	if change.ID != "" {
		if err := correctionNotPending(ctx, tx, change.ID); err != nil {
			return record.JobSpec{}, err
		}
	}
	if !previous.Scope.SameMembership(input.Scope) {
		return record.JobSpec{}, fmt.Errorf("%w: shared-release scope must be checked before branch adoption", ErrInvalidRequest)
	}
	revision := previous
	if revision.ID == "" || revision.Source != spec.Source {
		revision = record.Revision{Scope: input.Scope, ID: record.RevisionID("revision_" + rand.Text()), ChangeID: change.ID, Previous: change.CurrentRevision, Source: spec.Source, CreatedAt: now, Shared: input.Shared}
		if err := tx.PutRevision(ctx, revision); err != nil {
			return record.JobSpec{}, err
		}
		change.CurrentRevision = revision.ID
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	}
	spec.ChangeID, spec.InputRevision = change.ID, revision.ID
	return spec, nil
}

// Request supplies a caller-owned identity and the intent to accept.
// Preserve the original request for retries, including after an uncertain commit.
// A stored [record.JobSpec] may contain source fields filled by intake and is not a
// substitute for the original request when retrying.
type Request struct {
	ID   record.RequestID
	Spec record.JobSpec
	// Branch adopts the explicit source as a revision of an existing change.
	// Publish also creates the contribution when the branch is not yet tracked.
	Branch *BranchInput
}

// Receipt confirms durable acceptance of one job. It does not establish driver
// pickup, provider admission, or completion. Equivalent retries return the same
// job identity and acceptance time.
type Receipt struct {
	// Source and ChangeID identify the accepted work, including a joined retry.
	Source     record.Source
	ChangeID   record.ChangeID
	RequestID  record.RequestID
	JobID      record.JobID
	AcceptedAt time.Time
}

// Submit validates and durably accepts a request in one state transaction.
// A Branch input adopts its frozen source as a revision in that transaction.
// It copies and normalizes the requested inputs, binds an existing revision when
// selected, and records a queued job. It performs no preparation or provider work.
//
// Reusing an ID with equivalent intent returns the original receipt, even after
// the job progresses or its change advances or closes. Different intent returns
// an error wrapping state.ErrConflict. Every error returns a zero receipt,
// including a state commit whose outcome is uncertain; retry with the same ID
// rather than creating a new request.
func (e *Engine) Submit(ctx context.Context, request Request) (Receipt, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return Receipt{}, errNoState
	}
	if !validToken(string(request.ID)) {
		return Receipt{}, fmt.Errorf("%w: a nonempty request ID without whitespace or control characters is required", ErrInvalidRequest)
	}
	spec, err := normalizeSpec(request.Spec)
	if err != nil {
		return Receipt{}, err
	}
	if request.Branch != nil {
		branch := *request.Branch
		if branch.InferredTarget != nil {
			target := *branch.InferredTarget
			target.Variants = maps.Clone(target.Variants)
			branch.InferredTarget = &target
		}
		// The shared files a branch changes are read from the immutable
		// source here, before the transaction: the lookup runs git, and
		// the writer lock is not held over a subprocess.
		branch.Shared = e.sharedFiles(ctx, spec.Source)
		request.Branch = &branch
	}
	if err := validateBranchInput(request.Branch, spec); err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	intent := spec
	if intent.InputRevision != "" {
		intent.ChangeID = ""
	}
	var payload []byte
	if request.Branch == nil {
		payload, err = json.Marshal(intent)
	} else {
		payload, err = json.Marshal(struct {
			Spec   record.JobSpec
			Branch BranchInput
		}{intent, *request.Branch})
	}
	if err != nil {
		return Receipt{}, err
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		previous, err := tx.Request(ctx, request.ID)
		if err == nil {
			if previous.Kind != record.JobRequest || !bytes.Equal(previous.Payload, payload) {
				return state.ErrConflict
			}
			job, err := tx.JobForRequest(ctx, request.ID)
			if err != nil {
				return err
			}
			if spec.ChangeID != "" && spec.ChangeID != job.ChangeID {
				return state.ErrConflict
			}
			receipt = Receipt{RequestID: request.ID, JobID: job.ID, AcceptedAt: job.AcceptedAt, Source: job.Spec.Source, ChangeID: job.ChangeID}
			return nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		now := e.now()
		if now.IsZero() {
			return ErrInvalidRequest
		}
		var accepted record.JobSpec
		if request.Branch == nil {
			accepted, err = bindRevision(ctx, spec, tx)
		} else {
			accepted, err = adoptBranch(ctx, tx, spec, *request.Branch, now)
		}
		if err != nil {
			return err
		}
		if accepted.Preparation != nil && accepted.Preparation.Correction != nil {
			correction := *accepted.Preparation.Correction
			if _, err := correctionCurrent(ctx, tx, correction); err != nil {
				return err
			}
			if err := correctionIdle(ctx, tx, correction.ChangeID, ""); err != nil {
				return err
			}
			accepted.ChangeID = correction.ChangeID
		}
		id := record.JobID("job_" + rand.Text())
		job, joined, err := acceptPreparation(ctx, tx, id, request.ID, accepted, now)
		if err != nil {
			return err
		}
		if joined != nil {
			if err := tx.PutRequest(ctx, record.AcceptedRequest{ID: request.ID, Kind: record.JobRequest, Payload: payload, AcceptedAt: now, JoinedJob: joined.ID}); err != nil {
				return err
			}
			receipt = Receipt{RequestID: request.ID, JobID: joined.ID, AcceptedAt: joined.AcceptedAt, Source: joined.Spec.Source, ChangeID: joined.ChangeID}
			return nil
		}
		accepted = job.Spec
		if err = tx.PutRequest(ctx, record.AcceptedRequest{ID: request.ID, Kind: record.JobRequest, Payload: payload, AcceptedAt: now}); err != nil {
			return err
		}
		if err = tx.PutJob(ctx, job); err != nil {
			return err
		}
		if accepted.Action == record.Publish {
			job, err := tx.Job(ctx, id)
			if err != nil {
				return err
			}
			if accepted.ChangeID == "" || accepted.Source.Commit != accepted.Publication.Desired.Head {
				return ErrInvalidRequest
			}
			change, err := tx.Change(ctx, accepted.ChangeID)
			if err != nil {
				return err
			}
			if change.Branch != accepted.Publication.SourceBranch() {
				return ErrInvalidRequest
			}
			if err := policy.PublicationEvidence(ctx, tx, job, *job.Spec.Publication); err != nil {
				return err
			}
			action := record.PublicationAction{ID: record.PublicationID("publication_" + rand.Text()), JobID: id, ChangeID: accepted.ChangeID, RevisionID: accepted.InputRevision, Spec: *accepted.Publication, State: record.PublicationPending}
			if err := policy.ValidatePublicationAction(job, action); err != nil {
				return err
			}
			if err := supersedeSettledPublication(ctx, tx, action); err != nil {
				return err
			}
			if err := tx.PutPublication(ctx, action); err != nil {
				return fmt.Errorf("accept publication (another job may own this remote branch): %w", err)
			}
		}
		receipt = Receipt{RequestID: request.ID, JobID: id, AcceptedAt: now, Source: job.Spec.Source, ChangeID: job.ChangeID}
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// bindRevision resolves an existing revision from a transaction's snapshot and
// freezes its source into a new job specification. Verification may select an
// older revision of an open change; other actions require its current revision.
// Requests with an explicit source and no input revision pass through unchanged.
func bindRevision(ctx context.Context, spec record.JobSpec, reader state.Reader) (record.JobSpec, error) {
	if spec.InputRevision == "" {
		return spec, nil
	}
	revision, err := reader.Revision(ctx, spec.InputRevision)
	if err != nil {
		return record.JobSpec{}, err
	}
	change, err := reader.Change(ctx, revision.ChangeID)
	if err != nil {
		return record.JobSpec{}, err
	}

	if spec.ChangeID != "" && spec.ChangeID != change.ID {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s does not belong to change %s", ErrInvalidRequest, revision.ID, spec.ChangeID)
	}
	if change.Disposition != record.ChangeOpen {
		return record.JobSpec{}, fmt.Errorf("%w: change %s is %s", ErrInvalidRequest, change.ID, change.Disposition)
	}
	if spec.Action != record.Verify && change.CurrentRevision != revision.ID {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s is no longer current for change %s", ErrStaleRevision, revision.ID, change.ID)
	}
	if err := validateSource(revision.Source); err != nil {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s: %v", state.ErrInvalid, revision.ID, err)
	}
	if spec.Action == record.Publish && revision.Source.Commit == "" {
		return record.JobSpec{}, fmt.Errorf("%w: publication requires a committed revision", ErrInvalidRequest)
	}
	if err := correctionNotPending(ctx, reader, change.ID); err != nil {
		return record.JobSpec{}, err
	}
	spec.ChangeID = change.ID
	spec.Source = revision.Source
	return spec, nil
}

// supersedeSettledPublication frees the remote head branch a settled
// publication still holds. An uncertain pull request request keeps its
// state when its job settles as needing attention, so that nobody writes
// to that head unknowingly; a person who looked and asked to publish again
// is the someone it waited for, and the new request's binding has already
// observed the forge, so it updates a pull request that exists and creates
// one that does not. A publication whose job is still running keeps the
// head, and the new request is refused as before.
func supersedeSettledPublication(ctx context.Context, tx state.Tx, action record.PublicationAction) error {
	spec := action.Spec
	stale, err := tx.ActivePublicationForHead(ctx, spec.Forge, spec.HeadRepository, spec.HeadBranch)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	job, err := tx.Job(ctx, stale.JobID)
	if err != nil {
		return err
	}
	if !job.State.Terminal() {
		return fmt.Errorf("%w: job %s still owns the remote branch %s:%s", ErrRequestConflict, job.ID, spec.HeadRepository, spec.HeadBranch)
	}
	stale.State = record.PublicationNeedsAttention
	stale.LastError = "superseded by publication " + string(action.ID) + " after its job settled: " + stale.LastError
	return tx.PutPublication(ctx, stale)
}
