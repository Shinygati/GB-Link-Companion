package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePk3 builds a box Pokémon the way the games store it.
func makePk3(pid, otid uint32, species int, exp uint32, ivs [6]int) []byte {
	b := make([]byte, pk3Size)
	le := binary.LittleEndian
	le.PutUint32(b, pid)
	le.PutUint32(b[4:], otid)
	copy(b[8:18], []byte{0xBB, 0xBC, 0xBD, 0xFF, 0, 0, 0, 0, 0, 0}) // "ABC"
	b[18] = 2                                                       // English
	b[19] = 2                                                       // has a species
	copy(b[20:27], []byte{0xCE, 0xD9, 0xE7, 0xE8, 0xFF, 0, 0})      // "Test"
	var dec [48]byte
	o := pk3Order[pid%24]
	g, m := dec[12*o[0]:], dec[12*o[3]:]
	le.PutUint16(g, uint16(species))
	le.PutUint32(g[4:], exp)
	var iv uint32
	for i, v := range ivs {
		iv |= uint32(v) << (5 * i)
	}
	le.PutUint32(m[4:], iv)
	le.PutUint16(m[2:], 5|3<<7|4<<11) // met at Lv 5 in Emerald, Poké Ball
	var sum uint16
	key := pid ^ otid
	for i := 0; i < 12; i++ {
		w := le.Uint32(dec[4*i:])
		sum += uint16(w) + uint16(w>>16)
		le.PutUint32(b[32+4*i:], w^key)
	}
	le.PutUint16(b[28:], sum)
	return b
}

// makeSave builds an Emerald save with both slots good (slot 0 the newer one).
func makeSave(t *testing.T, boxes [g3BoxCount][g3BoxSlots][]byte, c0, c1 uint32) []byte {
	raw := make([]byte, 0x20000)
	rng := rand.New(rand.NewSource(1))
	sizes := g3SectorSizes["EMERALD"]
	for slot, counter := range []uint32{c0, c1} {
		st := make([]byte, g3StorageSize)
		for b := range boxes {
			for i, p := range boxes[b] {
				if p != nil {
					copy(st[g3StorageBoxes+(b*g3BoxSlots+i)*pk3Size:], p)
				}
			}
		}
		copy(st[g3StorageNames:], []byte{0xBC, 0xC9, 0xD2, 0x00, 0xA2, 0xFF}) // "BOX 1"
		for i := 0; i < g3Sectors; i++ {
			id := (i + 3) % g3Sectors // rotated, as the games leave them
			off := slot*g3SlotSize + i*g3SectorSize
			sec := raw[off : off+g3SectorSize]
			if id >= 5 {
				copy(sec[:sizes[id]], st[(id-5)*g3SectorData:])
			} else {
				rng.Read(sec[:sizes[id]])
			}
			if id == 0 {
				copy(sec, []byte{0xCE, 0xD9, 0xE7, 0xE8, 0xFF, 0, 0, 0}) // "Test"
				binary.LittleEndian.PutUint16(sec[0x0A:], 12345)
				binary.LittleEndian.PutUint16(sec[0x0C:], 54321)
			}
			if id == 1 { // the party: none
				binary.LittleEndian.PutUint32(sec[g3PartyOffset["EMERALD"]:], 0)
			}
			binary.LittleEndian.PutUint16(sec[0xFF4:], uint16(id))
			binary.LittleEndian.PutUint16(sec[0xFF6:], g3Checksum(sec[:sizes[id]]))
			binary.LittleEndian.PutUint32(sec[0xFF8:], g3Signature)
			binary.LittleEndian.PutUint32(sec[0xFFC:], counter)
		}
	}
	return raw
}

