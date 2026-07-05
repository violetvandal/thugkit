// Package build is the in-process, zero-dependency core that produces a THUG2
// edition from a clean pristine base + the mod sources. It is the Go port of the
// portable half of rebuild-playable.sh: base copy, no-CD exe, WidescreenFix,
// mod apply (via the apply package), custom tags (via the tag package), the HUD
// fix .asi, and the optional HQ audio/video overlay.
//
// It deliberately does NOT shell out (so the binary stays static, zero-dep, and
// cross-platform): all archive extraction (ISO/MSI/7z) and the optional Python
// CAS asset steps (panty/stickers/decks/playas) are the bash orchestrator's job,
// which hands this core already-extracted directories.
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/violetvandal/thugkit/apply"
	"github.com/violetvandal/thugkit/tag"
)

// Options configure a build. Empty optional fields skip their step.
type Options struct {
	PristineDir string // clean master: <PristineDir>/Data + root exes/dlls (required)
	Dest        string // output edition dir (required)
	ModsDir     string // mods/ root for apply.Run (required unless ApplyMods=false)

	NoCDExe    string // user-supplied no-CD THUG2.exe ("" = keep pristine exe)
	WSFixZip   string // ThirteenAG WidescreenFix .zip ("" = skip)
	HQAudioDir string // PRE-EXTRACTED HQ pack root containing Game/Data/... ("" = skip)

	Only        []string // restrict apply to these mod names (iteration)
	HudFixASI   string   // prebuilt VV.HudFix.asi to copy into scripts/ ("" = skip)
	GlyphFixASI string   // prebuilt VV.GlyphFix.asi to copy into scripts/ ("" = skip)
	TagsDir     string   // dir of .GRF (-> Save/) + images (-> tag.Run grap_50+) ("" = skip)

	// SoundtrackQB, if set, bakes a skater_sfx.qb variant as the default soundtrack.
	// Default "" leaves the soundtrack untouched (the launch lane swaps it), matching
	// rebuild-playable.sh, which keeps the Original soundtrack in the build.
	SoundtrackQB string

	Fast bool // reset only Data/pre + re-apply mods (skip base copy / exe / WSFix / HQ)

	Logf func(string, ...any)
}

