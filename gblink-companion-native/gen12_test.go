package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// a blank save for game in region, with its lists set up and its checksums right
func blankGBSave(game, region string) []byte {
	d := make([]byte, gbSaveRAM)
	gen := 2
	if game == "RB" || game == "Y" {
		gen = 1
	}
	l := gbLayoutFor(gen, game, region)
	for _, o := range []int{l.party, l.currentBox} {
		d[o], d[o+1] = 0, 0xFF
	}
	name := []byte{0x80, 0x92, 0x50} // "AS"
	if region == gbJapanese {
		name = []byte{0x80, 0x81, 0x50} // "アイ"
	}
	if region == gbKorean {
		name = []byte{0x01, 0x01, 0x50} // "가"
	}
	copy(d[l.ot:], name)
	binary.BigEndian.PutUint16(d[l.tid:], 12345)
	if gen == 1 {
		d[l.checksum] = gbChecksum1(d[l.ot:l.checksum])
	} else {
		sum := gbChecksum2(d, l.sumEnd)
		binary.LittleEndian.PutUint16(d[l.sum1:], sum)
		binary.LittleEndian.PutUint16(d[l.sum2:], sum)
	}
	return d
}

func testMon2(species int, dv uint16, moves ...int) GBMon {
	raw := make([]byte, pk2Single)
	b := raw[3 : 3+pk2Party]
	b[0] = byte(species)
	for i, m := range moves {
		b[2+i] = byte(m)
		b[0x17+i] = 35
	}
	binary.BigEndian.PutUint16(b[6:], 4321)
	b[8], b[9], b[10] = 0, 0x10, 0x00 // 4096 exp
	binary.BigEndian.PutUint16(b[0x15:], dv)
	b[0x1F] = 15
	copy(raw[3+pk2Party:], []byte{0x80, 0x81, 0x50})          // OT "AB"
	copy(raw[3+pk2Party+gbStrLen:], []byte{0x8F, 0x88, 0x50}) // "PI"
	raw[0], raw[1], raw[2] = 1, byte(species), 0xFF
	return GBMon{Gen: 2, Raw: raw}
}

func TestGBSaveRoundTrip(t *testing.T) {
	kinds := []struct{ game, region string }{{"C", gbIntl}, {"GS", gbIntl}, {"RB", gbIntl},
		{"C", gbJapanese}, {"GS", gbJapanese}, {"RB", gbJapanese}, {"GS", gbKorean}}
	for _, k := range kinds {
		t.Run(k.game+"-"+k.region, func(t *testing.T) {
			game := k.game
			raw := append(blankGBSave(game, k.region), make([]byte, 48)...) // with mGBA's clock after it
			s, err := parseGBSave(raw, "")
			if err != nil {
				t.Fatal(err)
			}
			if s.Game != game && !(game == "RB" && s.Game == "Y") || s.Region != k.region {
				t.Fatalf("found %s %q", s.Game, s.Region)
			}
			if n, id := s.Trainer(); id != 12345 || n == "" {
				t.Fatalf("trainer %q %d", n, id)
			}
			wantBoxes, wantSlots := map[string][2]int{"RB": {12, 20}, "C": {14, 20}, "GS": {14, 20}}[game], 0
			if k.region == gbJapanese {
				wantBoxes = map[string][2]int{"RB": {8, 30}, "C": {9, 30}, "GS": {9, 30}}[game]
			}
			wantSlots = wantBoxes[1]
			if len(s.Boxes) != wantBoxes[0] || s.Slots() != wantSlots || len(s.Boxes[0]) != wantSlots {
				t.Fatalf("%d boxes of %d", len(s.Boxes), s.Slots())
			}
			boxes := copyGBBoxes(s.Boxes)
			m := testMon2(25, 0xAAAA, 84, 45) // Pikachu, shiny DVs, Thundershock + Growl
			switch k.region {
			case gbJapanese:
				m = testMon2JP(25, 0xAAAA, 84, 45)
			case gbKorean:
				m = testMon2KOR(25, 0xAAAA, 84, 45)
			}
			mm, err := m.as(s.Gen)
			if err != nil {
				t.Fatal(err)
			}
			boxes[0][0] = mm
			boxes[3][5] = mm           // a box that isn't the current one
			boxes[1][wantSlots-1] = mm // the last slot of a box
			out := s.WithBoxes(boxes)
			if len(out) != len(raw) || !bytes.Equal(out[gbSaveRAM:], raw[gbSaveRAM:]) {
				t.Fatal("the clock data after the save changed")
			}
			ns, err := parseGBSave(out, game)
			if err != nil {
				t.Fatal("the new save doesn't open:", err)
			}
			got := ns.Boxes[0][0]
			if !got.Present() || got.Species() != 25 || got.DV16() != 0xAAAA || got.Moves()[0] != 84 || got.Nickname() != m.Nickname() || got.OT() != m.OT() || got.Region() != k.region {
				t.Fatalf("box 1 slot 1: present %v species %d nick %q region %q", got.Present(), got.Species(), got.Nickname(), got.Region())
			}
			if !ns.Boxes[3][0].Present() || !ns.Boxes[1][0].Present() { // the lists are packed: first slot
				t.Fatal("a box lost it")
			}
			if game == "RB" && out[ns.l.curBoxIndex]&0x80 == 0 {
				t.Fatal("the boxes weren't marked as in use")
			}
			switch {
			case game == "C" && k.region == gbIntl && !bytes.Equal(out[0x1209:0x1209+0xB7A], out[0x2009:0x2009+0xB7A]):
				t.Fatal("Crystal's backup copy wasn't made")
			case game == "C" && k.region == gbJapanese && !bytes.Equal(out[0x7209:0x7209+0xADA], out[0x2009:0x2009+0xADA]):
				t.Fatal("Japanese Crystal's backup copy wasn't made")
			case k.region == gbKorean && binary.LittleEndian.Uint16(out[0x7E6B:]) != gbChecksumKOR(out):
				t.Fatal("Korean Gold and Silver's second checksum is wrong")
			}
			// taking it out again empties the box
			boxes[0][0] = GBMon{}
			ns2, err := parseGBSave(ns.WithBoxes(boxes), game)
			if err != nil || ns2.Boxes[0][0].Present() {
				t.Fatal("emptying a slot", err)
			}
		})
	}
}

