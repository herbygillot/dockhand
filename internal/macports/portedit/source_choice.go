package portedit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/macports/sourcechoice"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// SourceMode is what a version update does about a GitHub port's source
// (update --source, [update] source, whose values config.UpdateSources
// lists): suggest, the default, leaves it as it is, but for a tarball
// port's move to GitHub's archive, and says where a release's own file is
// the source; choose moves to a release's own file where one is the
// source; keep leaves the source as it is; or ask for a release's file or
// GitHub's archive of the tag, refused where it doesn't qualify.
type SourceMode string

const (
	SourceSuggest  SourceMode = "suggest"
	SourceChoose   SourceMode = "choose"
	SourceKeep     SourceMode = "keep"
	SourceReleases SourceMode = "releases"
	SourceArchive  SourceMode = "archive"
)

// ChooseMovesGitPorts is the roadmap's D2: whether choose moves a port
// fetched with Git for its submodules to a release asset holding every
// one. It does, the recommended option, pending the
// person's pick; false leaves such a port on its clone unless
// --source releases asks.
const ChooseMovesGitPorts = true

// sourceComment is the roadmap's D3: the Portfile comment a move writes
// above its block. None, the recommended option, pending the person's
// pick: the commit's body and the pull request say it
// (MovedSource.Words).
func sourceComment(MovedSource) []string { return nil }

