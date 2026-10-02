package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portcreate"
	"github.com/herbygillot/dockhand/internal/model"
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
	// Assets are the files the release carries.
	Assets []string
	Files  map[string][]byte
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
	Build   portcreate.Build
	// Name and Category are the port's, unless the request names them.
	Name, Category string
	// Declared is what the project's own manifest says of it.
	Declared portcreate.Declared
	// License is the license line in MacPorts' words, from the manifest
	// (LicenseFrom names it) or, failing that, the forge (LicenseFrom
	// empty); empty where neither says one MacPorts has a name for.
	License, LicenseFrom string
	// Description is the port's one line: the manifest's, else the
	// forge's.
	Description string
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
	return observe(project)
}

// observe is what create makes of a project: its version, its build, and
// its license and one line, which its own manifest says nearer MacPorts'
// words than the forge does, the forge's being the fallback.
func observe(project Project) (Observed, error) {
	_, version, ok := portcreate.SplitTag(project.Tag)
	if !ok {
		return Observed{}, fmt.Errorf("%s's latest release is tagged %q, which names no version", project.Owner+"/"+project.Name, project.Tag)
	}
	build := portcreate.Detect(project.Files)
	observed := Observed{Project: project, Version: version, Build: build, Name: defaultName(project, build), Category: build.Category(),
		Declared: portcreate.Declare(project.Files, build), Description: project.Description}
	if license, ok := macports.License(observed.Declared.License); ok {
		observed.License, observed.LicenseFrom = license, observed.Declared.File
	} else if license, ok := macports.License(project.License); ok {
		observed.License = license
	}
	if observed.Declared.Description != "" {
		observed.Description = observed.Declared.Description
	}
	return observed, nil
}

