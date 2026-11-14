package main

import (
	"encoding/binary"
	"testing"
	"time"
)

// a 128 KB Gen 3 save with slot 0 written by save counter c0 and slot 1 by c1 (0: never saved)
func testGen3Save(c0, c1 uint32) []byte {
	b := make([]byte, 0x20000)
	for i := range b {
		b[i] = 0xFF
	}
	for slot, c := range []uint32{c0, c1} {
		if c != 0 {
			for i := 0; i < g3Sectors; i++ {
				writeTestSector(b, slot, i, (i+int(c))%g3Sectors, c)
			}
		}
	}
	return b
}

func writeTestSector(b []byte, slot, i, id int, counter uint32) {
	sec := b[slot*g3SlotSize+i*g3SectorSize:][:g3SectorSize]
	for k := 0; k < 0xF80; k++ {
		sec[k] = byte(counter)
	}
	binary.LittleEndian.PutUint16(sec[0xFF4:], uint16(id))
	binary.LittleEndian.PutUint32(sec[0xFF8:], g3Signature)
	binary.LittleEndian.PutUint32(sec[0xFFC:], counter)
}

func TestGen3HalfWritten(t *testing.T) {
	if gen3HalfWritten(testGen3Save(5, 6)) {
		t.Error("two whole slots counted as half written")
	}
	if gen3HalfWritten(testGen3Save(1, 0)) {
		t.Error("a first save (other slot empty) counted as half written")
	}
	if gen3HalfWritten(make([]byte, 0x8000)) {
		t.Error("a 32 KB save counted as Gen 3")
	}
	// the game is writing slot 0 for save 7: its first 4 sectors are new, the 5th is erased,
	// the rest still from save 5
	b := testGen3Save(5, 6)
	for i := 0; i < 4; i++ {
		writeTestSector(b, 0, i, (i+7)%g3Sectors, 7)
	}
	for k := 0; k < g3SectorSize; k++ {
		b[4*g3SectorSize+k] = 0xFF
	}
	if !gen3HalfWritten(b) {
		t.Error("a half-written slot wasn't noticed")
	}
}

func TestSaveSettle(t *testing.T) {
	var w saveSettle
	t0 := time.Now()
	whole := testGen3Save(5, 6)
	if s, _ := w.observe(whole, t0); s {
		t.Fatal("settled on first sight")
	}
	if s, _ := w.observe(whole, t0.Add(2*time.Second)); s {
		t.Fatal("settled before saveQuietTime")
	}
	if s, first := w.observe(whole, t0.Add(4*time.Second)); !s || !first {
		t.Fatal("not settled after saveQuietTime")
	}
	if _, first := w.observe(whole, t0.Add(5*time.Second)); first {
		t.Fatal("offered the same save twice")
	}
	if !w.quiet(whole, t0.Add(5*time.Second)) {
		t.Fatal("quiet() disagrees")
	}

	// a half-written save that stopped changing (the game paused mid-save) waits longer
	half := testGen3Save(5, 6)
	writeTestSector(half, 0, 0, 7%g3Sectors, 7)
	t1 := t0.Add(10 * time.Second)
	w.observe(half, t1)
	if w.quiet(whole, t1.Add(5*time.Second)) {
		t.Fatal("quiet() for a save that isn't the one last seen")
	}
	if s, _ := w.observe(half, t1.Add(5*time.Second)); s {
		t.Fatal("a half-written save settled")
	}
	if s, _ := w.observe(half, t1.Add(gen3WaitLimit+time.Second)); !s {
		t.Fatal("a slot that stays half written was waited on for ever")
	}
}

func TestIsGBLinkUSB(t *testing.T) {
	for _, c := range []struct {
		vid, pid string
		want     bool
	}{
		{"2FE3", "000A", true}, {"2fe3", "0001", true}, // firmware 2.x
		{"1209", "B011", true}, {"1209", "b011", true}, // firmware 3.0.0
		{"1209", "0001", false}, {"2E8A", "000A", false}, {"", "", false},
	} {
		if got := isGBLinkUSB(c.vid, c.pid); got != c.want {
			t.Errorf("isGBLinkUSB(%q, %q) = %v", c.vid, c.pid, got)
		}
	}
}

func TestSaveTypeChoice(t *testing.T) {
	if chooseSaveType(3, 4) != 4 {
		t.Error("64 KB flash not taken as 128 KB for a 128 KB game")
	}
	for _, chip := range []int{1, 2, 5, 6} { // RAM and EEPROM are never turned into flash
		if chooseSaveType(chip, 4) != chip {
			t.Errorf("save type %d changed to flash", chip)
		}
	}
	if knownSaveType("AXPE") != 4 || knownSaveType("BPEE") != 4 || knownSaveType("AGBJ") != 0 {
		t.Error("knownSaveType")
	}
	if romSaveType([]byte("xxFLASH1M_V103xx")) != 4 || romSaveType([]byte("FLASH512_V131")) != 3 || romSaveType([]byte("EEPROM_V124")) != 0 {
		t.Error("romSaveType")
	}
}
