// One Pokémon as Ruby, Sapphire, Emerald, FireRed and LeafGreen keep it in the PC: 80 bytes, the
// last 48 of them encrypted and shuffled (pret's struct BoxPokemon). Used by the Pokémon Bank.
package main

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const pk3Size = 80 // a Pokémon in a PC box (the party adds 20 bytes of stats after it)

// where the growth, attacks, EVs and misc blocks are (0-3), by personality % 24
var pk3Order = [24][4]int{
	{0, 1, 2, 3}, {0, 1, 3, 2}, {0, 2, 1, 3}, {0, 3, 1, 2}, {0, 2, 3, 1}, {0, 3, 2, 1},
	{1, 0, 2, 3}, {1, 0, 3, 2}, {2, 0, 1, 3}, {3, 0, 1, 2}, {2, 0, 3, 1}, {3, 0, 2, 1},
	{1, 2, 0, 3}, {1, 3, 0, 2}, {2, 1, 0, 3}, {3, 1, 0, 2}, {2, 3, 0, 1}, {3, 2, 0, 1},
	{1, 2, 3, 0}, {1, 3, 2, 0}, {2, 1, 3, 0}, {3, 1, 2, 0}, {2, 3, 1, 0}, {3, 2, 1, 0},
}

const (
	speciesEgg   = 412 // the egg's picture and name
	speciesUnown = 201
	speciesMax   = 411 // the last real species (Chimecho)
)

// Pk3 is a Pokémon read from a box: its 80 bytes as stored, and what they say.
type Pk3 struct {
	Raw      [pk3Size]byte // as stored (encrypted)
	PID      uint32
	OTID     uint32
	Species  int
	Item     int
	Exp      uint32
	Friend   int
	Moves    [4]int
	PP       [4]int
	EVs      [6]int // HP Atk Def Spe SpA SpD
	IVs      [6]int // HP Atk Def Spe SpA SpD
	Ability  int    // which of the species' two abilities (0, 1)
	Egg      bool
	BadEgg   bool // the checksum is wrong (the games show it as a Bad Egg)
	Lang     int  // 1 Japanese, 2 English, 3 French, 4 Italian, 5 German, 7 Spanish
	Nick     []byte
	OTName   []byte
	OTFemale bool
	MetLevel int
	MetPlace int // the place it was met or hatched (pret's MAPSEC_*; 0xFE an in-game trade, 0xFF a fateful encounter)
	MetGame  int
	Pokerus  int    // strain (high 4 bits) and days left (low 4 bits): 0 never had it
	Ribbons  uint32 // the ribbons (and, in bit 31, whether it obeys: Mew and Deoxys)
	PPUps    int    // two bits a move
	Ball     int
	Markings int
}

// Empty: a free slot (the games leave it all zeros).
func pk3Empty(b []byte) bool {
	if len(b) < pk3Size {
		return true
	}
	if binary.LittleEndian.Uint32(b) == 0 && binary.LittleEndian.Uint32(b[4:]) == 0 {
		for _, c := range b[:pk3Size] {
			if c != 0 {
				return false
			}
		}
		return true
	}
	return false
}

