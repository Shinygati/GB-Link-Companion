// What the Pokémon Bank shows besides the save: the species' names and the few facts it needs
// (growth rate for the level, gender ratio, abilities), and the box icons, as PKHeX shows them.
//
// They're read from the player's own game, never shipped with the app: from a ROM of the same
// game in the games list if there is one (quick), else from the cartridge over the link cable
// (once: they're kept in the Pokemon Bank folder). The icons are the same in every language
// and in Ruby, Sapphire, Emerald, FireRed and LeafGreen, so they're kept once for all of them.
package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	iconCount  = 440 // the party icons: the species, the egg, then Unown's letters B to ?
	iconBytes  = 512 // the first of an icon's two frames: 32x32, 4 bits a dot
	abilityMax = 78
	speciesTbl = speciesEgg + 1 // species 0-412 in the name and base stat tables
)

const bankDataVersion = 2 // names-*.json made before this lack the moves, items and places

type bankData struct {
	V         int            `json:"v"`
	Code      string         `json:"code"`
	Species   []string       `json:"species"` // by the games' species number
	Growth    []uint8        `json:"growth"`
	Gender    []uint8        `json:"gender"` // 0 always male, 254 always female, 255 genderless
	Abil      [][2]uint8     `json:"abil"`
	Abilities []string       `json:"abilities"`
	Base      [][6]uint8     `json:"base"`   // base stats: HP Atk Def Spe SpA SpD
	Moves     []string       `json:"moves"`  // by move number ("" when not found)
	Items     []string       `json:"items"`  // by item number
	Places    map[int]string `json:"places"` // by place number (MAPSEC_*)
}

type bankIcons struct {
	Tiles  []byte // iconCount x iconBytes
	PalIdx []byte // iconCount
	Pals   []byte // 6 palettes x 16 colours (BGR555)
}

// the English games' icon tables (pret's symbol files: gMonIconTable, gMonIconPaletteIndices,
// gMonIconPaletteTable)
var englishIconTables = map[string][3]uint32{
	"BPEE0": {0x0857BCA8, 0x0857C388, 0x0857C540},
	"BPRE0": {0x083D37A0, 0x083D3E80, 0x083D4038},
	"BPRE1": {0x083D3810, 0x083D3EF0, 0x083D40A8},
	"BPGE0": {0x083D35DC, 0x083D3CBC, 0x083D3E74},
	"BPGE1": {0x083D364C, 0x083D3D2C, 0x083D3EE4},
}

// romReader reads bytes from a game: a ROM file, or the cartridge over the link
type romReader func(addr uint32, n int) ([]byte, error)

func fileReader(rom []byte) romReader {
	return func(addr uint32, n int) ([]byte, error) {
		off := int(addr) - 0x08000000
		if off < 0 || off+n > len(rom) {
			return nil, fmt.Errorf("0x%08X is outside the ROM", addr)
		}
		return rom[off : off+n], nil
	}
}

// cartReader reads the cartridge in 32 KB pages (each read once). progress counts the pages.
func cartReader(h *Helper, progress func(pages int)) romReader {
	pages := map[int][]byte{}
	return func(addr uint32, n int) ([]byte, error) {
		off := int(addr) - 0x08000000
		if off < 0 || off+n > 0x2000000 {
			return nil, fmt.Errorf("0x%08X is outside the ROM", addr)
		}
		out := make([]byte, 0, n)
		for at := off; at < off+n; {
			p := at / romChunk
			page, ok := pages[p]
			if !ok {
				var err error
				page, err = h.ReadROM(p*romChunk, romChunk, len(pages), len(pages)+1)
				if err != nil {
					return nil, err
				}
				pages[p] = page
				if progress != nil {
					progress(len(pages))
				}
			}
			e := min(off+n, (p+1)*romChunk)
			out = append(out, page[at-p*romChunk:e-p*romChunk]...)
			at = e
		}
		return out, nil
	}
}

func isROMPtr(v uint32) bool { return v >= 0x08000000 && v < 0x0A000000 }

