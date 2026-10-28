// Writing ROMs to GBA flash carts and reproduction carts, as GB-Link Cart Doctor does it
// (web/gba_flash.js, GPL-3.0). This builds small programs of cartridge operations and Cart
// Doctor's dumper on the GBA runs them (gbacart.go). Detection works like FlashGBX (each
// profile's "read flash ID", then the chip's own description, CFI); carts no profile knows get
// Burn2Slot's scan (github.com/vrodin/Burn2Slot, MIT). Only the sectors the ROM covers are erased,
// skipping those that already hold the right data, then 64 KB at a time is written and checked.
package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"
	"strings"
	"time"
)

const (
	copEnd, copW16, copW8S, copRD16, copRD8S, copWait, copDelay, copCfg, copProg, copVerify, copCRC32, copShow, copSRCmd = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12
	copBlank                                                                                                             = 0xFFFFFFFF
	copSame                                                                                                              = 0xFFFFFFFF
	copStOK, copStTimeout, copStBadOp, copStOverflow                                                                     = 0, 1, 2, 3
	copSetAMD, copSetIntel                                                                                               = 1, 2
	copKNone, copKAbs, copKSA, copKPA                                                                                    = 0, 1, 2, 3
	copKImm, copKBS, copKPD                                                                                              = 1, 2, 3
	copShowDetect, copShowErase, copShowWrite, copShowVerify, copShowDone, copShowFail                                   = 1, 2, 3, 4, 5, 6
	copBufMax                                                                                                            = 0x20000
	copResultWords                                                                                                       = 0x1000 - 2

	gbafChunk      = 0x10000
	gbafEraseMS    = 30000
	gbafUnitMS     = 250
	gbafBankWindow = 0x2000000
)

type copHandle struct{ index, at, words int }

type copBlob struct{ blob int }

// copProgram is one cart op program.
type copProgram struct {
	ops         [][4]any
	blobs       [][]byte
	resultWords int
}

func (p *copProgram) op(o, a, b, c any, words int) copHandle {
	h := copHandle{len(p.ops), p.resultWords, words}
	p.ops = append(p.ops, [4]any{o, a, b, c})
	p.resultWords += words
	return h
}

func (p *copProgram) attach(b []byte) copBlob {
	p.blobs = append(p.blobs, b)
	return copBlob{len(p.blobs) - 1}
}
func (p *copProgram) w16(a, v int) copHandle  { return p.op(copW16, a, v&0xFFFF, 0, 0) }
func (p *copProgram) w8s(a, v int) copHandle  { return p.op(copW8S, a, v&0xFF, 0, 0) }
func (p *copProgram) rd16(a, n int) copHandle { return p.op(copRD16, a, n, 0, (n+3)/4) }
func (p *copProgram) wait(a, v, mask, ms int) copHandle {
	return p.op(copWait, a, uint32(mask&0xFFFF)<<16|uint32(v&0xFFFF), ms, 1)
}
func (p *copProgram) cfg(ref copBlob, n int) copHandle { return p.op(copCfg, ref, n, 0, 0) }
func (p *copProgram) prog(ref copBlob, a, n int) copHandle {
	return p.op(copProg, ref, a, n, 2)
}
func (p *copProgram) verify(ref any, a, n int) copHandle {
	if ref == nil {
		ref = uint32(copBlank)
	}
	return p.op(copVerify, ref, a, n, 2)
}
func (p *copProgram) crc32(a, n int) copHandle { return p.op(copCRC32, a, n, 0, 1) }
// show: what the GBA's screen says, with its bar at pct. The GBA takes whole percents, so pct is
// rounded to the nearest (cutting the fraction off left its bar up to 1% behind the PC's).
func (p *copProgram) show(id int, pct float64) copHandle {
	return p.op(copShow, id, max(0, min(100, int(pct+0.5))), 0, 0)
}
func (p *copProgram) srcmd(a int, v any) copHandle {
	val := uint32(0xFFFFFFFF)
	if v != nil {
		val = uint32(v.(int) & 0xFFFF)
	}
	return p.op(copSRCmd, a, val, 0, 0)
}

func (p *copProgram) build() ([]byte, error) {
	size := (len(p.ops) + 1) * 16
	offs := make([]int, len(p.blobs))
	for i, b := range p.blobs {
		offs[i] = size
		size = (size + len(b) + 3) &^ 3
	}
	size = (size + 0xFF) &^ 0xFF
	if size > copBufMax {
		return nil, fmt.Errorf("cart op program too large")
	}
	if p.resultWords > copResultWords {
		return nil, fmt.Errorf("cart op program reads too much")
	}
	buf := make([]byte, size)
	val := func(v any) uint32 {
		switch x := v.(type) {
		case int:
			return uint32(x)
		case uint32:
			return x
		case copBlob:
			return uint32(offs[x.blob])
		}
		return 0
	}
	for i, o := range p.ops {
		for k := 0; k < 4; k++ {
			binary.LittleEndian.PutUint32(buf[i*16+k*4:], val(o[k]))
		}
	}
	for i, b := range p.blobs {
		copy(buf[offs[i]:], b)
	}
	return buf, nil
}

type copResult struct {
	words            []uint32
	executed, status int
}

func (r *copResult) word(h copHandle, k int) uint32 { return r.words[2+h.at+k] }
func (r *copResult) bytes(h copHandle, n int) []byte {
	out := make([]byte, h.words*4)
	for i := 0; i < h.words; i++ {
		binary.LittleEndian.PutUint32(out[4*i:], r.words[2+h.at+i])
	}
	return out[:n]
}