// a Japanese Pokémon (names of 6), and a Korean one (Hangul OT)
func testMon2JP(species int, dv uint16, moves ...int) GBMon {
	m := testMon2(species, dv, moves...)
	raw := append([]byte(nil), m.Raw[:3+pk2Party]...)
	raw = append(raw, 0x80, 0x81, 0x50, 0x50, 0x50, 0x50) // OT アイ
	raw = append(raw, 0xCF, 0x50, 0x50, 0x50, 0x50, 0x50) // ま
	return GBMon{Gen: 2, Raw: raw}
}

func testMon2KOR(species int, dv uint16, moves ...int) GBMon {
	m := testMon2(species, dv, moves...)
	copy(m.Raw[3+pk2Party:], []byte{0x01, 0x01, 0x50}) // OT 가
	return m
}

func TestGBRegions(t *testing.T) {
	jp, kr, in := testMon2JP(25, 0, 84), testMon2KOR(25, 0, 84), testMon2(25, 0, 84)
	if jp.Region() != gbJapanese || kr.Region() != gbKorean || in.Region() != gbIntl {
		t.Fatal("regions", jp.Region(), kr.Region(), in.Region())
	}
	if jp.OT() != "アイ" || kr.OT() != "가" || in.OT() != "AB" {
		t.Fatalf("names %q %q %q", jp.OT(), kr.OT(), in.OT())
	}
	p1, err := jp.toPK1()
	if err != nil || p1.Region() != gbJapanese || len(p1.File()) != 59 || p1.OT() != "アイ" {
		t.Fatal("a Japanese Pokémon to generation 1", err)
	}
	if r, err := gbMonFromFile(p1.File(), 1); err != nil || r.Identity() != p1.Identity() {
		t.Fatal("reading a Japanese .pk1", err)
	}
	if _, err := kr.toPK1(); err == nil {
		t.Fatal("a Korean Pokémon can't go to generation 1")
	}
	// the bank keeps them to their own games
	b := &GBBankSession{}
	raw := blankGBSave("C", gbIntl)
	b.save, _ = parseGBSave(raw, "")
	if b.fits(&jp) == "" || b.fits(&kr) == "" || b.fits(&in) != "" {
		t.Fatal("international Crystal should only take international Pokémon")
	}
	b.save, _ = parseGBSave(blankGBSave("GS", gbJapanese), "")
	if b.fits(&jp) != "" || b.fits(&in) == "" {
		t.Fatal("Japanese Gold should only take Japanese Pokémon")
	}
}

func TestGBConversions(t *testing.T) {
	m := testMon2(25, 0xAAAA, 84, 45)
	if !m.Shiny() {
		t.Error("DVs AAAA should be shiny")
	}
	p1, err := m.toPK1()
	if err != nil {
		t.Fatal(err)
	}
	if p1.Species() != 25 || p1.body()[0] != 0x54 || p1.DV16() != 0xAAAA || p1.EXP() != 4096 || p1.TID() != 4321 {
		t.Fatalf("to gen 1: species %d internal %x", p1.Species(), p1.body()[0])
	}
	if hp := binary.BigEndian.Uint16(p1.body()[1:]); hp == 0 {
		t.Fatal("a boxed gen 2 Pokémon should come over with its HP")
	}
	back := p1.toPK2()
	if back.Identity() != m.Identity() || back.Species() != 25 || back.Moves() != m.Moves() {
		t.Fatal("round trip changed it")
	}
	if _, err := testMon2(152, 0, 33).toPK1(); err == nil {
		t.Error("Chikorita can't go to generation 1")
	}
	if _, err := testMon2(25, 0, 200).toPK1(); err == nil {
		t.Error("a generation 2 move can't go to generation 1")
	}
	f := m.File()
	if len(f) != pk2Single || f[2] != 0xFF {
		t.Fatal("pk2 file")
	}
	if r, err := gbMonFromFile(f, 2); err != nil || r.Identity() != m.Identity() {
		t.Fatal("reading the pk2 back", err)
	}
}
