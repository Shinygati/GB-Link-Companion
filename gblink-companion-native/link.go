// GB-Link transport, GBA multiboot, and the protocol of the helper program that runs on the GBA
// (the Burn2Slot for GB-Link flasher, built with GB-Link Companion's screens: see b2s/gba/proto.h).
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// ------------------------------------------------------------------------- GB-Link serial
// GB-Link firmware serial (CDC-ACM) framing: 'G' 'B' channel len_lo len_hi payload
const (
	chCommand = 0
	chData    = 1

	cmdSetMode    = 0x00
	cmdSetTiming  = 0x30
	cmdVoltage3V3 = 0x40
	modeGBLink    = 0x02
	gblinkVID     = "2FE3"
)

// LinkError is a problem talking to the adapter or the GBA, with a message for the player.
type LinkError struct{ msg string }

func (e *LinkError) Error() string { return e.msg }
func linkErr(f string, a ...any) error {
	return &LinkError{fmt.Sprintf(f, a...)}
}

type GBLink struct {
	port   serial.Port
	name   string
	mu     sync.Mutex
	cond   *sync.Cond
	frames [][]byte
	closed bool
	broken bool // the adapter was unplugged (or stopped responding): every exchange fails straight away
}

// Broken says the adapter has gone: unplugged, or not answering. A new connection is needed.
func (l *GBLink) Broken() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.broken
}

func (l *GBLink) setBroken() {
	l.mu.Lock()
	l.broken = true
	l.cond.Broadcast()
	l.mu.Unlock()
}

// FindGBLinkPort returns the serial port of the first GB-Link adapter plugged in.
func FindGBLinkPort() string {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return ""
	}
	for _, p := range ports {
		if p.IsUSB && strings.EqualFold(p.VID, gblinkVID) {
			return p.Name
		}
	}
	return ""
}

func OpenGBLink(name string) (*GBLink, error) {
	p, err := serial.Open(name, &serial.Mode{BaudRate: 115200})
	if err != nil {
		if runtime.GOOS == "linux" && strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			return nil, linkErr("Linux didn't let this app open the GB-Link adapter (%s). Add yourself to the group "+
				"that owns it - usually: sudo usermod -aG dialout $USER (on Arch: uucp) - then log out and back in.", name)
		}
		return nil, linkErr("Couldn't open the GB-Link adapter on %s (%v). Is another program using it?", name, err)
	}
	p.SetReadTimeout(50 * time.Millisecond)
	l := &GBLink{port: p, name: name}
	l.cond = sync.NewCond(&l.mu)
	go l.readLoop()
	return l, nil
}

func (l *GBLink) Close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.port.Close()
}

