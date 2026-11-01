package portedit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// SourceMode is what a version update does about a GitHub port's source
// (update --source, [update] source, whose values config.UpdateSources
// lists): choose a release's own file where
// one is the source, keep the source as it is, or ask for a release's
// file or GitHub's archive of the tag, refused where it doesn't qualify.
type SourceMode string

const (
	SourceChoose   SourceMode = "choose"
	SourceKeep     SourceMode = "keep"
	SourceReleases SourceMode = "releases"
	SourceArchive  SourceMode = "archive"
)

// ChooseMovesGitPorts is the roadmap's D2: whether choose, the default,
// moves a port fetched with Git for its submodules to a release asset
// holding every one. It does, the recommended option, pending the
// person's pick; false leaves such a port on its clone unless
// --source releases asks.
const ChooseMovesGitPorts = true

// sourceComment is the roadmap's D3: the Portfile comment a move writes
// above its block. None, the recommended option, pending the person's
// pick: the commit's body and the pull request say it
// (MovedSource.Words).
func sourceComment(MovedSource) []string { return nil }

// ReleaseFiles reads what choosing a GitHub port's source reads: the
// names of the files uploaded to its release, and the forge's archive of
// a commit, the tag's, which says what the repository holds there
// (upstream.Service).
type ReleaseFiles interface {
	ReleaseAssets(ctx context.Context, port macports.PortInfo, release model.Release) ([]string, error)
	SourceArchive(ctx context.Context, port macports.PortInfo, source model.GitSource, directory string) (string, error)
}

// MovedSource is an update's move of a GitHub port's source, which the
// edit records (model.SourceMove).
type MovedSource = model.SourceMove

// chosenSource is what choosing found: the move made, with its edit and
// evaluation, and the asset's download, which the update's archives
// reuse; or, where nothing moved, why, said as a notice.
type chosenSource struct {
	moved    *MovedSource
	contents []byte
	after    macports.Snapshot
	fetched  []distfetch.Download
	notice   string
}

// platformWords are the words a binary bundle's name carries, which a
// source archive's doesn't.
var platformWords = []string{"darwin", "macos", "osx", "linux", "windows", "win", "x86_64", "amd64", "arm64", "aarch64", "universal", "musl"}

// sourceCandidates are the release's assets that may be its source, by
// name: a suffix MacPorts extracts and no platform or architecture word,
// ${project}-${version}.tar.gz first, the github PortGroup's default.
// That a candidate is the source is known by its content alone
// (judgeSources).
func sourceCandidates(assets []string, project, version string) []string {
	var candidates []string
	for _, name := range assets {
		extraction, ok := macports.ExtractionOf(name)
		if !ok || binaryName(strings.TrimSuffix(name, extraction.Suffix)) {
			continue
		}
		candidates = append(candidates, name)
	}
	preferred := project + "-" + version + ".tar.gz"
	if i := slices.Index(candidates, preferred); i > 0 {
		candidates = append([]string{preferred}, slices.Delete(candidates, i, i+1)...)
	}
	return candidates
}

// binaryName reports a file's base that names a platform or architecture:
// tool-1.0-darwin-arm64, tool_win64.
func binaryName(base string) bool {
	lower := strings.ToLower(base)
	if strings.Contains(lower, "x86_64") || strings.Contains(lower, "x86-64") {
		return true
	}
	for _, token := range strings.FieldsFunc(lower, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' }) {
		for _, word := range platformWords {
			if rest, ok := strings.CutPrefix(token, word); ok && strings.Trim(rest, "0123456789") == "" {
				return true
			}
		}
	}
	return false
}

// sourceTree is what an archive holds below its one top directory: its
// regular files, each directory with how many regular files are under
// it, and .gitmodules' text where it has one. top is empty where the
// archive doesn't extract to one directory.
type sourceTree struct {
	top        string
	files      map[string]bool
	dirs       map[string]int
	gitmodules []byte
}

