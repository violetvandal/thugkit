package build

import (
	"io"
	"os"
	"path/filepath"
)

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// copyFile copies src to dst, preserving the source mode and mtime, creating
// parent dirs as needed.
func copyFile(src, dst string) error {
	sfi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, sfi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Chmod(dst, sfi.Mode().Perm())
	return os.Chtimes(dst, sfi.ModTime(), sfi.ModTime())
}

// sameFile reports whether dst already matches src by size and mtime (the cheap
// rsync-style heuristic — enough to make repeat mirrors fast and correct, since
// our sources change wholesale, not in place at the same size+time).
func sameFile(src os.FileInfo, dst string) bool {
	dfi, err := os.Stat(dst)
	if err != nil {
		return false
	}
	return !dfi.IsDir() && dfi.Size() == src.Size() && dfi.ModTime().Equal(src.ModTime())
}

// mirrorDir makes dst an exact mirror of src: copies missing/changed files and
// removes anything in dst that is not in src. Pure Go (rsync --delete analogue).
func mirrorDir(src, dst string, logf func(string, ...any)) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	// 1. copy missing/changed src -> dst; record the set of src-relative paths.
	want := map[string]bool{}
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		want[rel] = true
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if sameFile(fi, target) {
			return nil
		}
		return copyFile(p, target)
	})
	if err != nil {
		return err
	}
	// 2. prune dst entries not present in src (deepest-first so dirs empty out).
	var extra []string
	_ = filepath.Walk(dst, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dst, p)
		if err != nil || rel == "." {
			return nil
		}
		if !want[rel] {
			extra = append(extra, p)
		}
		return nil
	})
	for i := len(extra) - 1; i >= 0; i-- {
		if err := os.RemoveAll(extra[i]); err != nil {
			return err
		}
	}
	return nil
}

// copyRootFiles copies the game-root loose files (dlls, launcher, icon, urls)
// from the pristine base into the edition dir. Mirrors rebuild-playable.sh.
func copyRootFiles(pristine, dest string, logf func(string, ...any)) error {
	names := []string{"binkw32.dll", "gdiplus.dll", "THUG2.ico",
		"Launcher.exe", "Launcher_fr.exe", "Launcher_gr.exe"}
	for _, n := range names {
		src := filepath.Join(pristine, n)
		if fileExists(src) {
			if err := copyFile(src, filepath.Join(dest, n)); err != nil {
				return err
			}
		}
	}
	// *.url
	ents, _ := os.ReadDir(pristine)
	for _, e := range ents {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".url" {
			if err := copyFile(filepath.Join(pristine, e.Name()), filepath.Join(dest, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// overlayHQAudio overlays a pre-extracted HQ pack (hqDir/Game/Data/...) onto the
// edition's Data/, without deleting anything (an overlay, not a mirror). The bash
// orchestrator extracts the 7z first, excluding pcm.* (which preserves PC dialog).
// Idempotent: skips if the HQ music marker is already in place (> 8 MB).
func overlayHQAudio(hqDir, dest string, logf func(string, ...any)) error {
	srcData := filepath.Join(hqDir, "Game", "Data")
	if !dirExists(srcData) {
		// allow the pack to be rooted at Data/ directly
		if dirExists(filepath.Join(hqDir, "Data")) {
			srcData = filepath.Join(hqDir, "Data")
		} else {
			return nil
		}
	}
	marker := filepath.Join(dest, "Data", "streams", "music", "8541624c.bik")
	if fi, err := os.Stat(marker); err == nil && fi.Size() > 8*1024*1024 {
		logf("  HQ audio already applied (skip)")
		return nil
	}
	return filepath.Walk(srcData, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcData, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, "Data", rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}
