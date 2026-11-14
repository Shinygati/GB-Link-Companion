// The size of a game's save, from its ROM. The helper on the GBA finds the save chip by asking it
// for its ID; if that answer goes wrong once (a 128 KB flash chip taken for a 64 KB one, or for
// RAM), only half of a 128 KB save is copied, and the .sav is cut to that half. Pokémon Ruby,
// Sapphire, Emerald, FireRed and LeafGreen keep their newer save in either half, so every second
// save then looks corrupted (the game loads the one before), in mGBA and anywhere else the .sav
// is used, while the cartridge itself is fine. The save library the game was built with names
// the size, so it decides when the chip's answer says less.
package main

import (
	"bytes"
	"encoding/binary"
	"os"
)

// readSaveChecked reads the cart's save. A Pokémon save that comes back with a slot half there
// (which the game never leaves behind, except when it's switched off mid-save) is read again, and
// only a copy that two reads agree on is used: what arrived damaged isn't kept as the save.
func (s *Session) readSaveChecked() ([]byte, error) {
	read := func() ([]byte, error) {
		d, err := s.helper.ReadSave(s.saveType, func(d, t int) { s.ui.SetProgress("Loading your save from the cartridge", d, t) })
		s.ui.ClearProgress()
		return d, err
	}
	data, err := read()
	if err != nil || !gen3HalfWritten(data) {
		return data, err
	}
	s.ui.Log("The save read from the cartridge has a half-written slot: reading it again to check.")
	for try := 0; try < 3; try++ {
		again, err := read()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(again, data) {
			if gen3HalfWritten(again) {
				s.ui.Log("The cartridge's save reads the same twice, half-written slot and all (blocks %s differ between the slots' save counts).", halfBlocks(again))
			}
			return again, nil
		}
		s.ui.Log("The second read differs (blocks %s): the link or the cartridge's contacts gave a bad read.", diffBlocks(data, again))
		data = again
		if !gen3HalfWritten(data) {
			return data, nil
		}
	}
	return nil, linkErr("The save kept reading differently from the cartridge - clean the cartridge's contacts and check the link cable, then try again.")
}

// diffBlocks: the 4 KB blocks where a and b differ, for the log
func diffBlocks(a, b []byte) string {
	out := ""
	for off := 0; off < len(a) && off < len(b); off += saveChunk {
		e := min(off+saveChunk, len(a), len(b))
		if !bytes.Equal(a[off:e], b[off:e]) {
			if out != "" {
				out += ", "
			}
			out += itoa(off / saveChunk)
		}
	}
	return out
}

// halfBlocks: the sectors of the half-written slot whose save count isn't the slot's first
// sector's, for the log
func halfBlocks(b []byte) string {
	out := ""
	for slot := 0; slot < 2; slot++ {
		first := -1
		for i := 0; i < g3Sectors; i++ {
			off := slot*g3SlotSize + i*g3SectorSize
			c := int(binary.LittleEndian.Uint32(b[off+0xFFC:]))
			sig := binary.LittleEndian.Uint32(b[off+0xFF8:]) == g3Signature
			if first < 0 && sig {
				first = c
			} else if !sig || c != first {
				if out != "" {
					out += ", "
				}
				out += itoa(off / saveChunk)
			}
		}
	}
	return out
}

// romSaveType: the save type the ROM's save library is for (saveTypes), or 0 if it doesn't say
// (EEPROM: 512 B or 8 KB, which only the chip can tell).
func romSaveType(rom []byte) int {
	switch {
	case bytes.Contains(rom, []byte("FLASH1M_V")):
		return 4
	case bytes.Contains(rom, []byte("FLASH512_V")), bytes.Contains(rom, []byte("FLASH_V")):
		return 3
	case bytes.Contains(rom, []byte("SRAM_F_V")), bytes.Contains(rom, []byte("SRAM_V")):
		return 1
	}
	return 0
}

// knownSaveType: games whose save size is known by their code, whatever the chip answers: the
// Gen 3 Pokémon games (every language) have 128 KB flash.
func knownSaveType(code string) int {
	if len(code) == 4 {
		switch code[:3] {
		case "AXV", "AXP", "BPE", "BPR", "BPG":
			return 4
		}
	}
	return 0
}

// chooseSaveType: the chip's answer, unless it's 64 KB flash and the ROM says 128 KB flash. Only
// that: a chip that answered as RAM is RAM (a repro cartridge), and flash commands would damage it.
func chooseSaveType(chip, rom int) int {
	if chip == 3 && rom == 4 {
		return 4
	}
	return chip
}

// romSave: the save type the game's ROM is for (read once a session; 0 if not known).
func (s *Session) romSave() int {
	if !s.romRead {
		s.romRead = true
		if s.game != nil && s.game.ROM != "" {
			if b, err := os.ReadFile(s.game.ROM); err == nil {
				s.romType = romSaveType(b)
			}
		}
	}
	return s.romType
}

// fixSaveType: a save type kept from an earlier session that's too small for the game.
func (s *Session) fixSaveType() {
	if t := chooseSaveType(s.saveType, s.romSave()); t != s.saveType && s.saveType != 0 {
		s.ui.Log("This game's save is %s, not %s as was noted before: the whole save is copied from now on.",
			saveTypes[t].Name, saveTypes[s.saveType].Name)
		s.saveType = t
		s.state.SaveType = t
		s.game.saveState(s.state)
	}
}