// readSourceTree walks an archive (archive.Walk).
func readSourceTree(ctx context.Context, filename string) (sourceTree, error) {
	tree := sourceTree{files: map[string]bool{}, dirs: map[string]int{}}
	shared := true
	err := archive.Walk(ctx, filename, func(member archive.Member) error {
		name, ok := member.Clean()
		if !ok {
			return nil
		}
		first, rest, nested := strings.Cut(name, "/")
		switch {
		case tree.top == "":
			tree.top = first
		case first != tree.top:
			shared = false
		}
		if !nested {
			if member.Regular {
				shared = false
			}
			return nil
		}
		if !member.Regular {
			if _, ok := tree.dirs[rest]; !ok {
				tree.dirs[rest] = 0
			}
			return nil
		}
		tree.files[rest] = true
		for dir := path.Dir(rest); dir != "."; dir = path.Dir(dir) {
			tree.dirs[dir]++
		}
		if rest == ".gitmodules" {
			data, err := io.ReadAll(io.LimitReader(member.Body, 1<<20))
			if err != nil {
				return err
			}
			tree.gitmodules = data
		}
		return nil
	})
	if err != nil {
		return sourceTree{}, err
	}
	if !shared {
		tree.top = ""
	}
	return tree, nil
}

// has reports a file or directory below the top.
func (t sourceTree) has(name string) bool {
	_, dir := t.dirs[name]
	return t.files[name] || dir
}

// submodules are the paths .gitmodules names, in its order.
func (t sourceTree) submodules() []string {
	var paths []string
	scanner := bufio.NewScanner(bytes.NewReader(t.gitmodules))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if ok && strings.TrimSpace(key) == "path" {
			if name := strings.Trim(strings.TrimSpace(value), "/"); name != "" && !slices.Contains(paths, name) {
				paths = append(paths, name)
			}
		}
	}
	return paths
}

// sourceNeeds are what a release asset must hold to be the source: the
// root build and manifest files the repository has at the tag, the paths
// the Portfile names under ${worksrcpath} that the repository or a
// submodule has there, and, for a Git port, every submodule as a
// non-empty directory.
type sourceNeeds struct {
	files      []string
	submodules []string
}

// needsOf are what the tag's tree and the Portfile ask of an asset. A
// path the Portfile names that neither the tag nor a submodule has is the
// build's own, such as a build directory, and asked of no asset.
func needsOf(tag sourceTree, data []byte, git bool) sourceNeeds {
	var needs sourceNeeds
	if git {
		needs.submodules = tag.submodules()
	}
	for name := range tag.files {
		if !strings.Contains(name, "/") && (project.BuildFile(name) || project.Manifest(name)) {
			needs.files = append(needs.files, name)
		}
	}
	for _, named := range portfile.WorksrcPaths(data) {
		inSubmodule := slices.ContainsFunc(needs.submodules, func(sub string) bool { return named == sub || strings.HasPrefix(named, sub+"/") })
		if (tag.has(named) || inSubmodule) && !slices.Contains(needs.files, named) {
			needs.files = append(needs.files, named)
		}
	}
	slices.Sort(needs.files)
	return needs
}

// lacks is the first thing an asset's tree doesn't hold of what's needed,
// in words: "extracts to more than one directory", or a path; empty where
// it holds everything.
func (n sourceNeeds) lacks(tree sourceTree) string {
	if tree.top == "" {
		return "extracts to more than one directory"
	}
	for _, sub := range n.submodules {
		if tree.dirs[sub] == 0 {
			return "lacks " + sub
		}
	}
	for _, name := range n.files {
		if !tree.has(name) {
			return "lacks " + name
		}
	}
	return ""
}

// judgedAsset is one candidate as downloaded and read.
type judgedAsset struct {
	name     string
	tree     sourceTree
	download distfetch.Download
	lacks    string
}

// compressionOrder is which of one source in several compressions is
// taken: gzip, then bzip2, xz, lzip, and zip.
var compressionOrder = []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.lz", ".zip"}

// judgeSources is the asset that is the source, among those judged:
// passing ones with the same files are one source in several
// compressions, the first in compressionOrder taken; passing ones with
// different files are ambiguous, named, and none is chosen.
func judgeSources(judged []judgedAsset) (*judgedAsset, []string) {
	var passing []judgedAsset
	for _, asset := range judged {
		if asset.lacks == "" {
			passing = append(passing, asset)
		}
	}
	if len(passing) == 0 {
		return nil, nil
	}
	for _, other := range passing[1:] {
		if !sameFiles(passing[0].tree, other.tree) {
			var names []string
			for _, asset := range passing {
				names = append(names, asset.name)
			}
			return nil, names
		}
	}
	rank := func(name string) int {
		extraction, _ := macports.ExtractionOf(name)
		return slices.Index(compressionOrder, extraction.Implied)
	}
	chosen := slices.MinFunc(passing, func(a, b judgedAsset) int { return rank(a.name) - rank(b.name) })
	return &chosen, nil
}