func (l *GBLink) readLoop() {
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	fails := 0
	for {
		n, err := l.port.Read(chunk)
		l.mu.Lock()
		closed := l.closed
		l.mu.Unlock()
		if closed {
			return
		}
		if err != nil {
			if fails++; fails >= 10 { // half a second of errors: the adapter was unplugged
				l.setBroken()
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		fails = 0
		if n == 0 {
			continue
		}
		buf = append(buf, chunk[:n]...)
		for {
			i := bytes.Index(buf, []byte("GB"))
			if i < 0 {
				if len(buf) > 0 {
					buf = buf[len(buf)-1:]
				}
				break
			}
			if len(buf) < i+5 {
				buf = buf[i:]
				break
			}
			ch, ln := buf[i+2], int(buf[i+3])|int(buf[i+4])<<8
			if ln > 64 {
				buf = buf[i+1:]
				continue
			}
			if len(buf) < i+5+ln {
				buf = buf[i:]
				break
			}
			payload := append([]byte(nil), buf[i+5:i+5+ln]...)
			buf = append(buf[:0], buf[i+5+ln:]...)
			if ch == chData {
				l.mu.Lock()
				l.frames = append(l.frames, payload)
				l.cond.Broadcast()
				l.mu.Unlock()
			}
		}
	}
}

func (l *GBLink) writeFrame(channel byte, payload []byte) error {
	if l.Broken() {
		return linkErr("The GB-Link adapter was unplugged or stopped responding")
	}
	f := append([]byte{'G', 'B', channel, byte(len(payload)), byte(len(payload) >> 8)}, payload...)
	// On Windows, writing to an adapter that has been unplugged can hang rather than fail: give up
	// after a while and treat it as unplugged.
	done := make(chan error, 1)
	go func() { _, err := l.port.Write(f); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			l.setBroken()
			return linkErr("The GB-Link adapter was unplugged or stopped responding (%v)", err)
		}
	case <-time.After(2 * time.Second):
		l.setBroken()
		return linkErr("The GB-Link adapter was unplugged or stopped responding")
	}
	return nil
}

func (l *GBLink) SetMode(m byte) error { return l.writeFrame(chCommand, []byte{cmdSetMode, m}) }
func (l *GBLink) SetTiming(us int, bytesPerTransfer byte) error {
	return l.writeFrame(chCommand, []byte{cmdSetTiming, byte(us), byte(us >> 8), byte(us >> 16), bytesPerTransfer})
}
func (l *GBLink) SetVoltage3V3() error { return l.writeFrame(chCommand, []byte{cmdVoltage3V3}) }

func (l *GBLink) Drain() {
	l.mu.Lock()
	l.frames = nil
	l.mu.Unlock()
}

// Exchange sends bytes (<= 60) on the data channel and returns as many reply bytes.
func (l *GBLink) Exchange(tx []byte, timeout time.Duration) ([]byte, error) {
	if err := l.writeFrame(chData, tx); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(tx))
	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() { l.mu.Lock(); l.cond.Broadcast(); l.mu.Unlock() })
	defer timer.Stop()
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(out) < len(tx) {
		if l.broken {
			return nil, linkErr("The GB-Link adapter was unplugged or stopped responding")
		}
		if len(l.frames) == 0 {
			if time.Now().After(deadline) {
				// the adapter answers every word, GBA or not: silence means this connection is dead
				// (unplugged, or plugged in again as a new one)
				l.broken = true
				return nil, linkErr("No response from the GB-Link adapter")
			}
			l.cond.Wait()
			continue
		}
		out = append(out, l.frames[0]...)
		l.frames = l.frames[1:]
	}
	return out[:len(tx)], nil
}

// Words exchanges 32-bit words (big-endian on the wire), 15 per USB transfer.
func (l *GBLink) Words(words []uint32, timeout time.Duration) ([]uint32, error) {
	out := make([]uint32, 0, len(words))
	for i := 0; i < len(words); i += 15 {
		end := min(i+15, len(words))
		tx := make([]byte, 4*(end-i))
		for j, w := range words[i:end] {
			binary.BigEndian.PutUint32(tx[4*j:], w)
		}
		rx, err := l.Exchange(tx, timeout)
		if err != nil {
			return nil, err
		}
		for j := 0; j < end-i; j++ {
			out = append(out, binary.BigEndian.Uint32(rx[4*j:]))
		}
	}
	return out, nil
}

func (l *GBLink) word(w uint32) (uint32, error) {
	r, err := l.Words([]uint32{w}, 2*time.Second)
	if err != nil {
		return 0, err
	}
	return r[0], nil
}

// ----------------------------------------------------------------------------- multiboot
var errNoGBA = errors.New("no GBA waiting for multiboot")

// errEmeraldWCRunning: instead of a GBA waiting for multiboot, Emerald is already running with
// Wonder Cards (wcgba.go): its program answered.
var errEmeraldWCRunning = errors.New("Emerald with Wonder Cards is already running")

