package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// testProjectReader, when set, stands in for GitHub in create.
var testProjectReader engine.ProjectReader

func createCommand(s *settings, streams Streams) *cobra.Command {
	var where branchChoice
	var name, category string
	cmd := &cobra.Command{
		Use:   "create <url>",
		Short: "Write a new port's first Portfile from its project's URL",
		Long: `Reads a project on GitHub, its latest release, and the build files at that
release, and writes a new port's Portfile in the branch's working files: the
github PortGroup, and cargo, golang, cmake, meson, or python as its files
say, with the release's version. A Rust project's cargo.crates come from
its Cargo.lock. It then fills in the checksums as dockhand checksums does.

What it observed, it fills in; what it guessed, it marks with a
"# dockhand: unconfirmed" comment: the license from GitHub's detection, the
long description, and the category unless --category names it. The
maintainer is your config's maintainer, else nomaintainer, marked. The new
Portfile is staged, so the next check includes it. Nothing is committed.

The branch is --branch, else the one checked out here; --new starts one.
--name names the port when the project's name is not what it should be.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if testProjectReader != nil {
				e.ProjectReader = testProjectReader
			}
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
			if project.License != "" {
				license = "GitHub says " + project.License
			}
			header := fmt.Sprintf("%s %s · %s · %s", name, observed.Version, buildWords(observed), license)
			if project.Description != "" {
				header += fmt.Sprintf(" · %q", project.Description)
			}
			fmt.Fprintln(out, header)

			var branch model.Branch
			switch {
			case where.branch != "":
				branch, err = e.Resolve(ctx, where.branch)
			case where.new:
				var started bool
				if branch, started, err = startFor(ctx, e, name); err == nil && started {
					fmt.Fprintf(out, "Started %s from master %s (fetched just now)\n", branch.Name, engine.Short(branch.Base))
				}
			default:
				branch, err = e.Current(ctx)
				if errors.Is(err, engine.ErrNoBranch) {
					return errors.New("create writes in a branch: --new starts one, or --branch names one")
				}
			}
			if err != nil {
				return err
			}
			if category == "" && streams.terminal() {
				answer, err := ask(streams, fmt.Sprintf("? category [%s]: ", observed.Category))
				if err != nil {
					return err
				}
				category = answer
				if category == "" {
					category = observed.Category
				}
			}
			created, err := e.Create(ctx, engine.CreateRequest{Branch: branch, URL: args[0], Name: name, Category: category, Maintainer: s.file.Maintainer, Project: &project})
			if err != nil {
				return err
			}
			streams.emit(createdView(branch, created))
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
				fmt.Fprintf(out, "  cargo.crates: %s, from Cargo.lock\n", plural(created.Crates, "crate"))
			}
			switch {
			case created.ChecksumsProblem != "":
				fmt.Fprintf(out, "  checksums: not filled in: %s\n    dockhand checksums %s fills them in once that is fixed\n", firstLine(created.ChecksumsProblem), created.Port)
			case created.Checksums != nil && created.Checksums.Distfiles > 0:
				what := plural(created.Checksums.Distfiles, "distfile")
				if created.Crates > 0 {
					what += fmt.Sprintf(" + %s", plural(created.Crates, "crate"))
				}
				fmt.Fprintf(out, "  checksums: %s\n", what)
			}
			if len(created.Unconfirmed) > 0 {
				var marked []string
				for _, what := range created.Unconfirmed {
					if what == "license" && project.License != "" {
						what = "license (from GitHub's detection)"
					}
					marked = append(marked, what)
				}
				fmt.Fprintf(out, "  Unconfirmed, marked in the file: %s\n", strings.Join(marked, ", "))
			}
			fmt.Fprintf(out, "Next: dockhand edit %s, then dockhand check\n", created.Port)
			return nil
		},
	}
	where.flags(cmd)
	cmd.Flags().StringVar(&name, "name", "", "the port's name (default: the project's, in lower case)")
	cmd.Flags().StringVar(&category, "category", "", "the port's category (default: asked on a terminal, else guessed and marked)")
	return cmd
}

// buildWords says how a project builds, and the file that says so.
func buildWords(observed engine.Observed) string {
	if observed.Build.Evidence == "" {
		return observed.Build.Language()
	}
	return fmt.Sprintf("%s (%s)", observed.Build.Language(), observed.Build.Evidence)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

type createdJSON struct {
	Branch      branchRefJSON `json:"branch"`
	Port        string        `json:"port"`
	Directory   string        `json:"directory"`
	Version     string        `json:"version"`
	Build       string        `json:"build"`
	Category    string        `json:"category"`
	Crates      int           `json:"crates"`
	Unconfirmed []string      `json:"unconfirmed"`
	Checksums   string        `json:"checksums_problem,omitempty"`
}

func createdView(branch model.Branch, created engine.Created) createdJSON {
	return createdJSON{Branch: branchRef(branch), Port: created.Port, Directory: created.Directory, Version: created.Version, Build: created.Build.System,
		Category: created.Category, Crates: created.Crates, Unconfirmed: nonNil(created.Unconfirmed), Checksums: created.ChecksumsProblem}
}
