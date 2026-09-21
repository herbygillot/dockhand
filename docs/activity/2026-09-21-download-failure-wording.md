# 2026-09-21: dockhand's own download says where and why it failed

## Why

Preparation downloads each archive itself and records the error text as the
job's needs-attention detail, so the wording is all the user gets. A 404
already read well. The other branches did not: a body over the size limit
returned the fetch package's bare "response exceeds size limit" with no
file, URL, or limit; a short read while sniffing the first bytes returned the
raw I/O error; the encoded-content and empty-file refusals named no URL, and
the latter conflated two causes; the two-minute deadline surfaced as
"context deadline exceeded" without saying whose limit; a refused response
dropped the body where a CDN or rate limiter says why; and after a redirect
the message named only the final URL.

## What changed

- `fetch.StatusError` carries `Requested`, the URL asked for when a redirect
  led elsewhere, and `Reason`, the first line of a plain-text or JSON error
  body, capped at 200 characters. HTML bodies are left out. The error text
  is "HTTP 403 for <final>, redirected from <requested>: <reason>".
- `fetch.ErrTooLarge` is exported so a caller can name the limit.
- `portedit.downloadError` words every failure as "downloading <file> from
  <url>: <cause>": the size limit with its value, dockhand's own deadline
  with its duration, a transfer that stopped with the byte count reached,
  an HTML page or encoded content in place of the file, and an empty file.
  A transport error drops the URL that `url.Error` would repeat. A refusal
  keeps the status form, since the status names the URL, and a 404 keeps
  its note about unpublished assets. The parent context's own cancellation
  passes through unworded. Every cause stays reachable with `errors.Is` and
  `errors.As`.
- `Service.DownloadTimeout` makes the per-archive deadline configurable,
  two minutes when unset, so the wording can be tested.
- The unreachable "oversized" branch after the bounded read is gone.

## Evidence

- `TestDownloadRejectsErrorBodiesAndSizeOverflow` checks the wording of
  each refusal, that the URL is named once, the deadline message with a
  50ms limit, and a transport failure.
- `TestResponseStatusAndReaderErrors` checks the reason line and the
  requested URL after a redirect.
