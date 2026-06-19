// thugkit — single-binary, cross-platform THUG2 mod toolchain (Go).
// Phase 0: the portable "apply modpack" core, replacing prx.py + apply-mods.sh's
// portable pieces. Compiles NeverScript in-process and packs .prx with no deps.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/violetvandal/thugkit/apply"
	"github.com/violetvandal/thugkit/prx"
	"github.com/violetvandal/thugkit/tag"
)

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "thugkit: "+format+"\n", a...)
	os.Exit(1)
}

func readFile(path string) []byte {
	d, err := os.ReadFile(path)
	if err != nil {
		die("read %s: %v", path, err)
	}
	return d
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: thugkit <prx|...> ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "prx":
		cmdPrx(os.Args[2:])
	case "apply":
		cmdApply(os.Args[2:])
	case "tag":
		cmdTag(os.Args[2:])
	default:
		die("unknown command %q", os.Args[1])
	}
}

// cmdApply: thugkit apply <install> [--mods dir] [--layer all|binary|source] [--only a,b]
func cmdApply(args []string) {
	o := apply.Options{Layer: "all", ModsDir: "mods"}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mods":
			i++
			o.ModsDir = args[i]
		case "--layer":
			i++
			o.Layer = args[i]
		case "--only":
			i++
			o.Only = strings.Split(args[i], ",")
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) != 1 {
		die("usage: thugkit apply <install-dir> [--mods dir] [--layer all|binary|source] [--only a,b]")
	}
	o.Install = rest[0]
	if err := apply.Run(o); err != nil {
		die("%v", err)
	}
}

// cmdTag: thugkit tag <image> --gamedir <dir> [--name X] [--slot grap_50]
//
//	[--size 64|128|256] [--scale F] [--out dir] [--install]
func cmdTag(args []string) {
	o := tag.Options{Slot: "grap_50", Size: 64, Scale: 1.0, OutDir: "out"}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--gamedir":
			i++
			o.GameDir = args[i]
		case "--name":
			i++
			o.Name = args[i]
		case "--slot":
			i++
			o.Slot = args[i]
		case "--size":
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				die("--size: %v", err)
			}
			o.Size = n
		case "--scale":
			i++
			f, err := strconv.ParseFloat(args[i], 32)
			if err != nil {
				die("--scale: %v", err)
			}
			o.Scale = float32(f)
		case "--out":
			i++
			o.OutDir = args[i]
		case "--install":
			o.Install = true
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) != 1 || o.GameDir == "" {
		die("usage: thugkit tag <image> --gamedir <dir> [--name X] [--slot grap_50] [--size 64|128|256] [--scale F] [--out dir] [--install]")
	}
	o.Image = rest[0]
	res, err := tag.Run(o)
	if err != nil {
		die("%v", err)
	}
	fmt.Printf("✓ wrote %s\n✓ wrote %s\n", res.GRF, res.PRX)
	if o.Install {
		fmt.Printf("✓ installed sprite -> %s\n", res.InstalledPRX)
		if res.InstalledGRF != "" {
			fmt.Printf("✓ installed tag    -> %s\n", res.InstalledGRF)
		}
	} else {
		fmt.Printf("\nInstall (back up first):\n  copy %s -> %s/\n  copy %s -> %s/\n",
			res.GRF, res.SaveDir, res.PRX, res.PreDir)
	}
}

// cmdPrx mirrors prx.py: roundtrip|list|extract|replace|replacez.
func cmdPrx(args []string) {
	if len(args) < 1 {
		die("usage: thugkit prx <roundtrip|list|extract|replace|replacez> ...")
	}
	sub := args[0]
	switch sub {
	case "roundtrip": // prx roundtrip <prx>
		d := readFile(args[1])
		ver, entries, err := prx.Parse(d)
		if err != nil {
			die("parse: %v", err)
		}
		rebuilt, err := prx.Build(ver, entries)
		if err != nil {
			die("build: %v", err)
		}
		identical := len(rebuilt) == len(d)
		if identical {
			for i := range d {
				if d[i] != rebuilt[i] {
					identical = false
					break
				}
			}
		}
		fmt.Printf("files=%d  identical=%v\n", len(entries), identical)
	case "list": // prx list <prx>
		_, entries, err := prx.Parse(readFile(args[1]))
		if err != nil {
			die("parse: %v", err)
		}
		for _, e := range entries {
			fmt.Printf("%-9d comp=%-9d %s\n", e.Dsize, e.Csize, string(e.Name))
		}
	case "extract": // prx extract <prx> <name> <out>
		_, entries, err := prx.Parse(readFile(args[1]))
		if err != nil {
			die("parse: %v", err)
		}
		e := prx.Find(entries, args[2])
		if e == nil {
			die("not found: %s", args[2])
		}
		if err := os.WriteFile(args[3], e.RawData(), 0644); err != nil {
			die("write: %v", err)
		}
		fmt.Printf("extracted %s (%d bytes)\n", args[2], e.Dsize)
	case "replace", "replacez": // prx replace[z] <prx> <name> <newfile> <out>
		ver, entries, err := prx.Parse(readFile(args[1]))
		if err != nil {
			die("parse: %v", err)
		}
		e := prx.Find(entries, args[2])
		if e == nil {
			die("not found: %s", args[2])
		}
		newdata := readFile(args[3])
		if sub == "replacez" {
			if err := prx.ReplaceCompressed(e, newdata); err != nil {
				die("%v", err)
			}
		} else {
			prx.ReplaceRaw(e, newdata)
		}
		out, err := prx.Build(ver, entries)
		if err != nil {
			die("build: %v", err)
		}
		if err := os.WriteFile(args[4], out, 0644); err != nil {
			die("write: %v", err)
		}
		fmt.Printf("replaced %s with %d bytes (%s); wrote %s\n", args[2], len(newdata), sub, args[4])
	default:
		die("unknown prx subcommand %q", sub)
	}
}