// parsePk3 reads a box Pokémon. ok is false for an empty slot.
func parsePk3(b []byte) (p Pk3, ok bool) {
	if pk3Empty(b) {
		return p, false
	}
	copy(p.Raw[:], b[:pk3Size])
	le := binary.LittleEndian
	p.PID, p.OTID = le.Uint32(b), le.Uint32(b[4:])
	p.Nick = trimName(b[8:18])
	p.Lang = int(b[18])
	flags := b[19]
	p.OTName = trimName(b[20:27])
	p.Markings = int(b[27])
	key := p.PID ^ p.OTID
	var dec [48]byte
	var sum uint16
	for i := 0; i < 12; i++ {
		w := le.Uint32(b[32+4*i:]) ^ key
		le.PutUint32(dec[4*i:], w)
		sum += uint16(w) + uint16(w>>16)
	}
	p.BadEgg = sum != le.Uint16(b[28:]) || flags&1 != 0
	o := pk3Order[p.PID%24]
	g, a, e, m := dec[12*o[0]:], dec[12*o[1]:], dec[12*o[2]:], dec[12*o[3]:]
	p.Species = int(le.Uint16(g))
	p.Item = int(le.Uint16(g[2:]))
	p.Exp = le.Uint32(g[4:])
	p.PPUps = int(g[8])
	p.Friend = int(g[9])
	for i := 0; i < 4; i++ {
		p.Moves[i] = int(le.Uint16(a[2*i:]))
		p.PP[i] = int(a[8+i])
	}
	for i := 0; i < 6; i++ {
		p.EVs[i] = int(e[i])
	}
	p.Pokerus, p.MetPlace = int(m[0]), int(m[1])
	p.Ribbons = le.Uint32(m[8:])
	origin := le.Uint16(m[2:])
	p.MetLevel = int(origin & 0x7F)
	p.MetGame = int(origin>>7) & 0xF
	p.Ball = int(origin>>11) & 0xF
	p.OTFemale = origin>>15 != 0
	iv := le.Uint32(m[4:])
	for i := 0; i < 6; i++ {
		p.IVs[i] = int(iv>>(5*i)) & 31
	}
	p.Egg = iv>>30&1 != 0
	p.Ability = int(iv >> 31)
	return p, true
}

func trimName(b []byte) []byte {
	for i, c := range b {
		if c == 0xFF {
			return append([]byte(nil), b[:i]...)
		}
	}
	return append([]byte(nil), b...)
}

// Nature: 0-24 (Hardy ... Quirky).
func (p *Pk3) Nature() int { return int(p.PID % 25) }

// Shiny for its own Trainer ID and Secret ID.
func (p *Pk3) Shiny() bool {
	return (p.OTID>>16)^(p.OTID&0xFFFF)^(p.PID>>16)^(p.PID&0xFFFF) < 8
}

func (p *Pk3) TID() int { return int(p.OTID & 0xFFFF) }
func (p *Pk3) SID() int { return int(p.OTID >> 16) }

// UnownForm: Unown's letter (0 A ... 25 Z, 26 !, 27 ?).
func (p *Pk3) UnownForm() int {
	v := p.PID
	return int((v>>24&3)<<6|(v>>16&3)<<4|(v>>8&3)<<2|v&3) % 28
}

// IconIndex: which party icon the games show for it (Unown has one per letter, eggs their own).
func (p *Pk3) IconIndex() int {
	switch {
	case p.Egg:
		return speciesEgg
	case p.Species == speciesUnown && p.UnownForm() > 0:
		return speciesEgg + p.UnownForm() // SPECIES_UNOWN_B is 413
	}
	return p.Species
}

// Valid: a Pokémon the games will take (a real species, the checksum right).
func (p *Pk3) Valid() bool {
	return !p.BadEgg && p.Species >= 1 && p.Species <= speciesMax
}

// Identity: the same Pokémon wherever it is (its personality and its trainer's IDs), even
// after it has gained experience.
func (p *Pk3) Identity() uint64 { return uint64(p.PID)<<32 | uint64(p.OTID) }

// Japanese: its names are in the Japanese character set.
func (p *Pk3) Japanese() bool { return p.Lang == 1 }

// decodeGameText turns a name in the games' character set into text.
func decodeGameText(b []byte, japanese bool) string {
	set := gameCharset()["intl"]
	if japanese {
		set = gameCharset()["jp"]
	}
	var sb strings.Builder
	for _, c := range b {
		if c == 0xFF {
			break
		}
		if t, ok := set[c]; ok {
			sb.WriteString(t)
		} else {
			sb.WriteString("?")
		}
	}
	return strings.TrimSpace(sb.String())
}

