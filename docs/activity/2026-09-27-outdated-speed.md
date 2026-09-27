# 2026-09-27: outdated, eight times faster

`outdated --mine` over the person's 1,076 ports took 25½ minutes on 2026-09-26 ([note](2026-09-26-real-update.md)). serve does the same every day. It now takes 3 minutes, with a third of the GitHub requests, and on a sample the results are identical.

## Where the time went

An instrumented build timed each port's phases, on 33 of the person's ports picked evenly from the whole list, in the scratch clone and database:

| Phase | Share |
| --- | --- |
| Waiting on the network: GitHub's tag and release listings, livecheck pages | 62% |
| Evaluating the Portfile | 17% |
| MacPorts' `vercmp`, a fresh tclsh for each of about five comparisons | 17% |
| Evaluating candidate versions | 4% |

That was 1.45 seconds a port, and 26 minutes for 1,076, which is the run's time. Every part was per port, and the ports were done one after another.

A second instrumented build counted requests: 160 to GitHub for the 33 ports, 4.8 each. Two kinds were waste:

- **Tags and releases were listed 30 to a page**, go-github's default when no page size is given. grafana's tags, eight pages at 100, took about 25 at 30.
- **A tag was read twice.** Setting aside an old tag that compares newer (`predates`) reads the candidate tag, and the caller reads it again. Every tag read was also redundant after a listing, which names each tag's commit already.

## Changed

- **Ports are looked up concurrently** (`outdated.Service.Observe`), eight at once, or as many CPUs as there are, if fewer. The results keep the order they were asked in. Probing a candidate version writes it into the port's own Portfile in the shared projection, so subports of one Portfile still go one after another; only distinct Portfiles overlap. This is the whole-tree survey's rule (`assess`), which ran 41,000 ports this way. When interrupted, it reports the ports it finished, as before.
- **Listings ask for 100 to a page**, the most GitHub's API gives.
- **A repository remembers its tags** for as long as it is bound, which is one port's lookup: those its listing named, with their commits, and those it read one at a time. A long-running serve never reuses them, since each lookup binds its own. Tags from the `git ls-remote` fallback aren't kept, since git can't tell a tag on a commit from one on a blob.
- **Requests to GitHub's API are paced across the process**, one every 80 ms, which is 750 a minute. GitHub documents a secondary rate limit of 900 points a minute for REST endpoints, one point per GET, whatever the hourly allowance. Eight ports looked up at once would pass it without pacing. Only `api.github.com` is paced: an Enterprise server or a test's server is asked at once. go-github already stops sending requests after a secondary-limit refusal, until the time GitHub gives. A refused tag listing or tag read still falls back to `git ls-remote`.

## Measured

- **The 33 ports:** 56 seconds became 13, about 8 of them fixed setup (fetching master, staging the index). The results were identical to the old build's. GitHub requests went from 160 to 109 with the page size, and to 56 with the remembered tags: 1.7 a port.
- **All 1,076:** 25½ minutes became 3 (180 seconds), finding 177 with newer releases and 167 that couldn't be checked, none of them for a rate limit. Master had moved since the first run, so 177 isn't comparable to the 180 found then. The 167 are the same count, and remain the next coverage question.
- **The GitHub budget:** at the sample's rate, a whole run is about 1,800 requests of the 5,000 an hour GitHub allows the person. The allowance is per user, shared with the GitHub CLI and any other app acting for them. At the old rate it was about 5,200, more than the hour's allowance. At 750 a minute, pacing alone takes at least 2½ minutes for 1,800 requests, so it is close to setting the pace.

## Tests

- The pacer spaces requests asked for together, and lets go of one whose context ends.
- Only GitHub's own API is paced.
- A listing asks for 100 to a page, and a listed tag is not read again, while an unlisted one still is.
- A repository asks once for a tag read twice. That test used to expect the second read to reach the server.
- Several Portfiles looked up at once, against a real `port-tclsh`, keep the order they were asked in.
- `outdated`'s dependency guard allows `golang.org/x/sync/errgroup`, a concurrency primitive already vendored and used by `portedit/observe`, and still no integration.
- The touched packages pass under the race detector.

## Left

- **`vercmp` starts a tclsh for each comparison**, about five a port, 60 ms each. One interpreter per port would save that CPU. At eight ports at once, the pacing is closer to the limit than this is.
- **`outdated` prints nothing until it is done.** v3's commands don't show progress yet, and three minutes of silence is still a wait.
