package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/forge"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/newport"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/store"
)

// Project is what create observes of an upstream project: what its forge
// says of it, its latest release's tag, and the top-level build files at
// that tag.
type Project struct {
	Owner, Name string
	Description string
	Homepage    string
	// License is the forge's detection, as an SPDX identifier.
	License string
	Tag     string
	Files   map[string][]byte
}

// ProjectReader observes an upstream project from its URL.
type ProjectReader interface {
	Project(ctx context.Context, address string) (Project, error)
}

// CreateRequest asks for a new port's first Portfile.
type CreateRequest struct {
	Branch model.Branch
	URL    string
	// Name is the port's name; the project's, in lower case, when empty,
	// with py- before it for a Python project.
	Name string
	// Category is where it goes; guessed from the build system, and marked
	// so, when empty.
	Category string
	// Maintainer is the maintainers line; nomaintainer, marked, when empty.
	Maintainer string
	// Project is the project as ObserveProject found it; observed afresh
	// when nil.
	Project *Project
}

// Observed is an upstream project, and what create would make of it.
type Observed struct {
	Project Project
	Version string
	Build   newport.Build
	// Name and Category are the port's, unless the request names them.
	Name, Category string
}

// ObserveProject reads an upstream project, for a person to see before
// create writes anything.
func (e *Engine) ObserveProject(ctx context.Context, address string) (Observed, error) {
	reader, err := e.projectReader()
	if err != nil {
		return Observed{}, err
	}
	project, err := reader.Project(ctx, address)
	if err != nil {
		return Observed{}, err
	}
	_, version, ok := newport.SplitTag(project.Tag)
	if !ok {
		return Observed{}, fmt.Errorf("%s's latest release is tagged %q, which names no version", project.Owner+"/"+project.Name, project.Tag)
	}
	build := newport.Detect(project.Files)
	return Observed{Project: project, Version: version, Build: build, Name: defaultName(project, build), Category: build.Category()}, nil
}

// defaultName is a new port's name when none is given: the project's, in
// lower case, with py- before it for a Python project, as MacPorts names
// Python modules.
func defaultName(project Project, build newport.Build) string {
	name := strings.ToLower(project.Name)
	if build.System == "python" && !strings.HasPrefix(name, "py-") {
		name = "py-" + name
	}
	return name
}

// Created is what create wrote.
type Created struct {
	Port, Directory string
	Project         Project
	Version         string
	Build           newport.Build
	Crates          int
	Category        string
	// Unconfirmed names what the Portfile marks as guessed.
	Unconfirmed []string
	// Checksums is the refresh that filled them in; ChecksumsProblem says
	// why it could not.
	Checksums        *Update
	ChecksumsProblem string
}

// Create writes a new port's first Portfile from an upstream project, in
// the branch's worktree, and stages it, so the next check includes it
// (Design v3 §6.4). It fills in what it observed, marks what it guessed,
// and then fills in the checksums as dockhand checksums does. Nothing is
// committed.
func (e *Engine) Create(ctx context.Context, request CreateRequest) (Created, error) {
	worktree, err := e.worktree(ctx, request.Branch)
	if err != nil {
		return Created{}, err
	}
	var observed Observed
	if request.Project != nil {
		observed = Observed{Project: *request.Project}
		observed.Build = newport.Detect(observed.Project.Files)
	} else if observed, err = e.ObserveProject(ctx, request.URL); err != nil {
		return Created{}, err
	}
	project, build := observed.Project, observed.Build
	prefix, version, ok := newport.SplitTag(project.Tag)
	if !ok {
		return Created{}, fmt.Errorf("%s's latest release is tagged %q, which names no version", project.Owner+"/"+project.Name, project.Tag)
	}
	name := request.Name
	if name == "" {
		name = defaultName(project, build)
	}
	if !macports.ValidName(name) {
		return Created{}, fmt.Errorf("%q is not a port name; name it with --name", name)
	}
	spec := newport.Spec{Name: name, Category: request.Category, Owner: project.Owner, Project: project.Name, Version: version, TagPrefix: prefix,
		Description: project.Description, Homepage: project.Homepage, License: newport.License(project.License), Maintainer: request.Maintainer, Build: build}
	if spec.Category == "" {
		spec.Category, spec.CategoryGuessed = build.Category(), true
	}
	if strings.ContainsAny(spec.Category, "/ ") || spec.Category == "" {
		return Created{}, fmt.Errorf("%q is not a category", spec.Category)
	}
	if lock, ok := project.Files["Cargo.lock"]; ok && build.System == "cargo" {
		if spec.Crates, err = newport.CargoCrates(lock); err != nil {
			return Created{}, err
		}
	}
	directory := spec.Category + "/" + name
	if err := e.refuseExisting(ctx, worktree, name); err != nil {
		return Created{}, err
	}

	portfile := directory + "/Portfile"
	if err := expandFor(ctx, worktree, []string{portfile}); err != nil {
		return Created{}, err
	}
	path := filepath.Join(worktree.Root, filepath.FromSlash(portfile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Created{}, err
	}
	contents := newport.Write(spec)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return Created{}, fmt.Errorf("%s: %w", portfile, err)
	}
	_, err = file.Write(contents)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Created{}, err
	}
	if err := worktree.Add(ctx, portfile); err != nil {
		return Created{}, err
	}
	blob, err := worktree.BlobID(ctx, contents)
	if err != nil {
		return Created{}, err
	}
	subject := fmt.Sprintf("%s: new port, version %s", name, version)
	edit := model.Edit{ID: model.EditID(store.NewID("ed")), Branch: request.Branch.ID, Kind: model.EditCreate, Port: name, Directory: directory,
		Subject: subject, At: e.now(), Files: []model.EditedFile{{Path: portfile, After: model.ObjectID(blob)}}}
	if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: edit.At, Branch: request.Branch.ID, Kind: "branch.edit", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: created %s from %s", name, portfile, request.URL)})
		return err
	}); err != nil {
		return Created{}, err
	}

	created := Created{Port: name, Directory: directory, Project: project, Version: version, Build: build, Crates: len(spec.Crates),
		Category: spec.Category, Unconfirmed: spec.Unconfirmed()}
	update, err := e.Update(ctx, UpdateRequest{Branch: request.Branch, Action: record.RefreshChecksums, Port: name})
	switch {
	case ctx.Err() != nil:
		return created, ctx.Err()
	case err != nil:
		created.ChecksumsProblem = err.Error()
	default:
		created.Checksums = &update
	}
	return created, nil
}

