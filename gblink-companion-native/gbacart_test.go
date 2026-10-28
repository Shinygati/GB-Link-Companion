package main

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
	"time"
)

// fakeDumper plays the GBA side of GB-Link Cart Doctor's GBA cartridge program: each exchange
// answers with the word it had ready, then takes the word it got. It damages some of what it
// sends once, to check the retries and the ROM stream's seeks.
type fakeDumper struct {
	rom, save []byte
	out       uint32
	queue     []uint32
	state     string // idle, cmd, done, rom, savewait, save
	cmd       uint32
	pos       int // rom: the word in the record stream; save: the section
	stream    []uint32
	starts    map[int]int // rom: section -> where its record starts in stream
	secWords  []uint32
	damaged   map[int]bool
	seeks     int
}

func newFakeDumper(rom, save []byte) *fakeDumper {
	f := &fakeDumper{rom: rom, save: save, out: dgReady, state: "idle", damaged: map[int]bool{}}
	// the ROM as records: data, or a fill for a run of sections repeating one word
	f.starts = map[int]int{}
	sections := len(rom) / 256
	for s := 0; s < sections; {
		f.starts[s] = len(f.stream)
		w0 := binary.LittleEndian.Uint32(rom[s*256:])
		run := 0
		for t := s; t < sections; t++ {
			same := true
			for o := t * 256; o < t*256+256; o += 4 {
				if binary.LittleEndian.Uint32(rom[o:]) != w0 {
					same = false
					break
				}
			}
			if !same {
				break
			}
			run++
		}
		rotl := func(c uint32) uint32 { return c<<1 | c>>31 }
		if run >= 2 {
			h := uint32(drFill)<<24 | uint32(s)
			c := rotl(rotl(h)^w0) ^ uint32(run)
			f.stream = append(f.stream, h, w0, uint32(run), c)
			s += run
			continue
		}
		h := uint32(drData)<<24 | uint32(s)
		f.stream = append(f.stream, h)
		c := h
		for i := 0; i < 64; i++ {
			w := binary.LittleEndian.Uint32(rom[s*256+4*i:])
			f.stream = append(f.stream, w)
			c = rotl(c) ^ w
		}
		f.stream = append(f.stream, c)
		s++
	}
	f.stream = append(f.stream, uint32(drEnd)<<24|uint32(sections))
	return f
}

func (f *fakeDumper) Words(words []uint32, _ time.Duration) ([]uint32, error) {
	out := make([]uint32, len(words))
	for i, w := range words {
		out[i] = f.out
		f.take(w)
	}
	return out, nil
}

func (f *fakeDumper) next() {
	if len(f.queue) > 0 {
		f.out, f.queue = f.queue[0], f.queue[1:]
	}
}

func (f *fakeDumper) saveSection() {
	f.secWords = f.secWords[:0]
	x := uint32(0)
	for i := 0; i < 64; i++ {
		w := binary.LittleEndian.Uint32(f.save[f.pos*256+4*i:])
		x ^= w
		f.secWords = append(f.secWords, w)
	}
	f.secWords = append(f.secWords, x)
	if f.pos == 1 && !f.damaged[-1] { // section 1 arrives damaged the first time
		f.damaged[-1] = true
		f.secWords[5] ^= 0x100
	}
	f.queue = append([]uint32(nil), f.secWords...)
	f.next()
}

func (f *fakeDumper) romWord() {
	if f.pos >= len(f.stream) {
		f.out = 0
		return
	}
	w := f.stream[f.pos]
	if f.pos == 300 && !f.damaged[300] { // a damaged word in the middle of a record
		f.damaged[300] = true
		w ^= 1
	}
	if f.pos == 700 && !f.damaged[700] { // and one lost
		f.damaged[700] = true
		f.pos++
		w = f.stream[f.pos]
	}
	f.out = w
	f.pos++
}