// sameFiles reports two trees holding the same regular files.
func sameFiles(a, b sourceTree) bool {
	if len(a.files) != len(b.files) {
		return false
	}
	for name := range a.files {
		if !b.files[name] {
			return false
		}
	}
	return true
}

// templated is a name with the port's name or GitHub project and its
// version put back as the Portfile's variables: mindforger_2.5.0_tarball
// as ${name}_${version}_tarball. False where what's left isn't a literal
// word.
func templated(value string, info macports.PortInfo) (string, bool) {
	ghVersion, ghProject := info.Options["github.version"], info.Options["github.project"]
	versionWord, projectWord := "${version}", "${github.project}"
	if ghVersion == "" {
		ghVersion = info.Version
	}
	if ghVersion != info.Version {
		versionWord = "${github.version}"
	}
	if ghProject == "" || ghProject == info.Name {
		ghProject, projectWord = info.Name, "${name}"
	}
	const versionMark, projectMark = "\x00v\x00", "\x00p\x00"
	marked := strings.ReplaceAll(value, ghVersion, versionMark)
	marked = strings.ReplaceAll(marked, ghProject, projectMark)
	for _, part := range strings.FieldsFunc(marked, func(r rune) bool { return r == 0 }) {
		if part != "v" && part != "p" && !portfile.Literal(part) {
			return "", false
		}
	}
	marked = strings.ReplaceAll(marked, versionMark, versionWord)
	return strings.ReplaceAll(marked, projectMark, projectWord), true
}

// sourceMoveFor is the edit an asset takes: distname where its base isn't
// ${project}-${version}, the use_ option and extract.suffix its
// compression and name take, and worksrcdir where its top directory isn't
// its base. False where a name can't be written as a word.
func sourceMoveFor(asset judgedAsset, info macports.PortInfo, fromGit bool) (portfile.SourceMove, bool) {
	extraction, _ := macports.ExtractionOf(asset.name)
	base := strings.TrimSuffix(asset.name, extraction.Suffix)
	move := portfile.SourceMove{Mode: "releases", UseOption: extraction.UseOption, FromGit: fromGit}
	if extraction.Suffix != extraction.Implied {
		move.Suffix = extraction.Suffix
	}
	defaultBase := info.Options["github.project"] + "-" + info.Options["github.version"]
	ok := true
	if base != defaultBase {
		move.Distname, ok = templated(base, info)
	}
	if ok && asset.tree.top != base {
		move.Worksrcdir, ok = templated(asset.tree.top, info)
	}
	return move, ok
}

// sourceKind is what a GitHub port fetches now: tarball, archive, or
// releases, its github.tarball_from, or git where it clones.
func sourceKind(info macports.PortInfo) string {
	if info.GitFetched() {
		return "git"
	}
	return info.Options["github.tarball_from"]
}

