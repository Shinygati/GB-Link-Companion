// Streamed transfers with the helper on the GBA (helper 1.1 and later, b2s/gba/proto.h): saves
// and ROMs move the way GB-Link Cart Doctor moves them, as a plain stream of words in 256-byte
// sections that each end with an XOR check, instead of one acknowledged packet at a time. Ported
// from Cart Doctor's web/gba_cart.js (_streamIn, _streamOut and _streamRecords).
package main

import (
	"encoding/binary"
	"errors"
	"time"
)

const (
	cmdSaveDump  = 14
	cmdSavePut   = 15
	cmdROMStream = 16

	helperStreams = 0x00010100 // the first helper version with the streamed transfers

	sGo    = 0x4742600D
	sCont  = 0x4742C047
	sFail  = 0x4742BAD0
	sAbort = 0x4742AB0B
	sSPoll = 0x47425EED
	sFin   = 0x4742F1A1
	sSeek  = 0x53000000
	gReady = 0x52454459
	gDone  = 0x444F4E45
	gOkOk  = 0x4F4B4F4B
	gFail  = 0x4641494C
	rData  = 0xDA
	rFill  = 0xF1
	rEnd   = 0xE0

	streamSection  = 0x100
	streamWords    = streamSection / 4
	maxStreamSave  = 0x10000 // a command moves at most 64 KB of a save
	streamWordWait = 2 * time.Second
)

// errStreamDesync: the two ends lost step; the whole command is tried again.
var errStreamDesync = errors.New("link out of step")

// Streams says whether the helper running on the GBA knows the streamed transfers.
func (h *Helper) Streams() bool { return h.version >= helperStreams }

// startStream sends a streamed command's packet (the GBA answers with a gate, not a RESULT).
func (h *Helper) startStream(cmd int, args []uint32) error {
	if !h.waitIdle(8 * time.Second) {
		return linkErr("The GBA isn't responding")
	}
	if err := h.sendPacket(cmd, args, nil); err != nil {
		return err
	}
	h.shot.leave() // the GBA shows its own screen again
	return nil
}

// gate polls until the GBA says want (G_DONE once its work is done, G_READY when it's ready to
// receive), then answers it with goWord. A RESULT instead means the GBA turned the command down.
func (h *Helper) gate(want, goWord uint32, timeout time.Duration) error {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		r, err := h.link.word(wPoll)
		if err != nil {
			return err
		}
		switch {
		case r == want:
			_, err := h.link.word(goWord)
			return err
		case r>>24 == 0x52 && int(r>>16&0xFF) < len(stNames) && r&0xFFFF < 16: // a RESULT: refused
			st := int(r >> 16 & 0xFF)
			for i := 0; i <= int(r&0xFFFF); i++ { // its words, then IDLE
				h.link.word(wPoll)
			}
			return linkErr("The GBA refused: %s", stNames[st])
		case r == wIdle:
			return errStreamDesync // it gave up and is waiting for commands again
		}
		if r == 0xFFFFFFFF { // busy with the cartridge
			time.Sleep(2 * time.Millisecond)
		}
	}
	return linkErr("The GBA stopped responding")
}