// Multiboot sends a program to a GBA waiting in its BIOS (switched on holding START + SELECT).
// Port of GB-Link's multiboot.js. Returns errNoGBA if no GBA answered within wait.
func (l *GBLink) Multiboot(data []byte, wait time.Duration, cancel func() bool) error {
	if len(data) > 0x40000 {
		return linkErr("multiboot image too big")
	}
	padded := append(append([]byte(nil), data...), make([]byte, 0x10)...)
	l.SetMode(modeGBLink)
	time.Sleep(100 * time.Millisecond)
	l.SetTiming(36, 4)
	time.Sleep(50 * time.Millisecond)
	l.Drain()

	deadline := time.Now().Add(wait)
	for {
		if cancel != nil && cancel() {
			return linkErr("Cancelled")
		}
		r, err := l.word(0x6202)
		if err != nil {
			return err
		}
		if r>>16 == 0x7202 {
			break
		}
		if r>>24 == 0xA5 { // Emerald's Wonder Card program, already running
			return errEmeraldWCRunning
		}
		if time.Now().After(deadline) {
			return errNoGBA
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := l.word(0x6102); err != nil {
		return err
	}
	header := make([]uint32, 0, 0x60)
	for i := 0; i < 0xC0; i += 2 {
		header = append(header, uint32(padded[i])|uint32(padded[i+1])<<8)
	}
	if _, err := l.Words(header, 2*time.Second); err != nil {
		return err
	}
	for _, w := range []uint32{0x6200, 0x6202, 0x63D1} {
		if _, err := l.word(w); err != nil {
			return err
		}
	}
	token, err := l.word(0x63D1)
	if err != nil {
		return err
	}
	if token>>24 != 0x73 {
		return linkErr("Multiboot handshake failed - check the cable and try again")
	}
	crcA := (token >> 16) & 0xFF
	seed := 0xFFFF00D1 | crcA<<8
	crcA = (crcA + 0x0F) & 0xFF
	if _, err := l.word(0x6400 | crcA); err != nil {
		return err
	}
	fsize := (len(data) + 0x0F) &^ 0x0F
	token2, err := l.word(uint32(fsize-0x190) / 4)
	if err != nil {
		return err
	}
	crcB := (token2 >> 16) & 0xFF
	crcC := uint32(0xC387)
	crcStep := func(v uint32) {
		for b := 0; b < 32; b++ {
			bit := (crcC ^ v) & 1
			crcC >>= 1
			if bit != 0 {
				crcC ^= 0xC37B
			}
			v >>= 1
		}
	}
	for i := 0xC0; i < fsize; {
		n := min(15, (fsize-i)/4)
		batch := make([]uint32, n)
		for w := 0; w < n; w++ {
			off := i + w*4
			dat := binary.LittleEndian.Uint32(padded[off:])
			crcStep(dat)
			seed = seed*0x6F646573 + 1
			batch[w] = seed ^ dat ^ (0xFE000000 - uint32(off)) ^ 0x43202F2F
		}
		resp, err := l.Words(batch, 2*time.Second)
		if err != nil {
			return err
		}
		for w := 0; w < n; w++ {
			if resp[w]>>16 != uint32(i+w*4)&0xFFFF {
				return linkErr("Multiboot transfer error - check the cable and try again")
			}
		}
		i += n * 4
	}
	crcStep(0xFFFF0000 | crcB<<8 | crcA)
	l.word(0x0065)
	end := time.Now().Add(10 * time.Second)
	for {
		r, err := l.word(0x0065)
		if err != nil {
			return err
		}
		if r>>16 == 0x0075 {
			break
		}
		if time.Now().After(end) {
			return linkErr("The GBA didn't accept the program")
		}
		time.Sleep(10 * time.Millisecond)
	}
	l.word(0x0066)
	l.word(crcC & 0xFFFF)
	time.Sleep(time.Second)
	return nil
}

// ------------------------------------------------------------------- helper (flasher) protocol
const (
	wPoll  = 0x504F4C4C
	wIdle  = 0x49444C45
	wKeys  = 0x4B455953
	ckInit = 0x12345678

	cmdHello     = 1
	cmdSaveInfo  = 6
	cmdSaveRead  = 7
	cmdSaveWrite = 8
	cmdShow      = 9
	cmdROMInfo   = 10
	cmdROMRead   = 11
	cmdRTCRead   = 13
	msgControl   = 1
	msgStopped   = 2 // a backup was cancelled or failed
	msgBackupSav = 3 // the next save read is part of a backup
	msgBackupEnd = 4 // a backup finished
	romChunk     = 0x8000

	stOK         = 0
	stChecksum   = 3
	stVerifyFail = 5

	saveChunk     = 4096   // a save write: 4 KB a command (flash sectors)
	saveReadChunk = 0x4000 // a save read: 16 KB a command (MAX_SAVE_READ_CHUNK)
)

// chunkWords: a read's reply words: [checksum, data...], or [checksum, word] when the whole chunk
// is that one word (blank flash, padding). Returns the n bytes, or false if the checksum's wrong.
func chunkWords(d []uint32, n int) ([]byte, bool) {
	words := n / 4
	ck := uint32(ckInit)
	out := make([]byte, 0, n)
	if len(d) == 2 && words > 1 {
		for i := 0; i < words; i++ {
			ck = ckStep(ck, d[1])
			out = binary.LittleEndian.AppendUint32(out, d[1])
		}
	} else {
		if len(d) != words+1 {
			return nil, false
		}
		for _, w := range d[1:] {
			ck = ckStep(ck, w)
			out = binary.LittleEndian.AppendUint32(out, w)
		}
	}
	return out, ck == d[0]
}

var stNames = []string{"OK", "unknown flash chip", "timed out", "checksum error", "write failed",
	"verify failed", "bad command", "out of range", "no save chip"}

type SaveType struct {
	Name string
	Size int
}

var saveTypes = map[int]SaveType{
	1: {"SRAM / FRAM 32 KB", 0x8000}, 2: {"SRAM 64 KB", 0x10000}, 3: {"Flash 64 KB", 0x10000},
	4: {"Flash 128 KB", 0x20000}, 5: {"EEPROM 512 B", 0x200}, 6: {"EEPROM 8 KB", 0x2000},
}

func ckStep(c, w uint32) uint32 { return (c<<5 | c>>27) + w }

var errRetry = errors.New("link out of step")

type Helper struct {
	link *GBLink
	gap  int
	// between, if set, is called between the blocks of a save write: the GBA answers then, so the
	// caller can pass its buttons on to mGBA (linkMu is held by the caller; don't take it again)
	between func()
	// during, if set, is called while waiting for a command's result, until the result starts to
	// arrive (a save write: the GBA answers button polls meanwhile, see sio_service in the helper).
	// Called with linkMu held; it may use h.Keys().
	during func()
	shot   gbaScreen // a copy of the GBA's status screen, for "Save screenshot" (gbascreen.go)
	// the helper's version (CMD_HELLO); 1.1 and later know the streamed transfers (stream.go)
	version uint32
	// stop, if set, is asked during a streamed ROM dump whether to stop (Cancel)
	stop func() bool
}

func (h *Helper) Configure() {
	h.link.SetMode(modeGBLink)
	time.Sleep(100 * time.Millisecond)
	h.link.SetTiming(h.gap, 4)
	time.Sleep(20 * time.Millisecond)
	h.link.Drain()
}

func (h *Helper) waitIdle(timeout time.Duration) bool {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		r, err := h.link.Words([]uint32{wPoll, wPoll, wPoll, wPoll}, 2*time.Second)
		if err == nil && r[3] == wIdle {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (h *Helper) sendPacket(cmd int, args []uint32, data []uint32) error {
	words := []uint32{0x42000000 | uint32(cmd)<<16 | uint32(len(args))}
	words = append(words, args...)
	words = append(words, data...)
	ck := uint32(ckInit)
	for _, w := range words {
		ck = ckStep(ck, w)
	}
	words = append(words, ck)
	for i := 0; i < len(words); i += 120 {
		end := min(i+120, len(words))
		replies, err := h.link.Words(words[i:end], 2*time.Second)
		if err != nil {
			return err
		}
		for j, r := range replies {
			k := i + j
			want := uint32(wIdle)
			if k != 0 {
				want = 0xA5000000 | uint32(k)&0xFFFFFF
			}
			if r != want {
				return errRetry
			}
		}
	}
	return nil
}

func (h *Helper) getResult(counts []int, timeout time.Duration) (int, []uint32, error) {
	end := time.Now().Add(timeout)
	var pending []uint32
	need := 15
	resultSeen := false
	for time.Now().Before(end) {
		if h.during != nil && !resultSeen {
			h.during()
		}
		polls := make([]uint32, need)
		for i := range polls {
			polls[i] = wPoll
		}
		r, err := h.link.Words(polls, 2*time.Second)
		if err != nil {
			return 0, nil, err
		}
		pending = append(pending, r...)
		for _, w := range r {
			if w>>24 == 0x52 {
				resultSeen = true
			}
		}
		need = 15
		waiting := false
		for i := 0; i < len(pending); i++ {
			w := pending[i]
			if w>>24 != 0x52 {
				continue
			}
			st, cnt := int(w>>16&0xFF), int(w&0xFFFF)
			if st >= len(stNames) || !containsInt(counts, cnt) {
				continue
			}
			if i+cnt+1 >= len(pending) {
				pending = pending[i:]
				need = max(15, min(240, cnt+2-len(pending)))
				waiting = true
				break
			}
			if pending[i+cnt+1] == wIdle {
				return st, pending[i+1 : i+1+cnt], nil
			}
			// A word went missing (or one too many) on the way: once the GBA's IDLE shows up
			// after it, ask for the result again; until then keep the result in hand.
			after := pending[i+cnt+1:]
			if containsWord(after, wIdle) {
				return 0, nil, errRetry
			}
			if len(after) < 64 {
				pending = pending[i:]
				waiting = true
				break
			}
		}
		if !waiting && len(pending) > 64 {
			pending = pending[len(pending)-32:]
		}
	}
	return 0, nil, linkErr("The GBA stopped responding")
}

func containsWord(a []uint32, v uint32) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}

func containsInt(a []int, v int) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}

// Command sends one command and returns its status and result words, retrying if the link slips.
func (h *Helper) Command(cmd int, args, data []uint32, timeout time.Duration, counts []int, idleWait time.Duration) (int, []uint32, error) {
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		w := idleWait
		if attempt > 0 {
			w = 3 * time.Second
		}
		if !h.waitIdle(w) {
			return 0, nil, linkErr("The GBA isn't responding")
		}
		if err := h.sendPacket(cmd, args, data); err != nil {
			if err != errRetry {
				return 0, nil, err
			}
			last = err
			if attempt >= 1 && h.gap < 300 {
				h.gap += 40
				h.link.SetTiming(h.gap, 4)
			}
			continue
		}
		if cmd != cmdStatus {
			h.shot.leave() // the GBA shows its own screen again for any command but a status one
		}
		st, res, err := h.getResult(counts, timeout)
		if err == errRetry {
			last = errors.New("reply garbled")
			continue
		}
		if err != nil {
			return 0, nil, err
		}
		if st == stChecksum {
			last = errors.New("checksum error")
			continue
		}
		return st, res, nil
	}
	return 0, nil, linkErr("Link kept failing (%v)", last)
}

// recover: after a reply went missing, the GBA may still be sending it. Let it finish, then
// report whether it's listening again (true = worth asking again).
func (h *Helper) recover() bool {
	h.Configure()
	return h.waitIdle(6 * time.Second)
}

// Alive reports whether the helper is running on the GBA right now.
func (h *Helper) Alive(timeout time.Duration) bool {
	h.Configure()
	if !h.waitIdle(timeout) {
		return false
	}
	st, d, err := h.Command(cmdHello, nil, nil, 3*time.Second, []int{1}, timeout)
	if err == nil && st == stOK && len(d) == 1 {
		h.version = d[0]
	}
	return err == nil && st == stOK
}

// Keys returns the buttons held on the GBA (KEYINPUT bits, 1 = pressed), or -1 if no answer.
func (h *Helper) Keys() int {
	r, err := h.link.Words([]uint32{wKeys, wPoll}, 500*time.Millisecond)
	if err != nil || r[0] != wIdle || r[1]>>24 != 0x4B {
		return -1
	}
	return int(r[1] & 0x3FF)
}

func (h *Helper) ShowController() error { return h.Show(msgControl) }

// Show puts one of the helper's status messages (msg*) on the GBA's screen.
func (h *Helper) Show(msg int) error {
	_, _, err := h.Command(cmdShow, []uint32{uint32(msg)}, nil, 3*time.Second, []int{0}, time.Second)
	return err
}

type CartInfo struct {
	Type    int
	FlashID uint32
	Title   string
	Code    string
}

func (h *Helper) SaveInfo() (CartInfo, error) {
	_, d, err := h.Command(cmdSaveInfo, nil, nil, 60*time.Second, []int{6}, 8*time.Second)
	if err != nil {
		return CartInfo{}, err
	}
	raw := make([]byte, 16)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(raw[4*i:], d[2+i])
	}
	return CartInfo{Type: int(d[0]), FlashID: d[1], Title: printable(raw[:12]), Code: printable(raw[12:16])}, nil
}