// ------------------------------------------------------------ Burn2Slot's detection
var b2sSeqs = [6][6]int{
	{0x555, 0x2AA, 0x555, 0x555, 0x2AA, 0x555}, {0xAAA, 0x555, 0xAAA, 0xAAA, 0x555, 0xAAA},
	{0xAAA, 0x554, 0xAAA, 0xAAA, 0x554, 0xAAA}, {0x5555, 0x2AAA, 0x5555, 0x5555, 0x2AAA, 0x5555},
	{0xAAAA, 0x5554, 0xAAAA, 0xAAAA, 0x5554, 0xAAAA}, {0x1554, 0xAAA, 0x1554, 0x1554, 0xAAA, 0x1554},
}
var b2sBus1 = []int{15, 7, 14, 6, 13, 5, 12, 4, 0, 8, 1, 9, 2, 10, 3, 11}
var b2sBus2 = []int{0, 8, 1, 9, 6, 14, 7, 15, 2, 10, 3, 11, 4, 12, 5, 13}
var b2sWiringNames = []string{"straight", "D0/D1 swapped", "data lines reordered (type 1)",
	"data lines reordered (type 2)", "8-bit, doubled", "8-bit, doubled, D0/D1 swapped"}

func b2sSwapTo(bus []int, v int) int {
	r := 0
	for i := 0; i < 16; i++ {
		r |= (v >> i & 1) << bus[i]
	}
	return r
}
func b2sSwapFrom(bus []int, v int) int {
	r := 0
	for i := 0; i < 16; i++ {
		r |= (v >> bus[i] & 1) << i
	}
	return r
}
func b2sBusOut(bus, v int) int {
	switch bus {
	case 1:
		return swapBitPair(v, 0, 1)
	case 2:
		return b2sSwapTo(b2sBus1, v)
	case 3:
		return b2sSwapTo(b2sBus2, v)
	case 4:
		return (v<<8 | v) & 0xFFFF
	case 5:
		return swapBitPair((v<<8|v)&0xFFFF, 0, 1)
	}
	return v
}
func b2sBusIn(bus, v int) int {
	switch bus {
	case 1, 5:
		return swapBitPair(v, 0, 1)
	case 2:
		return b2sSwapFrom(b2sBus1, v)
	case 3:
		return b2sSwapFrom(b2sBus2, v)
	}
	return v
}

func f64(v int) any { return float64(v) }

func b2sAMDProfile(seq, bus int, cfi *cfiInfo, id []byte) *FlashProfile {
	s := b2sSeqs[seq]
	a := func(i int) any { return f64(s[i] * 2) }
	v := func(x int) any { return f64(b2sBusOut(bus, x)) }
	none := []any{nil, nil, nil}
	p := &FlashProfile{ID: "B2S_AMD", Names: []string{"AMD-style flash, " + b2sWiringNames[bus] + " (Burn2Slot detection)"},
		CommandSet: "AMD", Synthetic: true, FlashIDs: [][]int{bytesToInts(id)},
		Commands: map[string][][]any{
			"reset":                 {{a(0), v(0xAA)}, {a(1), v(0x55)}, {a(2), v(0xF0)}},
			"read_identifier":       {{a(0), v(0xAA)}, {a(1), v(0x55)}, {a(2), v(0x90)}},
			"sector_erase":          {{a(0), v(0xAA)}, {a(1), v(0x55)}, {a(2), v(0x80)}, {a(3), v(0xAA)}, {a(4), v(0x55)}, {"SA", v(0x30)}},
			"sector_erase_wait_for": {none, none, none, none, none, {"SA", f64(0xFFFF), f64(0xFFFF)}},
			"single_write":          {{a(0), v(0xAA)}, {a(1), v(0x55)}, {a(2), v(0xA0)}, {"PA", "PD"}},
			"single_write_wait_for": {none, none, none, none},
		}}
	if cfi != nil && len(cfi.Regions) > 0 {
		r := cfi.Regions
		if cfi.BootFlag == 3 {
			r = make([][2]int, len(cfi.Regions))
			for i, x := range cfi.Regions {
				r[len(r)-1-i] = x
			}
		}
		p.SectorSize = mustJSON(r)
		p.FlashSize = cfi.DeviceSize
	} else {
		p.SectorSize = mustJSON(0x10000) // no map: erase every 64 KB as Burn2Slot does
	}
	return p
}

func bytesToInts(b []byte) []int {
	out := make([]int, len(b))
	for i, x := range b {
		out[i] = int(x)
	}
	return out
}

// ----------------------------------------------------------------------------- the engine
type GBAFlashcart struct {
	d        *gbaDumper
	log      func(string, ...any)
	detected *FlashDetect
	lastFail int
	lastErr  error
}

func (f *GBAFlashcart) exec(p *copProgram, timeout time.Duration) (*copResult, error) {
	buf, err := p.build()
	if err != nil {
		return nil, err
	}
	var words []uint32
	for attempt := 1; ; attempt++ {
		if err := f.d.cancelled(); err != nil {
			return nil, err
		}
		words, err = f.d.runOps(buf, timeout)
		if err == nil {
			break
		}
		fe, ok := err.(*FlashError)
		if !ok || (fe.Code != "desync" && fe.Code != "checksum") || attempt >= 3 {
			return nil, err
		}
		f.log("Link error (%v): trying again.", err)
	}
	r := &copResult{words: words, executed: int(words[0]), status: int(words[1])}
	if r.status == copStBadOp || r.status == copStOverflow {
		return nil, &FlashError{"payload", fmt.Sprintf("The GBA refused cart op %d (status %d).", r.executed, r.status)}
	}
	return r, nil
}