// expFor: the experience a Pokémon of a growth rate (0-5, pret's GROWTH_*) needs for a level.
func expFor(rate, n int) int {
	switch rate {
	case 1: // erratic
		switch {
		case n <= 50:
			return (100 - n) * n * n * n / 50
		case n <= 68:
			return (150 - n) * n * n * n / 100
		case n <= 98:
			return (1911 - 10*n) / 3 * n * n * n / 500
		}
		return (160 - n) * n * n * n / 100
	case 2: // fluctuating
		switch {
		case n <= 15:
			return (24 + (n+1)/3) * n * n * n / 50
		case n <= 36:
			return (14 + n) * n * n * n / 50
		}
		return (32 + n/2) * n * n * n / 50
	case 3: // medium slow
		return 6*n*n*n/5 - 15*n*n + 100*n - 140
	case 4: // fast
		return 4 * n * n * n / 5
	case 5: // slow
		return 5 * n * n * n / 4
	}
	return n * n * n // medium fast
}

// levelFor: the level that much experience gives.
func levelFor(rate int, exp uint32) int {
	lv := 1
	for n := 2; n <= 100; n++ {
		if int64(exp) >= int64(expFor(rate, n)) {
			lv = n
		} else {
			break
		}
	}
	return lv
}

var natureNamesEN = [25]string{"Hardy", "Lonely", "Brave", "Adamant", "Naughty", "Bold", "Docile", "Relaxed",
	"Impish", "Lax", "Timid", "Hasty", "Serious", "Jolly", "Naive", "Modest", "Mild", "Quiet", "Bashful",
	"Rash", "Calm", "Gentle", "Sassy", "Careful", "Quirky"}

var ballNames = map[int]string{1: "Master Ball", 2: "Ultra Ball", 3: "Great Ball", 4: "Poké Ball", 5: "Safari Ball",
	6: "Net Ball", 7: "Dive Ball", 8: "Nest Ball", 9: "Repeat Ball", 10: "Timer Ball", 11: "Luxury Ball", 12: "Premier Ball"}

var metGameNames = map[int]string{1: "Sapphire", 2: "Ruby", 3: "Emerald", 4: "FireRed", 5: "LeafGreen", 15: "Colosseum / XD"}

// RibbonCount: how many ribbons it has (each contest rank counts as one).
func (p *Pk3) RibbonCount() int {
	n := 0
	for i := 0; i < 5; i++ { // Cool, Beauty, Cute, Smart, Tough: 0-4 (none, Normal ... Master)
		n += int(p.Ribbons>>(3*i)) & 7
	}
	for bit := 15; bit < 27; bit++ {
		n += int(p.Ribbons>>bit) & 1
	}
	return n
}

// HiddenPower: the type and power of its Hidden Power (from its IVs).
func (p *Pk3) HiddenPower() (string, int) {
	// the IVs in the order the formula takes them: HP Atk Def Spe SpA SpD (as stored)
	t, pw := 0, 0
	for i, v := range p.IVs {
		t |= (v & 1) << i
		pw |= (v >> 1 & 1) << i
	}
	return hiddenPowerTypes[t*15/63], pw*40/63 + 30
}

var hiddenPowerTypes = []string{"Fighting", "Flying", "Poison", "Ground", "Rock", "Bug", "Ghost", "Steel",
	"Fire", "Water", "Grass", "Electric", "Psychic", "Ice", "Dragon", "Dark"}

// natureEffect: what a nature raises and lowers ("" for the neutral ones).
func natureEffect(n int) (up, down string) {
	stats := []string{"Atk", "Def", "Spe", "SpA", "SpD"}
	if n/5 == n%5 {
		return "", ""
	}
	return stats[n/5], stats[n%5]
}

