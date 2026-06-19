# thugkit

A single static Go binary that applies the **THUG2: Violet Vandal Edition** mod
layer onto a *Tony Hawk's Underground 2* (PC) install. It is the engine behind
the **Revert** toolkit — it compiles NeverScript in-process and packs `.prx`
archives with **no runtime dependencies** (no Python, no external compiler), so
it cross-compiles cleanly to Windows, Linux, and Steam Deck.

> Status: early. Private while the wider project takes shape; will open up when
> ready.

## What it does

- **`prx`** — read / list / extract / replace files inside THUG2 `.prx` (PRE)
  archives, including LZSS (de)compression. Round-trips byte-identically.
- **`apply`** — read a mod set (`mods.list` + per-mod `mod.conf` / `inject.list`)
  and apply it to an install: compile `.ns` → `.qb` in-process and inject into
  the right archive (raw, or LZSS-compressed for the size-capped `qb_scripts`).

```
thugkit prx <roundtrip|list|extract|replace|replacez> ...
thugkit apply <install-dir> [--mods <dir>] [--layer all|binary|source] [--only a,b]
```

## Build

```
go build -o thugkit ./cmd/thugkit
# cross-compile, zero deps:
GOOS=windows GOARCH=amd64 go build -o thugkit.exe ./cmd/thugkit
```

## Tests

```
go test ./...                          # hermetic unit tests (no game data needed)
go test ./prx -run x -fuzz FuzzLZSS    # fuzz the LZSS codec
```

The `verify_*.sh` / `verify_parity.py` scripts are integration harnesses that
compare against the reference Python/bash pipeline; they expect the surrounding
project layout (clean game data + `mods/`) and are not needed to build or unit-test.

## Dependency note

This module currently consumes the patched NeverScript compiler via a local
`replace github.com/byxor/NeverScript => ../neverscript`. To build standalone it
needs that fork checked out alongside; this will be converted to a submodule (or
a pinned module dependency) before the repo goes public.

## License

MIT — see [LICENSE](LICENSE).
