# The sweep live check

**Status: a procedure, not a design.** S13 gave three write verbs and one new
report a selector, and the two things that cannot be proven where they were
built are the two things that matter most about them: what the NDJSON stream
actually looks like over a thousand real ports, and whether the politeness
under `outdated` is real when the host on the other end is github.com rather
than a fake transport.

Nothing in the test suite makes a network request, deliberately — a test that
proved the pacer paces by making requests would be making the requests the
pacer exists to prevent — so everything about the pacing is proven against a
fake clock and a fake transport. This checklist is the maintainer proving it
once against the real thing.

Two parts, and they are ordered on purpose:

- **A. `bump --plan maintainer:me`.** The flagship: one NDJSON row per port,
  validated by `jq`, a census tail on stderr, and an exit code in `{0, 83}`.
- **B. `outdated` over one category.** The politeness: request counts you can
  read off the census, a rerun that costs nothing, and a wall you can see.

Every step says the exact command, what output means it worked, and what to
capture if it does not. A finding here is not a step to retry: it is a defect
to write down, with the captured stream attached.

---

## 0. Before anything

```
cd <dockhand checkout>
git status --short                                  # expect: clean, or only what you meant
make check && go test -race ./... && GOOS=linux go build ./... && go vet ./...
LIVEPROOF_NETWORK=1 scripts/liveproof.sh check
go build -o /tmp/dockhand ./cmd/dockhand
/tmp/dockhand doctor
gh auth status
jq --version
```

- **Worked:** the gate is green, the live proof reports every recorded
  invocation identical, `doctor` finds MacPorts and a ports tree, `gh auth
  status` reports a logged-in account, and `jq` is present.
- **A finding:** the live proof reporting *any* difference. Stop. S13 adds
  capability and every single-target invocation must be byte-identical; a
  moved byte there is a defect that outranks everything below.
- **Not a finding, but stop anyway:** `gh auth status` reporting no account.
  Part A resolves `me` through `gh api user`, and part B's authoritative
  witness is the releases API. Without gh both run, and both run differently
  from what this document describes.

Everything below runs against your `macports-ports` checkout. Use
`/tmp/dockhand` explicitly so an older installed binary cannot be the thing
under test.

Set these once:

```
export PORTS_TREE=$HOME/Source/macports-ports
export OUT=$(mktemp -d /tmp/sweep-live.XXXXXX); echo "$OUT"
```

---

## A. `bump --plan maintainer:me`

`--plan` writes nothing: no branch, no note, no commit. It resolves each
port's latest version, plans the bump, and emits the row. That is the whole
of it, and it is why this is the one selector-scale write-verb invocation
safe to run against a real tree.

### A1. Know what you are about to ask for

```
/tmp/dockhand classify -t "$PORTS_TREE" maintainer:me 2>&1 | tail -20
```

- **Worked:** the `selector:` lines on stderr name the two keys and their
  counts — `gh:<handle> names N port(s)` and `mail:<addr> names M port(s)` —
  and the census reports roughly N ports classified. On this tree that is
  about 1070 and 1072.
- **A finding:** `sweep: cannot tell who you are`. The identity seams are
  wired on every verb now; this message means one of them failed, and the
  rest of the sentence says which.
- **Note the count.** Part A resolves `latest` for every one of them, which
  is a livecheck phase and a releases call per port. It takes a while by
  design: the requests are paced.

### A2. The run

```
time /tmp/dockhand bump --plan -t "$PORTS_TREE" maintainer:me \
  > "$OUT/bump.ndjson" 2> "$OUT/bump.err"
echo "exit: $?"
```

- **Worked:** an exit of `0` or `83`, and nothing else. `0` says every port
  was either planned or declined; `83` says some rows were neither, and the
  message on stderr names how many and where to start.
- **A finding:** any other exit code. `2` is a usage refusal and means the
  invocation never reached the sweep at all; `1` means something threw that
  nobody banded.
- **A finding:** the run finishing in under a minute over a thousand ports.
  That is the signature of a resolution that is not paced, which is the
  defect this whole design exists to prevent. Capture `bump.err` and stop.

### A3. Validate the stream with jq

Every line is one object, and the field set is fixed. These are the checks,
each of which must print `true`:

```
# Every line parses, and there is one line per port that was not excluded.
jq -s 'length' "$OUT/bump.ndjson"

# The six fields every row carries.
jq -s 'all(has("port") and has("outcome") and has("code") and has("family"))' "$OUT/bump.ndjson"

# port is never empty: a row that names nowhere is unusable.
jq -s 'all(.port | type == "string" and length > 0)' "$OUT/bump.ndjson"

# outcome is a member of the closed vocabulary.
jq -s 'all(.outcome | IN("minted","superseded","advanced","planned","applied",
                         "unchanged","declined","excluded","abandoned","failed"))' "$OUT/bump.ndjson"

# family is a member of the exit vocabulary — never the empty string, which
# is what a row built without a twin publishes.
jq -s 'all(.family | IN("success","declined","refused","upstream","pending",
                        "verdict","environment","tree","partial","usage","failure"))' "$OUT/bump.ndjson"

# --plan realizes nothing, so no row may claim a branch.
jq -s 'all(.outcome | IN("minted","superseded","advanced","applied") | not)' "$OUT/bump.ndjson"
```