func flashable(profiles []*FlashProfile) []*FlashProfile {
	var out []*FlashProfile
	for _, p := range profiles {
		set := commandSet(p)
		if (set == "AMD" || set == "INTEL" || set == "SHARP") &&
			(p.cmd("single_write") != nil || p.cmd("buffer_write") != nil || p.cmd("page_write") != nil) &&
			(p.cmd("sector_erase") != nil || p.cmd("chip_erase") != nil) {
			out = append(out, p)
		}
	}
	return out
}

// GBAFlashableProfiles: the GBA cart types that can be written.
func GBAFlashableProfiles() []*FlashProfile { return flashable(GBAFlashProfiles()) }

func copCmds(p *copProgram, cmds [][]any, base int) {
	for _, c := range cmds {
		if isNil(el(c, 0)) || !isNum(c[0]) {
			continue
		}
		p.w16(base+num(c[0]), num(el(c, 1)))
	}
}

// Detect identifies the flash chip and picks a profile.
func (f *GBAFlashcart) Detect() (*FlashDetect, error) {
	profiles := GBAFlashableProfiles()
	f.detected = nil
	prog := &copProgram{}
	prog.show(copShowDetect, 0)
	romH := prog.rd16(0, 8)
	type m29 struct {
		p             *FlashProfile
		before, after copHandle
	}
	var ms []m29
	for _, p := range profiles {
		if !p.M29W640 {
			continue
		}
		copCmds(prog, p.cmd("reset"), 0)
		b := prog.rd16(0, 8)
		copCmds(prog, p.cmd("read_identifier"), 0)
		a := prog.rd16(0, 8)
		copCmds(prog, p.cmd("reset"), 0)
		ms = append(ms, m29{p, b, a})
	}
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return nil, err
	}
	rom := r.bytes(romH, 8)
	for _, m := range ms {
		before, after := r.bytes(m.before, 8), r.bytes(m.after, 8)
		if string(before) != string(after) && strings.HasPrefix(string(after), string(intsToBytes(m.p.FlashIDs[0]))) {
			return f.finishDetect([]*FlashProfile{m.p}, after, "M29W640 check", nil)
		}
	}
	var resets [][][]any
	type group struct {
		rid, reset [][]any
		h          copHandle
	}
	var groups []*group
	for _, p := range profiles {
		if p.M29W640 {
			continue
		}
		if c := p.cmd("reset"); c != nil && !containsCmds(resets, c) {
			resets = append(resets, c)
		}
		if c := p.cmd("unlock"); c != nil && !containsCmds(resets, c) {
			resets = append(resets, c)
		}
		rid := p.cmd("read_identifier")
		if len(rid) > 0 {
			dup := false
			for _, g := range groups {
				dup = dup || sameCmds(g.rid, rid)
			}
			if !dup {
				groups = append(groups, &group{rid: rid, reset: p.cmd("reset")})
			}
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return num(groups[i].rid[0][0]) < num(groups[j].rid[0][0]) })
	prog = &copProgram{}
	for _, c := range resets {
		copCmds(prog, c, 0)
	}
	for _, g := range groups {
		copCmds(prog, g.reset, 0)
		copCmds(prog, g.rid, 0)
		g.h = prog.rd16(0, 8)
		copCmds(prog, g.reset, 0)
	}
	prog.w16(0, 0xFF)
	prog.w16(0, 0xF0)
	if r, err = f.exec(prog, 60*time.Second); err != nil {
		return nil, err
	}
	type method struct {
		rid [][]any
		id  []byte
	}
	var methods []method
	for _, g := range groups {
		if cmp := r.bytes(g.h, 8); string(cmp) != string(rom) {
			methods = append(methods, method{g.rid, cmp})
			f.log("Flash ID via 0x%X/0x%X: %s", num(g.rid[0][0]), num(el(g.rid[0], 1)), hexBytes(cmp))
		}
	}
	var cands []*FlashProfile
	for _, p := range profiles {
		if len(p.FlashIDs) == 0 || p.cmd("read_identifier") == nil {
			continue
		}
		for _, m := range methods {
			if !sameCmds(m.rid, p.cmd("read_identifier")) {
				continue
			}
			for _, id := range p.FlashIDs {
				if strings.HasPrefix(string(m.id), string(intsToBytes(id))) {
					cands = append(cands, p)
					break
				}
			}
			break
		}
	}
	var firstRid [][]any
	if len(methods) > 0 {
		firstRid = methods[0].rid
	}
	if len(cands) > 0 {
		for _, m := range methods {
			if sameCmds(m.rid, cands[0].cmd("read_identifier")) {
				return f.finishDetect(cands, m.id, "FlashGBX profiles", firstRid)
			}
		}
	}
	if d, err := f.detectB2S(rom, profiles); d != nil || err != nil {
		return d, err
	}
	seen := "none"
	if len(methods) > 0 {
		var s []string
		for _, m := range methods {
			s = append(s, hexBytes(m.id))
		}
		seen = strings.Join(s, ", ")
	}
	return nil, &FlashError{"no-flash", "No writable flash chip was found. Either this isn't a flash cart, or its chip isn't one the profiles know (flash IDs seen: " + seen + ")."}
}

func (f *GBAFlashcart) readCFIRaw(readCfi, reset [][]any) ([]byte, error) {
	prog := &copProgram{}
	copCmds(prog, readCfi, 0)
	h := prog.rd16(0, 0x400)
	copCmds(prog, reset, 0)
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return nil, err
	}
	return r.bytes(h, 0x400), nil
}

