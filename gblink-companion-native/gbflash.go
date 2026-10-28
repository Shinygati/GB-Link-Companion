// Writing ROMs to GB/GBC flash carts with the GBC's cart agent (gbcart.go), as GB-Link Cart
// Doctor does it (web/gb_flash.js, GPL-3.0): the chip is found the way FlashGBX finds it, banks
// are switched the way FlashGBX switches them for the cart's mapper, sectors that already hold
// the right data are skipped, and every byte is read back to check it. Only carts written through
// the cartridge's normal write (/WR) pin: a GBC can't drive the audio pin.
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	gbfChunk   = 0x800 // ROM bytes written per agent program
	gbfEraseMS = 20000 // longest wait for one sector to erase
	gbfBank    = 0x4000
)

// gbMapper: the cart's bank switching chip, driven the way FlashGBX does it when flashing.
type gbMapper struct {
	kind      string
	startAddr *int
	flashBank int
	max       int
	name      string
}

// gbWrite is one write the mapper needs (addr -1: a pause of value ms).
type gbWrite struct{ addr, value int }

func gbMapperFor(p *FlashProfile, romType int) *gbMapper {
	kind := "mbc5"
	m := p.MBC
	if str(m) == "manual" {
		m = float64(romType)
	}
	if isNum(m) {
		v := num(m)
		switch {
		case v >= 0x01 && v <= 0x03:
			kind = "mbc1"
		case (v >= 0x05 && v <= 0x06) || (v >= 0x0F && v <= 0x13) || v == 0xFF || v == 0x110:
			kind = "simple"
		case v == 0xFC:
			kind = "gbd"
		case v == 0x206:
			kind = "mbcx"
		}
	}
	mx := map[string]int{"mbc1": 0x200000, "simple": 0x400000, "gbd": 0x100000, "mbcx": 0x2000000}[kind]
	if mx == 0 {
		mx = 0x800000
	}
	name := map[string]string{"mbc1": "MBC1", "simple": "MBC3-style", "gbd": "MAC-GBD", "mbcx": "MBCX", "mbc5": "MBC5"}[kind]
	return &gbMapper{kind: kind, startAddr: p.StartAddr, flashBank: -1, max: mx, name: name}
}

// sel: the writes that switch to bank, and where the bank then appears.
func (m *gbMapper) sel(bank int) ([]gbWrite, int) {
	var w []gbWrite
	start := 0
	switch m.kind {
	case "mbc1":
		w = append(w, gbWrite{0x6000, 1}, gbWrite{0x2000, bank & 0xFF}, gbWrite{0x4000, (bank >> 5) & 0xFF})
		if bank&0x1F != 0 {
			start = 0x4000
		}
	case "simple":
		w = append(w, gbWrite{0x2100, bank & 0xFF})
		if bank != 0 {
			start = 0x4000
		}
	case "gbd":
		w = append(w, gbWrite{0x2000, bank & 0xFF})
		if bank != 0 {
			start = 0x4000
		}
	case "mbcx":
		fb, b := bank>>9, bank&0x1FF
		if b == 0 || fb != m.flashBank {
			w = append(w, gbWrite{0x0000, 0x05}, gbWrite{0x4000, 0x82}, gbWrite{0xA000, fb}, gbWrite{0x0000, 0x00}, gbWrite{-1, 100})
			m.flashBank = fb
		}
		w = append(w, gbWrite{0x3000, b >> 8}, gbWrite{0x2100, b & 0xFF})
		start = 0x4000
	default:
		w = append(w, gbWrite{0x3000, (bank >> 8) & 0xFF}, gbWrite{0x2100, bank & 0xFF})
		if bank != 0 {
			start = 0x4000
		}
	}
	if bank == 0 && m.startAddr != nil {
		start = *m.startAddr
	}
	return w, start
}

func (m *gbMapper) separates() bool { return m.kind == "mbc5" || m.kind == "mbcx" }
func (m *gbMapper) reselects() bool { return m.kind == "mbc1" }

// GBFlashcart writes a flash cart through a connected cart agent.
type GBFlashcart struct {
	c          *gbClient
	log        func(string, ...any)
	detected   *FlashDetect
	mapper     *gbMapper
	separators map[int]bool
	lastFail   int
	lastErr    error
}