// Stats works out its stats (HP Atk Def Spe SpA SpD) from its base stats, IVs, EVs, nature and level.
func (p *Pk3) Stats(base [6]int, level int) [6]int {
	var out [6]int
	for i := 0; i < 6; i++ {
		v := (2*base[i] + p.IVs[i] + p.EVs[i]/4) * level / 100
		if i == 0 {
			out[i] = v + level + 10
			if base[0] == 1 { // Shedinja
				out[i] = 1
			}
			continue
		}
		v += 5
		up, down := p.Nature()/5+1, p.Nature()%5+1 // 1 Atk ... 5 SpD (stored order after HP)
		if up != down {
			switch i {
			case up:
				v = v * 110 / 100
			case down:
				v = v * 90 / 100
			}
		}
		out[i] = v
	}
	return out
}

var languageNames = map[int]string{1: "Japanese", 2: "English", 3: "French", 4: "Italian", 5: "German", 7: "Spanish"}

// RibbonNames: its ribbons, as PKHeX names them.
func (p *Pk3) RibbonNames() []string {
	var out []string
	ranks := []string{"", " Super", " Hyper", " Master"}
	for i, cat := range []string{"Cool", "Beauty", "Cute", "Smart", "Tough"} {
		for r := 0; r < min(int(p.Ribbons>>(3*i))&7, 4); r++ { // each rank won means that ribbon and the ones below
			out = append(out, cat+ranks[r])
		}
	}
	others := []string{"Champion", "Winning", "Victory", "Artist", "Effort", "Battle Champion", "Regional Champion",
		"National Champion", "Country", "National", "Earth", "World"}
	for i, n := range others {
		if p.Ribbons>>(15+i)&1 != 0 {
			out = append(out, n)
		}
	}
	return out
}

// ------------------------------------------------------------------------- .pk3 files
// PKHeX keeps a .pk3 decrypted, with its four blocks in order (growth, attacks, EVs, misc); the
// games keep it encrypted and shuffled. PKHeX reads either: it decrypts a file whose checksum
// doesn't match its bytes as they are. These convert between the two.

// pk3ToFile: the 80 bytes as PKHeX saves them. A Pokémon whose checksum is wrong (a Bad Egg)
// is kept as the game had it.
func pk3ToFile(raw []byte) []byte {
	p, ok := parsePk3(raw)
	if !ok || p.BadEgg {
		return append([]byte(nil), raw[:pk3Size]...)
	}
	le := binary.LittleEndian
	out := append([]byte(nil), raw[:32]...)
	key := p.PID ^ p.OTID
	var dec [48]byte
	for i := 0; i < 12; i++ {
		le.PutUint32(dec[4*i:], le.Uint32(raw[32+4*i:])^key)
	}
	o := pk3Order[p.PID%24]
	for blk := 0; blk < 4; blk++ { // growth, attacks, EVs, misc
		out = append(out, dec[12*o[blk]:12*o[blk]+12]...)
	}
	return out
}

// pk3FromFile: a .pk3 (or a 100-byte party .pk3) as the games store it, or an error.
func pk3FromFile(data []byte) ([]byte, error) {
	if len(data) != pk3Size && len(data) != 100 {
		return nil, fmt.Errorf("a .pk3 is 80 or 100 bytes, this is %d", len(data))
	}
	le := binary.LittleEndian
	b := append([]byte(nil), data[:pk3Size]...)
	var sum uint16
	for i := 0; i < 24; i++ {
		sum += le.Uint16(b[32+2*i:])
	}
	if sum != le.Uint16(b[28:]) {
		return b, nil // not decrypted: as the games keep it (or damaged: kept as it is)
	}
	pid, otid := le.Uint32(b), le.Uint32(b[4:])
	if pid == 0 && otid == 0 && sum == 0 {
		return nil, fmt.Errorf("it's empty")
	}
	o := pk3Order[pid%24]
	var shuffled [48]byte
	for blk := 0; blk < 4; blk++ {
		copy(shuffled[12*o[blk]:], b[32+12*blk:32+12*blk+12])
	}
	key := pid ^ otid
	for i := 0; i < 12; i++ {
		le.PutUint32(b[32+4*i:], le.Uint32(shuffled[4*i:])^key)
	}
	return b, nil
}
