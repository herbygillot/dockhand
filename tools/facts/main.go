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
  facts generate [-tart DIR]... [-buildbot FILE] -out FILE`

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
	case "generate":
		var tartDirs list
		flags.Var(&tartDirs, "tart", "a directory of Tart probes (repeatable)")
		buildbot := flags.String("buildbot", "", "the buildbot harvest")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *out == "" {
			return errors.New("generate: -out is required")
		}
		return generate(tartDirs, *buildbot, *out)
	}
	return errors.New(usage)
}

type list []string

func (l *list) String() string     { return fmt.Sprint(*l) }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }
