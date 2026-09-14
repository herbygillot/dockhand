package workflow_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func inferenceFixture(t *testing.T) (*fixture, *boundPorts, workflow.VerificationRequest) {
	t.Helper()
	f, ports := bindingFixture(t)
	base, _, err := f.repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	source := commitPort(t, f, "candidate", "version 2\n")
	source.Base = record.ObjectID(base)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		revision := record.Revision{ID: "inference-revision", ChangeID: change.ID, Previous: change.CurrentRevision, Source: source, CreatedAt: f.now()}
		if err := tx.PutRevision(ctx, revision); err != nil {
			return err
		}
		change.Branch, change.CurrentRevision = "candidate", revision.ID
		change.Targets = []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile", Variants: map[string]bool{"debug": true, "x11": false}}}
		return tx.PutChange(ctx, change)
	}))
	request := bindRequest(f, "inferred")
	request.Selection = macports.Selection{}
	return f, ports, request
}

func TestInferredVerificationUsesRecordedScopeAndFreezesItsInput(t *testing.T) {
	f, ports, request := inferenceFixture(t)
	bound, err := f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "devel/fixture/Portfile", ports.selection.Selector)
	require.Equal(t, map[string]bool{"debug": true, "x11": false}, bound.Request.Spec.Targets[0].Variants)
	require.Equal(t, &bound.Request.Spec.Targets[0], bound.Request.Branch.InferredTarget)
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Empty(t, status.Jobs, "binding must not accept work")
	acceptedSource := bound.Request.Spec.Source
	commitPort(t, f, "candidate", "version 3\n")
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	attempt := f.attempt(t, receipt.JobID)
	require.Equal(t, acceptedSource, attempt.Spec.Source)
	require.Equal(t, bound.Request.Spec.Targets[0], attempt.Spec.Target)
	retry, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
}

func TestInferredVerificationPreservesAndOverridesSubportAndVariants(t *testing.T) {
	f, _, request := inferenceFixture(t)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Targets[0].Name, change.Targets[0].Subport = "fixture-devel", "fixture-devel"
		return tx.PutChange(ctx, change)
	}))
	bound, err := f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "fixture-devel", bound.Request.Spec.Targets[0].Subport)
	require.Equal(t, "fixture-devel", bound.Request.Spec.Targets[0].Name)
	request.Selection = macports.Selection{Subport: "fixture-tools", Variants: map[string]bool{"debug": false, "extra": true}}
	bound, err = f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "fixture-tools", bound.Request.Spec.Targets[0].Subport)
	require.Equal(t, map[string]bool{"debug": false, "extra": true, "x11": false}, bound.Request.Spec.Targets[0].Variants)
	require.Equal(t, "fixture-devel", bound.Request.Branch.InferredTarget.Name)
	request.Selection = macports.Selection{Selector: "fixture"}
	bound, err = f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.Nil(t, bound.Request.Branch.InferredTarget)
	require.Empty(t, bound.Request.Spec.Targets[0].Subport)
	require.Empty(t, bound.Request.Spec.Targets[0].Variants)
}

