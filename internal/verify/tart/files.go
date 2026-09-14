package tart

import (
	"archive/tar"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

func atomicFile(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".dockhand-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func safeToken(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-") && !strings.ContainsAny(s, "/\\") && strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}
func validPortfile(s string) bool {
	return fs.ValidPath(s) && !strings.ContainsAny(s, "\\\x00\r\n") && len(strings.Split(s, "/")) == 3 && filepath.Base(s) == "Portfile"
}

//go:embed guest.tcl
var guestScript []byte

type guestInput struct {
	Protocol int
	ID       string
	Digest   string
	Spec     record.BuildSpec
	Prefix   string
}
type guestResult struct {
	Protocol int
	ID       string
	Digest   string
	State    string
	Verdict  record.Verdict
	Steps    []record.StepResult
	Failure  *record.Failure
	Detail   string
}

// verifierDigest changes with the guest program and its launch protocol.
func verifierDigest() string {
	return digest(append([]byte("tart-verification-v2\x00"+guestExecScript+"\x00"+string(guestPlist("/prefix"))), guestScript...))
}

func makeInput(ctx context.Context, repo *git.Repository, request verify.Request, c Config, directory string, client *http.Client) (string, error) {
	if request.Spec.Source.Commit != "" {
		trees, err := repo.CommitTrees(ctx, []string{string(request.Spec.Source.Commit)})
		if err != nil {
			return "", err
		}
		if trees[string(request.Spec.Source.Commit)] != string(request.Spec.Source.Tree) {
			return "", fmt.Errorf("tart: source commit and tree disagree")
		}
	}
	snapshot, err := repo.Materialize(ctx, string(request.Spec.Source.Tree))
	if err != nil {
		return "", err
	}
	defer snapshot.Close()
	indexConfig := portindex.Config{
		Executable: c.PortIndexExecutable, Digest: c.PortIndexDigest,
		MirrorURL: c.PortIndexURL, CacheDirectory: filepath.Join(c.ArtifactDirectory, "indexes"),
	}
	if err = portindex.Stage(ctx, repo, request.Spec.Source, request.Spec.Config.Platform, indexConfig, snapshot.Root, client); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(directory, ".input-")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	output := tar.NewWriter(temp)
	err = filepath.WalkDir(snapshot.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(snapshot.Root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join("ports", rel))
		header.Uid = 0
		header.Gid = 0
		header.Uname = "root"
		header.Gname = "wheel"
		if info.IsDir() || info.Mode()&0111 != 0 {
			header.Mode = 0755
		} else {
			header.Mode = 0644
		}
		if err = output.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(output, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err == nil {
		input, _ := json.Marshal(guestInput{Protocol: 1, ID: string(request.ID), Digest: buildDigest(request.Spec), Spec: request.Spec, Prefix: c.GuestPrefix})
		for name, data := range map[string][]byte{"guest.tcl": guestScript, "input.json": input, "guest.plist": guestPlist(c.GuestPrefix)} {
			if err = output.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
				break
			}
			if _, err = output.Write(data); err != nil {
				break
			}
		}
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr = temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, "input.tar")
	return path, os.Rename(temp.Name(), path)
}