func TestPk3RoundTrip(t *testing.T) {
	raw := makePk3(0x12345678, 54321<<16|12345, 280, 1000000, [6]int{31, 30, 29, 28, 27, 26})
	p, ok := parsePk3(raw)
	if !ok || !p.Valid() {
		t.Fatalf("not read: %+v", p)
	}
	if p.Species != 280 || p.Exp != 1000000 || p.IVs != [6]int{31, 30, 29, 28, 27, 26} || p.MetLevel != 5 || p.MetGame != 3 || p.Ball != 4 {
		t.Fatalf("wrong fields: %+v", p)
	}
	if got := decodeGameText(p.Nick, false); got != "ABC" {
		t.Fatalf("nickname %q", got)
	}
	if levelFor(0, p.Exp) != 100 || levelFor(0, 125) != 5 || levelFor(0, 124) != 4 {
		t.Fatal("levels wrong")
	}
	raw[40] ^= 1
	if p, _ := parsePk3(raw); p.Valid() {
		t.Fatal("a damaged Pokémon should be a Bad Egg")
	}
	if _, ok := parsePk3(make([]byte, pk3Size)); ok {
		t.Fatal("an empty slot read as a Pokémon")
	}
}

func TestShiny(t *testing.T) {
	// TID 0, SID 0: shiny when the PID's halves XOR below 8
	p := Pk3{PID: 0x00070000, OTID: 0}
	if !p.Shiny() {
		t.Fatal("should be shiny")
	}
	p.PID = 0x00080000
	if p.Shiny() {
		t.Fatal("shouldn't be shiny")
	}
}

func TestSaveBoxes(t *testing.T) {
	var boxes [g3BoxCount][g3BoxSlots][]byte
	a := makePk3(1, 2, 1, 100, [6]int{})
	b := makePk3(3, 4, 4, 200, [6]int{})
	boxes[0][0], boxes[13][29] = a, b
	raw := makeSave(t, boxes, 7, 6)
	s, err := parseGen3Save(raw, "BPEE")
	if err != nil {
		t.Fatal(err)
	}
	if s.Slot != 0 || s.Counter != 7 {
		t.Fatalf("slot %d counter %d", s.Slot, s.Counter)
	}
	got := s.Boxes()
	if !bytes.Equal(got[0][0], a) || !bytes.Equal(got[13][29], b) || !pk3Empty(got[5][5]) {
		t.Fatal("boxes read wrong")
	}
	if n := s.BoxNames()[0]; n != "BOX 1" {
		t.Fatalf("box name %q", n)
	}
	if name, tid, sid := s.Trainer(); name != "Test" || tid != 12345 || sid != 54321 {
		t.Fatalf("trainer %s %d %d", name, tid, sid)
	}

	// move a to box 2, b to box 1 slot 0's place: written to slot 1, counter 8
	got[0][0], got[1][3] = got[13][29], got[0][0]
	got[13][29] = make([]byte, pk3Size)
	out := s.WithBoxes(got)
	if !bytes.Equal(out[:g3SlotSize], raw[:g3SlotSize]) {
		t.Fatal("the slot in use was changed")
	}
	if !bytes.Equal(out[2*g3SlotSize:], raw[2*g3SlotSize:]) {
		t.Fatal("the sectors after the slots were changed")
	}
	n, err := parseGen3Save(out, "BPEE")
	if err != nil {
		t.Fatal(err)
	}
	if n.Slot != 1 || n.Counter != 8 {
		t.Fatalf("new save: slot %d counter %d", n.Slot, n.Counter)
	}
	nb := n.Boxes()
	if !bytes.Equal(nb[0][0], b) || !bytes.Equal(nb[1][3], a) || !pk3Empty(nb[13][29]) {
		t.Fatal("new boxes wrong")
	}
	if n.Owner() != s.Owner() {
		t.Fatal("the trainer changed")
	}
	// and the next save after that goes back to slot 0, a newer one again
	if o2, _ := parseGen3Save(n.WithBoxes(nb), "BPEE"); o2.Slot != 0 || o2.Counter != 9 {
		t.Fatal("third save in the wrong slot")
	}
	// a broken newer slot: the older one is used
	bad := append([]byte(nil), out...)
	bad[g3SlotSize+100] ^= 0xFF
	if o, err := parseGen3Save(bad, "BPEE"); err != nil || o.Slot != 0 {
		t.Fatal("didn't fall back to the good slot")
	}
}