// Run executes the build steps in order. See package doc.
func Run(o Options) error {
	if o.Logf == nil {
		o.Logf = func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
	}
	if o.PristineDir == "" || o.Dest == "" {
		return fmt.Errorf("build: PristineDir and Dest are required")
	}
	if !dirExists(filepath.Join(o.PristineDir, "Data", "pre")) {
		return fmt.Errorf("build: pristine base missing %s/Data/pre", o.PristineDir)
	}
	if err := os.MkdirAll(filepath.Join(o.Dest, "Save"), 0o755); err != nil {
		return err
	}

	if o.Fast {
		o.Logf("[build] fast: reset Data/pre <- pristine")
		if err := mirrorDir(filepath.Join(o.PristineDir, "Data", "pre"),
			filepath.Join(o.Dest, "Data", "pre"), o.Logf); err != nil {
			return fmt.Errorf("reset Data/pre: %w", err)
		}
	} else {
		o.Logf("[build] base data <- pristine")
		if err := mirrorDir(filepath.Join(o.PristineDir, "Data"),
			filepath.Join(o.Dest, "Data"), o.Logf); err != nil {
			return fmt.Errorf("base data: %w", err)
		}
		if err := copyRootFiles(o.PristineDir, o.Dest, o.Logf); err != nil {
			return fmt.Errorf("root files: %w", err)
		}
		if o.NoCDExe != "" {
			o.Logf("[build] no-CD executable")
			if err := copyFile(o.NoCDExe, filepath.Join(o.Dest, "THUG2.exe")); err != nil {
				return fmt.Errorf("no-CD exe: %w", err)
			}
		} else if src := filepath.Join(o.PristineDir, "THUG2.exe"); fileExists(src) {
			// No no-CD exe supplied — keep pristine's THUG2.exe so the edition is
			// launchable. copyRootFiles deliberately skips THUG2.exe (expecting the
			// no-CD step to place it), so without this a fresh clone with no no-CD
			// source would build an edition with NO game exe at all.
			o.Logf("[build] keeping pristine THUG2.exe (no no-CD exe supplied)")
			if err := copyFile(src, filepath.Join(o.Dest, "THUG2.exe")); err != nil {
				return fmt.Errorf("copy pristine exe: %w", err)
			}
		}
		if o.WSFixZip != "" {
			o.Logf("[build] widescreen (WSFix winmm loader + scripts)")
			if err := installWSFix(o.WSFixZip, o.Dest, o.Logf); err != nil {
				return fmt.Errorf("widescreen: %w", err)
			}
		}
	}

	o.Logf("[build] data mods (via apply)")
	if err := apply.Run(apply.Options{
		Install: o.Dest,
		ModsDir: o.ModsDir,
		Layer:   "all",
		Only:    o.Only,
		Logf:    o.Logf,
	}); err != nil {
		return fmt.Errorf("apply mods: %w", err)
	}

	if o.TagsDir != "" {
		o.Logf("[build] custom Create-A-Graphic tags")
		if err := installTags(o.TagsDir, o.Dest, o.Logf); err != nil {
			return fmt.Errorf("tags: %w", err)
		}
	}

	if o.HudFixASI != "" {
		if err := installHudFix(o.HudFixASI, o.Dest, o.Logf); err != nil {
			return fmt.Errorf("hudfix: %w", err)
		}
	}

	if o.GlyphFixASI != "" {
		if err := installASI(o.GlyphFixASI, o.Dest, "VV.GlyphFix.asi", o.Logf); err != nil {
			return fmt.Errorf("glyphfix: %w", err)
		}
	}

	if !o.Fast && o.HQAudioDir != "" {
		o.Logf("[build] HQ audio/video overlay")
		if err := overlayHQAudio(o.HQAudioDir, o.Dest, o.Logf); err != nil {
			return fmt.Errorf("hq audio: %w", err)
		}
	}

	if o.SoundtrackQB != "" {
		o.Logf("[build] default soundtrack")
		if err := setDefaultSoundtrack(o.Dest, o.SoundtrackQB, o.Logf); err != nil {
			return fmt.Errorf("soundtrack: %w", err)
		}
	}

	o.Logf("[build] done -> %s", o.Dest)
	return nil
}

// installTags copies every .GRF in tagsDir verbatim into Dest/Save/, and builds
// every image into a custom tag (sprite into cagpieces grap_50+, .GRF into Save/).
// This is the Go replacement for tools/save/apply_tags.py on the build path.
func installTags(tagsDir, dest string, logf func(string, ...any)) error {
	ents, err := os.ReadDir(tagsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	saveDir := filepath.Join(dest, "Save")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return err
	}
	slot := 50
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		src := filepath.Join(tagsDir, name)
		switch strings.ToLower(filepath.Ext(name)) {
		case ".grf":
			if err := copyFile(src, filepath.Join(saveDir, name)); err != nil {
				return err
			}
			logf("  tag .GRF -> Save/%s", name)
		case ".png", ".jpg", ".jpeg", ".bmp", ".gif", ".tga", ".webp":
			res, err := tag.Run(tag.Options{
				Image: src, GameDir: dest,
				Slot: fmt.Sprintf("grap_%d", slot), Size: 64, Scale: 1.0,
				OutDir: filepath.Join(os.TempDir(), "thugkit-tagout"), Install: true,
			})
			if err != nil {
				return fmt.Errorf("tag %s: %w", name, err)
			}
			logf("  custom tag %q -> %s", res.Name, res.Slot)
			slot++
		}
	}
	return nil
}

func installHudFix(asi, dest string, logf func(string, ...any)) error {
	return installASI(asi, dest, "VV.HudFix.asi", logf)
}

// installASI copies a prebuilt .asi into the install's scripts/ dir (where the Ultimate ASI
// Loader, our winmm.dll proxy, picks it up alongside the WidescreenFix). No-op if there's no
// scripts/ dir (e.g. a vanilla build without widescreen has nothing to host it).
func installASI(asi, dest, name string, logf func(string, ...any)) error {
	scripts := filepath.Join(dest, "scripts")
	if !dirExists(scripts) {
		return nil
	}
	if err := copyFile(asi, filepath.Join(scripts, name)); err != nil {
		return err
	}
	logf("  installed %s -> scripts/", name)
	return nil
}
