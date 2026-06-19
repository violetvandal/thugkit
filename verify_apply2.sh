#!/usr/bin/env bash
# Prove the 3 non-identical files are functional-equivalent (compiler map-order
# nondeterminism), not a Go bug: decompile Go vs bash output and diff the source;
# also show bash-vs-bash is itself nondeterministic on the same files.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT="$PWD"; TK="$ROOT/tools/thugkit/thugkit"; NS="$ROOT/mods/lib/ns"; SRC="$ROOT/game-pristine-us/Data/pre"
A="$(mktemp -d)"; B="$(mktemp -d)"; A2="$(mktemp -d)"
trap 'rm -rf "$A" "$B" "$A2"' EXIT
for d in "$A" "$B" "$A2"; do mkdir -p "$d/pre"; cp "$SRC"/*.prx "$d/pre/"; done

bash mods/apply-mods.sh "$A"  --layer source >/dev/null 2>&1
"$TK" apply           "$B"  --mods mods --layer source >/dev/null 2>&1
bash mods/apply-mods.sh "$A2" --layer source >/dev/null 2>&1

extract_decompile() { # <dir> <archive> <entry> <out.ns>
  python3 - "$1/pre/$2" "$3" "/tmp/_e.qb" <<'PY'
import sys; sys.path.insert(0,'tools/prx'); import prx,lzss
_,es=prx.parse(open(sys.argv[1],'rb').read()); e=prx.find(es,sys.argv[2])
d=e['blob'][:e['dsize']] if e['csize']==0 else lzss.decompress(e['blob'][:e['csize']],e['dsize'])
open(sys.argv[3],'wb').write(d)
PY
  "$NS" -d /tmp/_e.qb -o "$4" >/dev/null 2>&1
}

declare -A FILES=(
  [BE_scripts.prx]='levels\BE\BE_sfx.qb'
  [NO_scripts.prx]='levels\NO\NO_sfx.qb'
  [qb_scripts.prx]='scripts\game\menu\gamemenu_pause.qb scripts\game\misc\global_flags.qb'
)
allok=1
for arch in "${!FILES[@]}"; do
  for entry in ${FILES[$arch]}; do
    extract_decompile "$A" "$arch" "$entry" /tmp/a.ns
    extract_decompile "$B" "$arch" "$entry" /tmp/b.ns
    short="$(basename "${entry//\\//}")"
    if diff -q /tmp/a.ns /tmp/b.ns >/dev/null 2>&1; then
      echo "  $short : Go vs bash DECOMPILE-IDENTICAL ✅ (functionally equal)"
    else
      echo "  $short : ❌ decompiled source DIFFERS — investigate"; allok=0
      diff /tmp/a.ns /tmp/b.ns | head -8
    fi
    # bash determinism: A vs A2 raw bytes
    if cmp -s <(python3 -c "import sys;sys.path.insert(0,'tools/prx');import prx;_,e=prx.parse(open('$A/pre/$arch','rb').read());x=prx.find(e,r'''$entry''');sys.stdout.buffer.write(x['blob'])") \
              <(python3 -c "import sys;sys.path.insert(0,'tools/prx');import prx;_,e=prx.parse(open('$A2/pre/$arch','rb').read());x=prx.find(e,r'''$entry''');sys.stdout.buffer.write(x['blob'])"); then
      :
    else
      echo "      (note: bash-vs-bash ALSO differs on $short → confirms inherent map-order nondeterminism)"
    fi
  done
done
echo
[ "$allok" = 1 ] && echo "RESULT: all 3 differing files are functionally identical (Go ≡ bash) ✅" || echo "RESULT: real difference found ❌"