- **Worked:** every expression prints `true`, and the first prints a number
  close to the portdir count from A1 (lower than the name count: two subports
  of one Portfile are one portdir, and the sweep says so on stderr).
- **A finding:** any `false`. Capture the offending rows and the count:

```
jq -c 'select(.family == "" or (.port | length) == 0)' "$OUT/bump.ndjson" | head
```

### A4. Read the shape of the answer

```
jq -r '.outcome' "$OUT/bump.ndjson" | sort | uniq -c | sort -rn
jq -r 'select(.outcome=="declined") | .reason' "$OUT/bump.ndjson" | sort | uniq -c | sort -rn
tail -20 "$OUT/bump.err"
```

- **Worked:** `declined` dominates, `already-current` dominates the declines,
  and the census tail on stderr agrees with the `uniq -c` arithmetic — the
  tail's total equals the line count of the file.
- **Worked:** a handful of `excluded` rows, each carrying `code` 10 and a
  `reason` beginning `excluded-`, and — for the ones a person pinned — a
  `remedy` saying a human has to look and the Portfile's own quoted comment
  in `detail`.
- **A finding:** the census total disagreeing with the line count. The
  invariant is one row per target, and it is the only way a reader can tell a
  complete sweep from a short one.
- **A finding:** a `failed` row whose `detail` mentions a rate limit or a
  refusal. That is a host the sweep should have stopped asking, and it means
  the wall did not go up. Capture the row.

### A5. Rerun it

```
time /tmp/dockhand bump --plan -t "$PORTS_TREE" maintainer:me \
  > "$OUT/bump2.ndjson" 2> "$OUT/bump2.err"
echo "exit: $?"
diff <(jq -S -c 'del(.detail)' "$OUT/bump.ndjson" | sort) \
     <(jq -S -c 'del(.detail)' "$OUT/bump2.ndjson" | sort) && echo IDENTICAL
```

- **Worked:** the second run is **markedly faster** than the first and the
  two streams are identical but for details. The speed is the observation
  cache doing its job: within the six-hour TTL the forge is not asked again.
- **A finding:** the second run taking as long as the first. The resolution
  is not reading the cache, and a maintainer running this twice in an
  afternoon is asking one forge two thousand redundant questions.
- **A finding:** rows that differ in `outcome` between the two runs. Nothing
  was written, so nothing should have moved.

### A6. What must be refused

```
/tmp/dockhand bump --plan --riders -t "$PORTS_TREE" maintainer:me; echo "exit: $?"
/tmp/dockhand bump --to 1.2.3 --plan -t "$PORTS_TREE" maintainer:me; echo "exit: $?"
/tmp/dockhand bump --plan --replace -t "$PORTS_TREE" maintainer:me; echo "exit: $?"
```

- **Worked:** all three exit `2` with a sentence naming the count and why the
  flag is singular. Nothing is planned and nothing is written.
- **A finding:** any of them running. `--to` over a selector would write one
  version onto a thousand different ports.

### A7. Leave nothing behind

```
git -C "$PORTS_TREE" status --short | head
git -C "$PORTS_TREE" branch --list 'dockhand/*' | head
```

- **Worked:** unchanged from before the run. `--plan` writes nothing.
- **A finding:** any new branch or any modified Portfile.

---

## B. `outdated` over one category

Part A exercised the write verbs' road. This exercises the report's, where
the staging and the census budget are visible.

Pick a category big enough to be interesting and small enough to be polite.
`devel` is thousands of ports; **do not** start there.

```
export CAT=fuse                                   # ~30 ports; substitute your own
/tmp/dockhand classify -t "$PORTS_TREE" "category:$CAT" 2>&1 | tail -3
```

### B1. A cold run, with the cache emptied

```
rm -rf "${XDG_CACHE_HOME:-$HOME/Library/Caches}/dockhand/upstream"
time /tmp/dockhand outdated -t "$PORTS_TREE" --json "category:$CAT" \
  > "$OUT/outdated.ndjson" 2> "$OUT/outdated.err"
echo "exit: $?"
cat "$OUT/outdated.err"
```

- **Worked:** exit `0`. The census tail on stderr carries the outcome lines
  and then the **budget lines**, which are the point of this whole exercise:

  ```
  31 ports examined
    outdated        2  (6.5%)
    current        28  (90.3%)
    excluded        1  (3.2%)
    ls-remote      27 asked  (27 fetched)
    releases        2 asked  (2 fetched)
    livecheck       2 asked  (2 fetched)
  ```

- **Read it as follows.** `ls-remote` is asked for every port that names a
  forge — the cheap witness, paid always. `releases` and `livecheck` are
  asked only for the **candidates**: the ports whose forge holds something
  that outranks what they ride, plus the ports with no forge to have asked.
  The staging is working when those two numbers are a small fraction of the
  first.