// readBankData reads the names and base stats from tables at these addresses.
func readBankData(read romReader, code string, names, info, abilities uint32, stride, aStride int) (*bankData, error) {
	d := &bankData{V: bankDataVersion, Code: code}
	raw, err := read(names, stride*speciesTbl)
	if err != nil {
		return nil, err
	}
	jp := len(code) == 4 && code[3] == 'J'
	for s := 0; s < speciesTbl; s++ {
		d.Species = append(d.Species, decodeGameText(raw[s*stride:(s+1)*stride], jp))
	}
	raw, err = read(info, 28*speciesTbl)
	if err != nil {
		return nil, err
	}
	for s := 0; s < speciesTbl; s++ {
		e := raw[28*s:]
		d.Growth = append(d.Growth, e[19])
		d.Gender = append(d.Gender, e[16])
		d.Abil = append(d.Abil, [2]uint8{e[22], e[23]})
		d.Base = append(d.Base, [6]uint8{e[0], e[1], e[2], e[3], e[4], e[5]})
	}
	raw, err = read(abilities, aStride*abilityMax)
	if err != nil {
		return nil, err
	}
	for a := 0; a < abilityMax; a++ {
		d.Abilities = append(d.Abilities, decodeGameText(raw[a*aStride:(a+1)*aStride], jp))
	}
	if len(d.Species) < 2 || d.Species[1] == "" || strings.Contains(d.Species[1], "?") {
		return nil, fmt.Errorf("the species' names weren't where they should be")
	}
	return d, nil
}

// readBankIcons reads the party icons from the icon tables.
func readBankIcons(read romReader, icons, palIdx, pals uint32) (*bankIcons, error) {
	ptrs, err := read(icons, 4*iconCount)
	if err != nil {
		return nil, err
	}
	ic := &bankIcons{Tiles: make([]byte, iconCount*iconBytes)}
	for i := 0; i < iconCount; i++ {
		p := binary.LittleEndian.Uint32(ptrs[4*i:])
		if !isROMPtr(p) {
			return nil, fmt.Errorf("icon %d isn't in the ROM", i)
		}
		t, err := read(p, iconBytes)
		if err != nil {
			return nil, err
		}
		copy(ic.Tiles[i*iconBytes:], t)
	}
	if ic.PalIdx, err = read(palIdx, iconCount); err != nil {
		return nil, err
	}
	ic.PalIdx = append([]byte(nil), ic.PalIdx...)
	table, err := read(pals, 6*8)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 6; i++ {
		p := binary.LittleEndian.Uint32(table[8*i:])
		if !isROMPtr(p) {
			return nil, fmt.Errorf("icon palette %d isn't in the ROM", i)
		}
		c, err := read(p, 32)
		if err != nil {
			return nil, err
		}
		ic.Pals = append(ic.Pals, c...)
	}
	for _, v := range ic.PalIdx {
		if v > 5 {
			return nil, fmt.Errorf("the icon palettes weren't where they should be")
		}
	}
	return ic, nil
}

// ------------------------------------------------------------------------------- the cache
func bankDataPath(code string) string { return filepath.Join(bankDir(), "names-"+code+".json") }
func bankIconsPath() string           { return filepath.Join(bankDir(), "icons.bin") }

func loadCachedBankData(code string) *bankData {
	b, err := os.ReadFile(bankDataPath(code))
	if err != nil {
		return nil
	}
	var d bankData
	if json.Unmarshal(b, &d) != nil || len(d.Species) != speciesTbl || d.V < bankDataVersion {
		return nil
	}
	return &d
}

func saveCachedBankData(d *bankData) {
	if b, err := json.Marshal(d); err == nil {
		writeFileSafely(bankDataPath(d.Code), b)
	}
}

func loadCachedIcons() *bankIcons {
	b, err := os.ReadFile(bankIconsPath())
	if err != nil || len(b) != iconCount*iconBytes+iconCount+6*32 {
		return nil
	}
	return &bankIcons{Tiles: b[:iconCount*iconBytes], PalIdx: b[iconCount*iconBytes : iconCount*iconBytes+iconCount],
		Pals: b[iconCount*iconBytes+iconCount:]}
}

func saveCachedIcons(ic *bankIcons) {
	var b bytes.Buffer
	b.Write(ic.Tiles)
	b.Write(ic.PalIdx)
	b.Write(ic.Pals)
	writeFileSafely(bankIconsPath(), b.Bytes())
}

