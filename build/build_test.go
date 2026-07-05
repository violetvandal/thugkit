package build

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quiet(string, ...any) {}

func TestMirrorDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "alpha")
	write(t, filepath.Join(src, "sub", "b.txt"), "bravo")

	if err := mirrorDir(src, dst, quiet); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "bravo" {
		t.Fatalf("b.txt not mirrored: %q", b)
	}

	// add an extraneous file in dst + change a source file, then re-mirror.
	write(t, filepath.Join(dst, "stale.txt"), "remove me")
	write(t, filepath.Join(src, "a.txt"), "ALPHA2")
	if err := mirrorDir(src, dst, quiet); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dst, "stale.txt")) {
		t.Fatal("stale.txt should have been pruned")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(b) != "ALPHA2" {
		t.Fatalf("a.txt not updated: %q", b)
	}
}

func TestInstallWSFix(t *testing.T) {
	dest := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "wsfix.zip")
	makeWSFixZip(t, zipPath)

	if err := installWSFix(zipPath, dest, quiet); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dest, "winmm.dll")) {
		t.Fatal("dinput8.dll must be installed AS winmm.dll")
	}
	if fileExists(filepath.Join(dest, "dinput8.dll")) {
		t.Fatal("dinput8.dll must NOT be present (would break the controller)")
	}
	if !fileExists(filepath.Join(dest, "scripts", "TonyHawksUnderground2.WidescreenFix.asi")) {
		t.Fatal(".asi not copied to scripts/")
	}
	if !fileExists(filepath.Join(dest, "scripts", "TonyHawksUnderground2.WidescreenFix.ini")) {
		t.Fatal(".ini not copied to scripts/")
	}

	// PARTYMOD refusal
	write(t, filepath.Join(dest, "THUG2PM.exe"), "x")
	if err := installWSFix(zipPath, dest, quiet); err == nil {
		t.Fatal("expected refusal on PARTYMOD install")
	}
}

func makeWSFixZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"Game/dinput8.dll": "LOADER",
		"Game/scripts/TonyHawksUnderground2.WidescreenFix.asi": "ASI",
		"Game/scripts/TonyHawksUnderground2.WidescreenFix.ini": "ResX=0",
		"Game/THUG2.url": "ignored",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallTagsGRF(t *testing.T) {
	dest := t.TempDir()
	tags := t.TempDir()
	write(t, filepath.Join(tags, "VioletVandal.GRF"), "grfbytes")
	write(t, filepath.Join(tags, "notes.txt"), "ignored non-tag")

	if err := installTags(tags, dest, quiet); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "Save", "VioletVandal.GRF")); string(b) != "grfbytes" {
		t.Fatalf(".GRF not copied to Save/: %q", b)
	}
}

func TestOverlayHQAudio(t *testing.T) {
	dest := t.TempDir()
	hq := t.TempDir()
	write(t, filepath.Join(hq, "Game", "Data", "streams", "music", "track.bik"), "HQ")
	write(t, filepath.Join(hq, "Game", "Data", "movies", "intro.bik"), "HQVID")

	if err := overlayHQAudio(hq, dest, quiet); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "Data", "streams", "music", "track.bik")); string(b) != "HQ" {
		t.Fatalf("music not overlaid: %q", b)
	}
	if !fileExists(filepath.Join(dest, "Data", "movies", "intro.bik")) {
		t.Fatal("movies not overlaid")
	}
}

func TestRunRequiresInputs(t *testing.T) {
	if err := Run(Options{Logf: quiet}); err == nil {
		t.Fatal("expected error with no PristineDir/Dest")
	}
}

// With no no-CD exe supplied, the build must keep pristine's THUG2.exe so the edition
// is actually launchable (regression: a fresh clone with no game-modded-vanilla source
// produced an edition with NO game exe -> Wine "ShellExecuteEx failed: File not found").
func TestRunKeepsPristineExeWhenNoNoCD(t *testing.T) {
	pristine := t.TempDir()
	dest := t.TempDir()
	mods := t.TempDir()
	write(t, filepath.Join(pristine, "Data", "pre", "x.prx"), "prx")
	write(t, filepath.Join(pristine, "THUG2.exe"), "MZ-pristine-exe")
	write(t, filepath.Join(mods, "mods.list"), "") // no mods to apply

	if err := Run(Options{PristineDir: pristine, Dest: dest, ModsDir: mods, Logf: quiet}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "THUG2.exe"))
	if err != nil {
		t.Fatalf("edition has no THUG2.exe: %v", err)
	}
	if string(got) != "MZ-pristine-exe" {
		t.Fatalf("THUG2.exe is not the pristine exe: %q", got)
	}
}
