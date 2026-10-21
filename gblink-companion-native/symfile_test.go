package main

import (
	"os"
	"strings"
	"testing"
)

// GBLINK_TEST_ELF: a decomp build's .elf (e.g. pokefirered_rev1.elf) to check the .elf reader.
func TestSymbolsFromELF(t *testing.T) {
	path := os.Getenv("GBLINK_TEST_ELF")
	if path == "" {
		t.Skip("set GBLINK_TEST_ELF")
	}
	dir := t.TempDir()
	rom := dir + "/game.gba"
	b, _ := os.ReadFile(path)
	os.WriteFile(dir+"/game.elf", b, 0o644)
	got := luaOverride(rom, "BPRE", func(f string, a ...any) { t.Logf(f, a...) })
	for _, want := range []string{`game = "FIRERED"`, "speciesNames = 0x08", "sb1ptr = 0x03005008", "layoutKanto = 0x08", "mapGroups = 0x08", "rmTiles = 0x08", "rmHero = 0x08", "sb2ptr = 0x0300500C"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
