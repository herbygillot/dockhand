package cli

import "github.com/spf13/cobra"

// usageTopic is `dockhand usage`, a HELP TOPIC and not a verb: cobra
// files a command with a Long and no RunE under "Additional help
// topics", and both `dockhand usage` and `dockhand help usage` print it.
//
// IT EXISTS BECAUSE THE HELP DESCRIBED EVERY VERB AND NO ROAD. A person
// reading `bump --help` learned what bump does and could not learn that
// a normal bump returns before its build finishes, that `status` is
// where the verdict arrives, or that `promote` opens a pull request
// rather than only pushing a branch. Each of those is written on the
// verb that owns it, which is exactly where somebody who does not yet
// know the shape of the tool will not look.
//
// The topic is not grouped, on purpose: the groups are questions a
// person arrives with, and this is not a thing to do — it is the map.
// TestEveryVerbIsFiledUnderAGroup exempts it by asking cobra whether it
// is runnable rather than by naming it.
func usageTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "A worked walkthrough: an upstream release to a pull request",
		Long: `Two roads from an upstream release to a pull request, and one
command that walks either of them.

    dockhand bump --to-pr cmark

THAT IS THE WHOLE TOOL for one port. On a machine that can verify, it
writes the branch, builds the port in a pristine VM, waits for the
verdict and opens the pull request. On a machine that cannot, it writes
the branch and opens the pull request, and the body says the change was
not pre-verified. Same command, same result, different amount of
waiting. ` + "`dockhand doctor`" + ` says which of the two this machine is.

SETTING UP THE VERIFIED ROAD is one command, once per machine and once
per macOS release. tart is not dockhand's to install.

    dockhand provision

--to-pr STAYS FOR THE BUILD, because the pass is what authorizes the
pull request and this invocation cannot hand you one without it. A
` + "`port build`" + ` takes minutes to hours. Ctrl-C is safe at any point:
the build is detached and owned by the record, so the work continues
and ` + "`dockhand status`" + ` will have the verdict. --timeout 90m stops
waiting AND stops the build, keeping its environment so you can still
look inside; without it there is no deadline.

    dockhand bump --to-pr --timeout 90m cmark

THE ROAD WITHOUT A BUILD is the same command and returns at once. Use
it when there is nothing to verify against, or when you have tested the
change yourself:

    dockhand bump --to-pr --no-verify cmark

THE SAME ROAD, ONE STEP AT A TIME, which is what a bump without --to-pr
does and what you will use for more than one port:

    dockhand outdated cmark        what does upstream have?
    dockhand bump cmark            write the branch, start the build,
                                   and RETURN — the build is not done
    dockhand status                every change, its standing, and what
                                   is blocking it
    dockhand log cmark --trace     watch the build as it is written
    dockhand promote cmark         push to your fork and open the PR

` + "`bump`" + ` returns before the build finishes on purpose: the ledger
outlives your terminal, so there is nothing to sit through. ` + "`status`" + `
takes no port — it reports the whole checkout — and is the verb to run
when you do not know what to do next.

` + "`promote`" + ` pushes AND opens a pull request. It refuses a change
whose verification failed; --ignore publishes past a failure and says
so in the body, and --no-pr pushes to your fork and stops there.

WHEN THE PORT HAS DEPENDENTS, a bump proposes revision bumps for the
ports built against it, verifies them together as a cohort in one
environment, and one pull request carries them all. --to-pr then waits
for every member. ` + "`dockhand status`" + ` lists them, and ` + "`dockhand dismiss`" + `
records that a person looked at a proposal and said no.

WHEN SOMETHING IS STUCK, in order of how much it takes back:

    dockhand cancel cmark          stop a running verification
    dockhand discard cmark         delete an in-flight branch
    dockhand purge                 remove every dockhand branch, pin,
                                   record and environment here

EXIT CODES are banded, so a script can tell a refusal from a failure
without reading English: 0 success, 10s declined, 20s refused, 30s the
environment, 40s the tree, 50s upstream, 60s pending, 70s a verdict, 80s
partial. ` + "`dockhand promote`" + ` exiting 70 is a port that failed to
build; exiting 20 is dockhand declining to publish it.`,
	}
}
