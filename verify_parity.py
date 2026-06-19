#!/usr/bin/env python3
# Verify the Go thugkit prx ops match the Python prx.py byte-for-byte.
# Run from repo root: python3 tools/thugkit/verify_parity.py
import os, sys, subprocess, tempfile
sys.path.insert(0, 'tools/prx')
import prx, lzss

TK = 'tools/thugkit/thugkit'
PRE = 'game-pristine-us/Data/pre'

def first_qb(arch):
    ver, es = prx.parse(open(arch, 'rb').read())
    for e in es:
        n = e['name'].split(b'\0', 1)[0].decode('latin1')
        if n.lower().endswith('.qb'):
            data = e['blob'][:e['dsize']] if e['csize'] == 0 else lzss.decompress(e['blob'][:e['csize']], e['dsize'])
            return n, data, (e['csize'] != 0)
    return None, None, None

ok = True
for f in ['BO_scripts', 'AU_scripts', 'NO_scripts', 'mainmenu_scripts', 'qb_scripts']:
    arch = f'{PRE}/{f}.prx'
    if not os.path.exists(arch):
        continue
    name, data, was_comp = first_qb(arch)
    if not name:
        continue
    with tempfile.TemporaryDirectory() as td:
        ent = f'{td}/entry.qb'; go = f'{td}/go.prx'; py = f'{td}/py.prx'; goz = f'{td}/goz.prx'
        open(ent, 'wb').write(data)
        # replace (uncompressed): go vs py must be byte-identical
        subprocess.run([TK, 'prx', 'replace', arch, name, ent, go], check=True, capture_output=True)
        subprocess.run(['python3', 'tools/prx/prx.py', 'replace', arch, name, ent, py], check=True, capture_output=True)
        same = open(go, 'rb').read() == open(py, 'rb').read()
        # replacez (compressed): go output must decode back to original via the python decoder
        subprocess.run([TK, 'prx', 'replacez', arch, name, ent, goz], check=True, capture_output=True)
        ver, es = prx.parse(open(goz, 'rb').read()); e = prx.find(es, name)
        dec = lzss.decompress(e['blob'][:e['csize']], e['dsize'])
        zok = (dec == data)
        comp_ratio = 100 * e['csize'] / max(1, e['dsize'])
        print(f'{f:18s} entry={name.split(chr(92))[-1]:28s} '
              f'replace==py:{"✅" if same else "❌"}  '
              f'replacez_decodes:{"✅" if zok else "❌"} ({comp_ratio:.0f}%)')
        ok = ok and same and zok

print('\nALL PARITY CHECKS PASSED ✅' if ok else '\nPARITY FAILURES ❌')
sys.exit(0 if ok else 1)