// defaultName is a new port's name when none is given: the project's, in
// lower case, with py- before it for a Python project, as MacPorts names
// Python modules.
func defaultName(project Project, build portcreate.Build) string {
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
	Build           portcreate.Build
	Crates          int
	Category        string
	// Unconfirmed names what the Portfile marks as guessed.
	Unconfirmed []string
	// Checksums is the refresh that filled them in; ChecksumsProblem says
	// why it could not.
	Checksums        *Update
	ChecksumsProblem string
	// HomepageFrom is the forge's plain-HTTP homepage, where its https form
	// answers and the Portfile has that, as MacPorts prefers.
	HomepageFrom string
	// PlainHTTP are the Portfile's URLs still over plain HTTP.
	PlainHTTP []PlainURL
	// MovedFrom is where a port this branch created was, where create
	// moved it to another category rather than write it.
	MovedFrom string
	// CategoryFrom is what a guessed category was guessed from.
	CategoryFrom string
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
		observed, err = observe(*request.Project)
	} else {
		observed, err = e.ObserveProject(ctx, request.URL)
	}
	if err != nil {
		return Created{}, err
	}
	project, build, version := observed.Project, observed.Build, observed.Version
	prefix, _, _ := portcreate.SplitTag(project.Tag)
	name := request.Name
	if name == "" {
		name = defaultName(project, build)
	}
	if !macports.ValidName(name) {
		return Created{}, fmt.Errorf("%q is not a port name; name it with --name", name)
	}
	// A port this branch created and hasn't committed is moved to the
	// category named, as it is, rather than refused as one to update (the
	// txt run's finding 1).
	if created, own, err := e.createdHere(ctx, worktree, request.Branch, name); err != nil || own {
		if err != nil {
			return Created{}, err
		}
		return e.moveCreated(ctx, worktree, request.Branch, created, request.Category)
	}
	spec := portcreate.Spec{Name: name, Category: request.Category, Owner: project.Owner, Project: project.Name, Version: version, TagPrefix: prefix,
		Description: observed.Description, Homepage: project.Homepage, License: observed.License, LicenseFrom: observed.LicenseFrom, Maintainer: request.Maintainer, Build: build,
		Binaries: portcreate.Binaries(project.Files, build), ReleaseAsset: portcreate.HasReleaseArchive(project.Assets, project.Name, version)}
	if spec.Category == "" {
		categories, err := treeCategories(ctx, worktree)
		if err != nil {
			return Created{}, err
		}
		spec.Category, spec.CategoryFrom = portcreate.GuessCategory(build, observed.Description, categories)
		spec.CategoryGuessed = true
	}
	// MacPorts prefers HTTPS: a forge's plain-HTTP homepage is written as
	// its https form where that answers.
	var homepageFrom string
	answered := map[string]bool{}
	if plain := e.plainHTTP(ctx, homepageOnly(spec.Homepage), answered); len(plain) > 0 && plain[0].Answers {
		homepageFrom, spec.Homepage = spec.Homepage, plain[0].HTTPS
	}
	if !macports.ValidCategory(spec.Category) {
		return Created{}, fmt.Errorf("%q is not a category", spec.Category)
	}
	if lock, ok := project.Files["Cargo.lock"]; ok && build.System == "cargo" {
		if spec.Crates, spec.Unfetched, err = portcreate.CargoCrates(lock); err != nil {
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
	contents := portcreate.Write(spec)
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
	// A Python project's versions start from the python PortGroup's
	// default, as MacPorts evaluates the Portfile just written.
	if build.System == "python" {
		if spec.PythonVersion = e.pythonDefault(ctx, worktree, directory, name); spec.PythonVersion != "" {
			contents = portcreate.Write(spec)
			if err := os.WriteFile(path, contents, 0o644); err != nil {
				return Created{}, err
			}
			if err := worktree.Add(ctx, portfile); err != nil {
				return Created{}, err
			}
		}
	}
	blob, err := worktree.BlobID(ctx, contents)
	if err != nil {
		return Created{}, err
	}
	subject := fmt.Sprintf("%s: new port, version %s", name, version)
	edit := model.Edit{ID: model.EditID(store.NewID("ed")), Branch: request.Branch.ID, Kind: model.EditCreate, Port: name, Directory: directory,
		Subject: subject, At: e.now(), Files: []model.EditedFile{{Path: portfile, After: model.ObjectID(blob)}}}
	if err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(edit); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: edit.At, Branch: request.Branch.ID, Kind: "branch.edit", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: created %s from %s", name, portfile, request.URL)})
		return err
	}, editRecorded(request.Branch.ID, edit.ID)); err != nil {
		return Created{}, err
	}

	created := Created{Port: name, Directory: directory, Project: project, Version: version, Build: build, Crates: len(spec.Crates),
		Category: spec.Category, CategoryFrom: spec.CategoryFrom, Unconfirmed: spec.Unconfirmed(), HomepageFrom: homepageFrom}
	update, err := e.Update(ctx, UpdateRequest{Branch: request.Branch, Action: model.EditChecksums, Port: name, answered: answered})
	switch {
	case ctx.Err() != nil:
		return created, ctx.Err()
	case err != nil:
		created.ChecksumsProblem = err.Error()
	default:
		created.Checksums = &update
		created.PlainHTTP = update.PlainHTTP
	}
	// Where the refresh couldn't look at the port's URLs, the homepage is
	// still said if it's plain HTTP.
	if len(created.PlainHTTP) == 0 {
		created.PlainHTTP = e.plainHTTP(ctx, homepageOnly(spec.Homepage), answered)
	}
	return created, nil
}

// homepageOnly is a port that names only a homepage, for asking about it
// before there's a Portfile to evaluate.
func homepageOnly(homepage string) macports.PortInfo {
	return macports.PortInfo{Options: map[string]string{"homepage": homepage}}
}