func TestSaveCounterWrap(t *testing.T) {
	var boxes [g3BoxCount][g3BoxSlots][]byte
	raw := makeSave(t, boxes, 0xFFFFFFFF, 0)
	s, err := parseGen3Save(raw, "BPRE")
	if err == nil {
		t.Fatal("an Emerald save read as FireRed (its sector sizes differ)")
	}
	s, err = parseGen3Save(raw, "BPEE")
	if err != nil || s.Slot != 1 {
		t.Fatalf("wrap: %v slot %d", err, s.Slot)
	}
}

func TestBankFamily(t *testing.T) {
	for code, want := range map[string]string{"BPEE": "EMERALD", "BPRE": "FRLG", "BPGD": "FRLG", "BPEJ": "", "AXVE": "", "BPRF": "FRLG"} {
		if got := bankFamily(code); got != want {
			t.Errorf("%s: %q", code, got)
		}
	}
}

func TestJournalRecovery(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GBLINK_COMPANION_HOME", dir)
	defer os.Unsetenv("GBLINK_COMPANION_HOME")

	a := makePk3(10, 20, 1, 100, [6]int{}) // going to the bank
	b := makePk3(30, 40, 4, 100, [6]int{}) // coming to the cartridge
	var before, after [g3BoxCount][g3BoxSlots][]byte
	before[0][0] = a
	after[0][1] = b
	bankAfter := newBank()
	bankAfter.Boxes[0].Slots[0] = newBankMon(a, "BPEE", "x")
	j := &bankJournal{Code: "BPEE", Bank: bankAfter,
		Deposited: []string{hex.EncodeToString(a)}, Withdrawn: []string{hex.EncodeToString(b)}}

	// the cartridge was written: the bank is the note's
	s, _ := parseGen3Save(makeSave(t, after, 2, 1), "BPEE")
	got, _ := finishJournal(j, s)
	if got.count() != 1 || got.Boxes[0].Slots[0] == nil {
		t.Fatalf("written: bank has %d", got.count())
	}
	// it wasn't: a stays on the cartridge (not in the bank), b goes back in the bank
	s, _ = parseGen3Save(makeSave(t, before, 2, 1), "BPEE")
	got, _ = finishJournal(j, s)
	if got.count() != 1 {
		t.Fatalf("not written: bank has %d", got.count())
	}
	p, _ := parsePk3(got.Boxes[0].Slots[0].bytes())
	if p.Identity() != 30<<32|40 {
		t.Fatal("not written: the wrong Pokémon is in the bank")
	}

	// saving and loading the bank, and the note
	if err := saveBank(got); err != nil {
		t.Fatal(err)
	}
	if err := saveBank(got); err != nil { // a second time: the first is kept in Backups
		t.Fatal(err)
	}
	back, err := loadBank()
	if err != nil || back.count() != 1 {
		t.Fatalf("load: %v %d", err, back.count())
	}
	if err := saveJournal(j); err != nil {
		t.Fatal(err)
	}
	if jj, err := loadJournal(); err != nil || jj == nil || len(jj.Deposited) != 1 {
		t.Fatal("note not read back")
	}
	clearJournal()
	if jj, _ := loadJournal(); jj != nil {
		t.Fatal("note not cleared")
	}
}

func TestPk3Files(t *testing.T) {
	raw := makePk3(0xDEADBEEF, 0x12345678, 25, 8000, [6]int{1, 2, 3, 4, 5, 6})
	f := pk3ToFile(raw)
	// PKHeX's layout: decrypted, the growth block first (species at 0x20)
	if binary.LittleEndian.Uint16(f[0x20:]) != 25 || binary.LittleEndian.Uint32(f[0x24:]) != 8000 {
		t.Fatal("not decrypted in order")
	}
	back, err := pk3FromFile(f)
	if err != nil || !bytes.Equal(back, raw) {
		t.Fatalf("decrypted file didn't come back: %v", err)
	}
	back, err = pk3FromFile(raw) // an encrypted one (as the games keep it) is taken as it is
	if err != nil || !bytes.Equal(back, raw) {
		t.Fatal("encrypted file changed")
	}
	party := append(append([]byte(nil), f...), make([]byte, 20)...)
	if back, err := pk3FromFile(party); err != nil || !bytes.Equal(back, raw) {
		t.Fatal("100-byte .pk3 not read")
	}
	if _, err := pk3FromFile(make([]byte, 50)); err == nil {
		t.Fatal("a 50-byte file was taken")
	}
	bad := append([]byte(nil), raw...)
	bad[50] ^= 1
	if !bytes.Equal(pk3ToFile(bad), bad) {
		t.Fatal("a Bad Egg should be saved as it is")
	}
}

