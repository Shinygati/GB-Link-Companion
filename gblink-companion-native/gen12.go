// Pokémon Red, Blue and Yellow (generation 1) and Gold, Silver and Crystal (generation 2) saves,
// for the Game Boy side of the Pokémon Bank. Ported from PKHeX (github.com/kwsch/PKHeX, GPL-3.0):
// SAV1.cs, SAV2.cs, SAV1Offsets.cs, SAV2Offsets.cs, PokeList1.cs, PokeList2.cs, PK1.cs, PK2.cs,
// SpeciesConverter.cs, ItemConverter.cs and StringConverter1.cs, so the boxes are read and written
// back exactly as PKHeX does it. The English and other international games are supported (the
// Japanese and Korean ones store their boxes differently).
//
// A Pokémon is kept the way PKHeX keeps one, as a "single list": [count, marker, 0xFF], its party
// data (44 bytes in generation 1, 48 in generation 2; a boxed one only has the first 33 or 32),
// then its trainer's name and its nickname, 11 bytes each. That's also what PKHeX's .pk1 and .pk2
// files are.
package main

import (
	"crypto/sha1"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	//go:embed assets/gen12/personal_rb
	personalRB []byte
	//go:embed assets/gen12/personal_c
	personalC []byte
	//go:embed assets/gen12/species_en.txt
	gbSpeciesText string
	//go:embed assets/gen12/moves_en.txt
	gbMovesText string
	//go:embed assets/gen12/items_g2_en.txt
	gbItemsText string
)

const (
	gbStrLen   = 11 // names in the international and Korean games (10 letters and the end mark)
	gbStrLenJP = 6  // names in the Japanese games (5 letters and the end mark)
	pk1Party   = 44
	pk1Box     = 33
	pk2Party   = 48
	pk2Box     = 32
	pk1Single  = 3 + pk1Party + 2*gbStrLen // 69: PKHeX's SIZE_1ULIST, a .pk1 file
	pk2Single  = 3 + pk2Party + 2*gbStrLen // 73: SIZE_2ULIST, a .pk2 file
	gbSlotFree = 0xFF
	gbSlotEgg  = 0xFD
	gbBoxSlots = 20 // in the international and Korean games (and in the bank's boxes)
	gbMaxSlots = 30 // the Japanese games' boxes hold 30

	sav1Boxes = 12
	sav2Boxes = 14
	gbSaveRAM = 0x8000
)

// ----------------------------------------------------------------------------- the data
type gbPersonal struct {
	HP, Atk, Def, Spe, SpA, SpD int
	Type1, Type2, CatchRate     byte
	Growth                      int
	Gender                      int // generation 2: the gender ratio (255: none)
}

func gen1Personal(species int) gbPersonal {
	if species < 0 || species > 151 {
		return gbPersonal{}
	}
	d := personalRB[species*0x1C:]
	return gbPersonal{HP: int(d[1]), Atk: int(d[2]), Def: int(d[3]), Spe: int(d[4]), SpA: int(d[5]), SpD: int(d[5]),
		Type1: d[6], Type2: d[7], CatchRate: d[8], Growth: int(d[0x13])}
}

func gen2Personal(species int) gbPersonal {
	if species < 0 || species > 251 {
		return gbPersonal{}
	}
	d := personalC[species*0x20:]
	return gbPersonal{HP: int(d[1]), Atk: int(d[2]), Def: int(d[3]), Spe: int(d[4]), SpA: int(d[5]), SpD: int(d[6]),
		Type1: d[7], Type2: d[8], CatchRate: d[9], Growth: int(d[0x16]), Gender: int(d[0xD])}
}

var gbSpeciesNames, gbMoveNames, gbItemNames []string

func gbNames() {
	if gbSpeciesNames == nil {
		gbSpeciesNames = strings.Split(strings.TrimRight(gbSpeciesText, "\n"), "\n")
		gbMoveNames = strings.Split(strings.TrimRight(gbMovesText, "\n"), "\n")
		gbItemNames = strings.Split(strings.TrimRight(gbItemsText, "\n"), "\n")
	}
}

func gbSpeciesName(s int) string {
	gbNames()
	if s > 0 && s < len(gbSpeciesNames) {
		return gbSpeciesNames[s]
	}
	return fmt.Sprintf("#%d", s)
}

func gbMoveName(m int) string {
	gbNames()
	if m > 0 && m < len(gbMoveNames) {
		return gbMoveNames[m]
	}
	return fmt.Sprintf("Move %d", m)
}

func gbItemName(i int) string {
	gbNames()
	if i > 0 && i < len(gbItemNames) {
		return gbItemNames[i]
	}
	if i == 0 {
		return ""
	}
	return fmt.Sprintf("Item %d", i)
}

