// Package tag turns any image into a THUG2 custom Create-A-Graphic tag:
// it encodes the image into a CAGR sprite, injects it into cagpieces.prx, and
// builds a correctly-checksummed .GRF that loads directly in-game.
//
// Go port of thug2_tag_importer.py (the preview-render step is omitted).
package tag

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/violetvandal/thugkit/grf"
	"github.com/violetvandal/thugkit/imgxbx"
	"github.com/violetvandal/thugkit/prx"
)

// Options configure a tag build.
type Options struct {
	Image   string // source image (PNG/JPG/...)
	GameDir string // THUG2 install root
	Name    string // tag name (default: sanitized image filename)
	Slot    string // CAGR clip-art slot to use (default grap_50)
	Size    int    // sprite resolution: 64/128/256 (default 64)
	Scale   float32
	OutDir  string // output folder (default "out")
	Install bool   // also copy into the game install (.orig backups)
}

// Result reports what was produced.
type Result struct {
	GRF, PRX         string // paths in OutDir
	Name, Slot       string
	InstalledGRF     string // if Install
	InstalledPRX     string
	SaveDir, PreDir  string
}

var nameSanitize = regexp.MustCompile(`[^A-Za-z0-9 ]`)

func Run(o Options) (*Result, error) {
	if o.Slot == "" {
		o.Slot = "grap_50"
	}
	if o.Size == 0 {
		o.Size = 64
	}
	if o.Scale == 0 {
		o.Scale = 1.0
	}
	if o.OutDir == "" {
		o.OutDir = "out"
	}
	cagPath, err := findCagpieces(o.GameDir)
	if err != nil {
		return nil, err
	}
	if o.Name == "" {
		o.Name = defaultName(o.Image)
	}
	if err := os.MkdirAll(o.OutDir, 0755); err != nil {
		return nil, err
	}

	img, err := imgxbx.Encode(o.Image, o.Size)
	if err != nil {
		return nil, err
	}
	newPrx, err := patchCagpieces(cagPath, o.Slot, img)
	if err != nil {
		return nil, err
	}
	grfBytes, err := grf.Build(o.Name, []grf.Layer{{
		TextureName: o.Slot, PosX: 32, PosY: 32, Rot: 0, Scale: o.Scale,
		HSVA: [4]uint32{0, 0, 100, 128},
	}}, 1)
	if err != nil {
		return nil, err
	}

	outPrx := filepath.Join(o.OutDir, "cagpieces.prx")
	outGrf := filepath.Join(o.OutDir, o.Name+".GRF")
	if err := os.WriteFile(outPrx, newPrx, 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(outGrf, grfBytes, 0644); err != nil {
		return nil, err
	}

	res := &Result{GRF: outGrf, PRX: outPrx, Name: o.Name, Slot: o.Slot,
		PreDir: filepath.Dir(cagPath), SaveDir: findSaveDir(o.GameDir)}

	if o.Install {
		backup(cagPath)
		if err := copyFile(outPrx, cagPath); err != nil {
			return nil, fmt.Errorf("install cagpieces: %w", err)
		}
		res.InstalledPRX = cagPath
		dest := filepath.Join(res.SaveDir, o.Name+".GRF")
		if _, err := os.Stat(dest); err == nil {
			backup(dest)
		}
		if err := os.MkdirAll(res.SaveDir, 0755); err == nil {
			if err := copyFile(outGrf, dest); err == nil {
				res.InstalledGRF = dest
			}
		}
	}
	return res, nil
}

func patchCagpieces(cagPath, slot string, img []byte) ([]byte, error) {
	d, err := os.ReadFile(cagPath)
	if err != nil {
		return nil, err
	}
	ver, entries, err := prx.Parse(d)
	if err != nil {
		return nil, err
	}
	e := prx.FindBySuffix(entries, slot+".img.xbx")
	if e == nil {
		return nil, fmt.Errorf("slot %q (%s.img.xbx) not found in cagpieces.prx", slot, slot)
	}
	prx.ReplaceRaw(e, img)
	return prx.Build(ver, entries)
}

func findCagpieces(gamedir string) (string, error) {
	for _, sub := range []string{"Data/pre", "data/pre"} {
		p := filepath.Join(gamedir, sub, "cagpieces.prx")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("could not find Data/pre/cagpieces.prx under %s", gamedir)
}

func findSaveDir(gamedir string) string {
	for _, sub := range []string{"Data/Game/Save", "Save", "data/game/save"} {
		p := filepath.Join(gamedir, sub)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return filepath.Join(gamedir, "Save")
}

func defaultName(image string) string {
	base := strings.TrimSuffix(filepath.Base(image), filepath.Ext(image))
	base = strings.TrimSpace(nameSanitize.ReplaceAllString(base, ""))
	if len(base) > 15 {
		base = base[:15]
	}
	if base == "" {
		return "CustomTag"
	}
	return base
}

// backup copies path -> path.orig once (preserving the true original).
func backup(path string) {
	b := path + ".orig"
	if _, err := os.Stat(b); err == nil {
		return // already backed up
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = copyFile(path, b)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
