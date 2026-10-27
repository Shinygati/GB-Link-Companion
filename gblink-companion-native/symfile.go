// Your own builds of the Pokemon decompilations: the addresses the mGBA script needs, from the
// build's symbol file next to the ROM (pokeemerald.sym from "make syms", or the .elf).
package main

import (
	"bufio"
	"debug/elf"
	"encoding/binary"
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
	{"roamerLoc", []string{"sRoamerLocation"}}, // the roamer's map (roamer.go)
	// the Wonder Cards' script engine, and when you're walking around
	{"scriptCtx", []string{"sGlobalScriptContext"}}, {"scriptStatus", []string{"sGlobalScriptContextStatus"}},
	{"lockControls", []string{"sLockFieldControls"}}, {"cmdTable", []string{"gScriptCmdTable"}},
	{"cmdTableEnd", []string{"gScriptCmdTableEnd"}}, {"ramScriptRet", []string{"gRamScriptRetAddr"}},
	{"intrTable", []string{"gIntrTable"}}, {"cb1Overworld", []string{"CB1_Overworld"}}, {"cb2Overworld", []string{"CB2_Overworld"}},
	{"helpRDisable", []string{"gHelpSystemToggleWithRButtonDisabled"}},
}

// symGame: which game a build is, from its symbols, for ROM hacks that changed the game code in
// the ROM's header: FireRed/LeafGreen have the Sevii Islands, Emerald keeps its save behind a
// pointer (and has the Battle Frontier), Ruby/Sapphire neither.
func symGame(syms map[string]uint32) string {
	_, sevii := syms["sSeviiMapsecs"]
	_, kanto := syms["sRegionMapSections_Kanto"]
	_, ptr := syms["gSaveBlock1Ptr"]
	switch {
	case sevii || kanto:
		return "FIRERED"
	case ptr:
		return "EMERALD"
	case syms["gSaveBlock1"] != 0:
		return "RUBY"
	}
	return ""
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
	if !ok {
		return false
	}
	if !at(sp+11, names[0]) { // a ROM hack may have renamed them: then the names must at least be names
		return symNamesLookRight(rom, syms)
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

// symNamesLookRight: for a ROM hack whose names differ from the original game's, are there names
// (the games' text, each ended by 0xFF in its 11 bytes) where the build's species and nature names
// should be? A .sym from another build points somewhere else, and that isn't text.
func symNamesLookRight(rom []byte, syms map[string]uint32) bool {
	set := gameCharset()["intl"]
	isName := func(off int64, max int) bool {
		if off < 0 || off+int64(max) > int64(len(rom)) {
			return false
		}
		for i := 0; i < max; i++ {
			c := rom[off+int64(i)]
			if c == 0xFF {
				return i > 0
			}
			if _, ok := set[c]; !ok {
				return false
			}
		}
		return false
	}
	sp := int64(syms["gSpeciesNames"]) - 0x08000000
	for s := int64(1); s <= 25; s++ { // the first 25 Pokémon
		if !isName(sp+11*s, 11) {
			return false
		}
	}
	nat, ok := syms["gNatureNamePointers"]
	if !ok {
		nat, ok = syms["gNatureNames"]
	}
	o := int64(nat) - 0x08000000
	if !ok || o < 0 || o+25*4 > int64(len(rom)) {
		return false
	}
	for n := int64(0); n < 25; n++ {
		p := int64(binary.LittleEndian.Uint32(rom[o+4*n:])) - 0x08000000
		if !isName(p, 13) {
			return false
		}
	}
	return true
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
	syms, path, romData := matchingSym(rom, log)
	if syms == nil {
		return ""
	}
	if game == "" { // a ROM hack with its own game code: which game it's built from
		if game = symGame(syms); game == "" {
			log("%s: couldn't tell which Pokémon game this is built from.", filepath.Base(path))
			return ""
		}
		log("%s has its own game code (%s): treating it as %s, from its symbols.", filepath.Base(rom), code, titleCase(game))
	}
	// sym: the app checked it against the ROM (the script doesn't check the names again: a hack
	// may have renamed them)
	parts := []string{fmt.Sprintf("code = %q", code), fmt.Sprintf("game = %q", game), `build = "own build"`, "custom = true", "sym = true"}
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
	if game == "EMERALD" || game == "FIRERED" || game == "LEAFGREEN" { // your own Wonder Cards (wcsym.go)
		if o, err := deriveWCOffsets(romData, syms, readSymbolSizes(path)); err == nil {
			parts = append(parts, luaWCParts(o)...)
		}
	}
	log("Status screens: using the addresses in %s.", filepath.Base(path))
	return "{ " + strings.Join(parts, ", ") + " }"
}

// matchingSym: the symbols of the .sym/.elf next to the ROM that belongs to it (checked against the
// ROM), its path and the ROM's bytes; nil if there's none.
func matchingSym(rom string, log func(string, ...any)) (map[string]uint32, string, []byte) {
	cands := symCandidates(rom)
	if len(cands) == 0 {
		return nil, "", nil
	}
	romData, err := os.ReadFile(rom)
	if err != nil {
		log("Couldn't read %s (%v).", filepath.Base(rom), err)
		return nil, "", nil
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
		if _, ok := sy["gSpeciesNames"]; !ok {
			if _, exp := sy["gSpeciesInfo"]; exp && sy["gSaveBlock1Ptr"] != 0 {
				log("%s is from a pokeemerald-expansion build: its Pokémon data is laid out differently, so the status screens can't read it yet.", filepath.Base(c))
			}
			continue
		}
		if symMatchesROM(romData, sy) {
			syms, path = sy, c
			break
		}
		wrong = append(wrong, filepath.Base(c))
	}
	if syms == nil {
		for _, w := range wrong {
			log("%s isn't from the same build as %s (the Pokémon names aren't where it says): make it again with \"make syms\" after building this ROM.", w, filepath.Base(rom))
		}
		return nil, "", nil
	}
	return syms, path, romData
}
