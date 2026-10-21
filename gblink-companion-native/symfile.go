// Your own builds of the Pokemon decompilations: the addresses the mGBA script needs, from the
// build's symbol file next to the ROM (pokeemerald.sym from "make syms", or the .elf).
package main

import (
	"bufio"
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// the script's field -> the symbols it can come from (same list as the script's built-in tables)
var symFields = []struct {
	field string
	names []string
}{
	{"rng", []string{"gRngValue"}}, {"party", []string{"gPlayerParty"}}, {"enemy", []string{"gEnemyParty"}},
	{"sb1ptr", []string{"gSaveBlock1Ptr"}}, {"sb1", []string{"gSaveBlock1"}},
	{"sb2ptr", []string{"gSaveBlock2Ptr"}}, {"sb2", []string{"gSaveBlock2"}}, {"mapHeader", []string{"gMapHeader"}},
	{"main", []string{"gMain"}}, {"battleType", []string{"gBattleTypeFlags"}},
	{"opponent", []string{"gTrainerBattleOpponent_A", "gTrainerBattleOpponent"}},
	{"battleMons", []string{"gBattleMons"}}, {"battleStructPtr", []string{"gBattleStruct"}},
	{"partyIdx", []string{"gBattlerPartyIndexes"}}, {"speciesNames", []string{"gSpeciesNames"}},
	{"abilityNames", []string{"gAbilityNames"}}, {"natureNames", []string{"gNatureNamePointers", "gNatureNames"}},
	{"speciesInfo", []string{"gSpeciesInfo", "gBaseStats"}}, {"trainers", []string{"gTrainers"}},
	{"classNames", []string{"gTrainerClassNames"}}, {"mapEntries", []string{"gRegionMapEntries"}},
	{"mapNames", []string{"sMapNames"}}, {"seviiMapsecs", []string{"sSeviiMapsecs"}},
	{"frontPics", []string{"gMonFrontPicTable"}}, {"palettes", []string{"gMonPaletteTable"}},
	{"shinyPalettes", []string{"gMonShinyPaletteTable"}}, {"icons", []string{"gMonIconTable"}},
	{"iconPalIdx", []string{"gMonIconPaletteIndices"}}, {"iconPals", []string{"gMonIconPaletteTable"}},
	{"mapGroups", []string{"gMapGroups"}},
	{"rmTiles", []string{"sRegionMapBg_GfxLZ", "sRegionMapBkgnd_ImageLZ", "sRegionMap_Gfx"}},
	{"rmTilemap", []string{"sRegionMapBg_TilemapLZ", "sRegionMapBkgnd_TilemapLZ"}},
	{"rmPal", []string{"sRegionMapBg_Pal", "sRegionMapBkgnd_Pal", "sRegionMap_Pal"}},
	{"rmKanto", []string{"sKanto_Tilemap"}}, {"rmSevii123", []string{"sSevii123_Tilemap"}},
	{"rmSevii45", []string{"sSevii45_Tilemap"}}, {"rmSevii67", []string{"sSevii67_Tilemap"}},
	{"rmCursor", []string{"sRegionMapCursorSmallGfxLZ", "sRegionMapCursorSmall_ImageLZ", "sMapCursor_Gfx"}},
	{"rmCursorPal", []string{"sRegionMapCursorPal", "sRegionMapCursor_Pal", "sMapCursor_Pal"}},
	{"rmHero", []string{"sRegionMapPlayerIcon_BrendanGfx", "sRegionMapBrendanIcon_Image", "sPlayerIcon_Red"}},
	{"rmHeroPal", []string{"sRegionMapPlayerIcon_BrendanPal", "sRegionMapBrendanIcon_Pal", "sPlayerIcon_RedPal"}},
	{"rmHeroine", []string{"sRegionMapPlayerIcon_MayGfx", "sRegionMapMayIcon_Image", "sPlayerIcon_Leaf"}},
	{"rmHeroinePal", []string{"sRegionMapPlayerIcon_MayPal", "sRegionMapMayIcon_Pal", "sPlayerIcon_LeafPal"}},
	{"layoutKanto", []string{"sRegionMapSections_Kanto"}}, {"layoutSevii123", []string{"sRegionMapSections_Sevii123"}},
	{"layoutSevii45", []string{"sRegionMapSections_Sevii45"}}, {"layoutSevii67", []string{"sRegionMapSections_Sevii67"}},
}

var gameByCode = map[string]string{"AXV": "RUBY", "AXP": "SAPPHIRE", "BPE": "EMERALD", "BPR": "FIRERED", "BPG": "LEAFGREEN"}

// symCandidates: the symbol files that could belong to the ROM: "<rom>.sym" or "<rom>.elf" next to
// it first (a decomp build's pokeemerald.gba has pokeemerald.sym / pokeemerald.elf beside it),
// then any other .sym or .elf in the same folder (e.g. pokeemerald.sym beside a renamed ROM).
func symCandidates(rom string) []string {
	base := strings.TrimSuffix(rom, filepath.Ext(rom))
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	add(base + ".sym")
	add(base + ".elf")
	if ents, err := os.ReadDir(filepath.Dir(rom)); err == nil {
		for _, e := range ents {
			if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".sym" || ext == ".elf" {
				add(filepath.Join(filepath.Dir(rom), e.Name()))
			}
		}
	}
	return out
}

func findSymFile(rom string) string {
	if c := symCandidates(rom); len(c) > 0 {
		return c[0]
	}
	return ""
}

// gen3Text: ASCII capitals as the games' text (A-Z are 0xBB-0xD4), ended by 0xFF
func gen3Text(s string) []byte {
	b := make([]byte, 0, len(s)+1)
	for _, c := range s {
		b = append(b, byte(0xBB+c-'A'))
	}
	return append(b, 0xFF)
}

// symMatchesROM: are the build's Pokemon names where its symbols say, in this ROM?
func symMatchesROM(rom []byte, syms map[string]uint32) bool {
	names := langNames['E']
	if len(rom) >= 0xB0 {
		if n, ok := langNames[rom[0xAF]]; ok && rom[0xAF] != 'J' {
			names = n
		}
	}
	at := func(addr uint32, text string) bool {
		want := gameText(text, false)
		off := int64(addr) - 0x08000000
		return off >= 0 && off+int64(len(want)) <= int64(len(rom)) && string(rom[off:off+int64(len(want))]) == string(want)
	}
	sp, ok := syms["gSpeciesNames"]
	if !ok || !at(sp+11, names[0]) {
		return false
	}
	nat, ok := syms["gNatureNamePointers"]
	if !ok {
		nat, ok = syms["gNatureNames"]
	}
	if !ok || int64(nat)-0x08000000+4 > int64(len(rom)) || nat < 0x08000000 {
		return false
	}
	o := nat - 0x08000000
	p := uint32(rom[o]) | uint32(rom[o+1])<<8 | uint32(rom[o+2])<<16 | uint32(rom[o+3])<<24
	return at(p, names[4])
}

func readSymbols(path string) (map[string]uint32, error) {
	syms := map[string]uint32{}
	if strings.EqualFold(filepath.Ext(path), ".elf") {
		f, err := elf.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		list, err := f.Symbols()
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			if _, ok := syms[s.Name]; !ok {
				syms[s.Name] = uint32(s.Value)
			}
		}
		return syms, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() { // "02024744 g 00000258 gEnemyParty" (make syms), or nm's "02024744 B gEnemyParty"
		p := strings.Fields(sc.Text())
		if len(p) < 3 {
			continue
		}
		v, err := strconv.ParseUint(p[0], 16, 32)
		if err != nil {
			continue
		}
		name := p[len(p)-1]
		if _, ok := syms[name]; !ok {
			syms[name] = uint32(v)
		}
	}
	return syms, sc.Err()
}

// luaOverride turns the build's symbols into the script's table (OVERRIDE), or "" if there's
// no symbol file or it isn't a Pokemon game's.
func luaOverride(rom, code string, log func(string, ...any)) string {
	game := gameByCode[strings.ToUpper(code[:min(3, len(code))])]
	if game == "" {
		return ""
	}
	cands := symCandidates(rom)
	if len(cands) == 0 {
		return ""
	}
	romData, err := os.ReadFile(rom)
	if err != nil {
		log("Couldn't read %s (%v).", filepath.Base(rom), err)
		return ""
	}
	var syms map[string]uint32
	var wrong []string
	path := ""
	for _, c := range cands {
		sy, err := readSymbols(c)
		if err != nil {
			log("Couldn't read %s (%v).", filepath.Base(c), err)
			continue
		}
		if symMatchesROM(romData, sy) {
			syms, path = sy, c
			break
		}
		if _, ok := sy["gSpeciesNames"]; ok {
			wrong = append(wrong, filepath.Base(c))
		}
	}
	if syms == nil {
		for _, w := range wrong {
			log("%s isn't from the same build as %s (the Pokémon names aren't where it says): make it again with \"make syms\" after building this ROM.", w, filepath.Base(rom))
		}
		return ""
	}
	parts := []string{fmt.Sprintf("code = %q", code), fmt.Sprintf("game = %q", game), `build = "own build"`, "custom = true"}
	if l := gameLang(code); l != 'E' {
		parts = append(parts, fmt.Sprintf("lang = %q", string(l))) // its names are checked in that language
	}
	have := map[string]bool{}
	for _, f := range symFields {
		if (f.field == "sb1" && have["sb1ptr"]) || (f.field == "sb2" && have["sb2ptr"]) {
			continue // Emerald and FireRed/LeafGreen keep it behind a pointer
		}
		for _, n := range f.names {
			if v, ok := syms[n]; ok {
				parts = append(parts, fmt.Sprintf("%s = 0x%08X", f.field, v))
				have[f.field] = true
				break
			}
		}
	}
	for _, need := range []string{"speciesNames", "natureNames", "enemy", "main"} {
		if !have[need] {
			log("%s doesn't look like a Pokémon decomp's symbol file (no %s).", filepath.Base(path), need)
			return ""
		}
	}
	log("Status screens: using the addresses in %s.", filepath.Base(path))
	return "{ " + strings.Join(parts, ", ") + " }"
}
