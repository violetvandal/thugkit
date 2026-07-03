package apply

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/violetvandal/thugkit/prx"
)

func align4(x int) int { return (x + 3) &^ 3 }

// writePrx writes a minimal one-entry archive to disk.
func writePrx(t *testing.T, path, entryName string, data []byte) {
	t.Helper()
	nb := []byte(entryName)
	pad := align4(len(data)) - len(data)
	e := &prx.Entry{
		Dsize: uint32(len(data)),
		Nlen:  uint32(align4(len(nb) + 1)),
		Name:  nb,
		Blob:  append(append([]byte(nil), data...), make([]byte, pad)...),
	}
	blob, err := prx.Build(prx.Version, []*prx.Entry{e})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0644); err != nil {
		t.Fatal(err)
	}
}

func entryData(t *testing.T, path, name string) []byte {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, es, err := prx.Parse(d)
	if err != nil {
		t.Fatal(err)
	}
	e := prx.Find(es, name)
	if e == nil {
		t.Fatalf("entry %q not found in %s", name, path)
	}
	return e.RawData()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadConf(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mod.conf")
	writeFile(t, p, "# comment\ntype=prx-inject\nlayer=source\ntype=ignored-dup\n")
	conf, err := readConf(p)
	if err != nil {
		t.Fatal(err)
	}
	if conf["type"] != "prx-inject" { // head -1 semantics: first wins
		t.Errorf("type = %q", conf["type"])
	}
	if conf["layer"] != "source" {
		t.Errorf("layer = %q", conf["layer"])
	}
}

func TestForInjectList(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "inject.list"),
		"# header\n\nfoo.prx  some\\entry.qb  blob.bin\n")
	var got [][3]string
	err := forInjectList(dir, func(a, b, c string) error {
		got = append(got, [3]string{a, b, c})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != [3]string{"foo.prx", `some\entry.qb`, "blob.bin"} {
		t.Fatalf("parsed = %v", got)
	}
}

// TestApplyPrxInject exercises the full Run() path on a synthetic install+mod.
func TestApplyPrxInject(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "install")
	pre := filepath.Join(install, "Data", "pre")
	if err := os.MkdirAll(pre, 0755); err != nil {
		t.Fatal(err)
	}
	writePrx(t, filepath.Join(pre, "foo.prx"), `lvl\test.qb`, []byte("ORIGINAL-DATA"))

	mods := filepath.Join(root, "mods")
	writeFile(t, filepath.Join(mods, "mods.list"), "mymod\nabsent-mod\n")
	writeFile(t, filepath.Join(mods, "src", "mymod", "mod.conf"), "type=prx-inject\nlayer=source\n")
	writeFile(t, filepath.Join(mods, "src", "mymod", "inject.list"), `foo.prx  lvl\test.qb  blob.bin`+"\n")
	writeFile(t, filepath.Join(mods, "src", "mymod", "blob.bin"), "REPLACED-CONTENT")

	err := Run(Options{Install: install, ModsDir: mods, Layer: "all", Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	got := entryData(t, filepath.Join(pre, "foo.prx"), `lvl\test.qb`)
	if !bytes.Equal(got, []byte("REPLACED-CONTENT")) {
		t.Fatalf("entry = %q, want REPLACED-CONTENT", got)
	}
}

// TestApplyLayerFilter: a binary-layer mod is skipped when --layer source.
func TestApplyLayerFilter(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "install")
	pre := filepath.Join(install, "Data", "pre")
	os.MkdirAll(pre, 0755)
	writePrx(t, filepath.Join(pre, "foo.prx"), "t.qb", []byte("KEEP"))

	mods := filepath.Join(root, "mods")
	writeFile(t, filepath.Join(mods, "mods.list"), "binmod\n")
	writeFile(t, filepath.Join(mods, "src", "binmod", "mod.conf"), "type=prx-inject\nlayer=binary\n")
	writeFile(t, filepath.Join(mods, "src", "binmod", "inject.list"), "foo.prx  t.qb  blob.bin\n")
	writeFile(t, filepath.Join(mods, "src", "binmod", "blob.bin"), "SHOULD-NOT-APPLY")

	if err := Run(Options{Install: install, ModsDir: mods, Layer: "source", Logf: func(string, ...any) {}}); err != nil {
		t.Fatal(err)
	}
	if got := entryData(t, filepath.Join(pre, "foo.prx"), "t.qb"); !bytes.Equal(got, []byte("KEEP")) {
		t.Fatalf("binary-layer mod applied despite --layer source: %q", got)
	}
}

// TestApplyOptional: an optional prx-inject mod whose bring-your-own payload is
// absent is skipped (not fatal); the same mod applies once the payload is present;
// and a NON-optional mod with a missing payload still errors.
func TestApplyOptional(t *testing.T) {
	setup := func(t *testing.T, optional, withBlob bool) (install, mods string) {
		root := t.TempDir()
		install = filepath.Join(root, "install")
		pre := filepath.Join(install, "Data", "pre")
		if err := os.MkdirAll(pre, 0755); err != nil {
			t.Fatal(err)
		}
		writePrx(t, filepath.Join(pre, "foo.prx"), "t.qb", []byte("ORIGINAL"))
		mods = filepath.Join(root, "mods")
		writeFile(t, filepath.Join(mods, "mods.list"), "hqmod\n")
		conf := "type=prx-inject\nlayer=binary\n"
		if optional {
			conf += "optional=true\n"
		}
		writeFile(t, filepath.Join(mods, "src", "hqmod", "mod.conf"), conf)
		writeFile(t, filepath.Join(mods, "src", "hqmod", "inject.list"), "foo.prx  t.qb  blob.bin\n")
		if withBlob {
			writeFile(t, filepath.Join(mods, "src", "hqmod", "blob.bin"), "HQ-PAYLOAD")
		}
		return install, mods
	}

	t.Run("optional+missing -> skipped, no error", func(t *testing.T) {
		install, mods := setup(t, true, false)
		if err := Run(Options{Install: install, ModsDir: mods, Layer: "all", Logf: func(string, ...any) {}}); err != nil {
			t.Fatalf("optional mod with missing payload should not error, got: %v", err)
		}
		if got := entryData(t, filepath.Join(install, "Data", "pre", "foo.prx"), "t.qb"); !bytes.Equal(got, []byte("ORIGINAL")) {
			t.Fatalf("skipped mod still modified the archive: %q", got)
		}
	})

	t.Run("optional+present -> applied", func(t *testing.T) {
		install, mods := setup(t, true, true)
		if err := Run(Options{Install: install, ModsDir: mods, Layer: "all", Logf: func(string, ...any) {}}); err != nil {
			t.Fatal(err)
		}
		if got := entryData(t, filepath.Join(install, "Data", "pre", "foo.prx"), "t.qb"); !bytes.Equal(got, []byte("HQ-PAYLOAD")) {
			t.Fatalf("optional mod with payload not applied: %q", got)
		}
	})

	t.Run("non-optional+missing -> errors", func(t *testing.T) {
		install, mods := setup(t, false, false)
		if err := Run(Options{Install: install, ModsDir: mods, Layer: "all", Logf: func(string, ...any) {}}); err == nil {
			t.Fatal("non-optional mod with missing payload should error, got nil")
		}
	})
}

// TestApplyOverlay exercises the copyTree path: an overlay mod's Data/ tree is
// merged over the install (new files created, existing ones overwritten).
func TestApplyOverlay(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "install")
	os.MkdirAll(filepath.Join(install, "Data", "pre"), 0755)
	writeFile(t, filepath.Join(install, "Data", "textures", "old.txt"), "before")

	mods := filepath.Join(root, "mods")
	writeFile(t, filepath.Join(mods, "mods.list"), "skin\n")
	writeFile(t, filepath.Join(mods, "src", "skin", "mod.conf"), "type=overlay\nlayer=source\n")
	writeFile(t, filepath.Join(mods, "src", "skin", "Data", "textures", "old.txt"), "after")        // overwrite
	writeFile(t, filepath.Join(mods, "src", "skin", "Data", "textures", "new", "extra.txt"), "fresh") // new nested file

	if err := Run(Options{Install: install, ModsDir: mods, Layer: "all", Logf: func(string, ...any) {}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(install, "Data", "textures", "old.txt")); string(b) != "after" {
		t.Errorf("overlay did not overwrite existing file: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(install, "Data", "textures", "new", "extra.txt")); string(b) != "fresh" {
		t.Errorf("overlay did not create nested new file: %q", b)
	}
}

// TestIntegrationRealApply runs the real source-layer apply against the repo's
// game-pristine-us data + mods, if present. Skipped otherwise (data not in repo).
func TestIntegrationRealApply(t *testing.T) {
	repo := "../../.." // tools/thugkit/apply -> repo root
	srcPre := filepath.Join(repo, "game-pristine-us", "Data", "pre")
	modsDir := filepath.Join(repo, "mods")
	if _, err := os.Stat(srcPre); err != nil {
		t.Skip("game-pristine-us not present; skipping integration apply")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "mods.list")); err != nil {
		t.Skip("mods/ not present; skipping integration apply")
	}
	// stage a copy of just the archives the source mods touch + a few others
	install := t.TempDir()
	pre := filepath.Join(install, "Data", "pre")
	os.MkdirAll(pre, 0755)
	matches, _ := filepath.Glob(filepath.Join(srcPre, "*.prx"))
	for _, m := range matches {
		d, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(pre, filepath.Base(m)), d, 0644)
	}
	if err := Run(Options{Install: install, ModsDir: modsDir, Layer: "source", Logf: func(string, ...any) {}}); err != nil {
		t.Fatalf("real apply failed: %v", err)
	}
	// sanity: qb_scripts still parses and the modded menu entry is present
	d, err := os.ReadFile(filepath.Join(pre, "qb_scripts.prx"))
	if err != nil {
		t.Fatal(err)
	}
	_, es, err := prx.Parse(d)
	if err != nil {
		t.Fatalf("modded qb_scripts no longer parses: %v", err)
	}
	if prx.Find(es, `scripts\game\menu\gamemenu_pause.qb`) == nil {
		t.Fatal("gamemenu_pause.qb missing after apply")
	}
}