func TestBankFolders(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GBLINK_COMPANION_HOME", dir)
	defer os.Unsetenv("GBLINK_COMPANION_HOME")

	a := makePk3(1, 2, 1, 100, [6]int{})
	b := makePk3(3, 4, 4, 100, [6]int{})
	bank := newBank()
	bank.Boxes[0].Slots[4] = newBankMon(a, "BPEE", "")
	bank.Boxes[2].Name = "MY/BOX"
	bank.Boxes[2].Slots[29] = newBankMon(b, "BPEE", "")
	if err := saveBank(bank); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(bankBoxesDir(), "Box 01 - BANK 1", "Slot 05 - ABC - 00000001.pk3"))
	if len(files) != 1 {
		entries, _ := os.ReadDir(filepath.Join(bankBoxesDir(), "Box 01 - BANK 1"))
		t.Fatalf("file not named as expected: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(bankBoxesDir(), "Box 03 - MY_BOX")); err != nil {
		t.Fatal("box name not made safe")
	}
	// a .pk3 from PKHeX dropped in box 2, without a slot number
	os.WriteFile(filepath.Join(bankBoxesDir(), "Box 02 - BANK 2", "001 - BULBASAUR - 5.pk3"), pk3ToFile(makePk3(5, 6, 1, 100, [6]int{})), 0o644)
	os.WriteFile(filepath.Join(bankBoxesDir(), "Box 02 - BANK 2", "junk.pk3"), []byte("nope"), 0o644)
	got, err := loadBank()
	if err != nil {
		t.Fatal(err)
	}
	if got.count() != 3 || got.Boxes[0].Slots[4].bytes() == nil || got.Boxes[2].Slots[29].bytes() == nil || got.Boxes[1].Slots[0].bytes() == nil {
		t.Fatalf("loaded %d", got.count())
	}
	if !bytes.Equal(got.Boxes[0].Slots[4].bytes(), a) || got.Boxes[2].Name != "MY_BOX" || len(bankLoadWarnings) != 1 {
		t.Fatalf("wrong: %q %v", got.Boxes[2].Name, bankLoadWarnings)
	}
	// every save keeps the one before in Backups
	if err := saveBank(got); err != nil {
		t.Fatal(err)
	}
	backups, _ := os.ReadDir(bankBackupsDir())
	if len(backups) != 1 {
		t.Fatalf("%d backups", len(backups))
	}
	if again, _ := loadBank(); again.count() != 3 {
		t.Fatal("the loose file wasn't kept")
	}

	// a save cut short: the old folders were moved aside but the new ones not yet in place
	os.Rename(bankBoxesDir(), bankBoxesDir()+".old")
	if again, err := loadBank(); err != nil || again.count() != 3 {
		t.Fatal("didn't recover the old folders")
	}
	// ...or a new copy left half written: thrown away
	os.MkdirAll(bankBoxesDir()+".new/Box 01 - X", 0o755)
	if again, _ := loadBank(); again.count() != 3 {
		t.Fatal("a half-written copy was used")
	}
	if _, err := os.Stat(bankBoxesDir() + ".new"); err == nil {
		t.Fatal("the half-written copy is still there")
	}

	// an old bank.json is turned into folders
	os.RemoveAll(bankBoxesDir())
	old := newBank()
	old.Boxes[0].Slots[0] = newBankMon(b, "BPEE", "")
	data, _ := json.Marshal(old)
	os.WriteFile(bankPath(), data, 0o644)
	if conv, err := loadBank(); err != nil || conv.count() != 1 {
		t.Fatalf("bank.json not converted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bankBoxesDir(), "Box 01 - BANK 1")); err != nil {
		t.Fatal("no folders after converting")
	}
}

// a bank session with no link, for the editing tests
func editSession(cart [g3BoxCount][g3BoxSlots][]byte, bank Bank) *BankSession {
	for x := range cart {
		for i := range cart[x] {
			if cart[x][i] == nil {
				cart[x][i] = make([]byte, pk3Size)
			}
		}
	}
	return &BankSession{Ready: true, CartLoaded: true, Mode: modeMove, Multi: map[slotRef]bool{}, Marked: map[slotRef]bool{},
		orig: copyBoxes(cart), Cart: copyBoxes(cart), origBank: bank.clone(), Bank: bank}
}

func TestBankModes(t *testing.T) {
	var cart [g3BoxCount][g3BoxSlots][]byte
	for i := 0; i < 5; i++ {
		cart[0][i] = makePk3(uint32(100+i), 7, 1+i, 100, [6]int{})
	}
	b := editSession(cart, newBank())
	c := func(box, slot int) slotRef { return slotRef{Box: box, Slot: slot} }
	k := func(box, slot int) slotRef { return slotRef{Bank: true, Box: box, Slot: slot} }

	// Move: a drag swaps
	b.Drop(c(0, 0), c(0, 1))
	if p, _, _ := b.Mon(c(0, 0)); p.Species != 2 {
		t.Fatal("drag didn't swap")
	}
	// Move Multi: choose three, drag one of them into the bank
	b.SetMode(modeMulti)
	b.Tap(c(0, 2))
	b.Tap(c(0, 3))
	b.Tap(c(0, 4))
	b.Drop(c(0, 3), k(0, 28))
	if b.Bank.count() != 3 || b.get(k(0, 28)) == nil || b.get(k(0, 29)) == nil || b.get(k(1, 0)) == nil {
		t.Fatalf("group drop: %d in the bank", b.Bank.count())
	}
	if n := b.counts(); n.ToBank != 3 || n.ToCart != 0 {
		t.Fatalf("counts %+v", n)
	}
	if len(b.Multi) != 0 {
		t.Fatal("still chosen after the drag")
	}
	// choose them again, send them back to the cartridge (from box 1's first free slots)
	b.SelectRefs([]slotRef{k(0, 28), k(0, 29), k(1, 0)}, false)
	b.SendTo(false)
	if len(b.Multi) != 0 {
		t.Fatal("still chosen after sending")
	}
	if b.Bank.count() != 0 || b.get(c(0, 2)) == nil || b.get(c(0, 4)) == nil {
		t.Fatal("send to cartridge")
	}
	// choose, then click an empty slot: they go there
	b.ClearPicks()
	b.Tap(c(0, 0))
	b.Tap(c(0, 1))
	b.Tap(k(0, 10))
	if b.get(k(0, 10)) == nil || b.get(k(0, 11)) == nil || b.get(c(0, 0)) != nil {
		t.Fatal("click an empty slot")
	}
	// no room on the cartridge from the last slot: nothing moves
	b.SelectRefs([]slotRef{k(0, 10), k(0, 11)}, false)
	b.Drop(k(0, 10), c(13, 29))
	if b.get(k(0, 10)) == nil || b.get(c(13, 29)) != nil || b.Note == "" {
		t.Fatal("a group that doesn't fit should stay")
	}
	// Release: mark two, release them
	b.SetMode(modeRelease)
	b.Tap(k(0, 10))
	b.Tap(c(0, 2))
	if n := b.releaseMarked(); n != 2 {
		t.Fatalf("released %d", n)
	}
	if n := b.counts(); n.Released != 2 || n.ToBank != 1 {
		t.Fatalf("after release %+v", n)
	}
	b.undo()
	if n := b.counts(); n.Any || b.Bank.count() != 0 {
		t.Fatal("undo")
	}
	// a Bad Egg can't go on the cartridge
	bad := makePk3(9, 9, 9, 9, [6]int{})
	bad[40] ^= 1
	bk := newBank()
	bk.Boxes[0].Slots[0] = newBankMon(bad, "", "")
	b = editSession(cart, bk)
	b.Drop(k(0, 0), c(1, 0))
	if b.get(c(1, 0)) != nil || b.Note == "" {
		t.Fatal("a Bad Egg went on the cartridge")
	}
}

func TestStatsAndHiddenPower(t *testing.T) {
	p := Pk3{PID: 3, IVs: [6]int{31, 31, 31, 31, 31, 31}, EVs: [6]int{252, 252, 0, 0, 0, 0}} // Adamant: +Atk -SpA
	s := p.Stats([6]int{100, 100, 100, 100, 100, 100}, 100)
	if s[0] != 404 || s[1] != 328 || s[2] != 236 || s[4] != 212 {
		t.Fatalf("stats %v", s)
	}
	if ty, pw := p.HiddenPower(); ty != "Dark" || pw != 70 {
		t.Fatalf("hidden power %s %d", ty, pw)
	}
	if up, down := natureEffect(3); up != "Atk" || down != "SpA" {
		t.Fatal("Adamant")
	}
}

func TestFindMoveNames(t *testing.T) {
	enc := func(s string, n int) []byte {
		b := gameText(s, false)
		return append(b, make([]byte, n-len(b))...)
	}
	rom := make([]byte, 0x20000)
	names := 0x1000
	for i := 0; i < speciesTbl; i++ {
		copy(rom[names+11*i:], enc("MON", 11))
	}
	at := names + 11*speciesTbl + 37
	copy(rom[at:], enc("-", 13))
	for i := 1; i < moveCount; i++ {
		copy(rom[at+13*i:], enc(fmt.Sprintf("MOVE%d", i), 13))
	}
	m := findMoveNames(fileReader(rom), 0x08000000+uint32(names), 11)
	if len(m) != moveCount || m[1] != "MOVE1" || m[354] != "MOVE354" {
		t.Fatalf("moves %v", m[:3])
	}
}

func TestBoxSelect(t *testing.T) {
	var cart [g3BoxCount][g3BoxSlots][]byte
	cart[0][0], cart[0][1] = makePk3(1, 1, 1, 100, [6]int{}), makePk3(2, 2, 2, 100, [6]int{})
	b := editSession(cart, newBank())
	refs := []slotRef{{Box: 0, Slot: 0}, {Box: 0, Slot: 1}, {Box: 0, Slot: 2}}
	b.SelectRefs(refs, false) // Move: nothing
	if len(b.Multi) != 0 {
		t.Fatal("chose in Move")
	}
	b.SetMode(modeMulti)
	b.SelectRefs(refs, false)
	if len(b.Multi) != 2 { // the empty slot isn't chosen
		t.Fatalf("chose %d", len(b.Multi))
	}
	b.SelectRefs(refs[:1], false) // a new box starts again...
	if len(b.Multi) != 1 {
		t.Fatal("not replaced")
	}
	b.SelectRefs(refs[1:2], true) // ...unless Ctrl adds to it
	if len(b.Multi) != 2 {
		t.Fatal("not added")
	}
}

func TestPlacesAndRibbons(t *testing.T) {
	var d *bankData
	for m, want := range map[int]string{16: "Route 101", 199: "Mirage Tower", 0x58: "Pallet Town", 0xFE: "In-game Trade", 0xFF: "Fateful Encounter"} {
		if got := d.placeName(m); got != want {
			t.Errorf("place %d: %q", m, got)
		}
	}
	p := Pk3{Ribbons: 2 | 4<<3 | 1<<15 | 1<<26} // Cool Super, Beauty Master, Champion, World
	want := "Cool, Cool Super, Beauty, Beauty Super, Beauty Hyper, Beauty Master, Champion, World"
	if got := strings.Join(p.RibbonNames(), ", "); got != want || p.RibbonCount() != 8 {
		t.Fatalf("ribbons %q (%d)", got, p.RibbonCount())
	}
}
