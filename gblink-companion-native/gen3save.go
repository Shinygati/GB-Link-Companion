// The save of Pokémon Emerald, FireRed and LeafGreen (128 KB flash): two copies ("slots") of 14
// 4 KB sectors, the newer one in use. The Pokémon Bank reads the PC boxes from the newer slot and
// writes its changes as a new save in the other slot, the way the games save: if writing stops
// half way, the game still loads the save that was there before.
package main

import (
	"encoding/binary"
	"fmt"
)

const (
	g3SectorSize   = 0x1000
	g3SectorData   = 0xF80 // the data part of a sector (then its footer)
	g3Sectors      = 14    // in a slot
	g3SlotSize     = g3Sectors * g3SectorSize
	g3Signature    = 0x08012025
	g3BoxCount     = 14
	g3BoxSlots     = 30
	g3BoxNameLen   = 9
	g3StorageSize  = 8*g3SectorData + 0x7D0 // struct PokemonStorage, in sectors 5-13
	g3StorageBoxes = 4                      // where boxes[][] start in it (after currentBox)
	g3StorageNames = g3StorageBoxes + g3BoxCount*g3BoxSlots*pk3Size
)

// the size of each sector's data, by its number, for each game (as PKHeX checks them too)
var g3SectorSizes = map[string][g3Sectors]int{
	"EMERALD": {0xF2C, 0xF80, 0xF80, 0xF80, 0xF08, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0x7D0},
	"FRLG":    {0xF24, 0xF80, 0xF80, 0xF80, 0xEE8, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0xF80, 0x7D0},
}

// where SaveBlock1 keeps the party (count, then the six 100-byte Pokémon)
var g3PartyOffset = map[string]int{"EMERALD": 0x234, "FRLG": 0x34}

// bankFamily: the games the Pokémon Bank works with, by their header code ("" for the rest).
// The Japanese games are left out: their names use another character set.
func bankFamily(code string) string {
	if len(code) != 4 || code[3] == 'J' {
		return ""
	}
	switch code[:3] {
	case "BPE":
		return "EMERALD"
	case "BPR", "BPG":
		return "FRLG"
	}
	return ""
}

type Gen3Save struct {
	Raw     []byte
	Family  string
	Slot    int            // 0 or 1: the slot in use
	Counter uint32         // its save counter
	sector  [g3Sectors]int // where each sector (by number) is in Raw
}

func g3Checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+4 <= len(b); i += 4 {
		sum += binary.LittleEndian.Uint32(b[i:])
	}
	return uint16(sum>>16) + uint16(sum)
}

// readSlot checks one slot: every sector there, signed, with the right checksum.
func (s *Gen3Save) readSlot(n int) (counter uint32, where [g3Sectors]int, ok bool) {
	sizes := g3SectorSizes[s.Family]
	seen := 0
	for i := 0; i < g3Sectors; i++ {
		off := n*g3SlotSize + i*g3SectorSize
		sec := s.Raw[off : off+g3SectorSize]
		id := int(binary.LittleEndian.Uint16(sec[0xFF4:]))
		if binary.LittleEndian.Uint32(sec[0xFF8:]) != g3Signature || id >= g3Sectors || seen&(1<<id) != 0 {
			return 0, where, false
		}
		if g3Checksum(sec[:sizes[id]]) != binary.LittleEndian.Uint16(sec[0xFF6:]) {
			return 0, where, false
		}
		seen |= 1 << id
		where[id] = off
		counter = binary.LittleEndian.Uint32(sec[0xFFC:])
	}
	return counter, where, true
}

// parseGen3Save finds the save in use, as the games do (the newer of the two good slots).
func parseGen3Save(raw []byte, code string) (*Gen3Save, error) {
	fam := bankFamily(code)
	if fam == "" {
		return nil, fmt.Errorf("the Pokémon Bank only works with Pokémon Emerald, FireRed and LeafGreen")
	}
	if len(raw) < 2*g3SlotSize {
		return nil, fmt.Errorf("the save is %d bytes, too small for %s", len(raw), map[string]string{"EMERALD": "Emerald", "FRLG": "FireRed or LeafGreen"}[fam])
	}
	s := &Gen3Save{Raw: append([]byte(nil), raw...), Family: fam}
	c0, w0, ok0 := s.readSlot(0)
	c1, w1, ok1 := s.readSlot(1)
	switch {
	case ok0 && ok1:
		// the newer one; the counter can wrap around (pret's GetSaveValidStatus)
		if (c0 == 0xFFFFFFFF && c1 == 0) || (c1 > c0 && !(c1 == 0xFFFFFFFF && c0 == 0)) {
			s.Slot, s.Counter, s.sector = 1, c1, w1
		} else {
			s.Slot, s.Counter, s.sector = 0, c0, w0
		}
	case ok0:
		s.Slot, s.Counter, s.sector = 0, c0, w0
	case ok1:
		s.Slot, s.Counter, s.sector = 1, c1, w1
	default:
		return nil, fmt.Errorf("there's no good save on the cartridge (a new game, a damaged save, or a ROM hack's save laid out differently)")
	}
	return s, nil
}

// storage: struct PokemonStorage, put together from sectors 5-13.
func (s *Gen3Save) storage() []byte {
	out := make([]byte, 0, g3StorageSize)
	for id := 5; id < g3Sectors; id++ {
		n := g3SectorData
		if id == 13 {
			n = g3StorageSize - 8*g3SectorData
		}
		out = append(out, s.Raw[s.sector[id]:s.sector[id]+n]...)
	}
	return out
}

// Boxes: the 14 boxes' 30 slots (80 bytes each, as stored; empty slots all zeros).
func (s *Gen3Save) Boxes() [g3BoxCount][g3BoxSlots][]byte {
	st := s.storage()
	var out [g3BoxCount][g3BoxSlots][]byte
	for b := 0; b < g3BoxCount; b++ {
		for i := 0; i < g3BoxSlots; i++ {
			off := g3StorageBoxes + (b*g3BoxSlots+i)*pk3Size
			out[b][i] = append([]byte(nil), st[off:off+pk3Size]...)
		}
	}
	return out
}

// BoxNames: the boxes' names as the game shows them.
func (s *Gen3Save) BoxNames() [g3BoxCount]string {
	st := s.storage()
	var out [g3BoxCount]string
	for b := 0; b < g3BoxCount; b++ {
		off := g3StorageNames + b*g3BoxNameLen
		out[b] = decodeGameText(st[off:off+g3BoxNameLen], false)
		if out[b] == "" {
			out[b] = fmt.Sprintf("BOX %d", b+1)
		}
	}
	return out
}

// sb1: struct SaveBlock1, from sectors 1-4.
func (s *Gen3Save) sb1() []byte {
	sizes := g3SectorSizes[s.Family]
	var out []byte
	for id := 1; id <= 4; id++ {
		out = append(out, s.Raw[s.sector[id]:s.sector[id]+sizes[id]]...)
	}
	return out
}

// Party: the Pokémon in the party (the first 80 bytes of each: what a box keeps).
func (s *Gen3Save) Party() [][]byte {
	sb1 := s.sb1()
	off := g3PartyOffset[s.Family]
	n := min(int(binary.LittleEndian.Uint32(sb1[off:])), 6)
	var out [][]byte
	for i := 0; i < n; i++ {
		p := off + 4 + 100*i
		out = append(out, append([]byte(nil), sb1[p:p+pk3Size]...))
	}
	return out
}

// Trainer: the player's name, Trainer ID and Secret ID (SaveBlock2).
func (s *Gen3Save) Trainer() (name string, tid, sid int) {
	sb2 := s.Raw[s.sector[0]:]
	return decodeGameText(sb2[:7], false), int(binary.LittleEndian.Uint16(sb2[0x0A:])), int(binary.LittleEndian.Uint16(sb2[0x0C:]))
}

// Owner: who this save belongs to, to tell one cartridge's save from another's.
func (s *Gen3Save) Owner() string {
	name, tid, sid := s.Trainer()
	return fmt.Sprintf("%s|%05d|%05d", name, tid, sid)
}

// WithBoxes returns the whole save with the boxes replaced, written as a new save in the other
// slot (the save counter one higher). The slot in use isn't touched.
func (s *Gen3Save) WithBoxes(boxes [g3BoxCount][g3BoxSlots][]byte) []byte {
	st := s.storage()
	for b := 0; b < g3BoxCount; b++ {
		for i := 0; i < g3BoxSlots; i++ {
			off := g3StorageBoxes + (b*g3BoxSlots+i)*pk3Size
			src := boxes[b][i]
			if len(src) < pk3Size {
				src = make([]byte, pk3Size)
			}
			copy(st[off:off+pk3Size], src[:pk3Size])
		}
	}
	sizes := g3SectorSizes[s.Family]
	out := append([]byte(nil), s.Raw...)
	other := 1 - s.Slot
	for i := 0; i < g3Sectors; i++ { // the same order of sectors as the slot in use
		from := s.Slot*g3SlotSize + i*g3SectorSize
		to := other*g3SlotSize + i*g3SectorSize
		copy(out[to:to+g3SectorSize], s.Raw[from:from+g3SectorSize])
		sec := out[to : to+g3SectorSize]
		id := int(binary.LittleEndian.Uint16(sec[0xFF4:]))
		if id >= 5 {
			start := (id - 5) * g3SectorData
			copy(sec[:sizes[id]], st[start:start+sizes[id]])
		}
		binary.LittleEndian.PutUint16(sec[0xFF6:], g3Checksum(sec[:sizes[id]]))
		binary.LittleEndian.PutUint32(sec[0xFFC:], s.Counter+1)
	}
	return out
}