// ------------------------------------------------------------------------------ the sources
// fromROMFile takes the names (if the ROM is of this game and language) and the icons from a ROM.
func fromROMFile(path, code string) (*bankData, *bankIcons) {
	rom, err := os.ReadFile(path)
	if err != nil || len(rom) < 0x200 {
		return nil, nil
	}
	f, extra, err := findROMAddresses(rom)
	if err != nil {
		return nil, nil
	}
	read := fileReader(rom)
	var d *bankData
	romCode := string(rom[0xAC:0xB0])
	if romCode == code && (extra["japanese"] != 0) == (code[3] == 'J') {
		stride, aStride := extra["speciesStride"], extra["abilityStride"]
		if stride == 0 {
			stride = 11
		}
		if aStride == 0 {
			aStride = 13
		}
		d, _ = readBankData(read, code, f["speciesNames"], f["speciesInfo"], f["abilityNames"], stride, aStride)
		if d != nil {
			fillExtras(d, read, rom, code, int(rom[0xBC]), f["speciesNames"], stride, f)
		}
	}
	var ic *bankIcons
	if f["icons"] != 0 {
		ic, _ = readBankIcons(read, f["icons"], f["iconPalIdx"], f["iconPals"])
	}
	return d, ic
}

// gatherBankData finds the names and icons for a game: kept from before, from the games list's
// ROMs (unmodified dumps first), else from the cartridge (cart may be nil: not now).
func gatherBankData(lib []GameView, code string, rev int, cart *Helper, log func(string, ...any), progress func(pages int)) (*bankData, *bankIcons) {
	d, ic := loadCachedBankData(code), loadCachedIcons()
	defer func() { d.mergePlaces(); d.borrowNames() }()
	if d != nil && ic != nil {
		return d, ic
	}
	fam := func(c string) bool {
		return len(c) == 4 && strings.Contains("BPE BPR BPG AXV AXP", c[:3])
	}
	var roms []GameView
	for _, g := range lib {
		if fam(g.Code) && g.Verify == "ok" {
			roms = append(roms, g)
		}
	}
	for _, g := range lib {
		if fam(g.Code) && g.Verify != "ok" {
			roms = append(roms, g)
		}
	}
	for _, g := range roms {
		needNames, needIcons := d == nil && g.Code == code, ic == nil
		if !needNames && !needIcons {
			continue
		}
		gd, gic := fromROMFile(g.ROM, code)
		if d == nil && gd != nil {
			d = gd
			saveCachedBankData(d)
			log("Pokémon names read from %s.", filepath.Base(g.ROM))
		}
		if ic == nil && gic != nil {
			ic = gic
			saveCachedIcons(ic)
			log("Box icons read from %s.", filepath.Base(g.ROM))
		}
	}
	if cart == nil {
		return d, ic
	}
	read := cartReader(cart, progress)
	key := fmt.Sprintf("%s%d", code, rev)
	if d == nil {
		if r, ok := romfind().Roms[key]; ok {
			var err error
			stride, aStride := 11, 13
			if code[3] == 'J' { // the Japanese games' shorter names
				stride, aStride = 6, 7
			}
			d, err = readBankData(read, code, r.S["SPECIES_NAMES"], r.S["SPECIES_INFO"], r.S["ABILITY_NAMES"], stride, aStride)
			if err == nil {
				fillExtras(d, read, nil, code, rev, r.S["SPECIES_NAMES"], stride, nil)
				saveCachedBankData(d)
				log("Pokémon names read from the cartridge.")
			} else {
				log("Couldn't read the Pokémon names from the cartridge: %v", err)
			}
		}
	}
	if ic == nil {
		if t, ok := englishIconTables[key]; ok {
			var err error
			ic, err = readBankIcons(read, t[0], t[1], t[2])
			if err == nil {
				saveCachedIcons(ic)
				log("Box icons read from the cartridge.")
			} else {
				log("Couldn't read the box icons from the cartridge: %v", err)
			}
		}
	}
	return d, ic
}

// image: icon i (as Pk3.IconIndex) as a 32x32 picture, colour 0 see-through.
func (ic *bankIcons) image(i int) *image.NRGBA {
	if ic == nil || i < 0 || i >= iconCount {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	pal := int(ic.PalIdx[i]) * 32
	var cols [16]color.NRGBA
	for c := 1; c < 16; c++ {
		v := binary.LittleEndian.Uint16(ic.Pals[pal+2*c:])
		r, g, b := uint8(v&31), uint8(v>>5&31), uint8(v>>10&31)
		cols[c] = color.NRGBA{r<<3 | r>>2, g<<3 | g>>2, b<<3 | b>>2, 255}
	}
	t := ic.Tiles[i*iconBytes:]
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			v := t[((y>>3)*4+(x>>3))*32+(y&7)*4+(x&7)>>1]
			if x&1 == 1 {
				v >>= 4
			}
			if v&15 != 0 {
				img.SetNRGBA(x, y, cols[v&15])
			}
		}
	}
	return img
}

