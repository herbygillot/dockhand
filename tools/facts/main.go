package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `usage:
  facts buildbot -out FILE
  facts tart -out DIR [IMAGE...]
  facts runner -name IMAGE -out DIR
  facts host -name NAME -out DIR
  facts generate [-tart DIR]... [-runner DIR]... [-host DIR]... [-installer FILE]... [-buildbot FILE] -out FILE`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "facts:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	out := flags.String("out", "", "where to write")
	switch args[0] {
	case "buildbot":
		builds := flags.Int("builds", 20, "recent builds of each builder to try for a log with the header")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" {
			return errors.New("buildbot: -out is required")
		}
		return harvestBuildbot(ctx, buildbotURL, *builds, *out)
	case "tart":
		parallel := flags.Int("parallel", 2, "images probed at once; the Mac runs two VMs at most")
		executable := flags.String("tart", "tart", "the tart executable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" {
			return errors.New("tart: -out is required")
		}
		return harvestTart(ctx, *executable, *out, *parallel, flags.Args())
	case "runner":
		name := flags.String("name", "", "the runner's image label, macos-15-intel")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" || *name == "" {
			return errors.New("runner: -name and -out are required")
		}
		return harvestRunner(ctx, sourceRunner, *name, *out)
	case "host":
		name := flags.String("name", "", "a name for the Mac probed, aws-mac1-ventura")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" || *name == "" {
			return errors.New("host: -name and -out are required")
		}
		return harvestRunner(ctx, sourceHost, *name, *out)
	case "generate":
		var probeDirs list
		flags.Var(&probeDirs, "tart", "a directory of Tart probes (repeatable)")
		flags.Var(&probeDirs, "runner", "a directory of runner probes (repeatable)")
		flags.Var(&probeDirs, "host", "a directory of host probes, tools/facts/probes/... (repeatable)")
		var installers list
		flags.Var(&installers, "installer", "a reading of installers, tools/facts/installers/... (repeatable)")
		buildbot := flags.String("buildbot", "", "the buildbot harvest")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" {
			return errors.New("generate: -out is required")
		}
		return generate(probeDirs, installers, *buildbot, *out)
	}
	return errors.New(usage)
}

type list []string

func (l *list) String() string     { return fmt.Sprint(*l) }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }
