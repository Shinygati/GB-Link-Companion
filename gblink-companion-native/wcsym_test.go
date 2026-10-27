package main

import (
	"encoding/binary"
	"testing"
)

// asm puts Thumb halfwords at addr and the literal pool words right after (4-aligned); the ldr
// instructions' offsets are filled in from lits' indexes (ldrIdx: halfword index -> literal index).
func asm(rom []byte, addr uint32, code []uint16, lits []uint32, ldrIdx map[int]int) uint32 {
	off := int(addr - 0x08000000)
	pool := (off + 2*len(code) + 3) &^ 3
	for i, c := range code {
		if li, ok := ldrIdx[i]; ok {
			pc := (off + 2*i + 4) &^ 3
			c |= uint16((pool + 4*li - pc) / 4)
		}
		binary.LittleEndian.PutUint16(rom[off+2*i:], c)
	}
	for i, v := range lits {
		binary.LittleEndian.PutUint32(rom[pool+4*i:], v)
	}
	return uint32(pool + 4*len(lits) - off)
}

func TestDeriveWCOffsets(t *testing.T) {
	rom := make([]byte, 0x20000)
	syms, sizes := map[string]uint32{}, map[string]uint32{}
	put := func(name string, addr uint32, code []uint16, lits []uint32, ldr map[int]int) {
		syms[name], sizes[name] = addr, asm(rom, addr, code, lits, ldr)
	}
	// a hack whose save moved everything up by 0x100 (Emerald: mg 0x322C, rs 0x3728, flags 0x1270, vars 0x139C)
	const d = 0x100
	ptr := uint32(0x03005D8C)
	// GetSavedWonderCard: ldr r0,=gSaveBlock1Ptr; ldr r0,[r0]; ldr r1,=off; adds r0,r0,r1; bx lr
	put("GetSavedWonderCard", 0x08001000, []uint16{0x4800, 0x6800, 0x4900, 0x1840, 0x4770}, []uint32{ptr, 0x322C + d + 0x1C4}, map[int]int{0: 0, 2: 1})
	put("GetSavedWonderCardMetadata", 0x08001100, []uint16{0x4800, 0x6800, 0x4900, 0x1840, 0x4770}, []uint32{ptr, 0x322C + d + 0x314}, map[int]int{0: 0, 2: 1})
	// ClearRamScript: ... ldr r2,=rs ... ; ValidateSavedRamScript uses rs + 4 (made with movs/lsls/adds)
	put("ClearRamScript", 0x08001200, []uint16{0x4800, 0x6801, 0x4A00, 0x1889, 0x4770}, []uint32{ptr, 0x3728 + d}, map[int]int{0: 0, 2: 1})
	// rs+4 = 0x382C: movs r1,#0xE0; lsls r1,r1,#6 (0x3800); adds r1,#0x2C
	put("ValidateSavedRamScript", 0x08001300, []uint16{0x4800, 0x6800, 0x21E0, 0x0189, 0x312C, 0x1840, 0x4770}, []uint32{ptr}, map[int]int{0: 0})
	put("GetFlagPointer", 0x08001400, []uint16{0x4800, 0x6800, 0x4900, 0x1840, 0x4770}, []uint32{ptr, 0x1270 + d}, map[int]int{0: 0, 2: 1})
	// GetVarPointer, folded: vars - 0x8000 (+ 2*id)
	varsFolded := uint32(0x139C + d)
	varsFolded -= 0x8000
	put("GetVarPointer", 0x08001500, []uint16{0x4800, 0x6800, 0x4900, 0x1840, 0x4770}, []uint32{ptr, varsFolded}, map[int]int{0: 0, 2: 1})
	sizes["gSaveblock1"] = 0x3E08 + d

	o, err := deriveWCOffsets(rom, syms, sizes)
	if err != nil {
		t.Fatal(err)
	}
	if o.mg != 0x322C+d || o.rs != 0x3728+d || o.flags != 0x1270+d || o.vars != 0x139C+d {
		t.Fatalf("got %+v", o)
	}
	// the metadata doesn't agree: no cards
	binary.LittleEndian.PutUint32(rom[0x1100+16:], 0x5000)
	if _, err := deriveWCOffsets(rom, syms, sizes); err == nil {
		t.Fatal("disagreeing routines were accepted")
	}
	// a .sym without the routines: no cards
	if _, err := deriveWCOffsets(rom, map[string]uint32{}, sizes); err == nil {
		t.Fatal("no routines, but accepted")
	}
}