// speciesName: a species' name, with a stand-in when the names aren't known.
func (d *bankData) speciesName(s int) string {
	if s == speciesEgg {
		return "Egg"
	}
	if d != nil && s >= 0 && s < len(d.Species) && d.Species[s] != "" {
		return d.Species[s]
	}
	return fmt.Sprintf("Species %d", s)
}

func (d *bankData) level(p *Pk3) int {
	rate := 0
	if d != nil && p.Species < len(d.Growth) {
		rate = int(d.Growth[p.Species])
	}
	return levelFor(rate, p.Exp)
}

// gender: "M", "F" or "" (genderless, or not known)
func (d *bankData) gender(p *Pk3) string {
	if d == nil || p.Species >= len(d.Gender) {
		return ""
	}
	switch r := int(d.Gender[p.Species]); r {
	case 255:
		return ""
	case 254:
		return "F"
	case 0:
		return "M"
	default:
		if int(p.PID&0xFF) < r {
			return "F"
		}
		return "M"
	}
}

func (d *bankData) ability(p *Pk3) string {
	if d == nil || p.Species >= len(d.Abil) {
		return ""
	}
	a := d.Abil[p.Species][0]
	if p.Ability == 1 && d.Abil[p.Species][1] != 0 {
		a = d.Abil[p.Species][1]
	}
	if int(a) < len(d.Abilities) {
		return d.Abilities[a]
	}
	return ""
}

// ----------------------------------------------------------- moves, items and met places
const (
	moveCount = 355 // MOVE_NONE to Psycho Boost
	itemSize  = 44  // struct Item
	itemMax   = 377
	placesE   = 0xD5 // Emerald's places (MAPSEC_NONE)
	placesFR  = 109  // FireRed and LeafGreen's, from KANTO_MAPSEC_START
)

// the English games' item tables (gItems) and place names (gRegionMapEntries; FireRed and
// LeafGreen's gRegionMapSectionNames, from MAPSEC 0x58). Each is checked before it's used.
var englishItemTables = map[string]uint32{
	"BPEE0": 0x085839A0, "BPRE0": 0x083DB028, "BPRE1": 0x083DB098, "BPGE0": 0x083DAE64, "BPGE1": 0x083DAED4,
}
var englishPlaceTables = map[string]uint32{
	"BPEE0": 0x085A147C, "BPRE0": 0x083F1CAC, "BPRE1": 0x083F1D1C, "BPGE0": 0x083F1AE8, "BPGE1": 0x083F1B58,
}

// nameLen: the length of the name in an entry (up to its 0xFF), or -1 if it isn't a name.
func nameLen(e []byte) int {
	set := gameCharset()["intl"]
	for i, c := range e {
		if c == 0xFF {
			return i
		}
		if _, ok := set[c]; !ok {
			return -1
		}
	}
	return -1
}

// findMoveNames looks just after the species' names for the move names: 355 names of 13 bytes,
// the first one short ("-"). They follow the species in every language.
func findMoveNames(read romReader, speciesNames uint32, stride int) []string {
	start := speciesNames + uint32(stride*speciesTbl)
	buf, err := read(start, 0x6000)
	if err != nil {
		return nil
	}
	const st = 13
	for p := 0; p+st*moveCount <= len(buf); p++ {
		if n := nameLen(buf[p : p+st]); n < 1 || n > 2 {
			continue
		}
		ok := true
		for i := 1; i < moveCount && ok; i++ {
			ok = nameLen(buf[p+st*i:p+st*i+st]) >= 2
		}
		if ok {
			out := make([]string, moveCount)
			for i := 1; i < moveCount; i++ {
				out[i] = decodeGameText(buf[p+st*i:p+st*i+st], false)
			}
			return out
		}
	}
	return nil
}