- **A finding:** `livecheck` asked as often as `ls-remote` without `--deep`.
  The staging has stopped staging, and a full sweep would be thousands of
  MacPorts fetch phases against thousands of unrelated web sites.
- **A finding:** the run finishing faster than `0.5s × ls-remote asked`. The
  default pace is 500ms per host and github.com is nearly all of them, so a
  30-port category cannot honestly finish in five seconds. That arithmetic is
  the pacing, observed.

### B2. The rerun, which must cost nothing

```
time /tmp/dockhand outdated -t "$PORTS_TREE" --json "category:$CAT" \
  > "$OUT/outdated2.ndjson" 2> "$OUT/outdated2.err"
cat "$OUT/outdated2.err"
```

- **Worked:** the budget lines now read `0 asked (27 fresh)` and the run
  returns in about the time the evaluations take. Nothing was asked of any
  host, and the two JSON streams are identical.
- **Worked:** `.stages[].source` on the rows says `fresh`:

  ```
  jq -r '.stages[]?.source' "$OUT/outdated2.ndjson" | sort | uniq -c
  ```

- **A finding:** anything but `fresh` on the second run inside the TTL. The
  cache is not being read, and every sweep pays full price forever.

### B3. Revalidation, which must cost a 304 and no body

```
/tmp/dockhand outdated -t "$PORTS_TREE" --json --ttl 1s "category:$CAT" \
  2>&1 >/dev/null | tail -6
```

- **Worked:** the budget lines show `releases` with some `revalidated` in the
  breakdown. That is the ETag path: `gh` sent `If-None-Match`, GitHub
  answered 304, and no body crossed the wire.
- **A finding:** `releases` showing only `fetched` on a tree that has not
  moved. `gh` answers a 304 with a **non-zero exit** and the transport reads
  the status out of the error text; a change to `gh` that reworded it would
  make every unchanged feed pay for a body it did not need. Capture:

  ```
  gh api 'repos/cli/cli/releases?per_page=1' --include | grep -i '^etag'
  gh api 'repos/cli/cli/releases?per_page=1' --include -H 'If-None-Match: <that etag>'; echo "exit: $?"
  ```

  The second call's exit and its first line are the whole of what the
  transport depends on. Write both down.

### B4. The row shape

```
jq -s 'all(has("port") and has("outcome") and has("code") and has("family"))' "$OUT/outdated.ndjson"
jq -s 'all(.family | length > 0)' "$OUT/outdated.ndjson"
jq -c 'select(.outcome=="outdated") | {port,current,latest,sha,verdict}' "$OUT/outdated.ndjson"
jq -c 'select(.outcome=="excluded") | {port,code,family,reason}' "$OUT/outdated.ndjson"
```

- **Worked:** every outdated row carries `current`, `latest` and a `verdict`,
  and — where the answer came from a tag — a 40-character `sha`, which is the
  exact object a bump would fetch. Excluded rows carry code `10`, family
  `declined`, and a reason beginning `excluded-`.
- **A finding:** `family` empty on any row, or `code` 0 on an excluded or
  abandoned one. A row nothing examined must not read as a success.

### B5. What must be refused

```
/tmp/dockhand outdated -t "$PORTS_TREE" --deep -a; echo "exit: $?"
/tmp/dockhand outdated -t "$PORTS_TREE" --pace 1ms "category:$CAT"; echo "exit: $?"
```

- **Worked:** both exit `2`. `--deep` over the tree is a MacPorts livecheck
  phase for every port in it, serialized; `--pace 1ms` over a selector defeats
  the entire ruling from the command line. Both refusals name the count and
  the floor.
- **Worked:** `--deep` over the category alone is *allowed* and slower, and
  its census shows `livecheck` asked for every port rather than for the
  candidates:

  ```
  /tmp/dockhand outdated -t "$PORTS_TREE" --deep "category:$CAT" 2>&1 >/dev/null | tail -6
  ```

- **A finding:** either refusal not firing.

### B6. The wall, if you are unlucky enough to see it

Do not provoke this. If a run reports `walled` rows:

- **Worked (the design working):** the process still exits `0`, the walled
  rows carry family `upstream` and reason `witness-walled`, and the census
  tail ends with a line saying how many ports were not examined and that
  running again finishes them.
- **Capture, always:** the `detail` of the first walled row. It carries the
  host's own words, which is the only evidence of what tripped it:

  ```
  jq -r 'select(.outcome=="walled") | .detail' "$OUT/outdated.ndjson" | head -3
  ```

- **A finding:** walled rows and a non-zero exit, or walled rows with no
  remedy line in the tail. A host refusing dockhand is not a broken port.

---

## What to send back

Whatever happened, send:

- the exit code of each numbered command,
- `$OUT/bump.err` and `$OUT/outdated.err` whole — the census tails are the
  measurement,
- the first twenty lines of each NDJSON stream,
- any row `jq` flagged, in full,
- and, for anything in B, the elapsed time beside the `asked` counts. The
  pacing is only observable as the ratio between those two numbers.