func (f *GBAFlashcart) finishDetect(cands []*FlashProfile, id []byte, how string, firstRid [][]any) (*FlashDetect, error) {
	first := cands[0]
	readCfi := first.cmd("read_cfi")
	if readCfi == nil {
		a := 0
		if len(firstRid) > 0 {
			a = num(firstRid[0][0])
		}
		readCfi = [][]any{{f64(a), f64(0x98)}}
	}
	var cfi *cfiInfo
	raw, err := f.readCFIRaw(readCfi, first.cmd("reset"))
	if err != nil {
		if fe, ok := err.(*FlashError); ok && fe.Code == "cancelled" {
			return nil, err
		}
		if _, ok := err.(*LinkError); ok {
			return nil, err
		}
	} else {
		cfi = parseCFI(raw)
	}
	profile := first
	sizes := map[int]bool{}
	for _, p := range cands {
		if p.FlashSize > 0 {
			sizes[p.FlashSize] = true
		}
	}
	if len(sizes) > 1 {
		if p, err := f.pickBySize(cands, cfi); err != nil {
			return nil, err
		} else if p != nil {
			profile = p
		}
	}
	f.detected = &FlashDetect{Profile: profile, Candidates: cands, FlashID: id, CFI: cfi, Method: how}
	return f.detected, nil
}

func (f *GBAFlashcart) pickBySize(cands []*FlashProfile, cfi *cfiInfo) (*FlashProfile, error) {
	first := cands[0]
	if first.FlashBankSelectType == 1 {
		maxBanks := first.FlashSize/gbafBankWindow + 1
		prog := &copProgram{}
		type probe struct {
			bank   int
			lo, hi copHandle
		}
		var probes []probe
		for bank := 0; bank == 0 || bank < maxBanks; {
			f.selectBank(prog, first, bank)
			probes = append(probes, probe{bank, prog.crc32(0, 0x1000), prog.crc32(0x1FFF000, 0x1000)})
			if bank == 0 {
				bank = 1
			} else {
				bank <<= 1
			}
		}
		f.selectBank(prog, first, 0)
		r, err := f.exec(prog, 60*time.Second)
		if err != nil {
			return nil, err
		}
		sig := func(p probe) [2]uint32 { return [2]uint32{r.word(p.lo, 0), r.word(p.hi, 0)} }
		banks := 1
		for i := 1; i < len(probes); i++ {
			if sig(probes[i]) == sig(probes[0]) {
				break
			}
			banks = probes[i].bank << 1
		}
		for _, p := range cands {
			if p.FlashSize == gbafBankWindow*banks {
				return p, nil
			}
		}
		return nil, nil
	}
	if cfi != nil && cfi.DeviceSize > 0 {
		for _, p := range cands {
			if p.FlashSize == cfi.DeviceSize {
				return p, nil
			}
		}
	}
	// a small chip repeats itself across the address space: the header shows up again at its size
	prog := &copProgram{}
	head := prog.rd16(0xA0, 16)
	type probe struct {
		a int
		h copHandle
	}
	var probes []probe
	for a := 0x10000; a < gbafBankWindow; a *= 2 {
		probes = append(probes, probe{a, prog.rd16(a+0xA0, 16)})
	}
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return nil, err
	}
	want := string(r.bytes(head, 16))
	size := gbafBankWindow
	for _, p := range probes {
		if string(r.bytes(p.h, 16)) == want {
			size = p.a
			break
		}
	}
	for _, p := range cands {
		if p.FlashSize == size {
			return p, nil
		}
	}
	return nil, nil
}

func (f *GBAFlashcart) detectB2S(rom []byte, profiles []*FlashProfile) (*FlashDetect, error) {
	prog := &copProgram{}
	prog.w16(0, 0xFF)
	prog.w16(0, 0x50)
	prog.w16(0, 0x90)
	intel := prog.rd16(0, 4)
	prog.w16(0, 0xFF)
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return nil, err
	}
	iid := r.bytes(intel, 4)
	maker, dev := int(iid[0])|int(iid[1])<<8, int(iid[2])|int(iid[3])<<8
	if string(iid) != string(rom[:4]) && (dev == 0x8902 || dev == 0x8904 || dev == 0x887D || dev == 0x887E || dev>>8 == 0x88 ||
		(dev == 0xB0 && maker == 0xB0) || maker == 0x1C) {
		for _, g := range profiles {
			if g.ID == "AGB_Generic_0_90" {
				p := *g
				p.ID, p.Names, p.FlashIDs, p.Synthetic = "B2S_INTEL", []string{"Intel-style flash (Burn2Slot detection)"}, [][]int{bytesToInts(iid)}, true
				return f.finishDetect([]*FlashProfile{&p}, iid, "Burn2Slot Intel scan", nil)
			}
		}
	}
	resetAll := func(p *copProgram) {
		for bus := 0; bus < 6; bus++ {
			p.w16(0, b2sBusOut(bus, 0xF0))
		}
	}
	prog = &copProgram{}
	type try struct {
		seq, bus int
		h        copHandle
	}
	var tries []try
	for seq := range b2sSeqs {
		s := b2sSeqs[seq]
		for bus := 0; bus < 6; bus++ {
			resetAll(prog)
			prog.w16(s[0]*2, b2sBusOut(bus, 0xAA))
			prog.w16(s[1]*2, b2sBusOut(bus, 0x55))
			prog.w16(s[2]*2, b2sBusOut(bus, 0x90))
			tries = append(tries, try{seq, bus, prog.rd16(0, 4)})
		}
	}
	resetAll(prog)
	if r, err = f.exec(prog, 60*time.Second); err != nil {
		return nil, err
	}
	for _, t := range tries {
		raw := r.bytes(t.h, 4)
		if string(raw) == string(rom[:4]) {
			continue
		}
		id := b2sBusIn(t.bus, int(raw[2])|int(raw[3])<<8)
		if id>>8 == 0x22 || id == 0x7E7E || id == 0x57 || id == 0xF8 {
			out := func(v int) any { return f64(b2sBusOut(t.bus, v)) }
			s := b2sSeqs[t.seq]
			cfiRaw, err := f.readCFIRaw([][]any{{f64(0), out(0x50)}, {f64(0), out(0xFF)}, {f64(0xAA), out(0x98)}},
				[][]any{{f64(s[0] * 2), out(0xAA)}, {f64(s[1] * 2), out(0x55)}, {f64(s[2] * 2), out(0xF0)}})
			if err != nil {
				return nil, err
			}
			dec := make([]byte, 0x400)
			for i := 0; i < 0x400; i += 2 {
				w := b2sBusIn(t.bus, int(cfiRaw[i])|int(cfiRaw[i+1])<<8)
				dec[i], dec[i+1] = byte(w), byte(w>>8)
			}
			cfi := parseCFI(dec)
			p := b2sAMDProfile(t.seq, t.bus, cfi, raw)
			f.detected = &FlashDetect{Profile: p, Candidates: []*FlashProfile{p}, FlashID: raw, CFI: cfi, Method: "Burn2Slot AMD scan"}
			return f.detected, nil
		}
	}
	return nil, nil
}