func TestInferredVerificationRefusesUnknownAndChangedScope(t *testing.T) {
	for _, scenario := range []string{"untracked", "closed", "no-targets", "many-targets", "no-base", "missing-base", "renamed-target", "outside-port", "shared-resource", "neighbor-prefix"} {
		t.Run(scenario, func(t *testing.T) {
			f, ports, request := inferenceFixture(t)
			if scenario == "renamed-target" {
				ports.targetName = "renamed"
			} else if scenario == "outside-port" || scenario == "shared-resource" || scenario == "neighbor-prefix" {
				name := "devel/other/Portfile"
				if scenario == "shared-resource" {
					name = "_resources/port1.0/group/test-1.0.tcl"
				}
				if scenario == "neighbor-prefix" {
					name = "devel/fixture-extra/Portfile"
				}
				editInferenceBranch(t, f, name)
			} else {
				require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
					change, err := tx.Change(ctx, "change")
					if err != nil {
						return err
					}
					switch scenario {
					case "untracked":
						change.Branch = "different"
					case "closed":
						change.Disposition = record.ChangeClosed
					case "no-targets":
						change.Targets = nil
					case "many-targets":
						change.Targets = append(change.Targets, record.Target{Name: "other", Portfile: "devel/other/Portfile"})
					default:
						revision, err := tx.Revision(ctx, change.CurrentRevision)
						if err != nil {
							return err
						}
						revision.Previous, revision.ID = revision.ID, "changed-base"
						revision.Source.Base = ""
						if scenario == "missing-base" {
							revision.Source.Base = record.ObjectID(strings.Repeat("f", 40))
						}
						if err := tx.PutRevision(ctx, revision); err != nil {
							return err
						}
						change.CurrentRevision = revision.ID
					}
					return tx.PutChange(ctx, change)
				}))
			}
			_, err := f.engine.BindVerification(t.Context(), request)
			require.ErrorContains(t, err, "specify")
			if scenario == "many-targets" {
				require.ErrorContains(t, err, "other (devel/other/Portfile)")
			}
			if scenario != "renamed-target" {
				require.Empty(t, ports.seenRoot, "scope refusal should precede evaluation")
			}
			request.Selection.Selector = "fixture"
			_, err = f.engine.BindVerification(t.Context(), request)
			require.NoError(t, err, "explicit selection must remain usable")
			status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
			require.NoError(t, err)
			require.Empty(t, status.Jobs)
		})
	}
}

func editInferenceBranch(t *testing.T, f *fixture, name string) {
	t.Helper()
	commit, tree, err := f.repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	tree, err = f.repo.EditTree(t.Context(), tree, []git.FileEdit{{Path: name, After: []byte("extra"), Mode: 0o100644}})
	require.NoError(t, err)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	after, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{commit}, Message: "extra edit", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: commit}, Desired: git.RefValue{Exists: true, Object: after}}}))
}

func TestInferredVerificationCapturesCurrentEditsAndRejectsDetachedScope(t *testing.T) {
	f, _, request := inferenceFixture(t)
	out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "checkout", "-f", "candidate").CombinedOutput()
	require.NoError(t, err, "%s", out)
	name := filepath.Join(f.repo.Root, "devel/fixture/Portfile")
	require.NoError(t, os.WriteFile(name, []byte("version 4\n"), 0600))
	request.Branch = ""
	bound, err := f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.Empty(t, bound.Request.Spec.Source.Commit)
	require.Equal(t, 1, bound.Request.Spec.Checkout.ModifiedFiles)
	require.NotNil(t, bound.Request.Branch.InferredTarget)
	explicit := request
	explicit.Branch = "candidate"
	committed, err := f.engine.BindVerification(t.Context(), explicit)
	require.NoError(t, err)
	require.NotEqual(t, bound.Request.Spec.Source.Tree, committed.Request.Spec.Source.Tree)
	require.NoError(t, os.WriteFile(filepath.Join(f.repo.Root, "devel/fixture/new.patch"), []byte("patch"), 0600))
	_, err = f.engine.BindVerification(t.Context(), request)
	require.ErrorContains(t, err, "stage these files")
	require.NoError(t, os.Remove(filepath.Join(f.repo.Root, "devel/fixture/new.patch")))
	out, err = exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "checkout", "--detach", "-f").CombinedOutput()
	require.NoError(t, err, "%s", out)
	_, err = f.engine.BindVerification(t.Context(), request)
	require.ErrorContains(t, err, "without a tracked contribution")
}

func TestInferredVerificationRechecksRecordedTargetAtAcceptance(t *testing.T) {
	f, _, request := inferenceFixture(t)
	bound, err := f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Targets[0].Variants["debug"] = false
		return tx.PutChange(ctx, change)
	}))
	_, err = f.engine.Submit(t.Context(), bound.Request)
	require.ErrorIs(t, err, workflow.ErrStaleRevision)
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Empty(t, status.Jobs)
}
