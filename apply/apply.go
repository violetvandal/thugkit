// Package apply is the Go port of mods/apply-mods.sh — it applies the THUG2 mod
// layer onto an install, compiling NeverScript in-process and packing .prx with
// no external dependencies. See apply-mods.sh for the original spec.
package apply

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	nscompiler "github.com/byxor/NeverScript/compiler"
	"github.com/violetvandal/thugkit/prx"
)

// Options configure a run. ModsDir is the mods/ root (holds mods.list, src/, packs/).
type Options struct {
	Install string   // THUG2 install root (contains Data/pre) or a Data/ dir
	ModsDir string   // mods/ root
	Layer   string   // "all" | "binary" | "source"
	Only    []string // if non-empty, only these mod names
	Logf    func(string, ...any)
}

// Run applies every mod listed in <ModsDir>/mods.list, in order.
func Run(o Options) error {
	if o.Logf == nil {
		o.Logf = func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
	}
	if o.Layer == "" {
		o.Layer = "all"
	}
	data := filepath.Join(o.Install, "Data")
	if !dirExists(filepath.Join(data, "pre")) {
		data = o.Install // allow passing Data/ directly
	}
	if !dirExists(filepath.Join(data, "pre")) {
		return fmt.Errorf("not a THUG2 install (no Data/pre/): %s", o.Install)
	}
	list := filepath.Join(o.ModsDir, "mods.list")
	mods, err := readLines(list)
	if err != nil {
		return fmt.Errorf("read mods.list: %w", err)
	}
	only := map[string]bool{}
	for _, m := range o.Only {
		if m != "" {
			only[m] = true
		}
	}
	o.Logf("[mods] target: %s   layer: %s", data, o.Layer)
	n := 0
	for _, mod := range mods {
		mod = strings.TrimSpace(mod)
		if mod == "" || strings.HasPrefix(mod, "#") {
			continue
		}
		if len(only) > 0 && !only[mod] {
			continue
		}
		if err := applyOne(&o, data, mod); err != nil {
			return fmt.Errorf("mod %s: %w", mod, err)
		}
		n++
	}
	o.Logf("[mods] done — processed %d mod entries.", n)
	return nil
}

func applyOne(o *Options, data, mod string) error {
	// resolve dir: packs/ then src/ (src wins if both exist, matching bash)
	var dir string
	for _, base := range []string{"packs", "src"} {
		cand := filepath.Join(o.ModsDir, base, mod)
		if dirExists(cand) {
			dir = cand
		}
	}
	if dir == "" {
		o.Logf("[mods:warn] mod '%s' not found in packs/ or src/ — skipping", mod)
		return nil
	}
	conf, err := readConf(filepath.Join(dir, "mod.conf"))
	if err != nil {
		return err
	}
	typ := conf["type"]
	layer := conf["layer"]
	if layer == "" {
		layer = "binary"
	}
	if !(o.Layer == "all" || o.Layer == layer) {
		o.Logf("[mods] skip %s (layer=%s, want=%s)", mod, layer, o.Layer)
		return nil
	}

	switch typ {
	case "prx-overlay", "overlay":
		src := filepath.Join(dir, "Data")
		if !dirExists(src) {
			return fmt.Errorf("type=%s but no Data/ tree", typ)
		}
		o.Logf("[mods] apply %s  (%s, %s)", mod, typ, layer)
		return copyTree(src, data)

	case "prx-inject":
		o.Logf("[mods] apply %s  (prx-inject, %s)", mod, layer)
		return forInjectList(dir, func(archive, name, blob string) error {
			tgt := filepath.Join(data, "pre", archive)
			newdata, err := os.ReadFile(filepath.Join(dir, blob))
			if err != nil {
				return fmt.Errorf("blob missing: %w", err)
			}
			if err := injectPrx(tgt, name, newdata, false); err != nil {
				return err
			}
			o.Logf("[mods]   injected %s -> %s :: %s", blob, archive, name)
			return nil
		})

	case "ns-inject":
		o.Logf("[mods] apply %s  (ns-inject, %s)", mod, layer)
		return forInjectList(dir, func(archive, name, nssrc string) error {
			tgt := filepath.Join(data, "pre", archive)
			qb, err := compileNS(filepath.Join(dir, nssrc))
			if err != nil {
				return fmt.Errorf("ns compile failed: %s: %w", nssrc, err)
			}
			// qb_scripts.prx has a ~1.43 MB load ceiling -> inject compressed.
			compressed := archive == "qb_scripts.prx"
			if err := injectPrx(tgt, name, qb, compressed); err != nil {
				return err
			}
			op := "replace"
			if compressed {
				op = "replacez"
			}
			o.Logf("[mods]   compiled+injected %s -> %s :: %s (%s)", nssrc, archive, name, op)
			return nil
		})

	default:
		return fmt.Errorf("unknown type %q", typ)
	}
}

// injectPrx replaces one entry in a .prx on disk (raw or LZSS-compressed).
func injectPrx(archivePath, name string, newdata []byte, compressed bool) error {
	d, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("base archive missing: %w", err)
	}
	ver, entries, err := prx.Parse(d)
	if err != nil {
		return fmt.Errorf("parse %s: %w", archivePath, err)
	}
	e := prx.Find(entries, name)
	if e == nil {
		return fmt.Errorf("entry not found in %s: %s", filepath.Base(archivePath), name)
	}
	if compressed {
		if err := prx.ReplaceCompressed(e, newdata); err != nil {
			return err
		}
	} else {
		prx.ReplaceRaw(e, newdata)
	}
	out, err := prx.Build(ver, entries)
	if err != nil {
		return err
	}
	return writeFileAtomic(archivePath, out)
}

// compileNS compiles a NeverScript .ns to .qb bytes in-process (thug2 target),
// with the same 300s infinite-loop guard the ns CLI uses.
func compileNS(nsPath string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "thugkit-*.qb")
	if err != nil {
		return nil, err
	}
	qbPath := tmp.Name()
	tmp.Close()
	defer os.Remove(qbPath)

	done := make(chan error, 1)
	go func() {
		var lexer nscompiler.Lexer
		var parser nscompiler.Parser
		var bc nscompiler.BytecodeCompiler
		bc.TargetGame = "thug2"
		if cerr := nscompiler.Compile(nsPath, qbPath, &lexer, &parser, &bc); cerr != nil {
			done <- cerr.ToError()
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			return nil, err
		}
	case <-time.After(300 * time.Second):
		return nil, errors.New("compiler timed out (likely infinite loop)")
	}
	return os.ReadFile(qbPath)
}

// --- small helpers ---

func forInjectList(dir string, fn func(archive, name, third string) error) error {
	lines, err := readLines(filepath.Join(dir, "inject.list"))
	if err != nil {
		return fmt.Errorf("no inject.list: %w", err)
	}
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 3 {
			return fmt.Errorf("malformed inject.list line: %q", ln)
		}
		if err := fn(f[0], f[1], f[2]); err != nil {
			return err
		}
	}
	return nil
}

func readConf(path string) (map[string]string, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, fmt.Errorf("no mod.conf: %w", err)
	}
	m := map[string]string{}
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if k, v, ok := strings.Cut(ln, "="); ok {
			if _, seen := m[strings.TrimSpace(k)]; !seen { // head -1 semantics
				m[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return m, nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// copyTree copies src/. over dst (merge), like cp -a --no-preserve=mode.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
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

// writeFileAtomic writes via a temp file + rename in the same dir.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".thugkit-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