func printable(b []byte) string {
	s := make([]byte, len(b))
	for i, c := range b {
		if c >= 32 && c < 127 {
			s[i] = c
		} else {
			s[i] = ' '
		}
	}
	return strings.TrimSpace(string(s))
}

// ReadSave reads the whole save; progress(done, total) is called as it goes.
func (h *Helper) ReadSave(stype int, progress func(done, total int)) ([]byte, error) {
	size := saveTypes[stype].Size
	if h.Streams() { // Cart Doctor's streamed transfer: 64 KB a command
		progress(0, size)
		out := make([]byte, 0, size)
		steps := (size + maxStreamSave - 1) / maxStreamSave
		for k := 0; k < steps; k++ {
			off := k * maxStreamSave
			b, err := h.streamReadSave(stype, off, min(maxStreamSave, size-off), k, steps, func(d int) { progress(off+d, size) })
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
		}
		return out, nil
	}
	out := make([]byte, 0, size)
	steps := (size + saveReadChunk - 1) / saveReadChunk
	progress(0, size)
	for k := 0; k < steps; k++ {
		off := k * saveReadChunk
		n := min(saveReadChunk, size-off)
		ok := false
		for try := 0; try < 4 && !ok; try++ {
			// args 3-4 (step k of steps) drive the progress bar on the GBA's screen
			st, d, err := h.Command(cmdSaveRead, []uint32{uint32(stype), uint32(off), uint32(n), uint32(k), uint32(steps)},
				nil, 20*time.Second, []int{0, 2, n/4 + 1}, 8*time.Second)
			if err != nil {
				if try < 3 && h.recover() {
					continue
				}
				return nil, err
			}
			if st != stOK {
				return nil, linkErr("Reading the save failed: %s", stNames[st])
			}
			if b, good := chunkWords(d, n); good {
				out = append(out, b...)
				ok = true
			}
		}
		if !ok {
			return nil, linkErr("The save kept arriving corrupted - check the cable")
		}
		progress(off+n, size)
	}
	return out, nil
}

