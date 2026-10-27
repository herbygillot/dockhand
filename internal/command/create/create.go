package create

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchChoice
	var name, category string
	cmd := &cobra.Command{
		Use:   "create <url | pypi:NAME | crates:NAME | go:MODULE>",
		Short: "Write a new port's first Portfile from its project's URL or registry name",
		Long: `Reads a project on GitHub, its latest release, and the build files at that
release, and writes a new port's Portfile in the branch's working files: the
github PortGroup, and cargo, golang, cmake, meson, or python as its files
say, with the release's version. A Rust project's cargo.crates come from
its Cargo.lock. It then fills in the checksums as dockhand checksums does.

What it observed, it fills in; what it guessed, it marks with a
"# dockhand: unconfirmed" comment: the license, from the project's
Cargo.toml or pyproject.toml where it declares one MacPorts has a name for,
else from GitHub's detection; the long description; and the category,
guessed from the build system unless --category names it. The description
is the manifest's one line where it has one, else GitHub's. The
maintainer is your config's maintainer, else nomaintainer, marked, with
the line to set: the one the ports naming your GitHub login write. submit
holds until each guess is confirmed and its mark removed. The new
Portfile is staged, so the next check includes it. Nothing is committed.

The branch is --branch, else the one checked out here; --new starts one.
--name names the port when the project's name is not what it should be.

In place of the URL, a registry name reads the project from where its
registry says its source is: pypi:NAME from PyPI's project URLs, crates:NAME
from the crate's repository on crates.io, go:MODULE from the module path or
its go-get answer.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			observed, err := e.ObserveProject(ctx, args[0])
			if err != nil {
				return err
			}
			if name == "" {
				name = observed.Name
			}
			out := streams.Out
			project := observed.Project
			license := "no license detected"
			switch {
			case observed.LicenseFrom != "":
				license = observed.LicenseFrom + " says " + observed.Declared.License
			case project.License != "":
				license = "GitHub says " + project.License
			}
			header := fmt.Sprintf("%s %s · %s · %s", name, observed.Version, buildWords(observed), license)
			if observed.Description != "" {
				header += fmt.Sprintf(" · %q", observed.Description)
			}
			fmt.Fprintln(out, header)

			var branch model.Branch
			switch {
			case where.Branch != "":
				// -b names an existing branch; --new=<name> starts one.
				var selection engine.Selection
				if selection, err = e.Select(ctx, engine.Selector{Branch: where.Branch, Policy: engine.SelectOrStart}, engine.WorkCreate); err == nil {
					branch = selection.Branch
				}
			case where.IsNew():
				// A name master has is refused before a branch is started
				// for it.
				var taken, unchecked, free string
				var base model.ObjectID
				if taken, base, unchecked, err = e.NameTaken(ctx, name); err != nil {
					return err
				}
				if taken != "" {
					return errors.New(engine.TakenWords(name, taken))
				}
				if unchecked != "" {
					fmt.Fprintf(streams.Err, "Couldn't check whether a subport named %s already exists: %s\n", name, unchecked)
				}
				if free = where.NewName(); free == "" {
					if free, err = e.NameFor(ctx, name, "new"); err != nil {
						return err
					}
				}
				if branch, err = e.Start(ctx, engine.StartRequest{Name: free, Base: base, Ports: []string{name}}); err == nil {
					fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, branch.Base.Short())
				}
			default:
				branch, err = e.Current(ctx)
				if errors.Is(err, engine.ErrNoBranch) {
					return errors.New("create needs a branch. Use --new to start one or --branch to choose an existing one.")
				}
			}
			if err != nil {
				return err
			}
			if category == "" && streams.Terminal() {
				answer, err := streams.Ask(fmt.Sprintf("? category [%s]: ", observed.Category))
				if err != nil {
					return err
				}
				category = answer
				if category == "" {
					category = observed.Category
				}
			}
			created, err := e.Create(ctx, engine.CreateRequest{Branch: branch, URL: args[0], Name: name, Category: category, Maintainer: s.File.Maintainer, Project: &project})
			if err != nil {
				// Interrupted once the port was written, the result says
				// what is there.
				if created.Port != "" {
					streams.Emit(createdView(branch, created))
				}
				return err
			}
			streams.Emit(createdView(branch, created))
			if created.MovedFrom != "" {
				fmt.Fprintf(out, "Moved %s from %s to %s, preserving all files and your edits.\n", created.Port, created.MovedFrom, created.Directory)
				fmt.Fprintln(out, "Next: "+branchflags.NextIn(ctx, e, branch, "dockhand edit "+created.Port, "dockhand check"))
				return nil
			}
			groups := []string{"github"}
			switch created.Build.System {
			case "go":
				groups = []string{"golang"}
			case "cargo", "cmake", "meson", "python":
				groups = append(groups, created.Build.System)
			}
			groupWord := "PortGroup"
			if len(groups) > 1 {
				groupWord += "s"
			}
			fmt.Fprintf(out, "Created %s/Portfile from the %s %s\n", created.Directory, strings.Join(groups, " and "), groupWord)
			if created.Crates > 0 {
				fmt.Fprintf(out, "  cargo.crates: %s, from Cargo.lock\n", prose.Plural(created.Crates, "crate"))
			}
			switch {
			case created.ChecksumsProblem != "":
				// Said whole, the rows a Go module before 1.17 needs by
				// hand with it, where the first line was (v0.4.0's known
				// issues).
				fmt.Fprintf(out, "  checksums: not generated: %s\n", strings.ReplaceAll(strings.TrimSpace(created.ChecksumsProblem), "\n", "; "))
			case created.Checksums != nil && created.Checksums.Distfiles > 0:
				what := prose.Plural(created.Checksums.Distfiles, "distfile")
				if created.Crates > 0 {
					what += fmt.Sprintf(" + %s", prose.Plural(created.Crates, "crate"))
				}
				fmt.Fprintf(out, "  checksums: %s\n", what)
			}
			if created.HomepageFrom != "" {
				fmt.Fprintf(out, "  homepage: using HTTPS, as MacPorts recommends (GitHub supplied %s)\n", created.HomepageFrom)
			}
			if len(created.Unconfirmed) > 0 {
				var marked []string
				for _, what := range created.Unconfirmed {
					switch {
					case what == "license" && observed.LicenseFrom != "":
						what = "license (from " + observed.LicenseFrom + ")"
					case what == "license" && observed.License != "":
						what = "license (from GitHub's detection)"
					case what == "category":
						// It picks the directory, so the guess is said with
						// what it chose, and create --category moves it.
						what = "category " + created.Category + " (guessed from " + created.CategoryFrom + "; create --category moves it)"
					}
					marked = append(marked, what)
				}
				fmt.Fprintf(out, "  Review these fields and remove their unconfirmed markers: %s\n", strings.Join(marked, ", "))
			}
			if s.File.Maintainer == "" {
				// The person's own line, as the ports at the branch's base
				// write their GitHub login, where it can be found (the
				// flyctl run).
				fmt.Fprintf(out, "  maintainers: nomaintainer (no maintainer configured). %s\n", engine.MaintainerWords(e.SuggestMaintainer(ctx, branch.Base), e.ConfigFile))
			}
			view.WritePlainHTTP(out, created.PlainHTTP)
			// A draft with no checksums isn't done: it exits 3, for a look,
			// where it exited 0 (v0.4.0's known issues).
			if created.ChecksumsProblem != "" {
				fmt.Fprintln(out, "Next: resolve the checksum problem above, then run dockhand checksums "+created.Port)
				return &term.ExitError{Code: term.ExitAttention}
			}
			fmt.Fprintln(out, "Next: "+branchflags.NextIn(ctx, e, branch, "dockhand edit "+created.Port, "dockhand check"))
			return nil
		},
	}
	where.Register(cmd)
	cmd.Flags().StringVar(&name, "name", "", "the port's name (default: the project's, in lower case)")
	cmd.Flags().StringVar(&category, "category", "", "port category (prompts in a terminal; otherwise uses a marked guess)")
	settings.SupportsJSON(cmd)
	return cmd
}

// buildWords says how a project builds, and the file that says so.
func buildWords(observed engine.Observed) string {
	if observed.Build.Evidence == "" {
		return observed.Build.Language()
	}
	return fmt.Sprintf("%s (%s)", observed.Build.Language(), observed.Build.Evidence)
}

type createdJSON struct {
	Branch      view.BranchRefJSON `json:"branch"`
	Port        string             `json:"port"`
	Directory   string             `json:"directory"`
	Version     string             `json:"version"`
	Build       string             `json:"build"`
	Category    string             `json:"category"`
	Crates      int                `json:"crates"`
	Unconfirmed []string           `json:"unconfirmed"`
	Checksums   string             `json:"checksums_problem,omitempty"`
	MovedFrom   string             `json:"moved_from,omitempty"`
}

func createdView(branch model.Branch, created engine.Created) createdJSON {
	return createdJSON{Branch: view.BranchRef(branch), Port: created.Port, Directory: created.Directory, Version: created.Version, Build: created.Build.System,
		Category: created.Category, Crates: created.Crates, Unconfirmed: view.NonNil(created.Unconfirmed), Checksums: created.ChecksumsProblem, MovedFrom: created.MovedFrom}
}