// SpeciesConverter (PKHeX): generation 1 stores its own species numbers.
var table1NationalToInternal = []byte{0x00, 0x99, 0x09, 0x9A, 0xB0, 0xB2, 0xB4, 0xB1, 0xB3, 0x1C, 0x7B, 0x7C, 0x7D, 0x70, 0x71, 0x72, 0x24, 0x96, 0x97, 0xA5, 0xA6, 0x05, 0x23, 0x6C, 0x2D, 0x54, 0x55, 0x60, 0x61, 0x0F, 0xA8, 0x10, 0x03, 0xA7, 0x07, 0x04, 0x8E, 0x52, 0x53, 0x64, 0x65, 0x6B, 0x82, 0xB9, 0xBA, 0xBB, 0x6D, 0x2E, 0x41, 0x77, 0x3B, 0x76, 0x4D, 0x90, 0x2F, 0x80, 0x39, 0x75, 0x21, 0x14, 0x47, 0x6E, 0x6F, 0x94, 0x26, 0x95, 0x6A, 0x29, 0x7E, 0xBC, 0xBD, 0xBE, 0x18, 0x9B, 0xA9, 0x27, 0x31, 0xA3, 0xA4, 0x25, 0x08, 0xAD, 0x36, 0x40, 0x46, 0x74, 0x3A, 0x78, 0x0D, 0x88, 0x17, 0x8B, 0x19, 0x93, 0x0E, 0x22, 0x30, 0x81, 0x4E, 0x8A, 0x06, 0x8D, 0x0C, 0x0A, 0x11, 0x91, 0x2B, 0x2C, 0x0B, 0x37, 0x8F, 0x12, 0x01, 0x28, 0x1E, 0x02, 0x5C, 0x5D, 0x9D, 0x9E, 0x1B, 0x98, 0x2A, 0x1A, 0x48, 0x35, 0x33, 0x1D, 0x3C, 0x85, 0x16, 0x13, 0x4C, 0x66, 0x69, 0x68, 0x67, 0xAA, 0x62, 0x63, 0x5A, 0x5B, 0xAB, 0x84, 0x4A, 0x4B, 0x49, 0x58, 0x59, 0x42, 0x83, 0x15}
var table1InternalToNational = []byte{0x00, 0x70, 0x73, 0x20, 0x23, 0x15, 0x64, 0x22, 0x50, 0x02, 0x67, 0x6C, 0x66, 0x58, 0x5E, 0x1D, 0x1F, 0x68, 0x6F, 0x83, 0x3B, 0x97, 0x82, 0x5A, 0x48, 0x5C, 0x7B, 0x78, 0x09, 0x7F, 0x72, 0x00, 0x00, 0x3A, 0x5F, 0x16, 0x10, 0x4F, 0x40, 0x4B, 0x71, 0x43, 0x7A, 0x6A, 0x6B, 0x18, 0x2F, 0x36, 0x60, 0x4C, 0x00, 0x7E, 0x00, 0x7D, 0x52, 0x6D, 0x00, 0x38, 0x56, 0x32, 0x80, 0x00, 0x00, 0x00, 0x53, 0x30, 0x95, 0x00, 0x00, 0x00, 0x54, 0x3C, 0x7C, 0x92, 0x90, 0x91, 0x84, 0x34, 0x62, 0x00, 0x00, 0x00, 0x25, 0x26, 0x19, 0x1A, 0x00, 0x00, 0x93, 0x94, 0x8C, 0x8D, 0x74, 0x75, 0x00, 0x00, 0x1B, 0x1C, 0x8A, 0x8B, 0x27, 0x28, 0x85, 0x88, 0x87, 0x86, 0x42, 0x29, 0x17, 0x2E, 0x3D, 0x3E, 0x0D, 0x0E, 0x0F, 0x00, 0x55, 0x39, 0x33, 0x31, 0x57, 0x00, 0x00, 0x0A, 0x0B, 0x0C, 0x44, 0x00, 0x37, 0x61, 0x2A, 0x96, 0x8F, 0x81, 0x00, 0x00, 0x59, 0x00, 0x63, 0x5B, 0x00, 0x65, 0x24, 0x6E, 0x35, 0x69, 0x00, 0x5D, 0x3F, 0x41, 0x11, 0x12, 0x79, 0x01, 0x03, 0x49, 0x00, 0x76, 0x77, 0x00, 0x00, 0x00, 0x00, 0x4D, 0x4E, 0x13, 0x14, 0x21, 0x1E, 0x4A, 0x89, 0x8E, 0x00, 0x51, 0x00, 0x00, 0x04, 0x07, 0x05, 0x08, 0x06, 0x00, 0x00, 0x00, 0x00, 0x2B, 0x2C, 0x2D, 0x45, 0x46, 0x47}

func national1(internal byte) int {
	if int(internal) < len(table1InternalToNational) {
		return int(table1InternalToNational[internal])
	}
	return 0
}

func internal1(national int) byte {
	if national >= 0 && national < len(table1NationalToInternal) {
		return table1NationalToInternal[national]
	}
	return 0
}

// ItemConverter (PKHeX): which generation 1 catch rate values are generation 2 held items, and
// the few the Time Capsule turns into another item.
var catchRateIsHeldItem = []byte{0x3F, 0xFF, 0xFF, 0xFD, 0xFF, 0xDF, 0x3B, 0xD2, 0x03, 0xFF, 0xFF, 0xFB, 0xEF, 0xFF, 0xE7, 0x7E,
	0x18, 0x9C, 0xC5, 0xF1, 0xFB, 0x77, 0xF0, 0xBF, 0xF7, 0xFF, 0xFF, 0xEF, 0xFF, 0xFF, 0x07, 0x00}

func isCatchRateHeldItem(r byte) bool { return catchRateIsHeldItem[r>>3]>>(r&7)&1 != 0 }

func itemFuture1(v byte) byte {
	if isCatchRateHeldItem(v) {
		return v
	}
	switch v {
	case 0x19:
		return 0x92 // Leftovers
	case 0x2D:
		return 0x53 // Bitter Berry
	case 0x32:
		return 0xAE // Gold Berry
	case 0x5A, 0x64, 0x78, 0x87, 0xBE, 0xC3, 0xDC, 0xFA, 0xFF:
		return 0xAD // Berry
	}
	return v
}

