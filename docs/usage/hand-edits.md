# Editing by hand

Every command reads the branch's files as they are, so any edit of yours, with any tool, is part of the next check, `tidy`, and `submit`. These are the commands for the edits dockhand doesn't make by itself, and for trying a branch's port with MacPorts.

## edit

`edit <port>` opens a port's Portfile in `$VISUAL` or `$EDITOR`, bringing its directory into a sparse worktree first. Without a terminal or an editor, or with `--no-open`, it prints the Portfile's path. Any other editing is yours to do in the worktree, with any tool; dockhand reads the files as they are.

## When update refuses

Where `update` refuses an edit it doesn't make, `--hand-edit` writes its best attempt for you to finish: the version, with the revision reset, and the checksums it could fetch. The attempt is yours, not dockhand's, so `tidy`'s plan, `status`, and the pull request's Description say it, and an unattended run holds on it. `ship`, `bump`, and `serve` never take it.

## checksums

```sh
dockhand checksums jq
```

This is what to run after editing a version by hand: it fetches the distfiles the Portfile names, a variant's own included, and writes their checksums. A port whose Portfile lists its crates or Go modules (`cargo.crates`, `go.vendors`) has its own archives refreshed with those lists set aside, as an update computes them. The lists go back byte for byte, since their checksums are Cargo.lock's or go.sum's. Where it can't find a declaration in the Portfile to edit, as one a PortGroup or an `eval` makes, it says which archive's and why, and prints the checksums every archive has now, laid out for the Portfile. A distfile that changed upstream under the same name, in a Portfile whose version the branch hasn't changed, is a stealth update, a comment or a homepage edited by hand included; a revision the branch already bumped by hand isn't bumped again. Dockhand says so and shows the checksums before and after. Since the source changed, it bumps the revision and sets `dist_subdir ${name}/${version}_${revision}`, so mirrors keep both archives. `--no-revbump` leaves the revision, for a change that needs no rebuild, and numbers the directory `${name}/${version}_1` instead. A later version update removes either, where every archive of the new version has a name of its own; where one keeps its name, the line keeps its versions apart on the mirrors, and stays. Both edits are evaluated as MacPorts reads them, and held to changing the port alone: where they would also change another port in the Portfile, such as a subport that inherits the revision, the checksums are refreshed and the rest is left for you, saying why.

## revbump

```sh
dockhand revbump gdal inkscape --subject "rebuild for poppler 25.09.0" --new=poppler-25.09
```

`revbump` increases each port's revision and records the reason as its commit subject, "<port>: <reason>". A revision shared by several subports is bumped for all of them.

## Trying a branch with MacPorts

To try a branch's port with MacPorts itself, `dockhand check --on local --keep` builds it in `/opt/local` as a check, recorded like any other, and leaves the branch's versions active; without `--keep`, the prefix is put back as it was ([the local provider](../local-provider.md)). By hand, `sources.conf` stays as it is. For a port whose dependencies the branch doesn't change, `cd "$(dockhand path jq-4f2a)/textproc/jq"` and `sudo port install` builds that directory's Portfile, as `sudo port -D <directory> install` does from anywhere. Where the branch changes a dependency too, give `port` a `sources.conf` of your own that lists the worktree first, ahead of the trees MacPorts' own names, so the branch's ports come from it and the rest as before; MacPorts reads it through `PORTSRC`, beside its own `macports.conf`, which `sudo` would drop, hence `sudo env`:

```sh
B=jq-4f2a; PORT=jq; MP_PREFIX=/opt/local
WT="$(dockhand path "$B")"
CONF="$HOME/.macports-branch"; mkdir -p "$CONF"
{ printf 'file://%s\n' "$WT"; grep -Ev '^[[:space:]]*(#|$)' "$MP_PREFIX/etc/macports/sources.conf"; } > "$CONF/sources.conf"
printf 'sources_conf %s\n' "$CONF/sources.conf" > "$CONF/macports.conf"
(cd "$WT" && "$MP_PREFIX/bin/portindex")
sudo env PORTSRC="$CONF/macports.conf" "$MP_PREFIX/bin/port" -N install --unrequested "depof:$PORT"
sudo env PORTSRC="$CONF/macports.conf" "$MP_PREFIX/bin/port" -N -s install "$PORT"
```

The worktree goes first because it's sparse, holding only the ports the branch changes; `depof:` names the port's direct dependencies, which come from MacPorts' archives where it has them, installed as unrequested, and `port` resolves the rest; the port itself builds from source (`-s`). Run `portindex` again after editing a Portfile: the worktree's `PortIndex` is Git's to ignore. The branch's versions are installed and active over your own in `/opt/local`, the earlier ones kept inactive for `port activate`. Tried on the main Mac with a revision bump of ijq: `port` read ijq from the worktree, its dependencies came from archives, only ijq built, and `/opt/local/etc/macports/sources.conf` was unchanged. dockhand never adds a worktree to MacPorts' own `sources.conf`: the file is root's, a tree there needs its port index kept current, and a worktree listed above the default tree would answer every `port` command with its versions, a branch left behind included.
