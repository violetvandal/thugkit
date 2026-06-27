package build

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/violetvandal/thugkit/prx"
)

// qbScriptsCeiling is the qb_scripts.prx load ceiling (~1.43 MB); exceeding it
// black-screens boot. We always inject compressed and verify against it.
const qbScriptsCeiling = 1499136

// setDefaultSoundtrack swaps the skater_sfx.qb jukebox-titles entry inside
// qb_scripts.prx for a prebuilt variant (e.g. skater_sfx_original.qb). Optional:
// the base build normally leaves the Original soundtrack and the launch lane swaps
// it; this exists for baking a default if desired.
func setDefaultSoundtrack(dest, qbVariant string, logf func(string, ...any)) error {
	prxPath := filepath.Join(dest, "Data", "pre", "qb_scripts.prx")
	raw, err := os.ReadFile(prxPath)
	if err != nil {
		return err
	}
	ver, ents, err := prx.Parse(raw)
	if err != nil {
		return err
	}
	e := prx.Find(ents, `scripts\game\skater\skater_sfx.qb`)
	if e == nil {
		return fmt.Errorf("skater_sfx.qb not found in qb_scripts.prx")
	}
	qb, err := os.ReadFile(qbVariant)
	if err != nil {
		return err
	}
	if err := prx.ReplaceCompressed(e, qb); err != nil {
		return err
	}
	out, err := prx.Build(ver, ents)
	if err != nil {
		return err
	}
	if len(out) > qbScriptsCeiling {
		return fmt.Errorf("qb_scripts.prx %d bytes exceeds boot ceiling %d", len(out), qbScriptsCeiling)
	}
	if err := os.WriteFile(prxPath, out, 0o644); err != nil {
		return err
	}
	logf("  soundtrack titles -> %s (qb_scripts.prx %d bytes)", filepath.Base(qbVariant), len(out))
	return nil
}
