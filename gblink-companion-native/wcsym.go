// Your own Wonder Cards (.wc3) for ROM hacks that come with a .sym file.
//
// The GB-Link team's cards are made for the exact original ROMs, so they only go to verified
// dumps. A hack built from pokeemerald or pokefirered can still take a card made for it, but the
// mGBA script writes the card into the save, and a hack may have moved things around in it. So the
// places in SaveBlock1 that receiving a card touches are read from the hack's own code, at the
// addresses its .sym gives, each checked against a second routine that must agree:
//
//   - the Mystery Gift card: GetSavedWonderCard returns &gSaveBlock1Ptr->mysteryGift.card
//     (the save's card + 0x1C4), GetSavedWonderCardMetadata the metadata (+ 0x314)
//   - the RAM script: ClearRamScript clears &gSaveBlock1Ptr->ramScript; ValidateSavedRamScript
//     reads it (and its data, 4 bytes on)
//   - the event flags: GetFlagPointer; the variables: GetVarPointer (optional: without them the
//     Mystery Gift variables aren't cleared, as the game would)
//
// If the card or the RAM script can't be pinned down, cards aren't offered for the hack.
package main

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	mgCardOff = 0x1C4 // struct MysteryGiftSave: the card (after its CRC)
	mgMetaOff = 0x314 // ...and the card's metadata
)

// wcSymOffsets: where a hack keeps what receiving a card touches, in SaveBlock1.
type wcSymOffsets struct {
	mg, rs, flags, vars uint32 // vars 0: not found
}

// funcValues: the values a short Thumb routine makes in its registers: words from its literal pool
// (ldr rX, =v), movs, lsls and adds of small numbers. Enough for "gSaveBlock1Ptr + offset".
func funcValues(rom []byte, addr, size uint32) map[uint32]bool {
	out := map[uint32]bool{}
	start := int(addr&^1) - 0x08000000
	if start < 0 || size == 0 || size > 0x400 || start+int(size) > len(rom) {
		return out
	}
	var reg [8]uint32
	var known [8]bool
	set := func(r int, v uint32) { reg[r], known[r] = v, true; out[v] = true }
	for i := start; i+2 <= start+int(size); i += 2 {
		x := binary.LittleEndian.Uint16(rom[i:])
		rd := int(x & 7)
		switch {
		case x&0xF800 == 0x4800: // ldr rd, [pc, #imm]
			r := int(x>>8) & 7
			if lit := ((i + 4) &^ 3) + int(x&0xFF)*4; lit+4 <= len(rom) {
				set(r, binary.LittleEndian.Uint32(rom[lit:]))
			}
		case x&0xF800 == 0x2000: // movs rd, #imm8
			set(int(x>>8)&7, uint32(x&0xFF))
		case x&0xF800 == 0x3000: // adds rd, #imm8
			if r := int(x>>8) & 7; known[r] {
				set(r, reg[r]+uint32(x&0xFF))
			}
		case x&0xF800 == 0x0000 && x>>6&31 != 0: // lsls rd, rm, #k
			if rm := int(x>>3) & 7; known[rm] {
				set(rd, reg[rm]<<(x>>6&31))
			}
		case x&0xFE00 == 0x1C00: // adds rd, rn, #imm3
			if rn := int(x>>3) & 7; known[rn] {
				set(rd, reg[rn]+uint32(x>>6&7))
			}
		case x&0xFE00 == 0x1800: // adds rd, rn, rm: both known
			rn, rm := int(x>>3)&7, int(x>>6)&7
			if known[rn] && known[rm] {
				set(rd, reg[rn]+reg[rm])
			} else {
				known[rd] = false
			}
		case x&0xFF00 == 0x4600: // mov rd, rm (high registers aside)
			d, m := int(x&7)|int(x>>4)&8, int(x>>3)&15
			if d < 8 && m < 8 && known[m] {
				set(d, reg[m])
			} else if d < 8 {
				known[d] = false
			}
		default:
			if x&0xE000 == 0x6000 || x&0xF000 == 0x8000 || x&0xF800 == 0x5800 { // loads/stores
				if x&0x0800 != 0 { // a load: rd now holds memory
					known[rd] = false
				}
			} else if x&0xFC00 == 0x4000 { // ALU ops on rd
				known[rd] = false
			}
		}
	}
	return out
}