// treeCategories are the categories the branch's tree has.
func treeCategories(ctx context.Context, worktree *git.Repository) ([]string, error) {
	_, tree, err := worktree.WorkingTree(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := worktree.ReadTree(ctx, tree)
	if err != nil {
		return nil, err
	}
	var categories []string
	for _, entry := range entries {
		if entry.Type == "tree" && macports.IsCategory(entry.Name) {
			categories = append(categories, entry.Name)
		}
	}
	return categories, nil
}

// createdHere is the port of a name this branch's create wrote, where its
// Portfile is still where create wrote it.
func (e *Engine) createdHere(ctx context.Context, worktree *git.Repository, branch model.Branch, name string) (model.Edit, bool, error) {
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return model.Edit{}, false, err
	}
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		if edit.Kind != model.EditCreate || edit.Port != name {
			continue
		}
		_, err := os.Stat(filepath.Join(worktree.Root, filepath.FromSlash(edit.Directory), "Portfile"))
		return edit, err == nil, nil
	}
	return model.Edit{}, false, nil
}

// moveCreated moves a port this branch created, and hasn't committed, to
// another category, its files as they are, the person's edits included,
// staged where they now are. Committed, it's the branch's history, which
// create doesn't rewrite; asked for its own category, it's already there.
func (e *Engine) moveCreated(ctx context.Context, worktree *git.Repository, branch model.Branch, created model.Edit, category string) (Created, error) {
	name, from := created.Port, created.Directory
	head, tree, err := worktree.WorkingTree(ctx)
	if err != nil {
		return Created{}, err
	}
	trees, err := worktree.CommitTrees(ctx, []string{head})
	if err != nil {
		return Created{}, err
	}
	if file, _, err := worktree.File(ctx, trees[head], from+"/Portfile"); err != nil || file.Exists {
		if err != nil {
			return Created{}, err
		}
		return Created{}, fmt.Errorf("%s is this branch's new port, at %s, and committed: dockhand edit %s edits it; create moves one only before it's committed", name, from, name)
	}
	if category == "" || category == path.Dir(from) {
		return Created{}, fmt.Errorf("%s is this branch's new port, at %s, not yet committed: dockhand edit %s edits it, and create --category <another> moves it, keeping your edits", name, from, name)
	}
	if !macports.ValidCategory(category) {
		return Created{}, fmt.Errorf("%q is not a category", category)
	}
	to := category + "/" + name
	if existing, err := worktree.ExistingPaths(ctx, tree, []string{to}); err != nil || len(existing) > 0 {
		if err != nil {
			return Created{}, err
		}
		return Created{}, fmt.Errorf("there is already a %s", to)
	}
	if err := expandFor(ctx, worktree, []string{to + "/Portfile"}); err != nil {
		return Created{}, err
	}
	target := filepath.Join(worktree.Root, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Created{}, err
	}
	if err := os.Rename(filepath.Join(worktree.Root, filepath.FromSlash(from)), target); err != nil {
		return Created{}, err
	}
	if err := worktree.AddAll(ctx, from, to); err != nil {
		return Created{}, err
	}
	// The move is create's still: it wrote what it wrote, now at to, so
	// what the person has changed since is as much theirs as it was.
	moved := model.Edit{ID: model.EditID(store.NewID("ed")), Branch: branch.ID, Kind: model.EditCreate, Port: name, Directory: to, Subject: created.Subject, At: e.now()}
	for _, file := range created.Files {
		moved.Files = append(moved.Files, model.EditedFile{Path: to + strings.TrimPrefix(file.Path, from), After: file.After})
	}
	if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddEdit(moved); err != nil {
			return err
		}
		_, err := tx.AppendEvent(model.Event{At: moved.At, Branch: branch.ID, Kind: "branch.edit", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: moved from %s to %s", name, from, to)})
		return err
	}); err != nil {
		return Created{}, err
	}
	return Created{Port: name, Directory: to, Category: category, MovedFrom: from}, nil
}

