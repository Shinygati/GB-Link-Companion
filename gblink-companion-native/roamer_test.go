package main

import (
	"encoding/binary"
	"testing"
)

// putRoutine writes a GetRoamerLocation-like routine at off: its halfwords, then its literal.
func putRoutine(rom []byte, off int, code []uint16, lit uint32) {
	for i, c := range code {
		binary.LittleEndian.PutUint16(rom[off+2*i:], c)
	}
	litOff := ((off + 4) &^ 3) + int(code[0]&0xFF)*4
	binary.LittleEndian.PutUint32(rom[litOff:], lit)
}

func TestFindRoamerLocation(t *testing.T) {
	rom := make([]byte, 0x10000)
	const want = 0x0203BC86
	// ldr r2,[pc,#8]; ldrb r3,[r2]; strb r3,[r0]; ldrb r2,[r2,#1]; strb r2,[r1]; bx lr; (pad) .word
	putRoutine(rom, 0x1000, []uint16{0x4A02, 0x7813, 0x7003, 0x7852, 0x700A, 0x4770}, want)
	// a decoy with another EWRAM pair, used only once
	putRoutine(rom, 0x2000, []uint16{0x4A02, 0x7813, 0x7003, 0x7852, 0x700A, 0x4770}, 0x02030000)
	// the other roamer routines' literal pools
	for i, v := range []uint32{want, want, want - 6, want - 6} {
		binary.LittleEndian.PutUint32(rom[0x3000+16*i:], v)
	}
	if got := findRoamerLocation(rom); got != want {
		t.Fatalf("found %08X, want %08X", got, want)
	}
	// the other order the compiler could pick: ldr r3; ldrb r2,[r3]; ldrb r1,[r3,#1]; strb r2,[r0]; strb r1,[r1]
	rom2 := make([]byte, 0x10000)
	putRoutine(rom2, 0x1000, []uint16{0x4B02, 0x781A, 0x7859, 0x7002, 0x7009, 0x4770}, want)
	for i := 0; i < 3; i++ {
		binary.LittleEndian.PutUint32(rom2[0x3000+16*i:], want)
	}
	if got := findRoamerLocation(rom2); got != want {
		t.Fatalf("other order: found %08X", got)
	}
	if got := findRoamerLocation(make([]byte, 0x1000)); got != 0 {
		t.Fatalf("empty ROM: %08X", got)
	}
}