// itemTableAt reads the item names from a gItems at addr, if that's what's there (each item's
// number is in its entry).
func itemTableAt(read romReader, addr uint32) []string {
	buf, err := read(addr, itemSize*itemMax)
	if err != nil {
		return nil
	}
	n := 0
	for i := 1; i < itemMax; i++ {
		if int(binary.LittleEndian.Uint16(buf[itemSize*i+14:])) != i || nameLen(buf[itemSize*i:itemSize*i+14]) < 1 {
			break
		}
		n = i
	}
	if n < 300 {
		return nil
	}
	out := make([]string, n+1)
	for i := 1; i <= n; i++ {
		out[i] = decodeGameText(buf[itemSize*i:itemSize*i+14], false)
	}
	return out
}

// findItemTable looks through a whole ROM for gItems.
func findItemTable(rom []byte) uint32 {
	le := binary.LittleEndian
	for p := 0; p+itemSize*itemMax <= len(rom); p += 4 {
		if le.Uint16(rom[p+itemSize+14:]) != 1 || le.Uint16(rom[p+2*itemSize+14:]) != 2 || le.Uint16(rom[p+14:]) != 0 {
			continue
		}
		ok := true
		for i := 3; i < 40 && ok; i++ {
			ok = int(le.Uint16(rom[p+itemSize*i+14:])) == i
		}
		if ok {
			return uint32(p) + 0x08000000
		}
	}
	return 0
}

// placesAt reads the place names from a table at addr: Emerald's entries {x, y, w, h, name},
// or FireRed and LeafGreen's list of names from 0x58. nil if it isn't such a table.
func placesAt(read romReader, addr uint32, emerald bool) map[int]string {
	n, size, first := placesFR, 4, 0x58
	if emerald {
		n, size, first = placesE, 8, 0
	}
	buf, err := read(addr, n*size)
	if err != nil {
		return nil
	}
	out := map[int]string{}
	for i := 0; i < n; i++ {
		ptr := binary.LittleEndian.Uint32(buf[i*size+size-4:])
		if !isROMPtr(ptr) {
			return nil
		}
		t, err := read(ptr, 24)
		if err != nil || nameLen(t) < 0 {
			if err == nil && i > 0 { // a name that isn't text: not this table
				return nil
			}
			continue
		}
		if name := decodeGameText(t, false); name != "" {
			out[first+i] = name
		}
	}
	if len(out) < n/2 {
		return nil
	}
	return out
}

// findPlaces finds the place names from the code that shows them (GetMapName): its literal pool
// holds the table's address.
func findPlaces(read romReader, getMapName uint32, emerald bool) map[int]string {
	if getMapName == 0 {
		return nil
	}
	code, err := read(getMapName&^3, 0x200)
	if err != nil {
		return nil
	}
	for i := 0; i+4 <= len(code); i += 4 {
		if v := binary.LittleEndian.Uint32(code[i:]); isROMPtr(v) && v&3 == 0 {
			if m := placesAt(read, v, emerald); m != nil {
				return m
			}
		}
	}
	return nil
}

// fillExtras reads the moves, items and places into d from a game (rom is the whole ROM, or nil
// for the cartridge: then only the English games' known tables and the code that shows the
// places are looked at).
func fillExtras(d *bankData, read romReader, rom []byte, code string, rev int, speciesNames uint32, stride int, f map[string]uint32) {
	key := fmt.Sprintf("%s%d", code, rev)
	emerald := strings.HasPrefix(code, "BPE")
	d.Moves = findMoveNames(read, speciesNames, stride)
	switch {
	case rom != nil:
		if a := findItemTable(rom); a != 0 {
			d.Items = itemTableAt(read, a)
		}
	case englishItemTables[key] != 0:
		d.Items = itemTableAt(read, englishItemTables[key])
	}
	switch {
	case f != nil && emerald && f["mapEntries"] != 0:
		d.Places = placesAt(read, f["mapEntries"], true)
	case f != nil && !emerald && f["mapNames"] != 0:
		d.Places = placesAt(read, f["mapNames"], false)
	case englishPlaceTables[key] != 0:
		d.Places = placesAt(read, englishPlaceTables[key], emerald)
	}
	if d.Places == nil {
		if r, ok := romfind().Roms[key]; ok {
			d.Places = findPlaces(read, r.S["GET_MAP_NAME"], emerald)
		}
	}
}

