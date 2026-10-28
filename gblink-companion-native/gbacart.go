// GB-Link Cart Doctor's GBA cart dumper (assets/cartdoctor/gba-cart-dumper_mb.gba, based on
// FIX94's GBA Link Cable Dumper), used here to write ROMs to GBA flash carts: it runs small
// programs of cartridge reads and writes ("cart ops") that gbaflash.go builds. Ported from
// Cart Doctor's web/gba_cart.js (GPL-3.0).
package main

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"time"
)

//go:embed assets/cartdoctor/gba-cart-dumper_mb.gba
var gbaDumperImage []byte

const (
	dmMagic       = 0x47420000
	dmPoll        = 0x00000000
	dmGo          = 0x4742600D
	dmCont        = 0x4742C047
	dmFail        = 0x4742BAD0
	dmAbort       = 0x4742AB0B
	dgReady       = 0x52454459
	dgDone        = 0x444F4E45
	dgOkOk        = 0x4F4B4F4B
	dgFail        = 0x4641494C
	dmCmdDumpROM  = 1
	dmCmdDumpSave = 2
	dmCmdInfo     = 6
	dmCmdRun      = 7

	// the ROM dump stream (Cart Doctor's streamRom): records tagged R_DATA (one section), R_FILL
	// (a run of sections repeating one word) or R_END; M_SEEK sends the GBA back to a section
	dmSPoll = 0x47425EED
	dmFin   = 0x4742F1A1
	dmSeek  = 0x53000000
	drData  = 0xDA
	drFill  = 0xF1
	drEnd   = 0xE0
	dmBatch = 14 // words per USB write, as Cart Doctor sends them

	dmRunMax    = 0x20000
	dmResultMax = 0x4000
)

// wordLink: 32-bit exchanges with the GBA (the GB-Link, or a stand-in in the tests).
type wordLink interface {
	Words(words []uint32, timeout time.Duration) ([]uint32, error)
}

// gbaDumper talks to the dumper once it's running on the GBA.
type gbaDumper struct {
	l    wordLink
	stop func() bool
}

func (d *gbaDumper) cancelled() error {
	if d.stop != nil && d.stop() {
		return &FlashError{"cancelled", "Cancelled"}
	}
	return nil
}

func (d *gbaDumper) xfer(w uint32) (uint32, error) {
	r, err := d.l.Words([]uint32{w}, 3*time.Second)
	if err != nil {
		return 0, err
	}
	return r[0], nil
}

