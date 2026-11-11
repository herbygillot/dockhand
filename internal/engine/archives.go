package engine

import (
	"context"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// ArchiveDirectory is where the checkout's kept archives are (decision
// 28), beside the database: each file named by the hex of its sha256. Each
// checkout the database serves keeps its own, so cleaning one up never
// weighs another's.
func (e *Engine) ArchiveDirectory() string {
	return filepath.Join(archivesRoot(e.options.Database), string(e.Repository))
}

// archivesRoot is where every checkout's kept archives are, beside the
// database.
func archivesRoot(database string) string {
	return filepath.Join(filepath.Dir(database), "archives")
}

// MasterReadings is the directory the readings of master's archives are
// kept in, beside the kept archives: what a first check compares against
// (binaryarchive.Catalog's Readings).
func (e *Engine) MasterReadings() string {
	return filepath.Join(e.ArchiveDirectory(), "master")
}

// archiveBytes words the size of archives: 820 MB, 1.4 GB.
func archiveBytes(archives []model.Archive) string {
	var n int64
	for _, archive := range archives {
		n += archive.Size
	}
	return prose.Bytes(n)
}

// Artifact is one archive a check's passed target built on one platform:
// its port, environment, MacPorts' name for it, and where it's kept on
// this Mac; Missing says why there's none, where it wasn't kept or is
// gone.
type Artifact struct {
	Port        string
	Environment model.Environment
	Name, Path  string
	Missing     string
}

// Artifacts are the archives a check's passed targets built, one for each
// port and platform it passed on, for otool and smoke tests by hand: a
// passed target's archive is kept by its digest, named nowhere a person
// sees (field testing's outdated-ports session, the person's "dockhand
// artifacts is a yes"). A target that didn't pass built none.
func (e *Engine) Artifacts(ctx context.Context, run model.Run) ([]Artifact, error) {
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	var artifacts []Artifact
	for _, target := range evidence.Targets {
		for _, cell := range target.Outcomes {
			if cell.Outcome != model.OutcomePassed {
				continue
			}
			artifact := Artifact{Port: string(target.Target.ID), Environment: cell.Environment}
			switch {
			case cell.Archive == "":
				artifact.Missing = "its archive wasn't kept"
			default:
				archive, kept, err := e.archives().Kept(ctx, cell.Archive)
				if err != nil {
					return nil, err
				}
				artifact.Name = archive.Name
				if kept {
					artifact.Path = e.archives().Path(cell.Archive)
				} else {
					artifact.Missing = "its archive is gone, as cleanup removes one no check uses"
				}
			}
			artifacts = append(artifacts, artifact)
		}
	}
	return artifacts, nil
}