// borrowNames fills in the move, item and place names a game's data doesn't have (they aren't
// read from the Japanese games) from another game read before: they're numbered the same in all.
func (d *bankData) borrowNames() {
	if d == nil || len(d.Moves) > 0 && len(d.Items) > 0 && len(d.Places) > 0 {
		return
	}
	files, _ := filepath.Glob(filepath.Join(bankDir(), "names-*.json"))
	for _, f := range files {
		c := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "names-"), ".json")
		if c == d.Code {
			continue
		}
		o := loadCachedBankData(c)
		if o == nil {
			continue
		}
		if len(d.Moves) == 0 {
			d.Moves = o.Moves
		}
		if len(d.Items) == 0 {
			d.Items = o.Items
		}
		if len(d.Places) == 0 {
			d.Places = o.Places
		}
	}
}

// mergePlaces fills in the places this game's table doesn't have (FireRed and LeafGreen have
// only Kanto's) from the other games of the same language read before.
func (d *bankData) mergePlaces() {
	if d == nil || len(d.Code) != 4 {
		return
	}
	files, _ := filepath.Glob(filepath.Join(bankDir(), "names-*.json"))
	for _, f := range files {
		c := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "names-"), ".json")
		if c == d.Code || len(c) != 4 || c[3] != d.Code[3] {
			continue
		}
		o := loadCachedBankData(c)
		if o == nil {
			continue
		}
		if d.Places == nil {
			d.Places = map[int]string{}
		}
		for k, v := range o.Places {
			if _, ok := d.Places[k]; !ok {
				d.Places[k] = v
			}
		}
	}
}

func (d *bankData) moveName(m int) string {
	if m == 0 {
		return ""
	}
	if d != nil && m < len(d.Moves) && d.Moves[m] != "" {
		return d.Moves[m]
	}
	return fmt.Sprintf("Move %d", m)
}

// gen3ItemsText: PKHeX's list of the Gen 3 items in English (GPL-3.0), by the games' item
// number: the names when they weren't read from the player's game.
//
//go:embed assets/gen3/items_g3_en.txt
var gen3ItemsText string

var gen3ItemNames = sync.OnceValue(func() []string { return strings.Split(strings.TrimRight(gen3ItemsText, "\n"), "\n") })

func (d *bankData) itemName(i int) string {
	if i == 0 {
		return ""
	}
	if d != nil && i < len(d.Items) && d.Items[i] != "" {
		return d.Items[i]
	}
	if n := gen3ItemNames(); i < len(n) && n[i] != "" && n[i] != "???" {
		return n[i]
	}
	return fmt.Sprintf("Item %d", i)
}

func (d *bankData) placeName(m int) string {
	if t, ok := metPlaceSpecial[m]; ok {
		return t
	}
	if m >= 0 && m < len(pkhexPlaces) {
		return pkhexPlaces[m]
	}
	if d != nil {
		if t, ok := d.Places[m]; ok {
			return t
		}
	}
	return fmt.Sprintf("place %d", m)
}

func (d *bankData) base(s int) ([6]int, bool) {
	var out [6]int
	if d == nil || s >= len(d.Base) {
		return out, false
	}
	for i, v := range d.Base[s] {
		out[i] = int(v)
	}
	return out, true
}

// offlineBankData: names and icons for the bank before any cartridge is loaded: the names kept
// from the last game read (or a ROM in the games list), the icons kept from before.
func offlineBankData(lib []GameView, log func(string, ...any)) (*bankData, *bankIcons) {
	files, _ := filepath.Glob(filepath.Join(bankDir(), "names-*.json"))
	var newest *bankData
	var when time.Time
	for _, f := range files {
		c := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "names-"), ".json")
		st, err := os.Stat(f)
		if err != nil || bankFamily(c) == "" {
			continue
		}
		if d := loadCachedBankData(c); d != nil && (newest == nil || st.ModTime().After(when)) {
			newest, when = d, st.ModTime()
		}
	}
	if newest != nil {
		d, ic := gatherBankData(lib, newest.Code, 0, nil, log, nil) // the icons, from a ROM if they aren't kept
		if d == nil {
			d = newest
			d.mergePlaces()
		}
		return d, ic
	}
	for _, g := range lib { // nothing kept yet: a ROM of one of the games
		if bankFamily(g.Code) != "" {
			if d, ic := gatherBankData(lib, g.Code, 0, nil, log, nil); d != nil {
				return d, ic
			}
		}
	}
	return nil, loadCachedIcons()
}