// refuseExisting refuses a name a port in the base, or in the branch,
// already has, in any category.
func (e *Engine) refuseExisting(ctx context.Context, worktree *git.Repository, name string) error {
	_, tree, err := worktree.WorkingTree(ctx)
	if err != nil {
		return err
	}
	entries, err := worktree.ReadTree(ctx, tree)
	if err != nil {
		return err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.Type == "tree" && !strings.HasPrefix(entry.Name, "_") && !strings.HasPrefix(entry.Name, ".") {
			candidates = append(candidates, entry.Name+"/"+name)
		}
	}
	existing, err := worktree.ExistingPaths(ctx, tree, candidates)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return fmt.Errorf("there is already a port %s, at %s; dockhand update %s updates it", name, existing[0], name)
	}
	return nil
}

func (e *Engine) projectReader() (ProjectReader, error) {
	if e.ProjectReader == nil {
		client := &github.Client{HTTP: http.DefaultClient, Credentials: github.SystemCredentials{Store: keychain.Store{}, Key: github.CredentialKey}}
		e.ProjectReader = githubProjects{client: &forgegithub.Client{Client: client, GitExecutable: e.options.Git}}
	}
	return e.ProjectReader, nil
}

// githubProjects observes projects on GitHub.
type githubProjects struct{ client *forgegithub.Client }

// projectFileLimit bounds each build file read at the release.
const projectFileLimit = 4 << 20

func (g githubProjects) Project(ctx context.Context, address string) (Project, error) {
	name, err := githubName(address)
	if err != nil {
		return Project{}, err
	}
	repository, err := g.client.Repository("https://github.com", name)
	if err != nil {
		return Project{}, err
	}
	owner, project, _ := strings.Cut(repository.Name(), "/")
	found := Project{Owner: owner, Name: project, Files: map[string][]byte{}}
	if described, ok := repository.(forge.DescribedRepository); ok {
		description, err := described.Describe(ctx)
		if err != nil {
			return Project{}, err
		}
		found.Description, found.Homepage, found.License = description.Description, description.Homepage, description.License
	}
	releases, ok := repository.(forge.ReleaseRepository)
	if !ok {
		return Project{}, errors.New("create reads releases from GitHub, and this client can't")
	}
	all, err := releases.Releases(ctx)
	if err != nil {
		return Project{}, err
	}
	var latest *forge.Release
	for i, release := range all {
		if !release.Draft && !release.Prerelease && (latest == nil || release.PublishedAt.After(latest.PublishedAt)) {
			latest = &all[i]
		}
	}
	if latest == nil {
		return Project{}, fmt.Errorf("%s has no release on GitHub; create names the version from the latest one", name)
	}
	found.Tag = latest.Tag
	tag, err := repository.Tag(ctx, latest.Tag)
	if err != nil {
		return Project{}, err
	}
	files, ok := repository.(forge.FileRepository)
	if !ok {
		return found, nil
	}
	for _, file := range newport.Files() {
		data, err := files.File(ctx, tag.Commit, file, projectFileLimit)
		switch {
		case errors.Is(err, forge.ErrNotFound):
		case err != nil:
			return Project{}, fmt.Errorf("reading %s at %s: %w", file, latest.Tag, err)
		default:
			found.Files[file] = data
		}
	}
	return found, nil
}

// githubName is the owner/name a GitHub URL names.
func githubName(address string) (string, error) {
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || !slices.Contains([]string{"github.com", "www.github.com"}, strings.ToLower(parsed.Host)) {
		return "", fmt.Errorf("create reads projects on GitHub so far, such as https://github.com/owner/project; for %s, write the Portfile yourself", address)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("%s names no repository", address)
	}
	return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), nil
}