// ------------------------------------------------------------------------------ writing
type gbaWritePlan struct {
	method     string
	unit, bs   int
	cfg        []byte
	bufferSize int
}

func gbaWritePlanFor(p *FlashProfile, cfi *cfiInfo) (*gbaWritePlan, error) {
	set := commandSet(p)
	bufSize := p.BufferSize
	if bufSize == 0 && cfi != nil {
		bufSize = cfi.BufferSize
	}
	var method string
	var cmds, waits [][]any
	switch {
	case p.cmd("buffer_write") != nil && bufSize > 0:
		method, cmds, waits = "buffered", p.cmd("buffer_write"), p.cmd("buffer_write_wait_for")
	case p.cmd("page_write") != nil && bufSize > 0:
		method, cmds, waits = "paged", p.cmd("page_write"), p.cmd("page_write_wait_for")
	case p.cmd("single_write") != nil:
		method, cmds, waits = "single", p.cmd("single_write"), p.cmd("single_write_wait_for")
	default:
		return nil, &FlashError{"unsupported", "This cart type has no way of writing the GBA can use."}
	}
	unit := 2
	if method != "single" {
		unit = bufSize
	}
	chips := 1
	for _, c := range cmds {
		if str(el(c, 0)) == "SA+2" {
			chips = 2
		}
	}
	bs := 0
	if method != "single" {
		bs = unit/2/chips - 1
	}
	if p.DoubleDie {
		bs = bs<<8 | bs
	}
	srMask, srValue := 0x80, 0x80
	if p.StatusRegisterMask != nil {
		srMask = *p.StatusRegisterMask
	}
	if p.StatusRegisterValue != nil {
		srValue = *p.StatusRegisterValue
	}
	addrKind := func(a any) (uint32, uint32, error) {
		switch {
		case a == nil:
			return copKNone, 0, nil
		case isNum(a):
			return copKAbs, uint32(num(a)), nil
		case str(a) == "PA":
			return copKPA, 0, nil
		case str(a) == "SA":
			return copKSA, 0, nil
		case strings.HasPrefix(str(a), "SA+"):
			n := 0
			fmt.Sscanf(str(a)[3:], "%d", &n)
			return copKSA, uint32(n), nil
		}
		return 0, 0, &FlashError{"unsupported", fmt.Sprintf("Unknown address %v in the profile", a)}
	}
	valKind := func(v any) (uint32, uint32, error) {
		switch {
		case v == nil:
			return copKNone, 0, nil
		case isNum(v):
			return copKImm, uint32(num(v)), nil
		case str(v) == "BS":
			return copKBS, 0, nil
		case str(v) == "PD":
			return copKPD, 0, nil
		}
		return 0, 0, &FlashError{"unsupported", fmt.Sprintf("Unknown value %v in the profile", v)}
	}
	setW := uint32(copSetIntel)
	if set == "AMD" {
		setW = copSetAMD
	}
	words := []uint32{setW, uint32(unit), uint32(bs), uint32(srValue&0xFFFF)<<16 | uint32(srMask&0xFFFF), gbafUnitMS, uint32(len(cmds))}
	if len(cmds) > 12 {
		return nil, &FlashError{"unsupported", "The cart type's write sequence is too long"}
	}
	for i, c := range cmds {
		var w []any
		if i < len(waits) {
			w = waits[i]
		}
		ak, a, err := addrKind(el(c, 0))
		if err != nil {
			return nil, err
		}
		vk, v, err := valKind(el(c, 1))
		if err != nil {
			return nil, err
		}
		wak, wa, err := addrKind(el(w, 0))
		if err != nil {
			return nil, err
		}
		wvk, wv := uint32(copKNone), uint32(0)
		if !isNil(el(w, 0)) {
			if wvk, wv, err = valKind(el(w, 1)); err != nil {
				return nil, err
			}
		}
		wm := uint32(0xFFFF)
		if !isNil(el(w, 2)) {
			wm = uint32(num(el(w, 2)))
		}
		words = append(words, ak|vk<<8|wak<<16|wvk<<24, a, v, wa, wv, wm)
	}
	cfg := make([]byte, len(words)*4)
	for i, w := range words {
		binary.LittleEndian.PutUint32(cfg[4*i:], w)
	}
	return &gbaWritePlan{method, unit, bs, cfg, bufSize}, nil
}