// NameTaken is the directory of the port master, fetched just now,
// already has by name, a subport's among them, which the port index
// knows; empty where none does. base is the master it read, for the
// branch create starts. create --new asks it before it starts a branch for
// the name, where it refused only once it had started one, left behind:
// sand's upstream name is textproc/sand's (the sand-runner port). Where
// the index couldn't say whether a subport has the name, unchecked says
// why, and a directory named for it is still found.
func (e *Engine) NameTaken(ctx context.Context, name string) (directory string, base model.ObjectID, unchecked string, err error) {
	if base, err = e.fetchMaster(ctx); err != nil {
		return "", "", "", err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return "", "", "", err
	}
	tree := trees[string(base)]
	directories, err := e.directoriesNamed(ctx, tree, name)
	if err != nil || len(directories) > 0 {
		return strings.Join(directories, ", "), base, "", err
	}
	reader, err := e.portReader()
	if err == nil {
		directory, err = reader.Directory(ctx, model.Source{Tree: model.ObjectID(tree)}, name)
	}
	switch {
	case errors.Is(err, ErrNoPort):
		return "", base, "", nil
	case err != nil:
		return "", base, err.Error(), nil
	}
	return directory, base, "", nil
}

// TakenWords says a port name already taken, and what to do.
func TakenWords(name, directory string) string {
	return fmt.Sprintf("there is already a port %s, at %s; --name names this one otherwise, or dockhand update %s updates that one", name, directory, name)
}

// refuseExisting refuses a name a port in the base, or in the branch,
// already has, in any category.
func (e *Engine) refuseExisting(ctx context.Context, worktree *git.Repository, name string) error {
	_, tree, err := worktree.WorkingTree(ctx)
	if err != nil {
		return err
	}
	categories, err := treeCategories(ctx, worktree)
	if err != nil {
		return err
	}
	var candidates []string
	for _, category := range categories {
		candidates = append(candidates, category+"/"+name)
	}
	existing, err := worktree.ExistingPaths(ctx, tree, candidates)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return errors.New(TakenWords(name, existing[0]))
	}
	return nil
}

func (e *Engine) projectReader() (ProjectReader, error) {
	return assemble(e, &e.ProjectReader, func() (ProjectReader, error) {
		return githubProjects{client: e.github()}, nil
	})
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
		if description.Name != "" && !strings.EqualFold(description.Name, repository.Name()) {
			return Project{}, fmt.Errorf("github: %s answered for %s", repository.Name(), description.Name)
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
	if assets, ok := repository.(forge.AssetRepository); ok {
		if found.Assets, err = assets.Assets(ctx, latest.Tag); err != nil {
			return Project{}, err
		}
	}
	tag, err := repository.Tag(ctx, latest.Tag)
	if err != nil {
		return Project{}, err
	}
	files, ok := repository.(forge.FileRepository)
	if !ok {
		return found, nil
	}
	for _, file := range portcreate.Files() {
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

// githubName is the owner/name of the GitHub project an address names,
// said in create's words when it isn't on GitHub.
func githubName(address string) (string, error) {
	name, err := github.PageRepository(address)
	if errors.Is(err, github.ErrNotGitHub) {
		return "", fmt.Errorf("create reads projects on GitHub so far, such as https://github.com/owner/project; for %s, write the Portfile yourself", address)
	}
	return name, err
}

// pythonDefault is the Python the python PortGroup defaults to for a new
// port, as MacPorts evaluates its Portfile in the working tree; empty
// where it can't be read, which the Portfile then marks.
func (e *Engine) pythonDefault(ctx context.Context, worktree *git.Repository, directory, name string) string {
	reader, err := e.portReader()
	if err != nil {
		return ""
	}
	_, tree, err := worktree.WorkingTree(ctx)
	if err != nil {
		return ""
	}
	ports, err := reader.Ports(ctx, model.Source{Tree: model.ObjectID(tree)}, directory, model.Environment{}, nil)
	if err != nil {
		return ""
	}
	for _, port := range ports {
		if port.Name == name {
			return port.Options["python.default_version"]
		}
	}
	return ""
}
