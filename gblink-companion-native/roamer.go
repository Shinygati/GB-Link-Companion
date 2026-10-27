// Roaming Pokémon (Latias/Latios in Hoenn, Entei/Raikou/Suicune in Kanto) on the GBA's map screen.
//
// The roamer itself is in the save (SaveBlock1's struct Roamer, read by the mGBA script), but where
// it is now isn't: the games keep that in sRoamerLocation, a 2-byte map group/number in EWRAM.
// The script has its address for the English games (pret's symbol files) and a .sym gives it for
// an own build; for every other ROM (other languages, ROM hacks) it's found here, in the code of
// the game's GetRoamerLocation:
//
//	void GetRoamerLocation(u8 *mapGroup, u8 *mapNum)
//	{
//	    *mapGroup = sRoamerLocation[MAP_GRP];
//	    *mapNum = sRoamerLocation[MAP_NUM];
//	}
//
// That's "ldr rB, =sRoamerLocation", two ldrb from [rB, #0] and [rB, #1] stored to [r0] and [r1],
// then "bx lr", whatever registers the compiler picked. Other routines can look the same (copying
// another 2-byte location), so of the matches the one used most by the rest of the code wins: the
// game's roamer code refers to sRoamerLocation (and sLocationHistory just before it) many times.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

func findRoamerLocation(rom []byte) uint32 {
	h := func(off int) uint16 { return binary.LittleEndian.Uint16(rom[off:]) }
	isEWRAM := func(a uint32) bool { return a >= 0x02000000 && a < 0x02040000 }
	cands := map[uint32]bool{}
	for off := 0; off+16 <= len(rom); off += 2 {
		ins := h(off)
		if ins&0xF800 != 0x4800 { // LDR rB, [PC, #imm]
			continue
		}
		base := int(ins>>8) & 7
		lit := ((off + 4) &^ 3) + int(ins&0xFF)*4
		if lit+4 > len(rom) || lit <= off {
			continue
		}
		a := binary.LittleEndian.Uint32(rom[lit:])
		if !isEWRAM(a) {
			continue
		}
		val := map[int]uint32{} // what each register holds: an address in sRoamerLocation's 2 bytes
		var stored [2]uint32
		ok := false
	walk:
		for k := 1; k <= 6 && off+2*k+2 <= lit; k++ {
			x := h(off + 2*k)
			rd, rn, imm := int(x&7), int(x>>3)&7, uint32(x>>6)&31
			switch {
			case x == 0x4770: // BX LR: done
				ok = stored[0] == a && stored[1] == a+1
				break walk
			case x&0xF800 == 0x7800 && rn == base && imm <= 1: // LDRB rd, [rB, #0/1]
				val[rd] = a + imm
				if rd == base {
					base = -1
				}
			case x&0xF800 == 0x7000 && imm == 0 && rn <= 1: // STRB rd, [r0/r1]
				v, have := val[rd]
				if !have {
					break walk
				}
				stored[rn] = v
			default:
				break walk
			}
		}
		if ok {
			cands[a] = true
		}
	}
	if len(cands) == 0 {
		return 0
	}
	// how often each candidate (and the 6 bytes before it, sLocationHistory) is in a literal pool
	uses := map[uint32]int{}
	for a := range cands {
		uses[a], uses[a-6] = 0, 0
	}
	for off := 0; off+4 <= len(rom); off += 4 {
		if v := binary.LittleEndian.Uint32(rom[off:]); isEWRAM(v) {
			if _, want := uses[v]; want {
				uses[v]++
			}
		}
	}
	best, bestN := uint32(0), 0
	for a := range cands {
		if n := uses[a] + uses[a-6]; n > bestN || (n == bestN && a < best) {
			best, bestN = a, n
		}
	}
	if bestN < 3 { // the roamer code uses it more than that
		return 0
	}
	return best
}

var roamerCache sync.Map // ROM path -> uint32 (0: not found)

// roamerLocationFor: sRoamerLocation's address in a ROM (found once a session), or 0.
func roamerLocationFor(romPath string) uint32 {
	if v, ok := roamerCache.Load(romPath); ok {
		return v.(uint32)
	}
	var a uint32
	if rom, err := os.ReadFile(romPath); err == nil {
		a = findRoamerLocation(rom)
	}
	roamerCache.Store(romPath, a)
	return a
}

// roamerOption: the script's line telling it where the roamer's location is ("" if not found).
func roamerOption(romPath string) string {
	if romPath == "" {
		return ""
	}
	if a := roamerLocationFor(romPath); a != 0 {
		return fmt.Sprintf("Q%08X\n", a)
	}
	return ""
}