func (f *GBAFlashcart) selectBank(p *copProgram, prof *FlashProfile, bank int) {
	switch prof.FlashBankSelectType {
	case 1:
		p.w8s(2, (bank&0xF)<<4)
		p.w8s(3, 0x40)
		p.w8s(4, 0x00)
	case 2: // Flash2Advance Ultra
		b1, b2 := 0, (bank%4)*0x400
		if bank >= 4 {
			b1 = 0x10
		}
		p.w16(0x987654*2, 0x5354)
		p.w16(0xE12345*2, 0xA55A)
		p.w8s(6, b1)
		p.w16(0x987654*2, 0x5354)
		p.w16(0xB5AC97*2, b2)
		p.w16(0x987654*2, 0x5354)
		p.w16(0xF12345*2, 0x9413)
	}
}

func localReset(p *copProgram, prof *FlashProfile, addr int) {
	base := 0
	if prof.ResetEvery > 0 {
		base = addr - addr%prof.ResetEvery
	}
	copCmds(p, prof.cmd("reset"), base)
}

func fullReset(p *copProgram, prof *FlashProfile, limit int) {
	if prof.ResetEvery > 0 {
		for j := 0; j < min(limit, gbafBankWindow); j += prof.ResetEvery {
			copCmds(p, prof.cmd("reset"), j)
		}
		return
	}
	copCmds(p, prof.cmd("reset"), 0)
}

func eraseSectorOps(p *copProgram, prof *FlashProfile, sa int) ([]copHandle, error) {
	localReset(p, prof, sa)
	if prof.WaitReadStatusRegister {
		if rs := prof.cmd("read_status_register"); len(rs) > 0 {
			p.srcmd(num(rs[0][0]), num(el(rs[0], 1)))
		}
	}
	var waits []copHandle
	wf := prof.cmd("sector_erase_wait_for")
	for i, c := range prof.cmd("sector_erase") {
		a, ok, err := resolveAddr(el(c, 0), sa)
		if err != nil {
			return nil, &FlashError{"unsupported", err.Error()}
		}
		if ok {
			p.w16(a, num(el(c, 1)))
		}
		if i < len(wf) && !isNil(el(wf[i], 0)) {
			wa, _, err := resolveAddr(wf[i][0], sa)
			if err != nil {
				return nil, &FlashError{"unsupported", err.Error()}
			}
			waits = append(waits, p.wait(wa, num(el(wf[i], 1)), num(el(wf[i], 2)), gbafEraseMS))
		}
	}
	if prof.WaitReadStatusRegister {
		p.srcmd(0, nil)
	}
	localReset(p, prof, sa)
	return waits, nil
}

func (f *GBAFlashcart) readProfileCFI(p *FlashProfile) *cfiInfo {
	set := commandSet(p)
	readCfi := p.cmd("read_cfi")
	if readCfi == nil {
		switch set {
		case "INTEL":
			readCfi = [][]any{{f64(0), f64(0x98)}}
		case "AMD":
			readCfi = [][]any{{f64(0xAA), f64(0x98)}}
		default:
			return nil
		}
	}
	raw, err := f.readCFIRaw(readCfi, p.cmd("reset"))
	if err != nil {
		return nil
	}
	return parseCFI(raw)
}

func (f *GBAFlashcart) chipSize(p *FlashProfile, cfi *cfiInfo) (int, error) {
	listed, reported := p.FlashSize, 0
	if cfi != nil {
		reported = cfi.DeviceSize
	}
	if listed == 0 || reported == 0 || reported <= listed || listed >= gbafBankWindow {
		if listed > 0 {
			return listed, nil
		}
		return reported, nil
	}
	prog := &copProgram{}
	f.selectBank(prog, p, 0)
	a, b := prog.rd16(0, 0xC0), prog.rd16(listed, 0xC0)
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return 0, err
	}
	start, past := r.bytes(a, 0xC0), r.bytes(b, 0xC0)
	open := true
	for i, x := range past {
		if int(x) != ((listed+(i&^1))>>(1+(i&1)*8))&0xFF {
			open = false
			break
		}
	}
	if allFF(start) || string(start) == string(past) || open {
		return listed, nil
	}
	f.log("The chip says it's %d MB, more than the %d MB listed for this cart type: using the chip's size.", reported>>20, listed>>20)
	return reported, nil
}

func (f *GBAFlashcart) showFail() {
	p := &copProgram{}
	p.show(copShowFail, 0)
	f.exec(p, 10*time.Second)
}

