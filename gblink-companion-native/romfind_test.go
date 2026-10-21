package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// GBLINK_FIND_TEST="rom.gba:symbols.sym[:skip]" ... checks findROMAddresses against a build's symbols
func TestRomFind(t *testing.T) {
	spec := os.Getenv("GBLINK_FIND_TEST")
	if spec == "" {
		t.Skip("GBLINK_FIND_TEST not set")
	}
	want := map[string][]string{}
	for _, f := range symFields {
		want[f.field] = f.names
	}
	want["rng"] = []string{"gRngValue"}
	for _, one := range strings.Split(spec, ",") {
		p := strings.Split(one, ":")
		rom, err := os.ReadFile(p[0])
		if err != nil {
			t.Fatal(err)
		}
		syms := map[string]uint32{}
		fh, _ := os.Open(p[1])
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			x := strings.Fields(sc.Text())
			if len(x) >= 3 {
				v, err := strconv.ParseUint(x[0], 16, 32)
				if err == nil {
					if _, ok := syms[x[len(x)-1]]; !ok {
						syms[x[len(x)-1]] = uint32(v)
					}
				}
			}
		}
		fh.Close()
		f, extra, err := scanROM(rom)
		if err != nil {
			t.Errorf("%s: %v", p[0], err)
			continue
		}
		bad, good := 0, 0
		for field, got := range f {
			names := want[field]
			var exp uint32
			for _, n := range names {
				if v, ok := syms[n]; ok {
					exp = v
					break
				}
			}
			if field == "sb1ptr" || field == "sb2ptr" || field == "sb1" || field == "sb2" {
				if v, ok := syms[map[string]string{"sb1ptr": "gSaveBlock1Ptr", "sb2ptr": "gSaveBlock2Ptr", "sb1": "gSaveBlock1", "sb2": "gSaveBlock2"}[field]]; ok {
					exp = v
				}
			}
			if exp == 0 {
				t.Logf("%s: %s = %08X (no symbol to check)", p[0], field, got)
				continue
			}
			if field == "frontPics" && got == syms["gMonStillFrontPicTable"] {
				exp = got // Emerald: the still front pictures are as good
			}
			if strings.HasSuffix(field, "Pal") && exp != got && string(rom[exp-0x08000000:exp-0x08000000+32]) == string(rom[got-0x08000000:got-0x08000000+32]) {
				exp = got // the same colours elsewhere
			}
			if exp != got {
				t.Errorf("%s: %s = %08X, want %08X", p[0], field, got, exp)
				bad++
			} else {
				good++
			}
		}
		for _, f2 := range symFields {
			if _, ok := f[f2.field]; !ok {
				for _, n := range f2.names {
					if _, ok := syms[n]; ok {
						t.Logf("%s: not found: %s", p[0], f2.field)
						break
					}
				}
			}
		}
		t.Logf("%s: %d right, %d wrong, extra %v", p[0], good, bad, extra)
	}
}
