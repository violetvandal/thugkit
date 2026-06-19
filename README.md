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

## Dependency

The patched NeverScript compiler is vendored as a **git submodule** at
`third_party/neverscript` (the public fork `github.com/violetvandal/NeverScript`,
pinned to the `thug2-runtime-safe-recompiler` branch); `go.mod` replaces
`github.com/byxor/NeverScript` with it. The repo is self-contained — just clone
with submodules:

```
git clone --recursive git@github.com:violetvandal/thugkit.git
# or, after a plain clone:
git submodule update --init --recursive
```

## License

MIT — see [LICENSE](LICENSE).