// deriveWCOffsets reads the offsets from the hack's code (see the top of this file).
func deriveWCOffsets(rom []byte, syms, sizes map[string]uint32) (wcSymOffsets, error) {
	var o wcSymOffsets
	vals := func(name string) map[uint32]bool {
		a, ok := syms[name]
		if !ok {
			return nil
		}
		n := sizes[name]
		if n == 0 {
			n = 0x60
		}
		return funcValues(rom, a, n)
	}
	inSave := func(v uint32) bool { return v >= 0x200 && v < 0x8000 }

	card, meta := vals("GetSavedWonderCard"), vals("GetSavedWonderCardMetadata")
	if card == nil || meta == nil {
		return o, fmt.Errorf("the .sym has no GetSavedWonderCard / GetSavedWonderCardMetadata")
	}
	for v := range card {
		if inSave(v) && v >= mgCardOff && meta[v-mgCardOff+mgMetaOff] {
			if o.mg != 0 && o.mg != v-mgCardOff {
				return o, fmt.Errorf("couldn't tell where the save keeps the Wonder Card")
			}
			o.mg = v - mgCardOff
		}
	}
	if o.mg == 0 {
		return o, fmt.Errorf("couldn't find where the save keeps the Wonder Card")
	}

	clr, valid := vals("ClearRamScript"), vals("ValidateSavedRamScript")
	if clr == nil || valid == nil {
		return o, fmt.Errorf("the .sym has no ClearRamScript / ValidateSavedRamScript")
	}
	for v := range clr {
		if inSave(v) && v > o.mg && (valid[v] || valid[v+4]) {
			if o.rs != 0 && o.rs != v {
				return o, fmt.Errorf("couldn't tell where the save keeps the card's script")
			}
			o.rs = v
		}
	}
	if o.rs == 0 {
		return o, fmt.Errorf("couldn't find where the save keeps the card's script")
	}

	var flagCands []uint32
	for v := range vals("GetFlagPointer") {
		if v >= 0x400 && v < o.mg {
			flagCands = append(flagCands, v)
		}
	}
	if len(flagCands) != 1 {
		return o, fmt.Errorf("couldn't find where the save keeps the event flags")
	}
	o.flags = flagCands[0]

	// the variables: vars[id - 0x4000], often folded into one number (vars - 0x8000)
	varCands := map[uint32]bool{}
	for v := range vals("GetVarPointer") {
		for _, c := range []uint32{v, v + 0x8000} {
			if c > o.flags && c < o.mg {
				varCands[c] = true
			}
		}
	}
	if len(varCands) == 1 {
		for c := range varCands {
			o.vars = c
		}
	}

	// it all has to fit in SaveBlock1
	for _, name := range []string{"gSaveBlock1", "gSaveblock1"} {
		if n := sizes[name]; n != 0 && o.rs+4+1000 > n {
			return o, fmt.Errorf("the card's script wouldn't fit in the save (%#x of %#x)", o.rs, n)
		}
	}
	return o, nil
}

// readSymbolSizes: the size column of a "make syms" file ("0801b020 g 00000014 GetSavedWonderCard").
func readSymbolSizes(path string) map[string]uint32 {
	out := map[string]uint32{}
	if strings.EqualFold(filepath.Ext(path), ".elf") {
		if f, err := elf.Open(path); err == nil {
			defer f.Close()
			if list, err := f.Symbols(); err == nil {
				for _, sy := range list {
					if _, ok := out[sy.Name]; !ok {
						out[sy.Name] = uint32(sy.Size)
					}
				}
			}
		}
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		p := strings.Fields(line)
		if len(p) != 4 {
			continue
		}
		var n uint32
		if _, err := fmt.Sscanf(p[2], "%x", &n); err == nil {
			if _, ok := out[p[3]]; !ok {
				out[p[3]] = n
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------- for the window

type hackWC struct {
	key  string // the cards' kind: an English/European or Japanese game key
	why  string // why not, if key == ""
	offs wcSymOffsets
}

var hackWCCache sync.Map // ROM path -> hackWC

// hackWonderCards: can this ROM hack take your own cards (it has a .sym that fits, and the
// offsets were found)? key "" and why if not.
func hackWonderCards(rom, code string) hackWC {
	if v, ok := hackWCCache.Load(rom); ok {
		return v.(hackWC)
	}
	h := hackWC{why: "This ROM isn't a verified original (a good dump in the No-Intro list), so the GB-Link team's cards can't go to it. " +
		"For a ROM hack: put its .sym file next to the ROM to send your own .wc3 cards."}
	if syms, path, rom := matchingSym(rom, func(string, ...any) {}); syms != nil {
		game := gameByCode[strings.ToUpper(code[:min(3, len(code))])]
		if game == "" {
			game = symGame(syms)
		}
		switch game {
		case "EMERALD", "FIRERED", "LEAFGREEN":
			if o, err := deriveWCOffsets(rom, syms, readSymbolSizes(path)); err != nil {
				h.why = "Your own Wonder Cards can't go to this ROM hack: " + err.Error() + "."
			} else {
				prefix := map[string]string{"EMERALD": "BPE", "FIRERED": "BPR", "LEAFGREEN": "BPG"}[game]
				lang := "E"
				if len(code) == 4 && code[3] == 'J' {
					lang = "J"
				}
				h = hackWC{key: prefix + lang + "0", offs: o}
			}
		default:
			h.why = "Ruby and Sapphire have no Wonder Cards: Mystery Gift came with Emerald, FireRed and LeafGreen."
		}
	}
	hackWCCache.Store(rom, h)
	return h
}

// luaWCParts: the script's fields for a hack's own cards (added to its .sym's addresses)
func luaWCParts(o wcSymOffsets) []string {
	p := []string{fmt.Sprintf("wcMg = 0x%X", o.mg), fmt.Sprintf("wcRs = 0x%X", o.rs), fmt.Sprintf("wcFlags = 0x%X", o.flags)}
	if o.vars != 0 {
		p = append(p, fmt.Sprintf("wcVars = 0x%X", o.vars))
	}
	return p
}
