package cli

import (
	"bytes"
	"context"
	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type detachOnAcceptance struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *detachOnAcceptance) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if bytes.Contains(p, []byte("Accepted job ")) {
		w.cancel()
	}
	return n, err
}

func TestVerifyCLISelectsExplicitWorkingTreeOrBranch(t *testing.T) {
	t.Parallel()
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "checkout", true: "branch"}[explicit], func(t *testing.T) {
			config, repo, _ := preparationCLI(t)
			command := exec.CommandContext(t.Context(), "git", "reset", "--hard", "HEAD")
			command.Dir = repo.Root
			out, err := command.CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "devel/fixture/Portfile"), []byte("PortSystem 1.0\nname fixture\nversion 2.0\ncategories devel\n"), 0600))
			config.Tart.Home = t.TempDir()
			config.Tart.Image = "base"
			config.Tart.Executable = filepath.Join(t.TempDir(), "tart")
			testsupport.WriteExecutable(t, config.Tart.Executable, "#!/bin/sh\n[ \"$1\" = list ] || exit 1\nprintf '%s\\n' '[{\"Name\":\"base\",\"Source\":\"local\",\"State\":\"stopped\"}]'\n")
			image := filepath.Join(config.Tart.Home, "vms", "base")
			require.NoError(t, os.MkdirAll(image, 0700))
			for _, name := range []string{"config.json", "disk.img", "nvram.bin"} {
				require.NoError(t, os.WriteFile(filepath.Join(image, name), []byte("fixture"), 0600))
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stderr := &detachOnAcceptance{cancel: cancel}
			var stdout bytes.Buffer
			args := []string{"verify", "fixture", "--json", "-v"}
			if explicit {
				args = append(args, "--adopt", "candidate")
			} else {
				args = append(args, "--working-tree")
			}
			err = Run(ctx, args, Streams{Out: &stdout, Err: stderr}, config)
			require.ErrorIs(t, err, context.Canceled)
			status, err := app.FilteredStatus(t.Context(), config, workflow.StatusFilter{})
			require.NoError(t, err)
			require.Len(t, status.Jobs, 1)
			job := status.Jobs[0].Job
			require.Equal(t, record.JobQueued, job.State)
			if explicit {
				require.Nil(t, job.Spec.Checkout)
				require.NotEmpty(t, job.Spec.Source.Commit)
				require.Contains(t, stderr.String(), "Source: committed branch candidate")
			} else {
				require.NotNil(t, job.Spec.Checkout)
				require.Empty(t, job.Spec.Source.Commit)
				require.Equal(t, 1, job.Spec.Checkout.ModifiedFiles)
				require.Contains(t, stderr.String(), "1 modified files captured")
			}
			require.Contains(t, stderr.String(), "Target: fixture")
		})
	}
}
