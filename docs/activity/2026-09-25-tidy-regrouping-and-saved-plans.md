# 2026-09-25: tidy regrouping and saved plans

Design v3 §6.9's "Change groups [g]" and §8's `tidy --plan --out <file>` / `tidy --apply <file>`.

## What changed

- **`TidyPlan.Regroup(spec, author)`** rearranges the proposed commits.
  - The spec names the new commits in order, each one or more of the plan's numbered commits joined with `+`. For example, `2 1+3` makes commit 2 first, then one commit of 1 and 3. Every commit must be named exactly once.
  - A combined commit:
    - takes the first member's message that has a subject, with a note asking the person to check it describes the whole commit;
    - joins the members' ports, paths, and the existing commits they absorb, in history order;
    - uses one author when its commits have one. It needs `--author` when they have several, as `tidy` already requires.
  - A combined commit is never unambiguous, so it is always shown for review.
  - Reordering is allowed. The groups' paths don't overlap, so any order ends at the same final tree, and `ApplyTidy` still checks that it does.
- **`tidy --group "2 1+3"`** does this from the command line. The grouping is the person's explicit choice, so like `--squash --message` it applies without a terminal. On a terminal, the review prompt gains **Change groups [g]**. A spec that is wrong is reported, and the plan is left as it was.
- **Saved plans:**
  - `tidy --plan --out <file>` writes a versioned JSON plan with:
    - the branch;
    - the base, head, and working tree it is bound to;
    - each commit's message, author, paths, and the commits it combines.
  - Messages and authors may be edited in the file.
  - `tidy --apply <file>` loads the file and refuses it when any of these changed:
    - the branch's base;
    - its commits;
    - its files.

    The error says which one changed. It also refuses a plan whose commits don't cover exactly the branch's changes, naming what is missing or extra.
  - Blocking problems, such as a missing subject or author, are worked out again from the file.
  - Saving the plan was the review, so an applicable plan is applied as it stands.
- **`ApplyTidy`** now also refuses a plan whose branch base moved after it was made.

## Tests

- **Engine:**
  - regrouping by reordering and combining, and every malformed spec;
  - author choice for a combined commit made from two people's commits;
  - applying a regrouped plan;
  - a saved plan with an edited message round-trips and applies;
  - saved plans that are partial, unknown, or a future version are refused;
  - a saved plan goes stale after the files are edited, and again after it has been applied.
- **Command:**
  - `--out` without `--plan` is refused;
  - `--plan --group "2 1" --out` saves the plan, and `--apply` applies it and then refuses it as stale;
  - after `restore`, the interactive `[g]`, with one mistaken spec, combines both commits and applies them.