// ReleaseFiles reads what choosing a GitHub port's source reads: the
// files uploaded to its release, as the forge describes them, and the forge's archive of
// a commit, the tag's, which says what the repository holds there
// (upstream.Service).
type ReleaseFiles interface {
	ReleaseAssets(ctx context.Context, port macports.PortInfo, release model.Release) ([]model.ReleaseAsset, error)
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
func sourceMoveFor(asset sourcechoice.Judged, info macports.PortInfo, fromGit bool) (portfile.SourceMove, bool) {
	extraction, _ := macports.ExtractionOf(asset.Asset.Name)
	base := strings.TrimSuffix(asset.Asset.Name, extraction.Suffix)
	move := portfile.SourceMove{Mode: "releases", UseOption: extraction.UseOption, Command: extraction.Command, Depends: extraction.Depends, FromGit: fromGit}
	// zstd's suffix is written always: Base 2.12's default is gzip's.
	if extraction.Suffix != extraction.Implied || extraction.Command != "" {
		move.Suffix = extraction.Suffix
	}
	defaultBase := info.Options["github.project"] + "-" + info.Options["github.version"]
	ok := true
	if base != defaultBase {
		move.Distname, ok = templated(base, info)
	}
	if ok && asset.Inventory.Top != base {
		move.Worksrcdir, ok = templated(asset.Inventory.Top, info)
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
// stays; an archive port stays unless --source releases asks. Suggest,
// the default, tests as choose does and moves nothing, saying where an
// asset is the source (sourceSuggestion). Nothing chosen leaves the edit
// as it was, said as a notice; a mode asked for that doesn't qualify is
// refused, naming the test it failed.
func (s *Service) chooseSource(ctx context.Context, request Request, input *sourceInput, contents []byte, versioned macports.Snapshot) (chosenSource, error) {
	mode := request.SourceMode
	if mode == "" {
		mode = SourceSuggest
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
	case from == "releases" && mode == SourceArchive:
		// Kept, it updated, fetched the release's assets, and said nothing
		// of the policy it didn't follow (#123).
		return kept, ErrUnsupported.Because("--source archive: %s fetches a release asset, and update doesn't move a port back to GitHub's archive of the tag", input.target.Name)
	case from == "releases":
		return kept, nil
	case from == "git" && mode == SourceArchive:
		return kept, ErrUnsupported.Because("--source archive: GitHub's archive of the tag leaves out the Git submodules %s clones", input.target.Name)
	case from == "git":
		wantRelease = mode == SourceReleases || mode == SourceSuggest || ChooseMovesGitPorts
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
	candidates := sourcechoice.Candidates(assets, next.Options["github.project"], next.Options["github.version"])
	if len(candidates) == 0 {
		return refuse(fmt.Sprintf("release %s carries no source archive", release.Tag))
	}
	directory, err := input.scratchDir(request)
	if err != nil {
		return kept, err
	}
	progress.Report(ctx, "Reading release %s's %s for %s's source", release.Tag, prose.And(model.AssetNames(candidates)), input.target.Name)
	tagPath, err := s.Releases.SourceArchive(ctx, next, model.GitSource{URL: next.Options["git.url"], Ref: release.Tag, Commit: model.ObjectID(release.Commit)}, directory)
	if err != nil {
		if ctx.Err() != nil {
			return kept, ctx.Err()
		}
		return refuse(fmt.Sprintf("GitHub's archive of %s couldn't be read while checking release %s's files (%v)", release.Tag, release.Tag, err))
	}
	tag, err := project.ReadInventory(ctx, tagPath)
	_ = os.Remove(tagPath)
	if err != nil {
		return refuse(fmt.Sprintf("GitHub's archive of %s couldn't be read (%v)", release.Tag, err))
	}
	needs := sourcechoice.NeedsOf(tag, portfile.WorksrcPaths(input.data), from == "git")
	store := s.Archives.Store(directory)
	var judged []sourcechoice.Judged
	downloads := map[string]distfetch.Download{}
	for _, asset := range candidates {
		download, err := store.Fetch(ctx, next, distfetch.Source{Name: asset.Name, URL: asset.URL})
		if err != nil {
			if ctx.Err() != nil {
				return kept, ctx.Err()
			}
			judged = append(judged, sourcechoice.Judged{Asset: asset, Lacks: fmt.Sprintf("couldn't be downloaded (%v)", err)})
			continue
		}
		downloads[asset.Name] = download
		inventory, err := project.ReadInventory(ctx, download.Path)
		candidate := sourcechoice.Judged{Asset: asset, Inventory: inventory}
		if err != nil {
			candidate.Lacks = "isn't an archive dockhand reads"
		} else {
			candidate.Lacks = needs.Lacks(inventory)
		}
		judged = append(judged, candidate)
	}
	chosen, err := sourcechoice.Choose(judged)
	var ambiguous sourcechoice.Ambiguous
	var none sourcechoice.NoneHolds
	switch {
	case errors.As(err, &ambiguous):
		return refuse(fmt.Sprintf("release %s has multiple qualifying source archives (%s) with different contents; none was chosen", release.Tag, prose.And(ambiguous.Names)))
	case errors.As(err, &none):
		return refuse(fmt.Sprintf("release %s carries %s, which %s", release.Tag, none.First.Asset.Name, none.First.Lacks))
	case err != nil:
		return kept, err
	}
	move, ok := sourceMoveFor(chosen, next, from == "git")
	if !ok {
		return refuse(fmt.Sprintf("release %s's %s has a filename or top-level directory that dockhand can't write into the Portfile automatically", release.Tag, chosen.Asset.Name))
	}
	moved := &MovedSource{From: from, To: "releases", Release: release.Tag, Asset: chosen.Asset.Name, Submodules: needs.Submodules}
	move.Comment = sourceComment(*moved)
	edited, ok := portfile.MoveSource(contents, move)
	if !ok {
		return refuse(fmt.Sprintf("release %s carries %s, but the Portfile's github.tarball_from, fetch.type, or hook isn't one literal line at its top", release.Tag, chosen.Asset.Name))
	}
	evaluated, err := s.evaluateEdit(ctx, input, edited)
	if err != nil {
		if ctx.Err() != nil {
			return kept, ctx.Err()
		}
		return refuse(fmt.Sprintf("release %s carries %s, but the Portfile couldn't be evaluated after switching to it (%v)", release.Tag, chosen.Asset.Name, err))
	}
	if why := movedAsWritten(evaluated.after.Ports[input.target.Name], chosen); why != "" {
		return refuse(fmt.Sprintf("release %s carries %s, but the Portfile moved to it %s", release.Tag, chosen.Asset.Name, why))
	}
	if mode == SourceSuggest {
		kept.notice = sourceSuggestion(*moved)
		return kept, nil
	}
	for _, line := range portfile.AddedLines(contents, edited) {
		moved.Lines = append(moved.Lines, line.Text)
	}
	progress.Report(ctx, "%s", moved.Words())
	return chosenSource{moved: moved, contents: edited, after: evaluated.after, fetched: []distfetch.Download{downloads[chosen.Asset.Name]}}, nil
}

// sourceSuggestion is suggest's notice of a release asset that is the
// source, where the update left the source as it was: "release 2.5.0's
// mindforger_2.5.0_tarball.tgz carries the source, doc, and
// deps/cmark-gfm; update --source releases, run instead of this update, fetches it".
func sourceSuggestion(moved MovedSource) string {
	carried := append([]string{"the source"}, moved.Submodules...)
	return fmt.Sprintf("release %s's %s carries %s; update --source releases, run instead of this update, fetches it", moved.Release, moved.Asset, prose.And(carried))
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
func movedAsWritten(port macports.PortInfo, asset sourcechoice.Judged) string {
	files, errs := syntax.ListValues(port.Options["distfiles"])
	switch {
	case port.GitFetched():
		return "still clones with Git"
	case port.Options["github.tarball_from"] != "releases":
		return "evaluates github.tarball_from " + port.Options["github.tarball_from"]
	case len(errs) > 0 || !slices.Equal(files, []string{asset.Asset.Name}):
		return fmt.Sprintf("fetches %s", prose.And(files))
	case port.Options["worksrcdir"] != asset.Inventory.Top:
		return "extracts to " + port.Options["worksrcdir"] + ", not " + asset.Inventory.Top
	}
	return ""
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