// chooseSource chooses a GitHub port's source for its new version, the
// version's edit as contents and its evaluation as versioned: a tarball
// port goes to a release asset that is the source, else GitHub's archive,
// as the version's edit moved it already; a port cloned for its
// submodules goes to an asset that is the source and holds each, else
// stays; an archive port stays unless --source releases asks. Nothing
// chosen leaves the edit as it was, said as a notice; a mode asked for
// that doesn't qualify is refused, naming the test it failed.
func (s *Service) chooseSource(ctx context.Context, request Request, input *sourceInput, contents []byte, versioned macports.Snapshot) (chosenSource, error) {
	mode := request.SourceMode
	if mode == "" {
		mode = SourceChoose
	}
	kept := chosenSource{contents: contents, after: versioned}
	if request.downloadsNothing {
		return kept, nil
	}
	release := request.Release
	spec, err := portsource.Interpret(input.info, portsource.Edit)
	if err != nil || spec.Forge != portsource.GitHub || release == nil || release.Pinned || release.Tag == "" {
		if mode == SourceReleases || mode == SourceArchive {
			return kept, ErrUnsupported.Because("--source %s is for a port on the github PortGroup updating to a release's tag", mode)
		}
		return kept, nil
	}
	from := sourceKind(input.info)
	wantRelease := false
	switch {
	case mode == SourceKeep:
		return kept, nil
	case from == "releases":
		return kept, nil
	case from == "git" && mode == SourceArchive:
		return kept, ErrUnsupported.Because("--source archive: GitHub's archive of the tag leaves out the Git submodules %s clones", input.target.Name)
	case from == "git":
		wantRelease = mode == SourceReleases || ChooseMovesGitPorts
	case from == "tarball":
		wantRelease = mode != SourceArchive
	case from == "archive":
		wantRelease = mode == SourceReleases
	}
	if !wantRelease {
		return kept, nil
	}
	asked := mode == SourceReleases
	stay := map[bool]string{true: "so the port stays on its clone", false: "so the port fetches GitHub's archive of the tag"}[from == "git"]
	if from == "archive" {
		stay = "so the port stays on GitHub's archive of the tag"
	}
	refuse := func(words string) (chosenSource, error) {
		if asked {
			return kept, ErrUnsupported.Because("--source releases: %s", words)
		}
		kept.notice = words + ", " + stay
		return kept, nil
	}
	next := versioned.Ports[input.target.Name]
	if from == "git" {
		if why := gitMoveBlocked(input, next, *release); why != "" {
			return refuse(why)
		}
		if _, ok := portfile.MoveSource(contents, portfile.SourceMove{Mode: "releases", FromGit: true}); !ok {
			return refuse("the Portfile's fetch.type git and submodule hook aren't the standard ones at its top")
		}
	}
	if s.Releases == nil {
		return refuse(fmt.Sprintf("release %s's files couldn't be listed here", release.Tag))
	}
	assets, err := s.Releases.ReleaseAssets(ctx, next, *release)
	if err != nil {
		if ctx.Err() != nil {
			return kept, ctx.Err()
		}
		return refuse(fmt.Sprintf("release %s's files couldn't be listed (%v)", release.Tag, err))
	}
	candidates := sourceCandidates(assets, next.Options["github.project"], next.Options["github.version"])
	if len(candidates) == 0 {
		return refuse(fmt.Sprintf("release %s carries no source archive", release.Tag))
	}
	directory, err := input.scratchDir(request)
	if err != nil {
		return kept, err
	}
	progress.Report(ctx, "Reading release %s's %s for %s's source", release.Tag, prose.And(candidates), input.target.Name)
	tagPath, err := s.Releases.SourceArchive(ctx, next, model.GitSource{URL: next.Options["git.url"], Ref: release.Tag, Commit: model.ObjectID(release.Commit)}, directory)
	if err != nil {
		if ctx.Err() != nil {
			return kept, ctx.Err()
		}
		return refuse(fmt.Sprintf("GitHub's archive of %s couldn't be read to compare release %s's files with (%v)", release.Tag, release.Tag, err))
	}
	tag, err := readSourceTree(ctx, tagPath)
	_ = os.Remove(tagPath)
	if err != nil {
		return refuse(fmt.Sprintf("GitHub's archive of %s couldn't be read (%v)", release.Tag, err))
	}
	needs := needsOf(tag, input.data, from == "git")
	store := s.Archives.Store(directory)
	var judged []judgedAsset
	for _, name := range candidates {
		download, err := store.Fetch(ctx, next, distfetch.Source{Name: name, URL: releaseAssetURL(*release, name)})
		if err != nil {
			if ctx.Err() != nil {
				return kept, ctx.Err()
			}
			judged = append(judged, judgedAsset{name: name, lacks: fmt.Sprintf("couldn't be downloaded (%v)", err)})
			continue
		}
		tree, err := readSourceTree(ctx, download.Path)
		asset := judgedAsset{name: name, tree: tree, download: download}
		if err != nil {
			asset.lacks = "isn't an archive dockhand reads"
		} else {
			asset.lacks = needs.lacks(tree)
		}
		judged = append(judged, asset)
	}
	chosen, ambiguous := judgeSources(judged)
	if len(ambiguous) > 0 {
		return refuse(fmt.Sprintf("release %s carries %s, each the source but with different files, so none was chosen", release.Tag, prose.And(ambiguous)))
	}
	if chosen == nil {
		return refuse(fmt.Sprintf("release %s carries %s, which %s", release.Tag, judged[0].name, judged[0].lacks))
	}
	move, ok := sourceMoveFor(*chosen, next, from == "git")
	if !ok {
		return refuse(fmt.Sprintf("release %s's %s is named in a way dockhand doesn't write", release.Tag, chosen.name))
	}
	moved := &MovedSource{From: from, To: "releases", Release: release.Tag, Asset: chosen.name, Submodules: needs.submodules}
	move.Comment = sourceComment(*moved)
	edited, ok := portfile.MoveSource(contents, move)
	if !ok {
		return refuse(fmt.Sprintf("release %s carries %s, but the Portfile's github.tarball_from, fetch.type, or hook isn't one literal line at its top", release.Tag, chosen.name))
	}
	evaluated, err := s.evaluateEdit(ctx, input, edited)
	if err != nil {
		if ctx.Err() != nil {
			return kept, ctx.Err()
		}
		return refuse(fmt.Sprintf("release %s carries %s, but the Portfile moved to it didn't evaluate (%v)", release.Tag, chosen.name, err))
	}
	if why := movedAsWritten(evaluated.after.Ports[input.target.Name], *chosen); why != "" {
		return refuse(fmt.Sprintf("release %s carries %s, but the Portfile moved to it %s", release.Tag, chosen.name, why))
	}
	for _, line := range portfile.AddedLines(contents, edited) {
		moved.Lines = append(moved.Lines, line.Text)
	}
	progress.Report(ctx, "%s", moved.Words())
	return chosenSource{moved: moved, contents: edited, after: evaluated.after, fetched: []distfetch.Download{chosen.download}}, nil
}