// The games' regions: Pokémon can be traded only within one, as in the games themselves (and PKHeX).
const (
	gbIntl     = ""   // English, French, German, Italian, Spanish
	gbJapanese = "jp" // Japanese: shorter names in another character set, 30 Pokémon to a box
	gbKorean   = "ko" // Korean Gold and Silver: names in Hangul, two bytes a letter
)

func gbRegionName(r string) string {
	switch r {
	case gbJapanese:
		return "Japanese"
	case gbKorean:
		return "Korean"
	}
	return "international (English, French, German, Italian or Spanish)"
}

// isHangul: PKHeX's StringConverter2KOR.IsHangul (a Korean name starts with a table byte, 1-11).
func isHangul(b []byte) bool { return len(b) > 0 && b[0] <= 0x0B }

// decodeGBText turns a name as the game stores it into text (0x50 ends it), as PKHeX's
// StringConverter1, StringConverter2 and StringConverter2KOR read it (tables in gen12_text.go).
func decodeGBText(b []byte, gen int, region string) string {
	if len(b) > 0 && b[0] == 0x5D {
		if region == gbJapanese {
			return "トレーナー"
		}
		return "TRAINER" // an in-game trade's "TRAINER"
	}
	var sb strings.Builder
	if region != gbJapanese && isHangul(b) {
		for i := 0; i < len(b); i++ {
			v := b[i]
			if v == 0 {
				break
			}
			t := gbTextKOR0
			if v <= 11 {
				if i++; i == len(b) {
					break
				}
				t, v = gbTextKOR[v], b[i]
			}
			if t[v] == 0 {
				break
			}
			sb.WriteRune(t[v])
		}
		return sb.String()
	}
	table := gbText2EN
	switch {
	case region == gbJapanese && gen == 1:
		table = gbText1JP
	case region == gbJapanese:
		table = gbText2JP
	case gen == 1:
		table = gbText1EN
	}
	for _, c := range b {
		r := table[c]
		if r == 0 {
			break
		}
		switch {
		case r == '{':
			sb.WriteString("PK")
		case r == '}':
			sb.WriteString("MN")
		case r == '․':
			sb.WriteByte('.')
		case r >= '０' && r <= '６' && region != gbJapanese: // English ligatures ('d 'l 'm 'r 's 't 'v)
			sb.WriteString("'" + string("dlmrstv"[r-'０']))
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ------------------------------------------------------------------------------ a Pokémon
// GBMon is one generation 1 or 2 Pokémon (a PKHeX single list).
type GBMon struct {
	Gen int
	Raw []byte
}

func (m GBMon) partySize() int {
	if m.Gen == 1 {
		return pk1Party
	}
	return pk2Party
}
func (m GBMon) body() []byte { return m.Raw[3 : 3+m.partySize()] }

// strLen: the length of its names (6 in a Japanese Pokémon, 11 otherwise: PKHeX's GBPKML).
func (m GBMon) strLen() int { return (len(m.Raw) - 3 - m.partySize()) / 2 }
func (m GBMon) OTRaw() []byte {
	return m.Raw[3+m.partySize() : 3+m.partySize()+m.strLen()]
}
func (m GBMon) NickRaw() []byte {
	return m.Raw[3+m.partySize()+m.strLen() : 3+m.partySize()+2*m.strLen()]
}

// Region: which games it can go to (PKHeX: Japanese by its name length, Korean by its OT's Hangul).
func (m GBMon) Region() string {
	switch {
	case m.strLen() == gbStrLenJP:
		return gbJapanese
	case m.Gen == 2 && isHangul(m.OTRaw()):
		return gbKorean
	}
	return gbIntl
}

// Present: the slot holds a Pokémon.
func (m GBMon) Present() bool {
	if len(m.Raw) < 3 {
		return false
	}
	k := m.Raw[1]
	return k != 0 && k != gbSlotFree
}

func (m GBMon) Species() int {
	if m.Gen == 1 {
		return national1(m.body()[0])
	}
	return int(m.body()[0])
}

func (m GBMon) Egg() bool { return m.Gen == 2 && m.Raw[1] == gbSlotEgg }

func (m GBMon) Moves() [4]int {
	b := m.body()
	o := 8
	if m.Gen == 2 {
		o = 2
	}
	return [4]int{int(b[o]), int(b[o+1]), int(b[o+2]), int(b[o+3])}
}

func (m GBMon) ppOff() int {
	if m.Gen == 1 {
		return 0x1D
	}
	return 0x17
}
func (m GBMon) PP(i int) int    { return int(m.body()[m.ppOff()+i] & 0x3F) }
func (m GBMon) PPUps(i int) int { return int(m.body()[m.ppOff()+i] >> 6) }

func (m GBMon) TID() int {
	if m.Gen == 1 {
		return int(binary.BigEndian.Uint16(m.body()[0xC:]))
	}
	return int(binary.BigEndian.Uint16(m.body()[6:]))
}

func (m GBMon) EXP() int {
	o := 0xE
	if m.Gen == 2 {
		o = 8
	}
	b := m.body()
	return int(b[o])<<16 | int(b[o+1])<<8 | int(b[o+2])
}

// StatExp: HP, Attack, Defense, Speed, Special.
func (m GBMon) StatExp() [5]int {
	o := 0x11
	if m.Gen == 2 {
		o = 0xB
	}
	var s [5]int
	for i := range s {
		s[i] = int(binary.BigEndian.Uint16(m.body()[o+2*i:]))
	}
	return s
}

func (m GBMon) DV16() int {
	o := 0x1B
	if m.Gen == 2 {
		o = 0x15
	}
	return int(binary.BigEndian.Uint16(m.body()[o:]))
}

// DVs: HP, Attack, Defense, Speed, Special (0-15).
func (m GBMon) DVs() [5]int {
	d := m.DV16()
	atk, def, spe, spc := d>>12&15, d>>8&15, d>>4&15, d&15
	hp := (atk&1)<<3 | (def&1)<<2 | (spe&1)<<1 | spc&1
	return [5]int{hp, atk, def, spe, spc}
}

// Shiny: in generation 2's terms (the same DVs shine in Gold, Silver and Crystal).
func (m GBMon) Shiny() bool {
	dv := m.DVs()
	return dv[2] == 10 && dv[3] == 10 && dv[4] == 10 && dv[1]&2 == 2
}

// Gender as generation 2 works it out ("", "M" or "F").
func (m GBMon) Gender() string {
	p := gen2Personal(m.Species())
	switch p.Gender {
	case 255:
		return ""
	case 254:
		return "F"
	case 0:
		return "M"
	}
	dv := m.DVs()
	if dv[1]<<4|dv[3] <= p.Gender {
		return "F"
	}
	return "M"
}

func (m GBMon) HeldItem() int {
	if m.Gen == 2 {
		return int(m.body()[1])
	}
	return 0
}

func (m GBMon) CatchRate() int {
	if m.Gen == 1 {
		return int(m.body()[7])
	}
	return 0
}

func (m GBMon) Friendship() int {
	if m.Gen == 2 {
		return int(m.body()[0x1B])
	}
	return 0
}

func (m GBMon) Pokerus() int {
	if m.Gen == 2 {
		return int(m.body()[0x1C])
	}
	return 0
}

// CaughtData: Crystal's record of where and when it was caught (0: none).
func (m GBMon) CaughtData() int {
	if m.Gen == 2 {
		return int(binary.BigEndian.Uint16(m.body()[0x1D:]))
	}
	return 0
}

func (m GBMon) BoxLevel() int {
	if m.Gen == 1 {
		return int(m.body()[3])
	}
	return int(m.body()[0x1F])
}

func (m GBMon) growth() int {
	if m.Gen == 1 {
		return gen1Personal(m.Species()).Growth
	}
	return gen2Personal(m.Species()).Growth
}

// Level from its experience (what the game shows once it's out of the box).
func (m GBMon) Level() int {
	l := levelFor(m.growth(), uint32(m.EXP()))
	if l < 1 {
		return max(1, m.BoxLevel())
	}
	return l
}

func (m GBMon) Nickname() string { return decodeGBText(m.NickRaw(), m.Gen, m.Region()) }
func (m GBMon) OT() string       { return decodeGBText(m.OTRaw(), m.Gen, m.Region()) }

// Valid: the species is one the game knows.
func (m GBMon) Valid() bool {
	s := m.Species()
	if m.Gen == 1 {
		return s >= 1 && s <= 151
	}
	return s >= 1 && s <= 251
}

// Identity tells Pokémon apart (generation 1 and 2 Pokémon have no PID): the same Pokémon has
// the same identity in either generation's form.
func (m GBMon) Identity() string {
	h := sha1.New()
	fmt.Fprintf(h, "%d|%d|%d|", m.Species(), m.TID(), m.DV16())
	h.Write(m.OTRaw())
	h.Write(m.NickRaw())
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// File: the PKHeX file for it (.pk1 or .pk2: the single list, PokeList.WrapSingle).
func (m GBMon) File() []byte {
	out := append([]byte(nil), m.Raw...)
	out[0] = 1
	out[2] = gbSlotFree
	return out
}

func (m GBMon) Ext() string { return fmt.Sprintf(".pk%d", m.Gen) }

// gbSingle: the size of a single list (a .pk1/.pk2 file) for names of strLen.
func gbSingle(gen, strLen int) int {
	if gen == 1 {
		return 3 + pk1Party + 2*strLen
	}
	return 3 + pk2Party + 2*strLen
}

// gbMonFromFile reads a .pk1 or .pk2 file: international, Korean (69/73 bytes) or Japanese (59/63).
func gbMonFromFile(data []byte, gen int) (GBMon, error) {
	if len(data) != gbSingle(gen, gbStrLen) && len(data) != gbSingle(gen, gbStrLenJP) {
		return GBMon{}, fmt.Errorf("it's %d bytes, a .pk%d file is %d (or %d from a Japanese game)", len(data), gen, gbSingle(gen, gbStrLen), gbSingle(gen, gbStrLenJP))
	}
	m := GBMon{Gen: gen, Raw: append([]byte(nil), data...)}
	m.Raw[0] = 1
	if !m.Present() {
		return GBMon{}, errors.New("there's no Pokémon in it")
	}
	return m, nil
}

// --------------------------------------------------------------------------- conversions
func (m GBMon) toPK2() GBMon {
	if m.Gen == 2 {
		return m
	}
	n := m.strLen()
	out := make([]byte, gbSingle(2, n))
	b := out[3 : 3+pk2Party]
	sp := m.Species()
	b[0] = byte(sp)
	copy(b[1:0x1B], m.body()[7:7+0x1A])
	b[1] = itemFuture1(m.body()[7])                                    // the catch rate becomes the held item
	b[0x1B] = 70                                                       // generation 2's base friendship
	b[0x1F] = byte(levelFor(gen2Personal(sp).Growth, uint32(m.EXP()))) // Stat_Level = CurrentLevel
	copy(out[3+pk2Party:], m.OTRaw())
	copy(out[3+pk2Party+n:], m.NickRaw())
	out[0], out[1], out[2] = 1, byte(sp), gbSlotFree
	return GBMon{Gen: 2, Raw: out}
}

// gen2Only: why it can't go into Red, Blue or Yellow ("" if it can).
func (m GBMon) gen2Only() string {
	if m.Gen == 1 {
		return ""
	}
	switch {
	case m.Region() == gbKorean:
		return "A Pokémon from Korean Gold or Silver can't go into Red, Blue or Yellow (there were no Korean ones)."
	case m.Egg():
		return "An Egg can't go into Red, Blue or Yellow."
	case m.Species() > 151:
		return gbSpeciesName(m.Species()) + " isn't in Red, Blue or Yellow."
	}
	for _, mv := range m.Moves() {
		if mv > 165 {
			return gbSpeciesName(m.Species()) + " knows " + gbMoveName(mv) + ", a move from Gold, Silver and Crystal. Like the Time Capsule, the bank won't send it to Red, Blue or Yellow."
		}
	}
	return ""
}

// gen1HP: the HP stat as generation 1 works it out.
func gen1HP(base, dv, statExp, level int) int {
	s := int(math.Ceil(math.Sqrt(float64(statExp)))) / 4
	return ((base+dv)*2+s)*level/100 + level + 10
}

func (m GBMon) toPK1() (GBMon, error) {
	if m.Gen == 1 {
		return m, nil
	}
	if why := m.gen2Only(); why != "" {
		return GBMon{}, errors.New(why)
	}
	n := m.strLen()
	out := make([]byte, gbSingle(1, n))
	b := out[3 : 3+pk1Party]
	copy(b[7:7+0x1A], m.body()[1:0x1B])
	sp := m.Species()
	p := gen1Personal(sp)
	b[0] = internal1(sp)
	b[5], b[6] = p.Type1, p.Type2
	// the held item became the catch rate; one that isn't an item value is put back as the species'
	if r := b[7]; !isCatchRateHeldItem(r) && !(sp == 25 && r == 0xA3) && r != p.CatchRate {
		b[7] = p.CatchRate
	}
	lvl := m.BoxLevel()
	if lvl == 0 {
		lvl = levelFor(p.Growth, uint32(m.EXP()))
	}
	hp := int(binary.BigEndian.Uint16(m.body()[0x22:]))
	if hp == 0 { // a boxed generation 2 Pokémon has no HP stored (it's healed): the full HP
		dv, se := m.DVs(), m.StatExp()
		hp = gen1HP(p.HP, dv[0], se[0], lvl)
	}
	binary.BigEndian.PutUint16(b[1:], uint16(hp))
	b[3], b[0x21] = byte(lvl), byte(lvl)
	copy(out[3+pk1Party:], m.OTRaw())
	copy(out[3+pk1Party+n:], m.NickRaw())
	out[0], out[1], out[2] = 1, b[0], gbSlotFree
	return GBMon{Gen: 1, Raw: out}, nil
}

// as: the Pokémon in generation gen's form.
func (m GBMon) as(gen int) (GBMon, error) {
	if gen == 1 {
		return m.toPK1()
	}
	return m.toPK2(), nil
}

// ------------------------------------------------------------------------------- the save
// GBSave is a Red/Blue/Yellow or Gold/Silver/Crystal save, read the way PKHeX reads it: the
// international games, the Japanese ones and Korean Gold and Silver.
type GBSave struct {
	Gen     int
	Game    string // "RB", "Y", "GS" or "C"
	Region  string // gbIntl, gbJapanese or gbKorean
	Raw     []byte // the whole .sav, as read (mGBA's clock data, or the rest of a bigger save memory, is kept)
	Boxes   [][]GBMon
	Current int
	inited  bool // generation 1: the boxes have been saved to their banks at least once
	Party   int
	l       gbLayout
}

// gbLayout: where a save keeps things (PKHeX's SAV1Offsets and SAV2Offsets).
type gbLayout struct {
	ot, otLen, tid                    int // the trainer's name and ID
	curBoxIndex, boxNames, boxNameLen int
	party, currentBox                 int
	boxes, slots, split               int // the boxes, the Pokémon in each, and how many are in the first bank
	strLen                            int
	checksum, starter, pikaFriend     int // generation 1
	sumEnd, sum1, sum2                int // generation 2
}

const s2Trainer = 0x2009 // generation 2: the trainer's ID and name, where the checksummed data starts

func gbLayoutFor(gen int, game, region string) gbLayout {
	if gen == 1 {
		if region == gbJapanese { // SAV1Offsets.JPN
			return gbLayout{ot: 0x2598, otLen: gbStrLenJP, tid: 0x25FB, curBoxIndex: 0x2842, party: 0x2ED5, currentBox: 0x302D,
				boxes: 8, slots: 30, split: 4, strLen: gbStrLenJP, checksum: 0x3594, starter: 0x29B9, pikaFriend: 0x2712}
		}
		return gbLayout{ot: 0x2598, otLen: gbStrLen, tid: 0x2605, curBoxIndex: 0x284C, party: 0x2F2C, currentBox: 0x30C0,
			boxes: 12, slots: 20, split: 6, strLen: gbStrLen, checksum: 0x3523, starter: 0x29C3, pikaFriend: 0x271C}
	}
	l := gbLayout{ot: s2Trainer + 2, otLen: gbStrLen, tid: s2Trainer, boxNameLen: 9, boxes: 14, slots: 20, split: 7, strLen: gbStrLen}
	set := func(curBoxIndex, boxNames, party, currentBox, sumEnd, sum1, sum2 int) {
		l.curBoxIndex, l.boxNames, l.party, l.currentBox, l.sumEnd, l.sum1, l.sum2 = curBoxIndex, boxNames, party, currentBox, sumEnd, sum1, sum2
	}
	switch {
	case region == gbJapanese:
		l.otLen, l.boxes, l.slots, l.split, l.strLen = gbStrLenJP, 9, 30, 6, gbStrLenJP
		if game == "C" {
			set(0x26E2, 0x26E5, 0x281A, 0x2D10, 0x2AE2, 0x2D0D, 0x7F0D)
		} else {
			set(0x2705, 0x2708, 0x283E, 0x2D10, 0x2C8B, 0x2D0D, 0x7F0D)
		}
	case region == gbKorean: // Gold and Silver only
		l.boxNameLen = 17
		set(0x26FC, 0x26FF, 0x28CC, 0x2DAE, 0x2DAA, 0x2DAB, 0x7E6B)
	case game == "C":
		set(0x2700, 0x2703, 0x2865, 0x2D10, 0x2B82, 0x2D0D, 0x1F0D)
	default:
		set(0x2724, 0x2727, 0x288A, 0x2D6C, 0x2D68, 0x2D69, 0x7E6D)
	}
	return l
}

func (l gbLayout) boxList(gen int) int {
	body := pk1Box
	if gen == 2 {
		body = pk2Box
	}
	return (l.strLen*2+body+1)*l.slots + 2
}

// boxOffset: where box i's list is kept (generation 2 leaves 2 bytes after each).
func (l gbLayout) boxOffset(gen, i int) int {
	size := l.boxList(gen)
	if gen == 2 {
		size += 2
	}
	if i < l.split {
		return 0x4000 + i*size
	}
	return 0x6000 + (i-l.split)*size
}

func gbChecksum1(d []byte) byte {
	var s byte
	for _, b := range d {
		s += b
	}
	return ^s
}

func gbChecksum2(d []byte, end int) uint16 {
	var s uint16
	for i := s2Trainer; i <= end; i++ {
		s += uint16(d[i])
	}
	return s
}

// gbChecksumKOR: Korean Gold and Silver's second checksum (PKHeX's GetKoreanChecksum).
func gbChecksumKOR(d []byte) uint16 {
	var s uint16
	for _, p := range [][2]int{{0x106B, 0x1533}, {0x1534, 0x1A12}, {0x1A13, 0x1C38}, {0x3DD8, 0x3F79}, {0x7E39, 0x7E6A}} {
		for i := p[0]; i < p[1]; i++ {
			s += uint16(d[i])
		}
	}
	return s
}

func (l gbLayout) checksumOK(d []byte, gen int) bool {
	if gen == 1 {
		return d[l.checksum] == gbChecksum1(d[l.ot:l.checksum])
	}
	return binary.LittleEndian.Uint16(d[l.sum1:]) == gbChecksum2(d, l.sumEnd)
}

// listOK: PKHeX's IsListValidG12 (a count no bigger than the list, and the end mark after it).
func listOK(d []byte, off, max int) bool {
	n := int(d[off])
	return n <= max && d[off+1+n] == 0xFF
}

// gbGameOfTitle: the game a cartridge header's title says ("" if it isn't one). The Japanese and
// Korean cartridges use the same titles; their saves tell them apart.
func gbGameOfTitle(title string) (gen int, game string) {
	t := strings.ToUpper(title)
	switch {
	case strings.HasPrefix(t, "POKEMON RED"), strings.HasPrefix(t, "POKEMON BLUE"), strings.HasPrefix(t, "POKEMON GREEN"):
		return 1, "RB"
	case strings.HasPrefix(t, "POKEMON YELLOW"):
		return 1, "Y"
	case strings.HasPrefix(t, "POKEMON_GLD"), strings.HasPrefix(t, "POKEMON_SLV"):
		return 2, "GS"
	case strings.HasPrefix(t, "PM_CRYSTAL"):
		return 2, "C"
	}
	return 0, ""
}

func gbGameName(game string) string {
	switch game {
	case "RB":
		return "Pokémon Red / Blue"
	case "Y":
		return "Pokémon Yellow"
	case "GS":
		return "Pokémon Gold / Silver"
	case "C":
		return "Pokémon Crystal"
	}
	return "this game"
}

// parseGBSave reads a save. hint is the game from the cartridge's header ("" to tell from the
// save). The kind of save is found as PKHeX's SaveUtil finds it (its lists where each kind keeps
// them, in the same order), and its checksum has to be right too.
func parseGBSave(raw []byte, hint string) (*GBSave, error) {
	if len(raw) < gbSaveRAM {
		return nil, fmt.Errorf("it's %d bytes; a Pokémon Game Boy save is 32 KB", len(raw))
	}
	d := raw[:gbSaveRAM]
	type kind struct {
		gen          int
		game, region string
	}
	all := []kind{{1, "RB", gbJapanese}, {1, "RB", gbIntl},
		{2, "GS", gbIntl}, {2, "C", gbIntl}, {2, "GS", gbJapanese}, {2, "C", gbJapanese}, {2, "GS", gbKorean}}
	listed := false
	for _, k := range all {
		switch hint {
		case "RB", "Y":
			if k.gen != 1 {
				continue
			}
		case "GS", "C":
			if k.game != hint {
				continue
			}
		}
		l := gbLayoutFor(k.gen, k.game, k.region)
		if !listOK(d, l.party, l.slots) || !listOK(d, l.currentBox, l.slots) {
			continue
		}
		listed = true
		if !l.checksumOK(d, k.gen) {
			continue
		}
		game := k.game
		if k.gen == 1 {
			game = hint
			if game == "" {
				game = "RB"
				if st := d[l.starter]; st == 0x54 || st == 0 && d[l.pikaFriend] != 0 { // SAV1.IsYellow
					game = "Y"
				}
			}
			return parseSAV1(raw, game, k.region, l), nil
		}
		return parseSAV2(raw, game, k.region, l), nil
	}
	names := map[string]string{"RB": "Red, Blue or Yellow", "Y": "Red, Blue or Yellow", "GS": "Gold or Silver", "C": "Crystal"}
	what := "Pokémon Red, Blue, Yellow, Gold, Silver or Crystal"
	if n := names[hint]; n != "" {
		what = "Pokémon " + n
	}
	if listed {
		return nil, errors.New("its checksum is wrong (it's a " + what + " save, but it's damaged or was never saved)")
	}
	return nil, errors.New("it isn't a save from " + what + " (or it's damaged)")
}

// unpackList: PokeList.Unpack, a box or party list as singles (empty slots: nil).
func unpackList(list []byte, gen, capacity, bodyLen, strLen int) []GBMon {
	party := pk1Party
	if gen == 2 {
		party = pk2Party
	}
	size := 3 + party + 2*strLen
	out := make([]GBMon, capacity)
	count := min(capacity, int(list[0]))
	ofsBody := 1 + capacity + 1
	ofsS1 := ofsBody + capacity*bodyLen
	ofsS2 := ofsS1 + capacity*strLen
	for i := 0; i < count; i++ {
		s := make([]byte, size)
		s[0], s[1] = 1, list[1+i]
		copy(s[3:], list[ofsBody+i*bodyLen:ofsBody+(i+1)*bodyLen])
		copy(s[3+party:], list[ofsS1+i*strLen:ofsS1+(i+1)*strLen])
		copy(s[3+party+strLen:], list[ofsS2+i*strLen:ofsS2+(i+1)*strLen])
		m := GBMon{Gen: gen, Raw: s}
		if m.Present() {
			out[i] = m
		}
	}
	return out
}

// mergeSingles: PokeList.MergeSingles, the singles written back into a list (present ones first,
// then the empty ones, as zeros).
// Returns false if there was nothing to write (all empty, and the list was empty too).
func mergeSingles(mons []GBMon, out []byte, gen, capacity, bodyLen, strLen int, destInited bool) bool {
	party := pk1Party
	if gen == 2 {
		party = pk2Party
	}
	count := 0
	for _, m := range mons {
		if m.Present() {
			count++
		}
	}
	if count == 0 && (!destInited || allZero(out[:1+capacity+1+capacity*(bodyLen+2*strLen)])) {
		return false // nothing to write: all empty, and the list was never set up
	}
	out[0] = byte(count)
	ctr, empty := 0, count
	for i := 0; i < capacity; i++ {
		var m GBMon
		if i < len(mons) {
			m = mons[i]
		}
		s := m.Raw
		if s == nil {
			s = make([]byte, 3+party+2*strLen)
		}
		marker := s[1]
		if marker == 0 {
			marker = gbSlotFree
		}
		idx := empty
		if marker != gbSlotFree {
			idx = ctr
			ctr++
		} else {
			empty++
		}
		out[1+idx] = marker
		start := 1 + capacity + 1
		copy(out[start+idx*bodyLen:start+(idx+1)*bodyLen], s[3:3+bodyLen])
		s1 := start + capacity*bodyLen + idx*strLen
		copy(out[s1:s1+strLen], s[3+party:3+party+strLen])
		s2 := start + capacity*bodyLen + capacity*strLen + idx*strLen
		copy(out[s2:s2+strLen], s[3+party+strLen:3+party+2*strLen])
	}
	out[1+capacity] = gbSlotFree
	return true
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func parseSAV1(raw []byte, game, region string, l gbLayout) *GBSave {
	d := raw
	s := &GBSave{Gen: 1, Game: game, Region: region, l: l, Raw: append([]byte(nil), raw...), Boxes: make([][]GBMon, l.boxes)}
	s.Current = int(d[l.curBoxIndex] & 0x7F)
	s.inited = d[l.curBoxIndex]&0x80 != 0
	for i := range s.Boxes {
		s.Boxes[i] = make([]GBMon, l.slots)
		if s.inited && i != s.Current {
			s.Boxes[i] = unpackList(d[l.boxOffset(1, i):], 1, l.slots, pk1Box, l.strLen)
		}
	}
	if s.Current < l.boxes {
		s.Boxes[s.Current] = unpackList(d[l.currentBox:], 1, l.slots, pk1Box, l.strLen)
	}
	s.Party = int(d[l.party])
	return s
}

func parseSAV2(raw []byte, game, region string, l gbLayout) *GBSave {
	s := &GBSave{Gen: 2, Game: game, Region: region, l: l, Raw: append([]byte(nil), raw...), Boxes: make([][]GBMon, l.boxes)}
	for i := range s.Boxes {
		s.Boxes[i] = unpackList(raw[l.boxOffset(2, i):], 2, l.slots, pk2Box, l.strLen)
	}
	s.Current = int(raw[l.curBoxIndex])
	s.Party = int(raw[l.party])
	return s
}

// Slots: how many Pokémon each of its boxes holds.
func (s *GBSave) Slots() int { return s.l.slots }

// Trainer: the player's name and ID.
func (s *GBSave) Trainer() (string, int) {
	l := s.l
	return decodeGBText(s.Raw[l.ot:l.ot+l.otLen], s.Gen, s.Region), int(binary.BigEndian.Uint16(s.Raw[l.tid:]))
}

// Owner: which save it is (the trainer and their ID), to match a cartridge to an unfinished change.
func (s *GBSave) Owner() string {
	n, id := s.Trainer()
	return fmt.Sprintf("%s|%05d|%s%s", n, id, s.Game, s.Region)
}

// GameName: the game, with the region when it isn't an international one.
func (s *GBSave) GameName() string {
	switch s.Region {
	case gbJapanese:
		return gbGameName(s.Game) + " (Japanese)"
	case gbKorean:
		return gbGameName(s.Game) + " (Korean)"
	}
	return gbGameName(s.Game)
}

func (s *GBSave) BoxNames() []string {
	out := make([]string, len(s.Boxes))
	for i := range out {
		out[i] = fmt.Sprintf("BOX %d", i+1)
		if s.Gen == 2 {
			o := s.l.boxNames + i*s.l.boxNameLen
			if n := strings.TrimSpace(decodeGBText(s.Raw[o:o+s.l.boxNameLen], 2, s.Region)); n != "" {
				out[i] = n
			}
		}
	}
	return out
}

// WithBoxes: the save with these boxes, written back as PKHeX's SAV1/SAV2 GetFinalData does:
// each changed box into its list (and the current box's copy), then the checksums, and in
// generation 2 the backup copy of the main data (or Korean Gold and Silver's second checksum).
func (s *GBSave) WithBoxes(boxes [][]GBMon) []byte {
	out := append([]byte(nil), s.Raw...)
	d := out[:gbSaveRAM]
	l := s.l
	if s.Gen == 1 {
		size := l.boxList(1)
		cur, inited := s.Current, s.inited
		if !inited {
			any := false
			for i := range boxes {
				if i != cur && hasAny(boxes[i]) {
					any = true
				}
			}
			if any {
				inited = true
				d[l.curBoxIndex] |= 0x80
			} else {
				cur = 0
				d[l.curBoxIndex] &= 0x80
			}
		}
		for i := range boxes {
			dest := d[l.boxOffset(1, i) : l.boxOffset(1, i)+size]
			written := mergeSingles(boxes[i], dest, 1, l.slots, pk1Box, l.strLen, inited)
			if i != cur {
				continue
			}
			curBox := d[l.currentBox : l.currentBox+size]
			if written {
				copy(curBox, dest)
			} else {
				// PKHeX passes "initialized" here too; the current box always lives in the main
				// data, so an emptied current box is written even before the banks are in use
				mergeSingles(boxes[i], curBox, 1, l.slots, pk1Box, l.strLen, true)
			}
		}
		if inited { // the banks' own checksums, as the game writes them (all the bank's boxes, then each one)
			for _, bank := range []int{0x4000, 0x6000} {
				all := bank + l.split*size
				d[all] = gbChecksum1(d[bank:all])
				for k := 0; k < l.split; k++ {
					o := bank + k*size
					d[all+1+k] = gbChecksum1(d[o : o+size])
				}
			}
		}
		d[l.checksum] = gbChecksum1(d[l.ot:l.checksum])
		return out
	}
	size := l.boxList(2)
	for i := range boxes {
		dest := d[l.boxOffset(2, i) : l.boxOffset(2, i)+size]
		if mergeSingles(boxes[i], dest, 2, l.slots, pk2Box, l.strLen, true) && i == s.Current {
			copy(d[l.currentBox:l.currentBox+size], dest)
		}
	}
	sum := gbChecksum2(d, l.sumEnd)
	binary.LittleEndian.PutUint16(d[l.sum1:], sum)
	binary.LittleEndian.PutUint16(d[l.sum2:], sum)
	switch {
	case s.Region == gbJapanese:
		n := 0xC83
		if s.Game == "C" {
			n = 0xADA
		}
		copy(d[0x7209:], d[s2Trainer:s2Trainer+n])
	case s.Region == gbKorean:
		binary.LittleEndian.PutUint16(d[0x7E6B:], gbChecksumKOR(d))
	case s.Game == "C":
		copy(d[0x1209:0x1209+0xB7A], d[0x2009:0x2009+0xB7A])
	default:
		copy(d[0x15C7:], d[0x2009:0x222F])
		copy(d[0x3D69:], d[0x222F:0x23D9])
		copy(d[0x0C6B:], d[0x23D9:0x2856])
		copy(d[0x7E39:], d[0x2856:0x288A])
		copy(d[0x10E8:], d[0x288A:0x2D69])
	}
	return out
}

func hasAny(box []GBMon) bool {
	for _, m := range box {
		if m.Present() {
			return true
		}
	}
	return false
}

// PartyMons: the Pokémon in the party (not moved by the bank, but counted for finishing a change).
func (s *GBSave) PartyMons() []GBMon {
	if s.Gen == 1 {
		return unpackList(s.Raw[s.l.party:], 1, 6, pk1Party, s.l.strLen)
	}
	return unpackList(s.Raw[s.l.party:], 2, 6, pk2Party, s.l.strLen)
}
