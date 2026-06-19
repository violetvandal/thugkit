#!/usr/bin/env bash
# End-to-end equivalence: apply-mods.sh (bash+python+ns) vs thugkit apply (Go).
# Run from repo root: bash tools/thugkit/verify_apply.sh
set -euo pipefail
cd "$(dirname "$0")/../.."   # repo root
ROOT="$PWD"
TK="$ROOT/tools/thugkit/thugkit"
SRC="$ROOT/game-pristine-us/Data/pre"
A="$(mktemp -d)"; B="$(mktemp -d)"
trap 'rm -rf "$A" "$B"' EXIT
mkdir -p "$A/pre" "$B/pre"
cp "$SRC"/*.prx "$A/pre/"; cp "$SRC"/*.prx "$B/pre/"

echo "### applying via apply-mods.sh (bash) -> A"
( cd "$ROOT" && bash mods/apply-mods.sh "$A" --layer source ) >/tmp/applyA.log 2>&1 || { echo "BASH APPLY FAILED"; tail -20 /tmp/applyA.log; exit 1; }
echo "### applying via thugkit (go) -> B"
( cd "$ROOT" && "$TK" apply "$B" --mods mods --layer source ) >/tmp/applyB.log 2>&1 || { echo "GO APPLY FAILED"; tail -20 /tmp/applyB.log; exit 1; }

echo
echo "### comparing every archive (byte-identical, else compare decompressed entry content)"
fail=0
for f in "$A"/pre/*.prx; do
  name="$(basename "$f")"; b="$B/pre/$name"
  if cmp -s "$f" "$b"; then
    echo "  $name : byte-identical ✅"
    continue
  fi
  # not byte-identical: compare decompressed content of every entry (functional equivalence)
  diff=$(python3 - "$f" "$b" <<'PY'
import sys; sys.path.insert(0,'tools/prx'); import prx,lzss
def entries(p):
    _,es=prx.parse(open(p,'rb').read()); out={}
    for e in es:
        n=e['name'].split(b'\0',1)[0]
        out[n]= e['blob'][:e['dsize']] if e['csize']==0 else lzss.decompress(e['blob'][:e['csize']],e['dsize'])
    return out
a=entries(sys.argv[1]); b=entries(sys.argv[2])
mism=[n for n in a if a.get(n)!=b.get(n)]
print('|'.join(n.decode('latin1') for n in mism))
PY
)
  if [ -z "$diff" ]; then
    echo "  $name : different bytes, but ALL entries decompress to identical content ✅ (LZSS encoding differs only)"
  else
    echo "  $name : ❌ CONTENT MISMATCH in: $diff"
    fail=1
  fi
done
echo
[ "$fail" = 0 ] && echo "RESULT: thugkit apply == apply-mods.sh (functionally identical) ✅" || { echo "RESULT: MISMATCH ❌"; exit 1; }