// gitMoveBlocked says why a port cloned with Git doesn't qualify for a
// move: its git.url isn't its repository, or git.branch isn't the
// release's tag, as a pinned commit's or a fork's; empty where it
// qualifies so far. The Portfile's shape, a hook of its own or a mode set
// in a block, is MoveSource's to refuse.
func gitMoveBlocked(input *sourceInput, next macports.PortInfo, release model.Release) string {
	repository := "https://github.com/" + release.Repository
	if gitURL := strings.TrimSuffix(next.Options["git.url"], ".git"); !strings.EqualFold(gitURL, repository) {
		return fmt.Sprintf("git.url, %s, isn't the release's repository", next.Options["git.url"])
	}
	if branch := next.Options["git.branch"]; branch != release.Tag {
		return fmt.Sprintf("git.branch, %s, isn't release %s's tag", branch, release.Tag)
	}
	if input.info.Options["checksums"] != "" {
		return "the port declares checksums beside its clone"
	}
	return ""
}

// movedAsWritten says how the moved port's evaluation isn't the move: it
// still clones, or fetches another file, or extracts to another
// directory; empty where it is.
func movedAsWritten(port macports.PortInfo, asset judgedAsset) string {
	files, errs := syntax.ListValues(port.Options["distfiles"])
	switch {
	case port.GitFetched():
		return "still clones with Git"
	case port.Options["github.tarball_from"] != "releases":
		return "evaluates github.tarball_from " + port.Options["github.tarball_from"]
	case len(errs) > 0 || !slices.Equal(files, []string{asset.name}):
		return fmt.Sprintf("fetches %s", prose.And(files))
	case port.Options["worksrcdir"] != asset.tree.top:
		return "extracts to " + port.Options["worksrcdir"] + ", not " + asset.tree.top
	}
	return ""
}

// releaseAssetURL is where GitHub serves a release's file.
func releaseAssetURL(release model.Release, name string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", release.Repository, url.PathEscape(release.Tag), url.PathEscape(name))
}

// scratchDir is where an update's source choice keeps what it downloads:
// the caller's archive directory where it keeps them for comparing, else
// one the session removes when it closes.
func (i *sourceInput) scratchDir(request Request) (string, error) {
	if request.KeepArchives != "" {
		return request.KeepArchives, nil
	}
	directory, err := scratch.Dir("source-")
	if err != nil {
		return "", err
	}
	i.scratch = append(i.scratch, directory)
	return directory, nil
}