// WriteSave writes the save (only the 4 KB blocks that differ from old, if old is known) and
// verifies it. Returns the number of blocks written.
func (h *Helper) WriteSave(stype int, data, old []byte, progress func(done, total int)) (int, error) {
	size := saveTypes[stype].Size
	if h.Streams() { // Cart Doctor's streamed transfer: the changed blocks, joined, up to 64 KB a command
		ranges := saveRanges(data, old, size)
		total := 0
		for _, r := range ranges {
			total += r[1] - r[0]
		}
		if total > 0 {
			progress(0, total)
		}
		done, blocks := 0, 0
		for k, r := range ranges {
			if err := h.streamWriteSave(stype, r[0], data[r[0]:r[1]], k, len(ranges), func(d int) { progress(done+d, total) }); err != nil {
				return blocks, err
			}
			done += r[1] - r[0]
			blocks += (r[1] - r[0] + saveChunk - 1) / saveChunk
			if h.between != nil && k+1 < len(ranges) {
				h.between()
			}
		}
		return blocks, nil
	}
	var chunks []int
	for off := 0; off < size; off += saveChunk {
		e := min(off+saveChunk, size)
		if old == nil || !bytes.Equal(data[off:e], old[off:e]) {
			chunks = append(chunks, off)
		}
	}
	if len(chunks) > 0 {
		progress(0, len(chunks))
	}
	for k, off := range chunks {
		n := min(saveChunk, size-off)
		words := make([]uint32, n/4)
		for i := range words {
			words[i] = binary.LittleEndian.Uint32(data[off+4*i:])
		}
		st, d, err := h.Command(cmdSaveWrite, []uint32{uint32(stype), uint32(off), uint32(n), uint32(k), uint32(len(chunks))},
			words, 60*time.Second, []int{0, 3}, 8*time.Second)
		for try := 0; err != nil && try < 2 && h.recover(); try++ { // writing a block again is harmless
			st, d, err = h.Command(cmdSaveWrite, []uint32{uint32(stype), uint32(off), uint32(n), uint32(k), uint32(len(chunks))},
				words, 60*time.Second, []int{0, 3}, 8*time.Second)
		}
		if err != nil {
			return k, err
		}
		if st == stVerifyFail {
			return k, linkErr("The cart's save didn't verify at 0x%05X - check the cart's contacts", d[0])
		}
		if st != stOK {
			return k, linkErr("Writing the save failed: %s", stNames[st])
		}
		progress(k+1, len(chunks))
		if h.between != nil && k+1 < len(chunks) {
			h.between()
		}
	}
	return len(chunks), nil
}