func (d *gbaDumper) pollReady(timeout time.Duration) error {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if err := d.cancelled(); err != nil {
			return err
		}
		w, err := d.xfer(dmPoll)
		if err != nil {
			return err
		}
		if w == dgReady {
			return nil
		}
		if w == dgDone {
			d.xfer(dmAbort)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return linkErr("The GBA program isn't answering. Check the cable and start again.")
}

// ping: the dumper answers.
func (d *gbaDumper) ping(timeout time.Duration) bool { return d.pollReady(timeout) == nil }

func (d *gbaDumper) command(cmd uint32) error {
	for attempt := 0; attempt < 5; attempt++ {
		if err := d.pollReady(10 * time.Second); err != nil {
			return err
		}
		r1, err := d.xfer(dmMagic | cmd)
		if err != nil {
			return err
		}
		if r1 != dgReady {
			continue
		}
		r2, err := d.xfer(dmGo)
		if err != nil {
			return err
		}
		if r2 == ^(dmMagic | cmd) {
			return nil
		}
	}
	return &FlashError{"desync", "The GBA program didn't take the command"}
}

func (d *gbaDumper) waitDone(timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		if err := d.cancelled(); err != nil {
			return err
		}
		w, err := d.xfer(dmPoll)
		if err != nil {
			return err
		}
		if w == dgDone {
			_, err := d.xfer(dmCont)
			return err
		}
		if w == dgReady {
			return &FlashError{"desync", "The GBA program went back to waiting in the middle of a job"}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return &FlashError{"timeout", "The GBA took too long"}
}

func (d *gbaDumper) readWords(n int) ([]uint32, error) {
	return d.l.Words(make([]uint32, n), 3*time.Second)
}

func (d *gbaDumper) streamIn(total int) ([]byte, error) { return d.streamInP(total, nil) }

// streamInP: streamIn, with progress, and the stop button (M_ABORT) checked between sections.
func (d *gbaDumper) streamInP(total int, progress func(done, total int)) ([]byte, error) {
	out := make([]byte, total)
	tx := make([]uint32, 65)
	retries := 0
	for s := 0; s < total/256; {
		if err := d.cancelled(); err != nil {
			d.xfer(dmAbort)
			return nil, err
		}
		resp, err := d.l.Words(tx, 3*time.Second)
		if err != nil {
			return nil, err
		}
		x := uint32(0)
		for _, w := range resp[:64] {
			x ^= w
		}
		if x == resp[64] {
			for i, w := range resp[:64] {
				binary.LittleEndian.PutUint32(out[s*256+4*i:], w)
			}
			if _, err := d.xfer(dmCont); err != nil {
				return nil, err
			}
			s++
			retries = 0
			if progress != nil {
				progress(s*256, total)
			}
			continue
		}
		if retries++; retries > 50 {
			d.xfer(dmAbort)
			return nil, &FlashError{"checksum", "The data kept arriving damaged"}
		}
		if _, err := d.xfer(dmFail); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *gbaDumper) streamOut(data []byte) error {
	retries := 0
	tx := make([]uint32, 65)
	for s := 0; s < len(data)/256; {
		x := uint32(0)
		for i := 0; i < 64; i++ {
			tx[i] = binary.LittleEndian.Uint32(data[s*256+4*i:])
			x ^= tx[i]
		}
		tx[64] = x
		if _, err := d.l.Words(tx, 3*time.Second); err != nil {
			return err
		}
		v, err := d.xfer(dmCont)
		if err != nil {
			return err
		}
		switch v {
		case dgOkOk:
			s++
			retries = 0
		case dgFail:
			if retries++; retries > 8 {
				return &FlashError{"checksum", "The data kept arriving damaged"}
			}
		default:
			return &FlashError{"desync", "The GBA program lost step"}
		}
	}
	return nil
}

// readInfo: the cartridge's ROM size and save size, and its header (nil: no cartridge).
func (d *gbaDumper) readInfo() (romSize, saveSize int, header []byte, err error) {
	if err = d.command(dmCmdInfo); err != nil {
		return
	}
	if err = d.waitDone(60 * time.Second); err != nil {
		return
	}
	sz, err := d.readWords(2)
	if err != nil {
		return
	}
	if sz[0] == 0xFFFFFFFF {
		return 0, 0, nil, nil
	}
	hw, err := d.readWords(0xC0 / 4)
	if err != nil {
		return
	}
	header = make([]byte, 0xC0)
	for i, w := range hw {
		binary.LittleEndian.PutUint32(header[4*i:], w)
	}
	return int(sz[0]), int(sz[1]), header, nil
}

// runOps runs a cart op program (a multiple of 256 bytes) and returns the result words.
func (d *gbaDumper) runOps(buf []byte, timeout time.Duration) ([]uint32, error) {
	n := len(buf)
	if n == 0 || n%256 != 0 || n > dmRunMax {
		return nil, &FlashError{"payload", "bad cart op program size"}
	}
	if err := d.command(dmCmdRun); err != nil {
		return nil, err
	}
	echo, err := d.l.Words([]uint32{uint32(n), ^uint32(n)}, 3*time.Second)
	if err != nil {
		return nil, err
	}
	if echo[1] != uint32(n) {
		return nil, &FlashError{"desync", "The GBA didn't take the cart op program"}
	}
	if err := d.streamOut(buf); err != nil {
		return nil, err
	}
	if err := d.waitDone(timeout); err != nil {
		return nil, err
	}
	b, err := d.readWords(1)
	if err != nil {
		return nil, err
	}
	bytes := int(b[0])
	if bytes == 0 || bytes%256 != 0 || bytes > dmResultMax {
		return nil, &FlashError{"desync", "bad cart op result size"}
	}
	out, err := d.streamIn(bytes)
	if err != nil {
		return nil, err
	}
	words := make([]uint32, bytes/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(out[4*i:])
	}
	return words, nil
}

// streamRecords receives a ROM dump (Cart Doctor's _streamRecords): the GBA sends the sections
// without waiting, each as a data record or a run of repeated words as one fill record; they're
// taken strictly in order, and after a bad or missing one the next batch starts with M_SEEK to
// send the GBA back to the first section still needed.
func (d *gbaDumper) streamRecords(total int, progress func(done, total int)) ([]byte, error) {
	sections := total / 256
	out := make([]byte, total)
	next, failures := 0, 0
	seek := false
	type record struct {
		header uint32
		words  []uint32
		need   int
	}
	var rec *record
	fail := func() { rec, seek = nil, true; failures++ }
	accept := func(r *record) bool {
		c := r.header
		for _, w := range r.words[:len(r.words)-1] {
			c = (c<<1 | c>>31) ^ w
		}
		if c != r.words[len(r.words)-1] {
			return false
		}
		base := next * 256
		if r.header>>24 == drData {
			for i, w := range r.words[:64] {
				binary.LittleEndian.PutUint32(out[base+4*i:], w)
			}
			next++
		} else {
			value, count := r.words[0], int(r.words[1])
			if count == 0 || count > sections-next {
				return false
			}
			for o := base; o < base+count*256; o += 4 {
				binary.LittleEndian.PutUint32(out[o:], value)
			}
			next += count
		}
		failures = 0
		if progress != nil {
			progress(next*256, total)
		}
		return true
	}
	tx := make([]uint32, dmBatch)
	for {
		if err := d.cancelled(); err != nil {
			d.xfer(dmAbort)
			return nil, err
		}
		if failures > 50 {
			d.xfer(dmAbort)
			if next == 0 {
				return nil, &FlashError{"desync", "The GBA didn't start sending the game"}
			}
			return nil, &FlashError{"checksum", fmt.Sprintf("Part %d of the game kept arriving damaged", next)}
		}
		for i := range tx {
			tx[i] = dmSPoll
		}
		first := 0
		if seek { // the answer to the seek word itself still comes from the old place
			tx[0], first, seek = dmSeek|uint32(next), 1, false
		}
		rx, err := d.l.Words(tx, 3*time.Second)
		if err != nil {
			return nil, err
		}
		for i := first; i < len(rx) && !seek; i++ {
			w := rx[i]
			if rec != nil {
				rec.words = append(rec.words, w)
				if len(rec.words) == rec.need {
					ok := accept(rec)
					rec = nil
					if !ok {
						fail()
					}
				}
				continue
			}
			tag, index := w>>24, int(w&0xFFFFFF)
			switch {
			case tag == drData && index == next:
				rec = &record{header: w, need: 65}
			case tag == drFill && index == next:
				rec = &record{header: w, need: 3}
			case tag == drEnd && index == sections && next == sections:
				if _, err := d.xfer(dmFin); err != nil {
					return nil, err
				}
				return out, nil
			default:
				fail()
			}
		}
	}
}

// dumpROM copies the whole game (Cart Doctor's dumpRom: the GBA finds its size).
func (d *gbaDumper) dumpROM(progress func(done, total int)) ([]byte, error) {
	if err := d.command(dmCmdDumpROM); err != nil {
		return nil, err
	}
	if err := d.waitDone(60 * time.Second); err != nil { // the size, and a scan for repeated runs
		return nil, err
	}
	sz, err := d.readWords(1)
	if err != nil {
		return nil, err
	}
	if sz[0] == 0xFFFFFFFF || sz[0] == 0 || sz[0] > 32<<20 || sz[0]%256 != 0 {
		return nil, &FlashError{"no-cart", "No GBA cartridge was found"}
	}
	return d.streamRecords(int(sz[0]), progress)
}

// dumpSave copies the cartridge's save (Cart Doctor's dumpSave).
func (d *gbaDumper) dumpSave(progress func(done, total int)) ([]byte, error) {
	if err := d.command(dmCmdDumpSave); err != nil {
		return nil, err
	}
	if err := d.waitDone(60 * time.Second); err != nil { // the sizes (an EEPROM has to be read)
		return nil, err
	}
	sz, err := d.readWords(2)
	if err != nil {
		return nil, err
	}
	if sz[0] == 0xFFFFFFFF {
		return nil, &FlashError{"no-cart", "No GBA cartridge was found"}
	}
	if sz[1] == 0 {
		return nil, &FlashError{"no-save", "This cartridge has no save memory"}
	}
	if err := d.waitDone(120 * time.Second); err != nil { // the GBA copies the save into its memory
		return nil, err
	}
	return d.streamInP(int(sz[1]), progress)
}
