package build

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// installWSFix applies ThirteenAG's WidescreenFix from its release zip, porting
// mods/apply-widescreen.sh. The load-bearing detail: the pack's Game/dinput8.dll
// is installed as <dest>/winmm.dll (NOT dinput8.dll) so Wine's native DirectInput
// stays in place for the controller; the .asi/.ini go in <dest>/scripts/.
// Refuses on a PARTYMOD install (THUG2PM.exe), which provides its own widescreen.
func installWSFix(zipPath, dest string, logf func(string, ...any)) error {
	if fileExists(filepath.Join(dest, "THUG2PM.exe")) {
		return fmt.Errorf("PARTYMOD install detected (THUG2PM.exe) — it already provides widescreen; do not add WSFix")
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	scripts := filepath.Join(dest, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		return err
	}
	wroteLoader := false
	for _, f := range zr.File {
		// normalize the in-zip path
		name := strings.ReplaceAll(f.Name, "\\", "/")
		base := path.Base(name)
		lower := strings.ToLower(name)
		switch {
		case strings.HasSuffix(lower, "game/dinput8.dll"):
			if err := extractZipFile(f, filepath.Join(dest, "winmm.dll")); err != nil {
				return err
			}
			wroteLoader = true
		case strings.Contains(lower, "game/scripts/") &&
			(strings.HasSuffix(lower, ".asi") || strings.HasSuffix(lower, ".ini")):
			if err := extractZipFile(f, filepath.Join(scripts, base)); err != nil {
				return err
			}
		}
	}
	if !wroteLoader {
		return fmt.Errorf("unexpected WSFix layout (no Game/dinput8.dll in %s)", zipPath)
	}
	logf("  installed ASI loader as winmm.dll + WSFix .asi/.ini -> scripts/")
	return nil
}

func extractZipFile(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
