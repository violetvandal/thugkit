#!/usr/bin/env bash
# Definitive functional-equivalence check for the 3 new-symbol files:
#   (a) decompiled source EXCLUDING the __register_checksums__ line must be identical
#   (b) the __register_checksums__ token SET (sorted) must be identical
# If both hold, Go output ≡ bash output (only checksum-registration ORDER differs).
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT="$PWD"; TK="$ROOT/tools/thugkit/thugkit"; NS="$ROOT/mods/lib/ns"; SRC="$ROOT/game-pristine-us/Data/pre"
A="$(mktemp -d)"; B="$(mktemp -d)"
trap 'rm -rf "$A" "$B"' EXIT
for d in "$A" "$B"; do mkdir -p "$d/pre"; cp "$SRC"/*.prx "$d/pre/"; done
bash mods/apply-mods.sh "$A" --layer source >/dev/null 2>&1
"$TK" apply           "$B" --mods mods --layer source >/dev/null 2>&1

decompile() { # <dir> <archive> <entry> -> stdout decompiled .ns
  python3 - "$1/pre/$2" "$3" <<'PY' > /tmp/_x.qb
import sys; sys.path.insert(0,'tools/prx'); import prx,lzss
_,es=prx.parse(open(sys.argv[1],'rb').read()); e=prx.find(es,sys.argv[2])
open('/tmp/_x.qb','wb').write(e['blob'][:e['dsize']] if e['csize']==0 else lzss.decompress(e['blob'][:e['csize']],e['dsize']))
PY
  "$NS" -d /tmp/_x.qb -o /dev/stdout 2>/dev/null
}
logic() { grep -v '__register_checksums__'; }                 # the actual script
cksums() { grep '__register_checksums__' | tr ' ' '\n' | sort | grep -v '^$'; }  # the name set

check() { # <archive> <entry>
  local short; short="$(basename "${2//\\//}")"
  decompile "$A" "$1" "$2" > /tmp/a.ns; decompile "$B" "$1" "$2" > /tmp/b.ns
  local lg=ok cs=ok
  diff <(logic </tmp/a.ns) <(logic </tmp/b.ns) >/dev/null 2>&1 || lg=DIFF
  diff <(cksums </tmp/a.ns) <(cksums </tmp/b.ns) >/dev/null 2>&1 || cs=DIFF
  if [ "$lg" = ok ] && [ "$cs" = ok ]; then
    echo "  $short : logic identical ✅   checksum-set identical ✅   (only registration order differs)"
  else
    echo "  $short : ❌ logic=$lg checksum-set=$cs"; ALLOK=0
    [ "$cs" = DIFF ] && { echo "    checksum-set delta:"; diff <(cksums </tmp/a.ns) <(cksums </tmp/b.ns) | grep '^[<>]' | head; }
  fi
}
ALLOK=1
check qb_scripts.prx 'scripts\game\misc\global_flags.qb'
check qb_scripts.prx 'scripts\game\menu\gamemenu_pause.qb'
check BE_scripts.prx 'levels\BE\BE_sfx.qb'
check NO_scripts.prx 'levels\NO\NO_sfx.qb'
echo
[ "$ALLOK" = 1 ] && echo "RESULT: Go ≡ bash on all new-symbol files (functionally identical) ✅" || echo "RESULT: real difference ❌"