func (f *fakeDumper) take(w uint32) {
	switch f.state {
	case "idle":
		if w&0xFFFF0000 == dmMagic && w != dmMagic {
			f.cmd, f.state, f.out = w&0xFFFF, "cmd", ^w
			return
		}
		f.out = dgReady
	case "cmd":
		if w != dmGo {
			f.state, f.out = "idle", dgReady
			return
		}
		f.state, f.out = "done", dgDone
		switch f.cmd {
		case dmCmdInfo:
			hdr := make([]uint32, 0xC0/4)
			for i := range hdr {
				hdr[i] = binary.LittleEndian.Uint32(f.rom[4*i:])
			}
			f.queue = append([]uint32{uint32(len(f.rom)), uint32(len(f.save))}, hdr...)
		case dmCmdDumpROM:
			f.queue = []uint32{uint32(len(f.rom))}
		case dmCmdDumpSave:
			f.queue = []uint32{uint32(len(f.rom)), uint32(len(f.save))}
		}
	case "done":
		if w == dmCont {
			if f.cmd == dmCmdDumpSave && len(f.queue) == 0 { // the save is in memory: send it
				f.state, f.pos = "save", 0
				f.saveSection()
				return
			}
			f.state = "words"
			f.next()
			return
		}
		f.out = dgDone
	case "words":
		if len(f.queue) > 0 {
			f.next()
			return
		}
		switch f.cmd {
		case dmCmdDumpROM:
			f.state, f.pos = "rom", 0
			f.romWord()
		case dmCmdDumpSave:
			f.state, f.out = "done", dgDone
		default:
			f.state, f.out = "idle", dgReady
		}
	case "rom":
		switch {
		case w == dmFin:
			f.state, f.out = "idle", dgReady
		case w&0xFF000000 == dmSeek:
			f.seeks++
			f.pos = f.starts[int(w&0xFFFFFF)]
			f.romWord()
		default:
			f.romWord()
		}
	case "save":
		if len(f.queue) > 0 {
			f.next()
			return
		}
		switch w {
		case dmCont:
			if f.pos++; f.pos == len(f.save)/256 {
				f.state, f.out = "idle", dgReady
				return
			}
			f.saveSection()
		case dmFail:
			f.secWords[5] = binary.LittleEndian.Uint32(f.save[f.pos*256+20:]) // sent right this time
			x := uint32(0)
			for _, v := range f.secWords[:64] {
				x ^= v
			}
			f.secWords[64] = x
			f.queue = append([]uint32(nil), f.secWords...)
			f.next()
		case dmAbort:
			f.state, f.out = "idle", dgReady
		default:
			f.out = 0
		}
	}
}

func TestGBADumperBackup(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	rom := make([]byte, 256<<10)
	rnd.Read(rom[:100<<10])
	for i := 100 << 10; i < 200<<10; i++ { // a long run of erased flash, sent as fills
		rom[i] = 0xFF
	}
	rnd.Read(rom[200<<10:])
	copy(rom[0xA0:], "POKEMON EMERBPEE")
	save := make([]byte, 0x10000)
	rnd.Read(save)
	f := newFakeDumper(rom, save)
	d := &gbaDumper{l: f}
	rs, ss, hdr, err := d.readInfo()
	if err != nil || rs != len(rom) || ss != len(save) || gbaHeaderText(hdr[0xA0:0xAC]) != "POKEMON EMER" || gbaHeaderText(hdr[0xAC:0xB0]) != "BPEE" {
		t.Fatalf("info: %v %d %d", err, rs, ss)
	}
	var last int
	got, err := d.dumpROM(func(done, total int) { last = done })
	if err != nil || !bytes.Equal(got, rom) || last != len(rom) {
		t.Fatalf("ROM: %v, equal %v, progress %d", err, bytes.Equal(got, rom), last)
	}
	if f.seeks < 2 {
		t.Fatalf("the damaged and lost words should each have needed a seek (%d)", f.seeks)
	}
	sv, err := d.dumpSave(nil)
	if err != nil || !bytes.Equal(sv, save) {
		t.Fatalf("save: %v", err)
	}
	// and again, from where it was left
	if sv, err = d.dumpSave(nil); err != nil || !bytes.Equal(sv, save) {
		t.Fatalf("save again: %v", err)
	}
}
