// Finding where a Pokémon ROM of another language keeps what the status screens read.
//
// The script has the addresses of the English games (and German Ruby/Sapphire) from pret's
// symbol files. For the other languages they're found here, when you press Play:
//   - RAM: Emerald, FireRed and LeafGreen in every language from the GB-Link team's table
//     (gblink-wondercards, tools/native-cards/roms.mjs, GPL-3.0), plus a few neighbours at the
//     same distance as in English. French, Italian and Spanish Ruby/Sapphire are laid out
//     like the German ones (pret's pokeruby builds those).
//   - The RNG: the routine that holds the RNG's constants also holds its address.
//   - ROM tables (names, base stats, trainers, pictures, the map's places) by their shape;
//     the region map pictures and layouts by their bytes, which every language shares.
//
// The result is cached by the ROM's SHA-1, and given to the script like a .sym's addresses.
package main

import (
	"bytes"
	"crypto/sha1"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed assets/romfind.json
var romfindJSON []byte

//go:embed assets/charset.json
var charsetJSON []byte

type romfindData struct {
	Roms map[string]struct {
		Family   string            `json:"family"`
		Language string            `json:"language"`
		Revision int               `json:"revision"`
		S        map[string]uint32 `json:"s"`
	} `json:"roms"`
	RSEurope map[string]uint32 `json:"rsEurope"`
	// English Ruby's routines that use each RAM address, found by their code in the other
	// languages' Ruby/Sapphire (BL targets and literal pools masked): the address is the
	// literal pool word at slot (FLAG_GET: the routine itself)
	RSSigs []struct {
		Field  string `json:"field"`
		Family string `json:"family"` // rs, emerald or frlg
		Code   string `json:"code"`
		Mask   string `json:"mask"`
		Slot   int    `json:"slot"`
	} `json:"rsSigs"`
	FP map[string]map[string]struct {
		Prefix string `json:"prefix"`
		Size   int    `json:"size"`
		CRC    uint32 `json:"crc"`
	} `json:"fp"`
}

var romfind = sync.OnceValue(func() *romfindData {
	var d romfindData
	json.Unmarshal(romfindJSON, &d)
	return &d
})

// the games' two character sets (code -> text), from pret's charmap.txt
var gameCharset = sync.OnceValue(func() map[string]map[byte]string {
	var raw map[string]map[string]string
	json.Unmarshal(charsetJSON, &raw)
	out := map[string]map[byte]string{}
	for set, m := range raw {
		out[set] = map[byte]string{}
		for k, v := range m {
			var n int
			fmt.Sscan(k, &n)
			out[set][byte(n)] = v
		}
	}
	return out
})

// gameText encodes text in a game's character set (japanese: kana), or nil if it can't.
func gameText(s string, japanese bool) []byte {
	set := gameCharset()["intl"]
	if japanese {
		set = gameCharset()["jp"]
	}
	rev := map[string]byte{}
	for c, t := range set {
		if old, ok := rev[t]; !ok || c > old && c >= 0xA1 {
			rev[t] = c
		}
	}
	var out []byte
	for _, r := range s {
		c, ok := rev[string(r)]
		if !ok {
			return nil
		}
		out = append(out, c)
	}
	return append(out, 0xFF)
}

// the first Pokémon, the second, the first two abilities and the first nature, in each language
var langNames = map[byte][5]string{
	'E': {"BULBASAUR", "IVYSAUR", "STENCH", "DRIZZLE", "HARDY"},
	'F': {"BULBIZARRE", "HERBIZARRE", "PUANTEUR", "CRACHIN", "HARDI"},
	'D': {"BISASAM", "BISAKNOSP", "DUFTNOTE", "NIESEL", "ROBUST"},
	'I': {"BULBASAUR", "IVYSAUR", "TANFO", "PIOGGERELLA", "ARDITA"},
	'S': {"BULBASAUR", "IVYSAUR", "HEDOR", "LLOVIZNA", "FUERTE"},
	'J': {"フシギダネ", "フシギソウ", "あくしゅう", "あめふらし", "がんばりや"},
}

type romScan struct {
	rom      []byte
	code     string // e.g. BPEF
	rev      int
	game     string // EMERALD, FIRERED, LEAFGREEN, RUBY, SAPPHIRE
	lang     byte
	japanese bool
	f        map[string]uint32
	extra    map[string]int // strides etc.
	missing  []string
}

func (r *romScan) u32(off int) uint32 {
	if off < 0 || off+4 > len(r.rom) {
		return 0
	}
	return binary.LittleEndian.Uint32(r.rom[off:])
}
func (r *romScan) at(addr uint32) int { return int(addr) - 0x08000000 }
func (r *romScan) isPtr(v uint32) bool {
	return v >= 0x08000000 && v < 0x08000000+uint32(len(r.rom))
}

// a 0xFF-terminated string starts at off and ends within max bytes
func (r *romScan) isStr(off, max int) bool {
	if off < 0 || off >= len(r.rom) || r.rom[off] == 0xFF {
		return false
	}
	for i := 0; i < max && off+i < len(r.rom); i++ {
		if r.rom[off+i] == 0xFF {
			return i > 0
		}
	}
	return false
}

// findROMAddresses: the script's addresses for a Pokémon ROM the script doesn't know, or an error
// saying why not. Cached in the settings folder by the ROM's SHA-1.
func findROMAddresses(rom []byte) (map[string]uint32, map[string]int, error) {
	sum := sha1.Sum(rom)
	cache := filepath.Join(configDir(), "rom-addresses", hex.EncodeToString(sum[:])+".json")
	type cached struct {
		Version int
		F       map[string]uint32
		Extra   map[string]int
		Err     string
	}
	const version = 5
	if b, err := os.ReadFile(cache); err == nil {
		var c cached
		if json.Unmarshal(b, &c) == nil && c.Version == version {
			if c.Err != "" {
				return nil, nil, fmt.Errorf("%s", c.Err)
			}
			return c.F, c.Extra, nil
		}
	}
	f, extra, err := scanROM(rom)
	c := cached{Version: version, F: f, Extra: extra}
	if err != nil {
		c.Err = err.Error()
	}
	if b, e := json.Marshal(c); e == nil {
		os.MkdirAll(filepath.Dir(cache), 0o755)
		os.WriteFile(cache, b, 0o644)
	}
	return f, extra, err
}

func scanROM(rom []byte) (map[string]uint32, map[string]int, error) {
	if len(rom) < 0x200 {
		return nil, nil, fmt.Errorf("not a GBA ROM")
	}
	r := &romScan{rom: rom, code: string(rom[0xAC:0xB0]), rev: int(rom[0xBC]), f: map[string]uint32{}, extra: map[string]int{}}
	r.game = map[string]string{"AXV": "RUBY", "AXP": "SAPPHIRE", "BPE": "EMERALD", "BPR": "FIRERED", "BPG": "LEAFGREEN"}[r.code[:3]]
	if r.game == "" {
		return nil, nil, fmt.Errorf("not a Pokémon Ruby, Sapphire, Emerald, FireRed or LeafGreen ROM")
	}
	r.lang = r.code[3]
	r.japanese = r.lang == 'J'
	if _, ok := langNames[r.lang]; !ok {
		return nil, nil, fmt.Errorf("this game's language (%c) isn't known", r.lang)
	}
	rs := r.game == "RUBY" || r.game == "SAPPHIRE"

	// RAM
	if rs {
		for k, v := range romfind().RSEurope { // the German layout (French, Italian and Spanish too)
			r.f[k] = v
		}
		flagGet := uint32(0)
		for k, v := range r.byCode("rs") { // from the game's own code: the Japanese games differ
			if k == "FLAG_GET" {
				flagGet = v
			} else {
				r.f[k] = v
			}
		}
		if v := r.flagsAddr(flagGet, r.f["sb1"]); v != 0 {
			r.extra["flagsOffset"] = int(v)
		}
	} else {
		known, ok := romfind().Roms[fmt.Sprintf("%s%d", r.code, r.rev)]
		if !ok {
			return nil, nil, fmt.Errorf("this revision of %s (%s v1.%d) isn't known", r.game, r.code, r.rev)
		}
		s := known.S
		r.f["main"], r.f["sb1ptr"], r.f["sb2ptr"] = s["MAIN"], s["SB1_PTR"], s["SB2_PTR"]
		r.f["party"], r.f["enemy"], r.f["mapHeader"] = s["PARTY"], s["ENEMY_PARTY"], s["MAP_HEADER"]
		r.f["battleMons"], r.f["battleStructPtr"], r.f["battleType"] = s["BATTLE_MONS"], s["BATTLE_STRUCT"], s["BATTLE_TYPE"]
		r.f["partyIdx"] = s["BATTLER_POSITIONS"] - 8 // gBattlerPartyIndexes, just before it (as in English)
		if r.game == "EMERALD" {
			r.f["opponent"] = s["WILD_ENCOUNTERS_DISABLED"] - 0x36 // gTrainerBattleOpponent_A
		} else {
			r.f["opponent"] = s["WILD_ENCOUNTERS_DISABLED"] - 0x2E
		}
		r.f["speciesNames"], r.f["speciesInfo"] = s["SPECIES_NAMES"], s["SPECIES_INFO"]
		if v := r.flagsOffset(s["FLAG_GET"]); v != 0 {
			r.extra["flagsOffset"] = int(v)
		}
		// Wonder Cards: running a card's script (wondercards.go and the script's wcStart)
		r.f["scriptCtx"], r.f["scriptStatus"], r.f["lockControls"] = s["SCRIPT_CONTEXT"], s["SCRIPT_STATUS"], s["CONTROLS_LOCKED"]
		r.f["cb1Overworld"], r.f["cb2Overworld"] = s["CB1_OVERWORLD"], s["CB2_OVERWORLD"]
		r.f["helpRDisable"] = s["HELP_R_DISABLE"]
		if s["INTR_VBLANK"] != 0 {
			r.f["intrTable"] = s["INTR_VBLANK"] - 0x10 // gIntrTable: its V-blank handler is the 5th
		}
		if t, e := r.scriptCmdTable(s["SCRIPT_CONTEXT"]); t != 0 {
			r.f["cmdTable"], r.f["cmdTableEnd"] = t, e
			r.f["ramScriptRet"] = r.ramScriptRet(t)
		}
		r.f["abilityNames"], r.f["natureNames"] = s["ABILITY_NAMES"], s["NATURE_NAMES"]
		// the map's places and map groups: in the literal pools of GetMapName and Overworld_GetMapHeaderByGroupAndId
		if g := r.poolTable(s["GET_MAP_HEADER"], r.isMapGroups); g != 0 {
			r.f["mapGroups"] = g
		}
		if r.game == "EMERALD" {
			if m := r.poolTable(s["GET_MAP_NAME"], func(a uint32) bool { return r.isMapEntries(a, 0xD5) }); m != 0 {
				r.f["mapEntries"] = m
			}
		} else if m := r.poolTable(s["GET_MAP_NAME"], r.isMapNames); m != 0 {
			r.f["mapNames"] = m
		}
	}
	if v := r.findRNG(); v != 0 {
		r.f["rng"] = v
	}

	// names
	names := langNames[r.lang]
	if r.f["speciesInfo"] == 0 {
		r.f["speciesInfo"] = r.findBaseStats()
	}
	if r.f["speciesNames"] == 0 {
		if a, s := r.findNameTable(names[0], names[1]); a != 0 {
			r.f["speciesNames"], r.extra["speciesStride"] = a, s
		}
	}
	if r.extra["speciesStride"] == 0 && r.f["speciesNames"] != 0 {
		r.extra["speciesStride"] = r.strideOf(r.f["speciesNames"], []int{11, 6}, 400)
	}
	if r.f["abilityNames"] == 0 {
		if a, s := r.findNameTable(names[2], names[3]); a != 0 {
			r.f["abilityNames"], r.extra["abilityStride"] = a, s
		}
	}
	if r.extra["abilityStride"] == 0 && r.f["abilityNames"] != 0 {
		r.extra["abilityStride"] = r.strideOf(r.f["abilityNames"], []int{13, 7}, 70)
	}
	if r.f["natureNames"] == 0 {
		r.f["natureNames"] = r.findNatureNames(names[4])
	}

	// where gMain keeps "in battle" (the Japanese games' gMain is laid out a little differently)
	fam := map[string]string{"RUBY": "rs", "SAPPHIRE": "rs", "EMERALD": "emerald", "FIRERED": "frlg", "LEAFGREEN": "frlg"}[r.game]
	if v := r.byCode(fam)["inBattle"]; v >= 0x400 && v < 0x480 {
		r.extra["inBattle"] = int(v)
	}
	delete(r.f, "inBattle")

	// tables found by their shape
	r.findPictures()
	r.findTrainers()
	if rs && r.f["mapEntries"] == 0 {
		r.f["mapEntries"] = r.findMapEntries(0x58)
	}
	if r.f["mapGroups"] == 0 {
		r.f["mapGroups"] = r.findMapGroups()
	}

	// the region map's pictures and layouts: the same bytes in every language
	fpFam := map[string]string{"RUBY": "RS", "SAPPHIRE": "RS", "EMERALD": "EMERALD", "FIRERED": "FRLG", "LEAFGREEN": "FRLG"}[r.game]
	for field, fp := range romfind().FP[fpFam] {
		pre, _ := hex.DecodeString(fp.Prefix)
		for off := 0; ; {
			i := bytes.Index(r.rom[off:], pre)
			if i < 0 {
				break
			}
			at := off + i
			if at%2 == 0 && at+fp.Size <= len(r.rom) && crc32.ChecksumIEEE(r.rom[at:at+fp.Size]) == fp.CRC {
				r.f[field] = uint32(at) + 0x08000000
				break
			}
			off = at + 1
		}
	}

	// what the screens can't do without
	for _, need := range []string{"main", "party", "enemy", "speciesNames", "speciesInfo", "natureNames", "abilityNames"} {
		if r.f[need] == 0 {
			r.missing = append(r.missing, need)
		}
	}
	if len(r.missing) > 0 {
		return nil, nil, fmt.Errorf("couldn't find %s in this ROM", strings.Join(r.missing, ", "))
	}
	for k, v := range r.f {
		if v == 0 {
			delete(r.f, k)
		}
	}
	if r.japanese {
		r.extra["japanese"] = 1
	}
	return r.f, r.extra, nil
}

// poolTable: the routine's literal pool words that are ROM addresses, the first that ok() takes
func (r *romScan) poolTable(routine uint32, ok func(uint32) bool) uint32 {
	if routine == 0 {
		return 0
	}
	start := r.at(routine &^ 1)
	for i := start; i < start+0x100 && i+2 <= len(r.rom); i += 2 {
		hw := binary.LittleEndian.Uint16(r.rom[i:])
		if hw&0xF800 != 0x4800 { // ldr rX, [pc, #imm]
			continue
		}
		slot := ((i + 4) &^ 3) + int(hw&0xFF)*4
		if v := r.u32(slot); r.isPtr(v) && ok(v) {
			return v
		}
	}
	return 0
}

// Random(): ldr gRngValue; ... 0x41C64E6D, 0x6073 in its pool, with the RNG's address beside them
func (r *romScan) findRNG() uint32 {
	mul := []byte{0x6D, 0x4E, 0xC6, 0x41}
	for off := 0; ; {
		i := bytes.Index(r.rom[off:], mul)
		if i < 0 {
			return 0
		}
		at := off + i
		off = at + 1
		if at%4 != 0 {
			continue
		}
		hasAdd := false
		for d := -8; d <= 8; d += 4 {
			if r.u32(at+d) == 0x6073 {
				hasAdd = true
			}
		}
		if !hasAdd {
			continue
		}
		for d := -8; d <= 8; d += 4 {
			if v := r.u32(at + d); v >= 0x03000000 && v < 0x03008000 {
				return v
			}
		}
	}
}

// species 1's base stats (45/49/49/45/65/65), then species 2's: the table is a record before
func (r *romScan) findBaseStats() uint32 {
	b1 := []byte{45, 49, 49, 45, 65, 65}
	b2 := []byte{60, 62, 63, 60, 80, 80}
	for off := 0; ; {
		i := bytes.Index(r.rom[off:], b1)
		if i < 0 {
			return 0
		}
		at := off + i
		off = at + 1
		if at%4 == 0 && bytes.Equal(r.rom[at+28:at+34], b2) {
			return uint32(at-28) + 0x08000000
		}
	}
}

// a table of fixed-size names: where name 1 is, and the size from where name 2 is
func (r *romScan) findNameTable(first, second string) (uint32, int) {
	a, b := gameText(first, r.japanese), gameText(second, r.japanese)
	if a == nil || b == nil {
		return 0, 0
	}
	for off := 0; ; {
		i := bytes.Index(r.rom[off:], a)
		if i < 0 {
			return 0, 0
		}
		at := off + i
		off = at + 1
		for _, stride := range []int{11, 13, 6, 7, 12, 8} {
			if stride >= len(a) && bytes.HasPrefix(r.rom[at+stride:], b) && at-stride >= 0 {
				return uint32(at-stride) + 0x08000000, stride
			}
		}
	}
}

// which stride makes every one of the first n entries a string
func (r *romScan) strideOf(base uint32, strides []int, n int) int {
	for _, s := range strides {
		ok := true
		for i := 1; i <= n && ok; i++ {
			ok = r.isStr(r.at(base)+s*i, s)
		}
		if ok {
			return s
		}
	}
	return strides[0]
}

// the nature names: a table of 25 pointers to strings, the first to nature 0's name
func (r *romScan) findNatureNames(first string) uint32 {
	a := gameText(first, r.japanese)
	if a == nil {
		return 0
	}
	for off := 0; ; {
		i := bytes.Index(r.rom[off:], a)
		if i < 0 {
			return 0
		}
		at := off + i
		off = at + 1
		var p [4]byte
		binary.LittleEndian.PutUint32(p[:], uint32(at)+0x08000000)
		for o2 := 0; ; {
			j := bytes.Index(r.rom[o2:], p[:])
			if j < 0 {
				break
			}
			t := o2 + j
			o2 = t + 1
			if t%4 != 0 {
				continue
			}
			good := true
			for k := 1; k < 25 && good; k++ {
				v := r.u32(t + 4*k)
				good = r.isPtr(v) && r.isStr(r.at(v), 16)
			}
			if good {
				return uint32(t) + 0x08000000
			}
		}
	}
}

// Front pictures, palettes, shiny palettes: tables of {pointer, (size,) tag} where the tag is the
// species (500 + the species for shiny palettes); party icons: 440 pointers to 1 KB pictures,
// then a byte per species (its palette, 0-5), then the 6 palettes.
func (r *romScan) findPictures() {
	tagTable := func(tagAt, tagBase int, size uint16) uint32 {
		for at := 0; at+8*30 < len(r.rom); at += 4 {
			ok := true
			for i := 1; i <= 25 && ok; i++ {
				e := at + 8*i
				ok = r.isPtr(r.u32(e)) && int(binary.LittleEndian.Uint16(r.rom[e+tagAt:])) == tagBase+i
				if ok && size != 0 {
					ok = binary.LittleEndian.Uint16(r.rom[e+4:]) == size
				}
			}
			if ok {
				return uint32(at) + 0x08000000
			}
		}
		return 0
	}
	r.f["frontPics"] = tagTable(6, 0, 0x800)
	r.f["palettes"] = tagTable(4, 0, 0)
	r.f["shinyPalettes"] = tagTable(4, 500, 0)
	if r.f["palettes"] != 0 && r.f["palettes"] == r.f["frontPics"] {
		r.f["palettes"] = 0
	}
	// icons: the 6 icon palettes {pointer, tag 0xDAC0 + i}; before them a palette byte for each
	// of the 440 icons, and before that the 440 icon pointers
	for at := 0; at+48 < len(r.rom); at += 4 {
		ok := true
		for i := 0; i < 6 && ok; i++ {
			ok = r.isPtr(r.u32(at+8*i)) && binary.LittleEndian.Uint16(r.rom[at+8*i+4:]) == uint16(0xDAC0+i)
		}
		if !ok {
			continue
		}
		idx, icons := at-440, at-440-4*440
		good := icons >= 0
		for i := 0; i < 440 && good; i++ {
			good = r.rom[idx+i] <= 5 && r.isPtr(r.u32(icons+4*i))
		}
		if good {
			r.f["icons"] = uint32(icons) + 0x08000000
			r.f["iconPalIdx"] = uint32(idx) + 0x08000000
			r.f["iconPals"] = uint32(at) + 0x08000000
			break
		}
	}
}

// gTrainers: records with a party size (1-6) and a party pointer at their end; the class names:
// the run of fixed-size names that the trainers' classes index into.
func (r *romScan) findTrainers() {
	type layout struct{ size, partySize, party, name, nameLen int }
	layouts := []layout{{40, 32, 36, 4, 12}, {36, 28, 32, 4, 8}, {32, 24, 28, 4, 6}}
	for _, l := range layouts {
		for at := 0; at+l.size*60 < len(r.rom); at += 4 {
			// trainer 0 is nobody: no party
			if r.u32(at+l.partySize) != 0 || r.u32(at+l.party) != 0 || r.rom[at+1] != 0 {
				continue
			}
			good := 0
			for i := 1; i <= 50; i++ {
				e := at + l.size*i
				n := r.u32(e + l.partySize)
				if n >= 1 && n <= 6 && r.isPtr(r.u32(e+l.party)) && r.rom[e+1] < 0x80 {
					good++
				}
			}
			if good >= 45 {
				r.f["trainers"] = uint32(at) + 0x08000000
				r.extra["trainerSize"], r.extra["trainerName"], r.extra["trainerNameLen"] = l.size, l.name, l.nameLen
				break
			}
		}
		if r.f["trainers"] != 0 {
			break
		}
	}
	if r.f["trainers"] == 0 {
		return
	}
	// classes used by the trainers
	maxClass := 0
	for i := 1; i < 600; i++ {
		e := r.at(r.f["trainers"]) + r.extra["trainerSize"]*i
		if e+40 > len(r.rom) || r.u32(e+r.extra["trainerSize"]-8) > 6 {
			break
		}
		if r.rom[e+1] >= 0x80 {
			continue
		}
		maxClass = max(maxClass, int(r.rom[e+1]))
	}
	// a run of maxClass+1 names, just before the trainers in every English build
	for _, stride := range []int{13, 11, 7} { // 11: the Japanese Ruby/Sapphire
		valid := func(e int) bool {
			if e < 0 || !r.isStr(e, stride) || r.rom[e] == 0x00 {
				return false
			}
			end := bytes.IndexByte(r.rom[e:e+stride], 0xFF)
			for k := end + 1; k < stride; k++ {
				if r.rom[e+k] != 0x00 {
					return false
				}
			}
			return true
		}
		for back := 1; back < 0x2000; back++ {
			base := r.at(r.f["trainers"]) - back
			ok := base >= 0
			for i := 0; i <= maxClass && ok; i++ {
				e := base + stride*i
				ok = r.isStr(e, stride) && r.rom[e] != 0x00
				end := bytes.IndexByte(r.rom[e:e+stride], 0xFF)
				for k := end + 1; ok && k < stride; k++ {
					ok = r.rom[e+k] == 0x00
				}
			}
			if ok && base+stride*(maxClass+1) <= r.at(r.f["trainers"]) {
				for valid(base - stride) { // the table may start earlier than the classes in use need
					base -= stride
				}
				r.f["classNames"], r.extra["classStride"] = uint32(base)+0x08000000, stride
				return
			}
		}
	}
}

// Ruby/Sapphire/Emerald's region map entries: {x, y, w, h, name pointer} for each place
func (r *romScan) isMapEntries(a uint32, n int) bool {
	at := r.at(a)
	for i := 0; i < n; i++ {
		e := at + 8*i
		if e+8 > len(r.rom) || r.rom[e] >= 28 || r.rom[e+1] >= 15 || r.rom[e+2] == 0 || r.rom[e+3] == 0 || r.rom[e+2] > 28 || r.rom[e+3] > 15 {
			return false
		}
		if v := r.u32(e + 4); !r.isPtr(v) || !(r.isStr(r.at(v), 24) || r.rom[r.at(v)] == 0xFF) {
			return false
		}
	}
	return true
}

func (r *romScan) findMapEntries(n int) uint32 {
	for at := 0; at+8*n < len(r.rom); at += 4 {
		if r.isMapEntries(uint32(at)+0x08000000, n) {
			return uint32(at) + 0x08000000
		}
	}
	return 0
}

// FireRed/LeafGreen's place names: 109 pointers to strings
func (r *romScan) isMapNames(a uint32) bool {
	at := r.at(a)
	for i := 0; i < 109; i++ {
		v := r.u32(at + 4*i)
		if !r.isPtr(v) || !r.isStr(r.at(v), 32) {
			return false
		}
	}
	return true
}

// gMapGroups: pointers to arrays of map header pointers; a map header starts with its layout
func (r *romScan) isMapGroups(a uint32) bool {
	at := r.at(a)
	for g := 0; g < 25; g++ {
		v := r.u32(at + 4*g)
		if !r.isPtr(v) {
			return false
		}
		h := r.u32(r.at(v))
		if !r.isPtr(h) || !r.isPtr(r.u32(r.at(h))) {
			return false
		}
	}
	return true
}

func (r *romScan) findMapGroups() uint32 {
	for at := 0; at+4*34 < len(r.rom); at += 4 {
		if r.isMapGroups(uint32(at) + 0x08000000) {
			return uint32(at) + 0x08000000
		}
	}
	return 0
}

// luaFound: the script's OVERRIDE table for a ROM found by findROMAddresses
func luaFound(rom []byte, f map[string]uint32, extra map[string]int, gameName string) string {
	parts := []string{fmt.Sprintf("code = %q", string(rom[0xAC:0xB0])), fmt.Sprintf("game = %q", gameName),
		"found = true"}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s = 0x%08X", k, f[k]))
	}
	keys = keys[:0]
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s = %d", k, extra[k]))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// foundOverride: for a Pokémon game in a language the script has no addresses for (all but
// English, and German Ruby/Sapphire), the ones found in its ROM, as the script's OVERRIDE.
// "" if it isn't needed, or they can't be found (the Log says why).
func foundOverride(rom, code string, log func(string, ...any)) string {
	if len(code) != 4 {
		return ""
	}
	game := gameByCode[strings.ToUpper(code[:3])]
	lang := code[3]
	if game == "" || (lang == 'E' || lang == 'D' && (game == "RUBY" || game == "SAPPHIRE")) && !testFind {
		return ""
	}
	name := titleCase(game)
	if n, ok := langNamesEN[lang]; ok {
		name = n + " " + name
	}
	data, err := os.ReadFile(rom)
	if err != nil {
		log("Couldn't read %s (%v).", filepath.Base(rom), err)
		return ""
	}
	f, extra, err := findROMAddresses(data)
	if err != nil {
		log("Status screens: %s isn't supported (%v). The GBA still works as a controller.", name, err)
		return ""
	}
	log("Status screens: found where %s keeps its data.", name)
	return luaFound(data, f, extra, game)
}

// flagsOffset: where SaveBlock1 keeps the event flags, from FlagGet: it calls GetFlagPointer, whose
// literal pool holds the offset (0x1270 in Emerald, 0xEE0 in FireRed/LeafGreen; the Japanese games'
// save is laid out a little differently). 0 if it isn't found.
func (r *romScan) flagsOffset(flagGet uint32) uint32 { return r.flagsAddr(flagGet, 0) }

// flagsAddr: as flagsOffset; with sb1 (Ruby/Sapphire's fixed gSaveBlock1) the pool may hold the
// flags' own address instead
func (r *romScan) flagsAddr(flagGet, sb1 uint32) uint32 {
	if flagGet == 0 {
		return 0
	}
	start := r.at(flagGet &^ 1)
	for i := start; i < start+0x20 && i+4 <= len(r.rom); i += 2 {
		hi := binary.LittleEndian.Uint16(r.rom[i:])
		lo := binary.LittleEndian.Uint16(r.rom[i+2:])
		if hi&0xF800 != 0xF000 || lo&0xF800 != 0xF800 { // bl GetFlagPointer
			continue
		}
		off := int(hi&0x7FF)<<12 | int(lo&0x7FF)<<1
		if off&(1<<22) != 0 {
			off -= 1 << 23
		}
		target := i + 4 + off
		for j := target; j >= 0 && j < target+0xC0 && j+4 <= len(r.rom); j += 2 {
			hw := binary.LittleEndian.Uint16(r.rom[j:])
			if hw&0xF800 == 0x4800 { // ldr rX, =offset
				v := r.u32(((j + 4) &^ 3) + int(hw&0xFF)*4)
				if sb1 != 0 && v > sb1 && v-sb1 >= 0x400 && v-sb1 < 0x2000 {
					return v - sb1
				}
				if v >= 0x400 && v < 0x2000 {
					return v
				}
			}
			// movs rX, #n; lsls rX, rX, #k (FireRed/LeafGreen: 0xEE << 4)
			next := binary.LittleEndian.Uint16(r.rom[j+2:])
			if rd := (hw >> 8) & 7; hw&0xF800 == 0x2000 && next&0xF800 == 0x0000 && next&7 == rd && (next>>3)&7 == rd {
				if v := uint32(hw&0xFF) << ((next >> 6) & 31); v >= 0x400 && v < 0x2000 {
					return v
				}
			}
		}
		return 0
	}
	return 0
}

// scriptCmdTable: gScriptCmdTable and its end, from the literal pool that sets up the global script
// context (ScriptContext_Init: the context's address next to both), checked to be the handlers.
func (r *romScan) scriptCmdTable(ctx uint32) (uint32, uint32) {
	if ctx == 0 {
		return 0, 0
	}
	var w [4]byte
	binary.LittleEndian.PutUint32(w[:], ctx)
	for off := 0; ; {
		i := bytes.Index(r.rom[off:], w[:])
		if i < 0 {
			return 0, 0
		}
		at := off + i
		off = at + 1
		if at%4 != 0 {
			continue
		}
		for a := at - 24; a <= at+24; a += 4 {
			t := r.u32(a)
			if !r.isPtr(t) {
				continue
			}
			for b := at - 24; b <= at+24; b += 4 {
				e := r.u32(b)
				n := int(e-t) / 4
				if e <= t || (e-t)%4 != 0 || n < 150 || n > 300 {
					continue
				}
				ok := true
				for k := 0; k < n && ok; k++ {
					h := r.u32(r.at(t) + 4*k)
					ok = h&1 == 1 && r.isPtr(h)
				}
				if ok {
					return t, e
				}
			}
		}
	}
}

// ramScriptRet: gRamScriptRetAddr, which script command 0x0C (gotoram) sets
func (r *romScan) ramScriptRet(table uint32) uint32 {
	h := r.u32(r.at(table)+4*0x0C) &^ 1
	start := r.at(h)
	for j := start; j >= 0 && j < start+0x40 && j+2 <= len(r.rom); j += 2 {
		hw := binary.LittleEndian.Uint16(r.rom[j:])
		if hw&0xF800 == 0x4800 {
			if v := r.u32(((j + 4) &^ 3) + int(hw&0xFF)*4); v >= 0x02000000 && v < 0x03008000 {
				return v
			}
		}
	}
	return 0
}

var testFind bool // tests: GBLINK_TEST_FIND, find the addresses even for a game the script knows

// byCode: for a game family, Ruby/Sapphire's RAM addresses, read from the literal pools of English Ruby's routines
// found in this ROM by their code (romfind.json rsSigs). Each field takes the address most of its
// routines agree on. FLAG_GET is the address of FlagGet.
func (r *romScan) byCode(family string) map[string]uint32 {
	votes := map[string]map[uint32]int{}
	for _, sg := range romfind().RSSigs {
		if sg.Family != family {
			continue
		}
		code, _ := hex.DecodeString(sg.Code)
		mask, _ := hex.DecodeString(sg.Mask)
		at := r.findMasked(code, mask)
		if at < 0 {
			continue
		}
		v := uint32(at) + 0x08000000
		if sg.Slot >= 0 {
			hw := binary.LittleEndian.Uint16(r.rom[at+sg.Slot:])
			if hw&0xF800 != 0x4800 {
				continue
			}
			v = r.u32(((at + sg.Slot + 4) &^ 3) + int(hw&0xFF)*4)
		}
		if votes[sg.Field] == nil {
			votes[sg.Field] = map[uint32]int{}
		}
		votes[sg.Field][v]++
	}
	out := map[string]uint32{}
	for f, vs := range votes {
		best, n := uint32(0), 0
		for v, c := range vs {
			if c > n || c == n && v < best {
				best, n = v, c
			}
		}
		out[f] = best
	}
	return out
}

// findMasked: where code (with mask) is in the ROM, once only; -1 if nowhere or more than once
func (r *romScan) findMasked(code, mask []byte) int {
	k := 0
	for k < len(mask) && mask[k] == 0xFF && k < 12 {
		k++
	}
	found := -1
	for off := 0; ; {
		i := bytes.Index(r.rom[off:], code[:k])
		if i < 0 {
			return found
		}
		at := off + i
		off = at + 1
		if at%2 != 0 || at+len(code) > len(r.rom) {
			continue
		}
		ok := true
		for j := k; j < len(code) && ok; j++ {
			ok = r.rom[at+j]&mask[j] == code[j]&mask[j]
		}
		if ok {
			if found >= 0 {
				return -1
			}
			found = at
		}
	}
}