// WriteROM writes rom to the start of the flash (profile nil: the one detected).
func (f *GBAFlashcart) WriteROM(rom []byte, profile *FlashProfile, eraseRest bool, progress func(done, total int, phase string)) (FlashResult, error) {
	var res FlashResult
	p := profile
	var cfi *cfiInfo
	if f.detected != nil && (p == nil || p == f.detected.Profile) {
		p, cfi = f.detected.Profile, f.detected.CFI
	} else if p != nil {
		cfi = f.readProfileCFI(p)
	}
	if p == nil {
		return res, &FlashError{"no-flash", "Detect the flash cart first."}
	}
	chip, err := f.chipSize(p, cfi)
	if err != nil {
		return res, err
	}
	capacity := chip
	if capacity == 0 {
		capacity = gbafBankWindow
	}
	if len(rom) > capacity {
		return res, &FlashError{"too-big", fmt.Sprintf("The ROM is %s, but the cart holds %s.", sizeText(len(rom)), sizeText(capacity))}
	}
	plan, err := gbaWritePlanFor(p, cfi)
	if err != nil {
		return res, err
	}
	unitPad := max(plan.unit, 2)
	total := (len(rom) + unitPad - 1) / unitPad * unitPad
	image := make([]byte, total)
	for i := range image {
		image[i] = 0xFF
	}
	copy(image, rom)
	bankSize := p.FlashBankSize
	if bankSize == 0 {
		bankSize = gbafBankWindow
	}
	sectors := sectorList(p, cfi, total)
	if sectors == nil && p.cmd("chip_erase") == nil {
		return res, &FlashError{"unsupported", "Couldn't read the chip's sector layout (CFI), and this cart type has no chip erase."}
	}
	f.log("Cart type: %s, %s writes, %d sectors to check.", p.Name(), plan.method, len(sectors))
	prog := &copProgram{}
	f.selectBank(prog, p, 0)
	fullReset(prog, p, capacity)
	copCmds(prog, p.cmd("unlock"), 0)
	if _, err := f.exec(prog, 60*time.Second); err != nil {
		return res, err
	}
	sectorData := func(s [2]int) []byte {
		out := make([]byte, s[1])
		for i := range out {
			out[i] = 0xFF
		}
		if s[0] < total {
			copy(out, image[s[0]:min(total, s[0]+s[1])])
		}
		return out
	}
	bankOf := func(off int) int { return off / bankSize }
	var todo [][2]int
	if sectors != nil {
		for i := 0; i < len(sectors); {
			if err := f.d.cancelled(); err != nil {
				return res, err
			}
			prog = &copProgram{}
			bank := bankOf(sectors[i][0])
			f.selectBank(prog, p, bank)
			type b struct {
				s [2]int
				h copHandle
			}
			var batch []b
			for i < len(sectors) && len(batch) < 256 && bankOf(sectors[i][0]) == bank {
				batch = append(batch, b{sectors[i], prog.crc32(sectors[i][0]%bankSize, sectors[i][1])})
				i++
			}
			prog.show(copShowVerify, float64(i)*100/float64(len(sectors)))
			r, err := f.exec(prog, 300*time.Second)
			if err != nil {
				return res, err
			}
			for _, x := range batch {
				if r.word(x.h, 0) == crc32.ChecksumIEEE(sectorData(x.s)) {
					res.Skipped++
				} else {
					todo = append(todo, x.s)
				}
			}
			if progress != nil {
				progress(i, len(sectors), "compare")
			}
		}
		if res.Skipped > 0 {
			f.log("%d sector(s) already hold the right data: skipped.", res.Skipped)
		}
	}
	if sectors != nil && eraseRest && chip == 0 {
		f.log("The chip's size isn't known, so data after the ROM is left as it is.")
	} else if sectors != nil && eraseRest {
		last := sectors[len(sectors)-1]
		romEnd := last[0] + last[1]
		var rest [][2]int
		for _, s := range sectorList(p, cfi, min(chip, bankSize)) {
			if s[0] >= romEnd {
				rest = append(rest, s)
			}
		}
		for i := 0; i < len(rest); {
			if err := f.d.cancelled(); err != nil {
				return res, err
			}
			prog = &copProgram{}
			f.selectBank(prog, p, 0)
			type b struct {
				s [2]int
				h copHandle
			}
			var batch []b
			for i < len(rest) && len(batch) < 256 {
				batch = append(batch, b{rest[i], prog.verify(nil, rest[i][0], rest[i][1])})
				i++
			}
			prog.show(copShowVerify, float64(i)*100/float64(len(rest)))
			r, err := f.exec(prog, 300*time.Second)
			if err != nil {
				return res, err
			}
			for _, x := range batch {
				if r.word(x.h, 0) != copSame {
					todo = append(todo, x.s)
					res.Cleared++
				}
			}
			if progress != nil {
				progress(i, len(rest), "compare")
			}
		}
		if res.Cleared > 0 {
			f.log("%d sector(s) after the ROM still hold old data: erasing them too.", res.Cleared)
		}
	}
	if sectors != nil {
		for i := 0; i < len(todo); {
			if err := f.d.cancelled(); err != nil {
				return res, err
			}
			prog = &copProgram{}
			bank := bankOf(todo[i][0])
			f.selectBank(prog, p, bank)
			type b struct {
				s     [2]int
				waits []copHandle
			}
			var batch []b
			for i < len(todo) && len(batch) < 8 && bankOf(todo[i][0]) == bank {
				w, err := eraseSectorOps(prog, p, todo[i][0]%bankSize)
				if err != nil {
					return res, err
				}
				batch = append(batch, b{todo[i], w})
				i++
			}
			prog.show(copShowErase, float64(i)*100/float64(len(todo)))
			r, err := f.exec(prog, time.Duration(len(batch)*gbafEraseMS)*time.Millisecond+30*time.Second)
			if err != nil {
				return res, err
			}
			if r.status != copStOK {
				at := batch[0].s[0]
				for _, x := range batch {
					for _, w := range x.waits {
						if w.index == r.executed-1 {
							at = x.s[0]
						}
					}
				}
				f.showFail()
				return res, &FlashError{"timeout", fmt.Sprintf("Erasing the sector at 0x%X took too long. Clean the cart's contacts and check the cart type.", at)}
			}
			if progress != nil {
				progress(i, len(todo), "erase")
			}
		}
	} else if err := f.chipErase(p); err != nil {
		return res, err
	}
	var ranges [][2]int
	addRange := func(start, end int) {
		for a := start; a < end; {
			n := min(end, (bankOf(a)+1)*bankSize, a+gbafChunk) - a
			ranges = append(ranges, [2]int{a, n})
			a += n
		}
	}
	if sectors != nil {
		sort.Slice(todo, func(i, j int) bool { return todo[i][0] < todo[j][0] })
		var cur *[2]int
		for _, s := range todo {
			start, end := s[0], min(s[0]+s[1], total)
			if start >= end {
				continue
			}
			if cur != nil && cur[1] == start {
				cur[1] = end
			} else {
				if cur != nil {
					addRange(cur[0], cur[1])
				}
				cur = &[2]int{start, end}
			}
		}
		if cur != nil {
			addRange(cur[0], cur[1])
		}
	} else {
		addRange(0, total)
	}
	totalBytes := 0
	for _, r := range ranges {
		totalBytes += r[1]
	}
	done := 0
	for _, rg := range ranges {
		if err := f.d.cancelled(); err != nil {
			return res, err
		}
		start, n := rg[0], rg[1]
		data := image[start : start+n]
		ok, err := f.writeRange(p, plan, bankSize, start, data, float64(done+n)*100/float64(max(totalBytes, 1)))
		if err != nil {
			return res, err
		}
		if !ok {
			var redo [][2]int
			for _, s := range sectors {
				if s[0] < start+n && s[0]+s[1] > start {
					redo = append(redo, s)
				}
			}
			if len(redo) == 0 {
				f.showFail()
				return res, f.lastErr
			}
			f.log("Checking failed near 0x%X: erasing and writing it again once.", f.lastFail)
			for _, s := range redo {
				prog = &copProgram{}
				f.selectBank(prog, p, bankOf(s[0]))
				if _, err := eraseSectorOps(prog, p, s[0]%bankSize); err != nil {
					return res, err
				}
				r, err := f.exec(prog, gbafEraseMS*time.Millisecond+30*time.Second)
				if err != nil {
					return res, err
				}
				if r.status != copStOK {
					f.showFail()
					return res, &FlashError{"timeout", fmt.Sprintf("Erasing the sector at 0x%X took too long.", s[0])}
				}
			}
			lo, hi := redo[0][0], min(total, redo[len(redo)-1][0]+redo[len(redo)-1][1])
			for a := lo; a < hi; {
				m := min(hi, (bankOf(a)+1)*bankSize, a+gbafChunk) - a
				ok, err := f.writeRange(p, plan, bankSize, a, image[a:a+m], float64(done+n)*100/float64(max(totalBytes, 1)))
				if err != nil {
					return res, err
				}
				if !ok {
					f.showFail()
					return res, f.lastErr
				}
				a += m
			}
		}
		done += n
		if !allFF(data) {
			res.Written += n
		}
		if progress != nil {
			progress(done, totalBytes, "write")
		}
	}
	prog = &copProgram{}
	fullReset(prog, p, capacity)
	f.selectBank(prog, p, 0)
	prog.show(copShowDone, 0)
	if _, err := f.exec(prog, 60*time.Second); err != nil {
		return res, err
	}
	res.Method = plan.method
	return res, nil
}