// ------------------------------------------------------------------------------- ROM dumps
type ROMInfo struct {
	Size    int    // bytes
	Guessed bool   // repro cart with other data after the game: Size is a best guess
	Repro   bool   // the cart didn't show where the game ends: a repro cart's flash chip
	Header  []byte // the first 0xC0 bytes
	Title   string
	Code    string
}

// ROMInfo checks the cartridge in the GBA: its ROM size and header.
func (h *Helper) ROMInfo() (ROMInfo, error) {
	st, d, err := h.Command(cmdROMInfo, nil, nil, 30*time.Second, []int{1 + 0xC0/4}, 8*time.Second)
	if err != nil {
		return ROMInfo{}, err
	}
	hdr := make([]byte, 0xC0)
	for i := 0; i < 0xC0/4; i++ {
		binary.LittleEndian.PutUint32(hdr[4*i:], d[1+i])
	}
	if st != stOK || d[0] == 0xFFFFFFFF {
		return ROMInfo{}, linkErr("No game cartridge was found in the GBA. Push it in firmly, then try again.")
	}
	return ROMInfo{Size: int(d[0] &^ 0xC0000000), Guessed: d[0]&0x80000000 != 0, Repro: d[0]&0x40000000 != 0, Header: hdr,
		Title: printable(hdr[0xA0:0xAC]), Code: printable(hdr[0xAC:0xB0])}, nil
}

// ReadROM reads bytes [off, off+n) of the cart ROM (n <= romChunk, a multiple of 4). step/steps
// drive the progress bar on the GBA's screen.
func (h *Helper) ReadROM(off, n, step, steps int) ([]byte, error) {
	var last error
	for try := 0; try < 4; try++ {
		st, d, err := h.Command(cmdROMRead, []uint32{uint32(off), uint32(n), uint32(step), uint32(steps)},
			nil, 20*time.Second, []int{2, n/4 + 1}, 8*time.Second)
		if err != nil {
			last = err
			if !h.recover() {
				break
			}
			continue
		}
		if st != stOK {
			return nil, linkErr("Reading the cartridge failed: %s", stNames[st])
		}
		out, good := chunkWords(d, n)
		if !good {
			last = errors.New("data corrupted")
			continue
		}
		return out, nil
	}
	return nil, linkErr("Reading the cartridge kept failing (%v). Check the link cable and try again.", last)
}