func (f *GBFlashcart) exec(p *gbProgram, timeout time.Duration) (*gbResult, error) {
	if err := f.c.checkCancel(); err != nil {
		return nil, err
	}
	return f.c.run(p, timeout)
}

func gbCmds(p *gbProgram, cmds [][]any) {
	for _, c := range cmds {
		if isNil(el(c, 0)) {
			continue
		}
		p.wr(num(c[0]), num(el(c, 1)))
	}
}

func gbWrites(p *gbProgram, ws []gbWrite) {
	for _, w := range ws {
		if w.addr < 0 {
			p.delay(w.value)
		} else {
			p.wr(w.addr, w.value)
		}
	}
}

// gbMapperDefaults: the mapper as at power-on: bank 1 at $4000, cart RAM off.
func gbMapperDefaults(p *gbProgram) {
	p.wr(0x0000, 0x00)
	p.wr(0x3000, 0x00)
	p.wr(0x2100, 0x01)
}

// Detect finds the flash chip and picks a profile.
func (f *GBFlashcart) Detect() (*FlashDetect, error) {
	f.detected = nil
	var profiles []*FlashProfile
	for _, p := range GBFlashProfiles() {
		if !p.ManualSelect {
			profiles = append(profiles, p)
		}
	}
	prog := &gbProgram{}
	gbMapperDefaults(prog)
	romH := prog.rd(0, 8)
	type m29 struct {
		p             *FlashProfile
		before, after gbHandle
	}
	var ms []m29
	for _, p := range profiles {
		if !p.M29W640 {
			continue
		}
		gbCmds(prog, p.cmd("reset"))
		b := prog.rd(0, 8)
		gbCmds(prog, p.cmd("read_identifier"))
		a := prog.rd(0, 8)
		gbCmds(prog, p.cmd("reset"))
		ms = append(ms, m29{p, b, a})
	}
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return nil, err
	}
	rom := append([]byte(nil), r.bytes(romH)...)
	for _, m := range ms {
		before, after := r.bytes(m.before), r.bytes(m.after)
		id := intsToBytes(m.p.FlashIDs[0])
		if string(before) != string(after) && strings.HasPrefix(string(after), string(id)) {
			return f.finishDetect([]*FlashProfile{m.p}, after, "M29W640 check")
		}
	}

	// FlashGBX's way: every profile's reset, then each different "read chip ID" command; one worked
	// if address 0 reads differently afterwards (the ID instead of the ROM)
	var resets [][][]any
	type group struct {
		rid, reset [][]any
		h          gbHandle
	}
	var groups []*group
	for _, p := range profiles {
		if p.M29W640 {
			continue
		}
		if rs := p.cmd("reset"); rs != nil && !containsCmds(resets, rs) {
			resets = append(resets, rs)
		}
		rid := p.cmd("read_identifier")
		if len(rid) > 0 && num(el(rid[0], 0)) <= 0x7000 {
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
	prog = &gbProgram{}
	for _, c := range resets {
		gbCmds(prog, c)
	}
	for _, g := range groups {
		gbMapperDefaults(prog)
		gbCmds(prog, g.reset)
		gbCmds(prog, g.rid)
		g.h = prog.rd(0, 8)
		gbCmds(prog, g.reset)
	}
	gbMapperDefaults(prog)
	prog.wr(0, 0xFF)
	prog.wr(0, 0xF0)
	if r, err = f.exec(prog, 60*time.Second); err != nil {
		return nil, err
	}
	type method struct {
		rid [][]any
		id  []byte
	}
	var methods []method
	for _, g := range groups {
		if cmp := r.bytes(g.h); string(cmp) != string(rom) {
			methods = append(methods, method{g.rid, append([]byte(nil), cmp...)})
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
	if len(cands) > 0 {
		for _, m := range methods {
			if sameCmds(m.rid, cands[0].cmd("read_identifier")) {
				return f.finishDetect(cands, m.id, "FlashGBX profiles")
			}
		}
	}
	// the chip answered an ID command no profile lists: the generic profile for that command
	for _, m := range methods {
		for _, p := range profiles {
			if strings.HasPrefix(p.ID, "DMG_Generic_") && sameCmds(p.cmd("read_identifier"), m.rid) {
				return f.finishDetect([]*FlashProfile{p}, m.id, "generic flash commands")
			}
		}
	}
	seen := "none"
	if len(methods) > 0 {
		var s []string
		for _, m := range methods {
			s = append(s, hexBytes(m.id))
		}
		seen = strings.Join(s, ", ")
	}
	return nil, &FlashError{"no-flash", "No writable flash chip was found. Either this isn't a flash cart, its flash is written through the " +
		"cartridge's audio pin (a GBC can't drive that), or its chip isn't one the profiles know (flash IDs seen: " + seen + ")."}
}

func intsToBytes(a []int) []byte {
	b := make([]byte, len(a))
	for i, v := range a {
		b[i] = byte(v)
	}
	return b
}

func containsCmds(list [][][]any, c [][]any) bool {
	for _, x := range list {
		if sameCmds(x, c) {
			return true
		}
	}
	return false
}

func (f *GBFlashcart) readCFI(p *FlashProfile) (*cfiInfo, error) {
	readCfi := p.cmd("read_cfi")
	if readCfi == nil {
		a := 0
		if rid := p.cmd("read_identifier"); len(rid) > 0 {
			a = num(rid[0][0])
		}
		readCfi = [][]any{{float64(a), float64(0x98)}}
	}
	prog := &gbProgram{}
	gbMapperDefaults(prog)
	gbCmds(prog, p.cmd("reset"))
	gbCmds(prog, readCfi)
	h := prog.rd(0, 0x400)
	gbCmds(prog, p.cmd("reset"))
	r, err := f.exec(prog, 60*time.Second)
	if err != nil {
		return nil, err
	}
	return parseCFI(r.bytes(h)), nil
}

func (f *GBFlashcart) finishDetect(cands []*FlashProfile, id []byte, how string) (*FlashDetect, error) {
	first := cands[0]
	cfi, err := f.readCFI(first)
	if err != nil {
		if ge, ok := err.(*GBError); ok {
			return nil, ge
		}
	}
	profile := first
	sizes := map[int]bool{}
	for _, p := range cands {
		if p.FlashSize > 0 {
			sizes[p.FlashSize] = true
		}
	}
	if len(sizes) > 1 && cfi != nil && cfi.DeviceSize > 0 {
		for _, p := range cands {
			if p.FlashSize == cfi.DeviceSize {
				profile = p
				break
			}
		}
	}
	f.detected = &FlashDetect{Profile: profile, Candidates: cands, FlashID: id, CFI: cfi, Method: how}
	return f.detected, nil
}

// pieces splits [off, off+n) into parts that each sit in one bank.
type gbPiece struct{ bank, off, n int }

func gbPieces(off, n int) []gbPiece {
	var out []gbPiece
	for a := off; a < off+n; {
		bank := a / gbfBank
		k := min(off+n, (bank+1)*gbfBank) - a
		out = append(out, gbPiece{bank, a, k})
		a += k
	}
	return out
}

func (f *GBFlashcart) sel(p *gbProgram, bank int) int {
	w, start := f.mapper.sel(bank)
	gbWrites(p, w)
	if f.mapper.separates() && f.separators[bank&0xFF] {
		p.wr(0x4000, 0x00)
	}
	return start
}

func conflictValues(p *FlashProfile) map[int]bool {
	out := map[int]bool{}
	for _, k := range []string{"single_write", "buffer_write", "page_write", "sector_erase", "chip_erase"} {
		for _, c := range p.cmd(k) {
			if len(c) >= 2 && isNum(c[1]) {
				out[num(c[1])&0xFF] = true
				break
			}
		}
	}
	return out
}

func (f *GBFlashcart) template(p *FlashProfile, bank int) ([]gbTmplStep, error) {
	var steps []gbTmplStep
	for _, c := range p.cmd("single_write") {
		pa, pd := str(el(c, 0)) == "PA", str(el(c, 1)) == "PD"
		if !pa && !isNum(el(c, 0)) {
			return nil, &FlashError{"unsupported", fmt.Sprintf("Unknown address %v in the profile", el(c, 0))}
		}
		if !pd && !isNum(el(c, 1)) {
			return nil, &FlashError{"unsupported", fmt.Sprintf("Unknown value %v in the profile", el(c, 1))}
		}
		s := gbTmplStep{}
		if pd {
			s.kind |= 1
		} else {
			s.value = num(c[1])
		}
		if pa {
			s.kind |= 2
		} else {
			s.addr = num(c[0])
		}
		steps = append(steps, s)
	}
	if f.mapper.reselects() {
		w, _ := f.mapper.sel(bank)
		for _, x := range w {
			steps = append(steps, gbTmplStep{0, x.addr, x.value})
		}
	}
	for _, w := range p.cmd("single_write_wait_for") {
		if !isNil(el(w, 0)) {
			return nil, &FlashError{"unsupported", "This profile's write sequence waits in a way the GBC agent can't."}
		}
	}
	return steps, nil
}

func (f *GBFlashcart) eraseSector(prog *gbProgram, p *FlashProfile, off int) ([]gbHandle, error) {
	bank := off / gbfBank
	sa := f.sel(prog, bank) + off%gbfBank
	gbCmds(prog, p.cmd("reset"))
	var waits []gbHandle
	wf := p.cmd("sector_erase_wait_for")
	for i, c := range p.cmd("sector_erase") {
		a, ok, err := resolveAddr(el(c, 0), sa)
		if err != nil {
			return nil, &FlashError{"unsupported", err.Error()}
		}
		if ok {
			prog.wr(a, num(el(c, 1)))
			if a >= 0x4000 && f.mapper.reselects() {
				w, _ := f.mapper.sel(bank)
				gbWrites(prog, w)
			}
		}
		if i < len(wf) && !isNil(el(wf[i], 0)) {
			wa, _, err := resolveAddr(wf[i][0], sa)
			if err != nil {
				return nil, &FlashError{"unsupported", err.Error()}
			}
			waits = append(waits, prog.wait(wa, num(el(wf[i], 2))&0xFF, num(el(wf[i], 1))&0xFF, gbfEraseMS))
		}
	}
	gbCmds(prog, p.cmd("reset"))
	return waits, nil
}

// WriteROM writes rom from flash offset 0 (profile nil: the one detected).
func (f *GBFlashcart) WriteROM(rom []byte, profile *FlashProfile, eraseRest bool, progress func(done, total int, phase string)) (FlashResult, error) {
	var res FlashResult
	p := profile
	var cfi *cfiInfo
	if f.detected != nil && (p == nil || p == f.detected.Profile) {
		p, cfi = f.detected.Profile, f.detected.CFI
	} else if p != nil {
		cfi, _ = f.readCFI(p)
	}
	if p == nil {
		return res, &FlashError{"no-flash", "Detect the flash cart first."}
	}
	romType := 0
	if len(rom) > 0x147 {
		romType = int(rom[0x147])
	}
	f.mapper = gbMapperFor(p, romType)
	f.separators = conflictValues(p)
	chip := p.FlashSize
	if chip == 0 && cfi != nil {
		chip = cfi.DeviceSize
	}
	capacity := f.mapper.max
	if chip > 0 {
		capacity = min(chip, f.mapper.max)
	}
	if len(rom) > capacity {
		return res, &FlashError{"too-big", fmt.Sprintf("The ROM is %s, but this cart takes %s.", sizeText(len(rom)), sizeText(capacity))}
	}
	total := max(0x8000, (len(rom)+gbfBank-1)/gbfBank*gbfBank)
	image := make([]byte, total)
	for i := range image {
		image[i] = 0xFF
	}
	copy(image, rom)
	sectors := sectorList(p, cfi, total)
	if sectors == nil && p.cmd("chip_erase") == nil {
		return res, &FlashError{"unsupported", "Couldn't read the chip's sector layout (CFI), and this profile has no chip erase."}
	}
	if sectors != nil {
		f.log("Profile: %s, %s banking, %d sectors.", p.Name(), f.mapper.name, len(sectors))
	} else {
		f.log("Profile: %s, %s banking, chip erase.", p.Name(), f.mapper.name)
	}
	prog := &gbProgram{}
	f.sel(prog, 0)
	gbCmds(prog, p.cmd("reset"))
	if _, err := f.exec(prog, 60*time.Second); err != nil {
		return res, err
	}
	dataOf := func(s [2]int) []byte {
		out := make([]byte, s[1])
		for i := range out {
			out[i] = 0xFF
		}
		if s[0] < total {
			copy(out, image[s[0]:min(total, s[0]+s[1])])
		}
		return out
	}
	all := sectors
	if all == nil {
		all = [][2]int{{0, total}}
	}
	state, err := f.survey(all, dataOf, true, progress)
	if err != nil {
		return res, err
	}
	var erase, write [][2]int
	for i, s := range all {
		blankWanted := allFF(dataOf(s))
		if state[i] == "same" || (state[i] == "blank" && blankWanted) {
			res.Skipped++
			continue
		}
		if state[i] != "blank" {
			erase = append(erase, s)
		}
		if !blankWanted {
			write = append(write, s)
		}
	}
	if res.Skipped > 0 && sectors != nil {
		f.log("%d sector(s) already hold the right data: skipped.", res.Skipped)
	}
	if sectors != nil && eraseRest {
		if limit := min(chip, f.mapper.max); limit <= 0 {
			f.log("The chip's size isn't known, so data after the ROM is left as it is.")
		} else {
			end := 0
			if n := len(sectors); n > 0 {
				end = sectors[n-1][0] + sectors[n-1][1]
			}
			var rest [][2]int
			for _, s := range sectorList(p, cfi, limit) {
				if s[0] >= end {
					rest = append(rest, s)
				}
			}
			rs, err := f.survey(rest, nil, false, progress)
			if err != nil {
				return res, err
			}
			for i, s := range rest {
				if rs[i] != "blank" {
					erase = append(erase, s)
					res.Cleared++
				}
			}
			if res.Cleared > 0 {
				f.log("%d sector(s) after the ROM still hold old data: erasing them too.", res.Cleared)
			}
		}
	}
	if sectors == nil {
		if len(erase) > 0 {
			if err := f.chipErase(p); err != nil {
				return res, err
			}
		}
	} else if err := f.eraseSectors(p, erase, progress); err != nil {
		return res, err
	}
	var ranges [][2]int
	src := write
	if sectors == nil {
		src = nil
		if len(erase) > 0 || len(write) > 0 {
			src = [][2]int{{0, total}}
		}
	}
	for _, s := range src {
		end := min(s[0]+s[1], total)
		for _, pc := range gbPieces(s[0], end-s[0]) {
			for a := pc.off; a < pc.off+pc.n; a += gbfChunk {
				n := min(gbfChunk, pc.off+pc.n-a)
				if !allFF(image[a : a+n]) {
					ranges = append(ranges, [2]int{a, n})
				}
			}
		}
	}
	totalBytes := 0
	for _, r := range ranges {
		totalBytes += r[1]
	}
	done, tmplBank := 0, -1
	for _, r := range ranges {
		if err := f.c.checkCancel(); err != nil {
			return res, err
		}
		bank := r[0] / gbfBank
		need := tmplBank < 0 || (f.mapper.reselects() && bank != tmplBank)
		ok, err := f.writeRange(p, r[0], image[r[0]:r[0]+r[1]], need)
		if err != nil {
			return res, err
		}
		tmplBank = bank
		if !ok {
			var redo [][2]int
			for _, s := range sectors {
				if s[0] <= r[0] && s[0]+s[1] > r[0] {
					redo = append(redo, s)
				}
			}
			if len(redo) == 0 {
				return res, f.lastErr
			}
			f.log("Checking failed near 0x%X: erasing and writing it again once.", f.lastFail)
			if err := f.eraseSectors(p, redo, nil); err != nil {
				return res, err
			}
			s := redo[0]
			for _, pc := range gbPieces(s[0], min(s[0]+s[1], total)-s[0]) {
				for a := pc.off; a < pc.off+pc.n; a += gbfChunk {
					m := min(gbfChunk, pc.off+pc.n-a)
					if allFF(image[a : a+m]) {
						continue
					}
					ok, err := f.writeRange(p, a, image[a:a+m], true)
					if err != nil {
						return res, err
					}
					if !ok {
						return res, f.lastErr
					}
				}
			}
			tmplBank = -1
		}
		done += r[1]
		if progress != nil {
			progress(done, totalBytes, "write")
		}
	}
	prog = &gbProgram{}
	gbCmds(prog, p.cmd("reset"))
	f.sel(prog, 0)
	gbMapperDefaults(prog)
	if _, err := f.exec(prog, 60*time.Second); err != nil {
		return res, err
	}
	res.Written, res.Method = totalBytes, f.mapper.name+" banking"
	return res, nil
}

// survey says for each sector whether it's "blank", "same" (already the right data) or "other".
func (f *GBFlashcart) survey(list [][2]int, dataOf func([2]int) []byte, compare bool, progress func(int, int, string)) ([]string, error) {
	state := make([]string, len(list))
	for i := range state {
		state[i] = "other"
	}
	idx := make([]int, len(list))
	for i := range idx {
		idx[i] = i
	}
	err := f.batch(idx, func(p *gbProgram, i int) []any {
		var hs []any
		for _, pc := range gbPieces(list[i][0], list[i][1]) {
			base := f.sel(p, pc.bank)
			hs = append(hs, p.blank(base+pc.off%gbfBank, pc.n))
		}
		return hs
	}, func(r *gbResult, i int, hs []any) {
		all := true
		for _, h := range hs {
			all = all && r.word(h.(gbHandle), 0) == gbNone
		}
		if all {
			state[i] = "blank"
		}
	}, func(k int) {
		if progress != nil {
			progress(k, len(list), "compare")
		}
	}, 64)
	if err != nil || !compare || dataOf == nil {
		return state, err
	}
	type crcH struct {
		h  gbHandle
		pc gbPiece
	}
	crcOf := func(items []int, lenOf func([2]int) int) (map[int]bool, error) {
		match := map[int]bool{}
		err := f.batch(items, func(p *gbProgram, i int) []any {
			var hs []any
			for _, pc := range gbPieces(list[i][0], lenOf(list[i])) {
				base := f.sel(p, pc.bank)
				hs = append(hs, crcH{p.crc(base+pc.off%gbfBank, pc.n), pc})
			}
			return hs
		}, func(r *gbResult, i int, hs []any) {
			want := dataOf(list[i])
			for _, x := range hs {
				c := x.(crcH)
				o := c.pc.off - list[i][0]
				if r.u32(c.h) != gbCRC32(want[o:o+c.pc.n]) {
					return
				}
			}
			match[i] = true
		}, func(int) {}, 4)
		return match, err
	}
	var cands []int
	for i := range list {
		if state[i] != "blank" {
			cands = append(cands, i)
		}
	}
	head, err := crcOf(cands, func(s [2]int) int { return min(s[1], 0x400) })
	if err != nil {
		return state, err
	}
	var big []int
	for i := range head {
		if list[i][1] > 0x400 {
			big = append(big, i)
		}
	}
	sort.Ints(big)
	full, err := crcOf(big, func(s [2]int) int { return s[1] })
	if err != nil {
		return state, err
	}
	for i := range head {
		if list[i][1] <= 0x400 || full[i] {
			state[i] = "same"
		}
	}
	return state, nil
}

func (f *GBFlashcart) batch(items []int, emit func(*gbProgram, int) []any, collect func(*gbResult, int, []any), prog func(int), maxItems int) error {
	for k := 0; k < len(items); {
		if err := f.c.checkCancel(); err != nil {
			return err
		}
		p := &gbProgram{}
		type b struct {
			i  int
			hs []any
		}
		var bs []b
		for k < len(items) && len(bs) < maxItems && p.room() > 200 && p.resultRoom() > 64 {
			bs = append(bs, b{items[k], emit(p, items[k])})
			k++
		}
		r, err := f.exec(p, 120*time.Second)
		if err != nil {
			return err
		}
		for _, x := range bs {
			collect(r, x.i, x.hs)
		}
		prog(k)
	}
	return nil
}

func (f *GBFlashcart) eraseSectors(p *FlashProfile, list [][2]int, progress func(int, int, string)) error {
	for k := 0; k < len(list); {
		if err := f.c.checkCancel(); err != nil {
			return err
		}
		prog := &gbProgram{}
		type b struct {
			s     [2]int
			waits []gbHandle
		}
		var bs []b
		for k < len(list) && len(bs) < 8 {
			w, err := f.eraseSector(prog, p, list[k][0])
			if err != nil {
				return err
			}
			bs = append(bs, b{list[k], w})
			k++
		}
		r, err := f.exec(prog, time.Duration(len(bs)*gbfEraseMS)*time.Millisecond+30*time.Second)
		if err != nil {
			return err
		}
		if !r.ok() {
			at := bs[0].s[0]
			for _, x := range bs {
				for _, w := range x.waits {
					if w.index == r.executed {
						at = x.s[0]
					}
				}
			}
			f.reset(p)
			return &FlashError{"timeout", fmt.Sprintf("Erasing the sector at 0x%X took too long. Clean the cart's contacts and check the cart type.", at)}
		}
		if progress != nil {
			progress(k, len(list), "erase")
		}
	}
	return nil
}

func (f *GBFlashcart) chipErase(p *FlashProfile) error {
	timeout := max(p.ChipEraseTimeout, 300) * 1000
	if p.ChipEraseTimeout > 0 {
		timeout = p.ChipEraseTimeout * 1000
	}
	prog := &gbProgram{}
	f.sel(prog, 0)
	gbCmds(prog, p.cmd("reset"))
	wf := p.cmd("chip_erase_wait_for")
	for i, c := range p.cmd("chip_erase") {
		if !isNil(el(c, 0)) {
			prog.wr(num(c[0]), num(el(c, 1)))
		}
		if i < len(wf) && !isNil(el(wf[i], 0)) {
			prog.wait(num(wf[i][0]), num(el(wf[i], 2))&0xFF, num(el(wf[i], 1))&0xFF, timeout)
		}
	}
	gbCmds(prog, p.cmd("reset"))
	f.log("Erasing the whole flash chip: this can take a few minutes.")
	r, err := f.exec(prog, time.Duration(timeout)*time.Millisecond+60*time.Second)
	if err != nil {
		return err
	}
	if !r.ok() {
		f.reset(p)
		return &FlashError{"timeout", "Erasing the flash chip took too long. Clean the cart's contacts and check the cart type."}
	}
	return nil
}

func (f *GBFlashcart) reset(p *FlashProfile) {
	prog := &gbProgram{}
	gbCmds(prog, p.cmd("reset"))
	if len(prog.bytes) > 0 {
		f.exec(prog, 10*time.Second)
	}
}

// writeRange writes and checks one range inside a bank; false if a byte reads back wrong.
func (f *GBFlashcart) writeRange(p *FlashProfile, start int, data []byte, template bool) (bool, error) {
	bank := start / gbfBank
	prog := &gbProgram{}
	if template {
		steps, err := f.template(p, bank)
		if err != nil {
			return false, err
		}
		prog.tmpl(steps)
	}
	addr := f.sel(prog, bank) + start%gbfBank
	h := prog.prog(addr, data)
	r, err := f.exec(prog, 120*time.Second)
	if err != nil {
		return false, err
	}
	off := r.word(h, 0)
	if r.status == gbStTimeout {
		f.reset(p)
		return false, &FlashError{"timeout", fmt.Sprintf("Writing at 0x%X took too long (status 0x%02X). Clean the cart's contacts, or pick another cart type.", start+off, r.byte(h, 2))}
	}
	if off == gbNone {
		return true, nil
	}
	f.lastFail = start + off
	f.lastErr = &FlashError{"verify", fmt.Sprintf("Checking failed at 0x%X: read 0x%02X, expected 0x%02X.", start+off, r.byte(h, 2), r.byte(h, 3))}
	return false, nil
}

var _ = strconv.Itoa
