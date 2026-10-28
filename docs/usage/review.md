# Reviewing pull requests

## Someone else's pull request

`adopt --pr <number>` brings someone's pull request into a branch of its own, `pr-<number>`, to look at and work on. Dockhand assumes no permission to push to their branch: `submit` pushes there only when they let maintainers edit and you have write access, and it never rewrites their description. Their Portfiles aren't yours: where dockhand evaluates them on this Mac, it says once whose they are, and a local check of the branch, which would run them as root, is refused. `review <pr> --check` adopts it and checks it in Tart, or where `--on` says.

## Reviewing one

```sh
dockhand review 35174               # the commit rules, and what update would find of it
dockhand review 35174 --check       # adopted, and checked in Tart
```

`dockhand review <pr>` applies the same commit rules to anyone's pull request, and says what `update` would find of it: what upstream's change means for each port it changes, against the base it leaves master at, as a branch's revision is assessed (its patches included, and each patch it drops, with whether that still applies), and the other ports that depend on them, from the index at that base, as candidates to look at. Nothing of it is recorded. It posts nothing unless you say so: `--comment`, `--request-changes`, or `--markdown` to print the text for pasting.