// streamIn receives n bytes (a multiple of 256) in checked sections.
func (h *Helper) streamIn(n int, progress func(done int)) ([]byte, error) {
	out := make([]byte, n)
	polls := make([]uint32, streamWords+1)
	retries := 0
	for s := 0; s < n/streamSection; {
		resp, err := h.link.Words(polls, streamWordWait)
		if err != nil {
			return nil, err
		}
		x := uint32(0)
		for _, w := range resp[:streamWords] {
			x ^= w
		}
		if x == resp[streamWords] {
			base := s * streamSection
			for i, w := range resp[:streamWords] {
				binary.LittleEndian.PutUint32(out[base+4*i:], w)
			}
			if _, err := h.link.word(sCont); err != nil {
				return nil, err
			}
			s++
			retries = 0
			if progress != nil {
				progress(s * streamSection)
			}
			continue
		}
		if retries++; retries > 50 {
			h.link.word(sAbort)
			return nil, linkErr("The data kept arriving damaged - check the link cable")
		}
		if _, err := h.link.word(sFail); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// streamOut sends data (a multiple of 256 bytes) in checked sections.
func (h *Helper) streamOut(data []byte, progress func(done int)) error {
	retries := 0
	tx := make([]uint32, streamWords+1)
	for s := 0; s < len(data)/streamSection; {
		base, x := s*streamSection, uint32(0)
		for i := 0; i < streamWords; i++ {
			tx[i] = binary.LittleEndian.Uint32(data[base+4*i:])
			x ^= tx[i]
		}
		tx[streamWords] = x
		if _, err := h.link.Words(tx, streamWordWait); err != nil {
			return err
		}
		v, err := h.link.word(sCont)
		if err != nil {
			return err
		}
		switch v {
		case gOkOk:
			s++
			retries = 0
			if progress != nil {
				progress(s * streamSection)
			}
		case gFail:
			if retries++; retries > 8 {
				return linkErr("The data kept arriving damaged - check the link cable")
			}
		default:
			return errStreamDesync
		}
	}
	return nil
}

// streamTry runs one streamed command up to 4 times, getting the link back in step in between.
func (h *Helper) streamTry(f func() error) error {
	var err error
	for try := 0; try < 4; try++ {
		if try > 0 && !h.recover() {
			break
		}
		if err = f(); err == nil {
			return nil
		}
		if err == errCancelled {
			return err
		}
		if le, ok := err.(*LinkError); ok && len(le.msg) > 16 && le.msg[:16] == "The GBA refused:" {
			return err
		}
	}
	if err == errStreamDesync {
		err = linkErr("The link kept losing step - check the link cable")
	}
	return err
}

// streamReadSave reads [off, off+n) of the save (n <= 64 KB, a multiple of 256).
func (h *Helper) streamReadSave(stype, off, n, step, steps int, progress func(done int)) ([]byte, error) {
	var out []byte
	err := h.streamTry(func() error {
		if err := h.startStream(cmdSaveDump, []uint32{uint32(stype), uint32(off), uint32(n), uint32(step), uint32(steps)}); err != nil {
			return err
		}
		if err := h.gate(gDone, sCont, 20*time.Second); err != nil {
			return err
		}
		var err error
		out, err = h.streamIn(n, progress)
		return err
	})
	return out, err
}

// streamWriteSave writes data at off (at most 64 KB; flash saves: whole 4 KB sectors). The GBA
// writes it to the save chip and checks it, then answers as for CMD_SAVE_WRITE.
func (h *Helper) streamWriteSave(stype, off int, data []byte, step, steps int, progress func(done int)) error {
	var st int
	var res []uint32
	err := h.streamTry(func() error {
		if err := h.startStream(cmdSavePut, []uint32{uint32(stype), uint32(off), uint32(len(data)), uint32(step), uint32(steps)}); err != nil {
			return err
		}
		if err := h.gate(gReady, sGo, 10*time.Second); err != nil {
			return err
		}
		if err := h.streamOut(data, progress); err != nil {
			return err
		}
		var err error
		st, res, err = h.getResult([]int{0, 3}, 60*time.Second)
		if err == errRetry {
			return errStreamDesync
		}
		if err == nil && st == stChecksum {
			return errStreamDesync
		}
		return err
	})
	if err != nil {
		return err
	}
	if st == stVerifyFail && len(res) > 0 {
		return linkErr("The cart's save didn't verify at 0x%05X - check the cart's contacts", res[0])
	}
	if st != stOK {
		return linkErr("Writing the save failed: %s", stNames[st])
	}
	return nil
}

// streamReadROM reads the first n bytes of the cartridge ROM (a multiple of 256). Runs of
// sections that repeat one word come over as a single record, so blank space costs nothing.
func (h *Helper) streamReadROM(n int, progress func(done, total int)) ([]byte, error) {
	var out []byte
	err := h.streamTry(func() error {
		if err := h.startStream(cmdROMStream, []uint32{uint32(n)}); err != nil {
			return err
		}
		// the GBA looks through the whole ROM for blank runs first
		if err := h.gate(gDone, sCont, 120*time.Second); err != nil {
			return err
		}
		var err error
		out, err = h.streamRecords(n, progress)
		return err
	})
	return out, err
}

// streamRecords receives the ROM stream (Cart Doctor's _streamRecords).
func (h *Helper) streamRecords(total int, progress func(done, total int)) ([]byte, error) {
	sections := total / streamSection
	out := make([]byte, total)
	rotl1 := func(c uint32) uint32 { return c<<1 | c>>31 }
	next := 0
	type record struct {
		header uint32
		words  []uint32
		need   int
	}
	var rec *record
	seek, failures := false, 0
	fail := func() { rec, seek = nil, true; failures++ }
	accept := func(r *record) bool {
		c := r.header
		for _, w := range r.words[:len(r.words)-1] {
			c = rotl1(c) ^ w
		}
		if c != r.words[len(r.words)-1] {
			return false
		}
		base := next * streamSection
		if r.header>>24 == rData {
			for i := 0; i < streamWords; i++ {
				binary.LittleEndian.PutUint32(out[base+4*i:], r.words[i])
			}
			next++
		} else {
			value, count := r.words[0], int(r.words[1])
			if count == 0 || count > sections-next {
				return false
			}
			for off := base; off < base+count*streamSection; off += 4 {
				binary.LittleEndian.PutUint32(out[off:], value)
			}
			next += count
		}
		failures = 0
		if progress != nil {
			progress(next*streamSection, total)
		}
		return true
	}
	tx := make([]uint32, 15)
	for {
		if h.stop != nil && h.stop() {
			h.link.word(sAbort)
			return nil, errCancelled
		}
		if failures > 50 {
			h.link.word(sAbort)
			if next == 0 {
				return nil, errStreamDesync
			}
			return nil, linkErr("Reading the cartridge kept failing at %d KB - check the link cable", next*streamSection>>10)
		}
		for i := range tx {
			tx[i] = sSPoll
		}
		first := 0
		if seek {
			tx[0] = sSeek | uint32(next) // its own reply still comes from the old place
			first, seek = 1, false
		}
		rx, err := h.link.Words(tx, streamWordWait)
		if err != nil {
			return nil, err
		}
		for i := first; i < len(rx) && !seek; i++ {
			w := rx[i]
			if rec != nil {
				rec.words = append(rec.words, w)
				if len(rec.words) == rec.need {
					r := rec
					rec = nil
					if !accept(r) {
						fail()
					}
				}
				continue
			}
			tag, index := w>>24, int(w&0xFFFFFF)
			switch {
			case tag == rData && index == next:
				rec = &record{header: w, need: streamWords + 1}
			case tag == rFill && index == next:
				rec = &record{header: w, need: 3}
			case tag == rEnd && index == sections && next == sections:
				h.link.word(sFin)
				return out, nil
			default:
				fail()
			}
		}
	}
}

// saveRanges: the changed 4 KB blocks of a save, joined into runs of at most 64 KB.
func saveRanges(data, old []byte, size int) [][2]int {
	var out [][2]int
	for off := 0; off < size; off += saveChunk {
		e := min(off+saveChunk, size)
		if old != nil && len(old) >= e && string(data[off:e]) == string(old[off:e]) {
			continue
		}
		if n := len(out); n > 0 && out[n-1][1] == off && out[n-1][1]-out[n-1][0] < maxStreamSave {
			out[n-1][1] = e
		} else {
			out = append(out, [2]int{off, e})
		}
	}
	return out
}
