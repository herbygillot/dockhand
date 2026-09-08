package record

// wire is the exact bytes Encode writes for populated(): two-space
// indent, no trailing newline, fields in declaration order, map keys
// sorted by encoding/json, and HTML escaping left on — which is why the
// two ">" in it read as \u003e, exactly as the notes on disk carry them.
//
// It is pinned as a literal rather than regenerated, because the point
// of the pin is that a change to the shape has to be typed out by a
// person who then has to agree it is what they meant.
const wire = `{
  "schema": 4,
  "sha": "7159f6b651e49cae47422560120e93ebc494acc9",
  "tree": "84638b5a25febc78bd8ac7cad517ef4d88764262",
  "change": {
    "schema": 1,
    "id": "chg-01HZ",
    "state": "extended",
    "branch": "dockhand/jq-1.9",
    "tip": "7159f6b651e49cae47422560120e93ebc494acc9",
    "pin": "refs/dockhand/pins/chg-01HZ",
    "slug": "jq-1.9",
    "content": "sha256:9f2c",
    "subjects": [
      {
        "port": "jq",
        "names": [
          "jq"
        ],
        "portdir": "textproc/jq",
        "intent": "bump",
        "target": "1.9",
        "reason": "upstream release"
      },
      {
        "port": "oniguruma",
        "names": [
          "oniguruma",
          "oniguruma-devel"
        ],
        "portdir": "devel/oniguruma",
        "intent": "bump-revision",
        "target": "rev2",
        "reason": "libjq install name moved"
      }
    ],
    "destination": "published",
    "asked_by": "human",
    "agent": "claude-code",
    "minted_via": "cohort",
    "crossing": "leaving-stable",
    "hold": {
      "origin": "crossing",
      "reason": "leaves stable",
      "at": "2026-09-01T02:00:00Z"
    },
    "riders": [
      "modeline"
    ],
    "findings": [
      {
        "kind": "abi-dependents",
        "diverged": [
          "Portfile"
        ],
        "ports": [
          "oniguruma"
        ],
        "candidates": [
          {
            "port": "oniguruma",
            "portdir": "devel/oniguruma",
            "proposed": true,
            "reason": "links libjq",
            "solo": true,
            "over": "jq",
            "forced": true
          }
        ],
        "criterion": "libjq.1.dylib compat 1.0.0 -\u003e 2.0.0",
        "source": "NEWS",
        "quote": "the shared library soname changed",
        "disposition": "accepted",
        "at": "2026-09-01T01:00:00Z"
      }
    ],
    "closes_ticket": "12345",
    "superseded_by": "dockhand/jq-1.9.1",
    "closed": "2026-09-01T03:00:00Z",
    "base": {
      "sha": "0ba1c0ffee",
      "committed_at": "2026-08-31T09:00:00Z"
    }
  },
  "runs": {
    "jq@Ancientos": {
      "state": "unsupported",
      "content": "sha256:9f2c"
    },
    "jq@Testos": {
      "ask": {
        "test": true,
        "keep_env": true,
        "from_source": true,
        "forced": "oniguruma"
      },
      "state": "passed",
      "content": "sha256:9f2c",
      "detail": "built and installed",
      "evidence": "built in a pristine VM",
      "lint": "2 warnings",
      "manifest": {
        "port": "jq",
        "version": "1.9",
        "platform": "Testos",
        "files": [
          "bin/jq"
        ],
        "dylibs": [
          {
            "path": "lib/libjq.2.dylib",
            "arch": "arm64",
            "install_name": "/opt/local/lib/libjq.2.dylib",
            "compat_version": "2.0.0",
            "current_version": "2.0.1"
          }
        ]
      },
      "baseline": {
        "port": "jq",
        "version": "1.8",
        "platform": "Testos",
        "files": [
          "bin/jq"
        ],
        "dylibs": null
      },
      "baseline_source": "binary archive",
      "links": [
        "bin/jq -\u003e libjq.2.dylib"
      ],
      "probes": [
        {
          "binary": "bin/jq",
          "argv": "jq --version",
          "output": "jq-1.9"
        }
      ],
      "at": "2026-09-01T00:40:00Z"
    },
    "oniguruma@Testos": {
      "state": "withheld",
      "content": "sha256:9f2c",
      "detail": "conflicts with jq in one guest",
      "blamed": "jq"
    }
  },
  "leases": {
    "Testos": {
      "schema": 1,
      "id": {
        "provider": "tart",
        "id": "vm-7",
        "started": "2026-09-01T00:00:00Z"
      },
      "request": "req-01HZ",
      "handle": "dockhand-jq-1.9",
      "change": "chg-01HZ",
      "owner": {
        "root": "/Users/x/ports",
        "host": "studio.local",
        "pid": 4821,
        "since": "2026-09-01T00:00:00Z"
      },
      "platform": "Testos",
      "phase": "finished",
      "test": true,
      "tree_as_of": "2026-08-30T12:00:00Z",
      "claim": {
        "by": "cycle-1",
        "at": "2026-09-01T00:05:00Z",
        "expires": "2026-09-01T01:05:00Z",
        "pass": "pass-9"
      },
      "release": {
        "requested": "2026-09-01T00:05:00Z",
        "by": "cycle-1",
        "done": "2026-09-01T05:00:00Z",
        "attempts": 2,
        "last_error": "provider refused once",
        "not_before": "2026-09-01T01:05:00Z"
      },
      "retain": "2026-09-01T04:00:00Z"
    }
  },
  "publication": {
    "number": 9876,
    "url": "https://github.com/macports/macports-ports/pull/9876",
    "published_by": "machine",
    "published_at": "2026-09-01T03:00:00Z",
    "unproven": 1
  }
}`