func (f *GBAFlashcart) chipErase(p *FlashProfile) error {
	timeout := 300 * 1000
	if p.ChipEraseTimeout > 0 {
		timeout = p.ChipEraseTimeout * 1000
	}
	prog := &copProgram{}
	f.selectBank(prog, p, 0)
	size := p.FlashSize
	if size == 0 {
		size = gbafBankWindow
	}
	fullReset(prog, p, size)
	if p.WaitReadStatusRegister {
		if rs := p.cmd("read_status_register"); len(rs) > 0 {
			prog.srcmd(num(rs[0][0]), num(el(rs[0], 1)))
		}
	}
	wf := p.cmd("chip_erase_wait_for")
	for i, c := range p.cmd("chip_erase") {
		if !isNil(el(c, 0)) {
			prog.w16(num(c[0]), num(el(c, 1)))
		}
		if i < len(wf) && !isNil(el(wf[i], 0)) {
			prog.wait(num(wf[i][0]), num(el(wf[i], 1)), num(el(wf[i], 2)), timeout)
		}
	}
	if p.WaitReadStatusRegister {
		prog.srcmd(0, nil)
	}
	fullReset(prog, p, size)
	prog.show(copShowErase, 100)
	f.log("Erasing the whole flash chip: this can take a few minutes.")
	r, err := f.exec(prog, time.Duration(timeout)*time.Millisecond+60*time.Second)
	if err != nil {
		return err
	}
	if r.status != copStOK {
		f.showFail()
		return &FlashError{"timeout", "Erasing the flash chip took too long. Clean the cart's contacts and check the cart type."}
	}
	return nil
}

func (f *GBAFlashcart) writeRange(p *FlashProfile, plan *gbaWritePlan, bankSize, start int, data []byte, pct float64) (bool, error) {
	addr := start % bankSize
	prog := &copProgram{}
	f.selectBank(prog, p, start/bankSize)
	prog.show(copShowWrite, pct)
	var vh copHandle
	var ph *copHandle
	if allFF(data) {
		vh = prog.verify(nil, addr, len(data))
	} else {
		cfg := prog.attach(plan.cfg)
		ref := prog.attach(data)
		prog.cfg(cfg, len(plan.cfg))
		h := prog.prog(ref, addr, len(data))
		ph = &h
		localReset(prog, p, addr)
		vh = prog.verify(ref, addr, len(data))
	}
	r, err := f.exec(prog, 180*time.Second)
	if err != nil {
		return false, err
	}
	if ph != nil && r.status == copStTimeout {
		at := start + int(r.word(*ph, 0)) - addr
		f.showFail()
		return false, &FlashError{"timeout", fmt.Sprintf("Writing at 0x%X took too long (status 0x%04X). Clean the cart's contacts, or pick another cart type.", at, r.word(*ph, 1))}
	}
	off := r.word(vh, 0)
	if off == copSame {
		return true, nil
	}
	ge := r.word(vh, 1)
	f.lastFail = start + int(off)
	f.lastErr = &FlashError{"verify", fmt.Sprintf("Checking failed at 0x%X: read 0x%04X, expected 0x%04X.", start+int(off), ge>>16, ge&0xFFFF)}
	return false, nil
}
