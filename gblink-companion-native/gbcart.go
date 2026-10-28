// Game Boy and Game Boy Color cartridges over GB-Link, the way GB-Link Cart Doctor reads them
// (github.com/GB-Link/gb-cart-doctor-web, GPL-3.0; ported from web/gb_cart.js,
// web/dump_receiver_fast.js and web/restore_sender_fast.js).
//
// A program sent to the GBA by multiboot switches it into its built-in Game Boy Color mode. The
// GBC program then shows a little menu and listens to the link cable, where the GB-Link drives the
// clock and every byte sent swaps with one from the GBC. Two ways of working with the cartridge:
//   - the "cart agent", uploaded into the GBC's RAM, runs short programs of cartridge reads and
//     writes (the header, flash carts: gbflash.go);
//   - the GBC program's own fast transfers for ROM dumps, save dumps and save restores: 256-byte
//     sections, each with an XOR check that's answered OK (or "again").
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"time"
)

var (
	//go:embed assets/cartdoctor/gba-switch-to-gbc_mb.gba
	gbcSwitchImage []byte
	//go:embed assets/cartdoctor/gbc-agent.bin
	gbcAgent []byte
)

const (
	cmdVoltage5V = 0x41
	gbByteGapUS  = 50 // a pause after every byte, so the GBC has the next one ready

	gbCmdDumpROM   = 0xC1 // same as pressing A, B or SELECT in the GBC menu
	gbCmdDumpSave  = 0xC2
	gbCmdRestore   = 0xC3
	gbCmdEnter     = 0xC5
	gbLoaderTag    = 0x5C
	gbLoaderGo     = 0xA5
	gbTagReady     = 0xA7
	gbTagDone      = 0xD7
	gbGo1          = 0x5A
	gbGo2          = 0x96
	gbAckOK        = 0x01
	gbNak          = 0x00
	gbAgentMax     = 0xE00
	gbProgMax      = 0xEFF
	gbResultMax    = 0xB00 - 5
	gbFrameMax     = 0xB00 - 2
	gbAgentVersion = 1
	gbChunk        = 56 // bytes per USB write

	gbopEnd, gbopWR, gbopRD, gbopWait, gbopDelay, gbopTmpl, gbopProg, gbopCRC, gbopBlank, gbopWrBlk, gbopInfo, gbopExit = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11
	gbStOK, gbStTimeout, gbStBad                                                                                        = 0, 1, 2
	gbNone                                                                                                              = 0xFFFF

	fastMagic   = 0xF5
	fastROM     = 1
	fastSRAM    = 2
	fastRestore = 3
	fastOK      = 0x01
	fastFail    = 0x00
	fastAbort   = 0xCA
)

var (
	fastROMBanks      = []int{2, 4, 8, 16, 32, 64, 128, 256, 512, 0, 0, 0, 0, 0, 0, 0, 0, 0, 72, 80, 96}
	fastSRAMBankSizes = []int{0, 8, 0x20, 0x20, 0x20, 0x20, 2}
	fastSRAMBanks     = []int{0, 1, 1, 4, 16, 8, 1}
)

// GBError is a problem with a GB cartridge job; Code is no-answer, link, rejected, old-agent or cancelled.
type GBError struct{ Code, Msg string }

func (e *GBError) Error() string { return e.Msg }

var errGBCancelled = &GBError{"cancelled", "Cancelled"}

// SetVoltage5V: in Game Boy Color mode the GBA's link port works at 5 V.
func (l *GBLink) SetVoltage5V() error { return l.writeFrame(chCommand, []byte{cmdVoltage5V}) }

// ConfigureGB sets the link up for the GBC program: GB link mode at 5 V, a byte at a time.
func (l *GBLink) ConfigureGB() {
	l.SetMode(modeGBLink)
	time.Sleep(100 * time.Millisecond)
	l.SetVoltage5V()
	time.Sleep(100 * time.Millisecond)
	l.SetTiming(gbByteGapUS, 1)
	time.Sleep(50 * time.Millisecond)
	l.Drain()
}

// ConfigureGBA sets the link back up for a program in GBA mode (3.3 V, 32-bit words).
func (l *GBLink) ConfigureGBA() {
	l.SetMode(modeGBLink)
	time.Sleep(100 * time.Millisecond)
	l.SetVoltage3V3()
	time.Sleep(100 * time.Millisecond)
	l.SetTiming(36, 4)
	time.Sleep(50 * time.Millisecond)
	l.Drain()
}

// ------------------------------------------------------------------------------- programs
// gbProgram is one program for the cart agent: steps it runs in order.
type gbProgram struct {
	bytes       []byte
	ops         int
	resultBytes int
}

// gbHandle finds a step's output in the result.
type gbHandle struct{ index, at, length int }

func (p *gbProgram) op(code byte, args []byte, results int) gbHandle {
	h := gbHandle{p.ops, p.resultBytes, results}
	p.bytes = append(append(p.bytes, code), args...)
	p.ops++
	p.resultBytes += results
	return h
}

func le16(v int) []byte { return []byte{byte(v), byte(v >> 8)} }

func (p *gbProgram) room() int       { return gbProgMax - 1 - len(p.bytes) }
func (p *gbProgram) resultRoom() int { return gbResultMax - p.resultBytes }
func (p *gbProgram) wr(addr, v int) gbHandle {
	return p.op(gbopWR, append(le16(addr), byte(v)), 0)
}
func (p *gbProgram) rd(addr, n int) gbHandle {
	return p.op(gbopRD, append(le16(addr), le16(n)...), n)
}

// wait keeps reading addr until (byte & mask) == value, giving up after ms.
func (p *gbProgram) wait(addr, mask, value, ms int) gbHandle {
	t := max(1, min(0xFFFF, (ms*64+999)/1000)) // in 15.625 ms ticks
	args := append(le16(addr), byte(mask), byte(value))
	return p.op(gbopWait, append(args, le16(t)...), 1)
}
func (p *gbProgram) delay(ms int) gbHandle { return p.op(gbopDelay, le16(ms), 0) }

// gbTmplStep: one write of the sequence a flash chip needs before each byte is programmed.
type gbTmplStep struct {
	kind  int // bit 0: write the byte being programmed; bit 1: at its own address
	addr  int
	value int
}

func (p *gbProgram) tmpl(steps []gbTmplStep) gbHandle {
	args := []byte{byte(len(steps))}
	for _, s := range steps {
		args = append(args, byte(s.kind), byte(s.value))
		args = append(args, le16(s.addr)...)
	}
	return p.op(gbopTmpl, args, 0)
}
func (p *gbProgram) prog(addr int, data []byte) gbHandle {
	args := append(le16(addr), le16(len(data))...)
	return p.op(gbopProg, append(args, data...), 4)
}
func (p *gbProgram) crc(addr, n int) gbHandle {
	return p.op(gbopCRC, append(le16(addr), le16(n)...), 4)
}
func (p *gbProgram) blank(addr, n int) gbHandle {
	return p.op(gbopBlank, append(le16(addr), le16(n)...), 2)
}
func (p *gbProgram) wrblk(addr int, data []byte) gbHandle {
	args := append(le16(addr), le16(len(data))...)
	return p.op(gbopWrBlk, append(args, data...), 0)
}
func (p *gbProgram) info() gbHandle { return p.op(gbopInfo, nil, 2) }
func (p *gbProgram) exit() gbHandle { return p.op(gbopExit, nil, 0) }

func (p *gbProgram) build() ([]byte, error) {
	if len(p.bytes) == 0 {
		return nil, errors.New("empty GB program")
	}
	if len(p.bytes) > gbProgMax-1 {
		return nil, errors.New("GB program too large")
	}
	if p.resultBytes > gbResultMax {
		return nil, errors.New("GB program reads too much")
	}
	return p.bytes, nil
}

type gbResult struct {
	status, executed int
	data             []byte
}

func (r *gbResult) ok() bool                { return r.status == gbStOK }
func (r *gbResult) bytes(h gbHandle) []byte { return r.data[h.at : h.at+h.length] }
func (r *gbResult) byte(h gbHandle, k int) int {
	return int(r.data[h.at+k])
}
func (r *gbResult) word(h gbHandle, k int) int {
	return int(r.data[h.at+k]) | int(r.data[h.at+k+1])<<8
}
func (r *gbResult) u32(h gbHandle) uint32 {
	d := r.data[h.at:]
	return uint32(d[0]) | uint32(d[1])<<8 | uint32(d[2])<<16 | uint32(d[3])<<24
}

func fletcher(b []byte, s [2]byte) [2]byte {
	a, c := s[0], s[1]
	for _, x := range b {
		a += x
		c += a
	}
	return [2]byte{a, c}
}

// ------------------------------------------------------------------------------ the client
type gbClient struct {
	l       *GBLink
	stop    func() bool // Cancel
	retries int
}

func (c *gbClient) checkCancel() error {
	if c.stop != nil && c.stop() {
		return errGBCancelled
	}
	return nil
}

// xchg sends bytes over the link and returns the bytes the GBC sent back.
func (c *gbClient) xchg(tx []byte) ([]byte, error) {
	rx := make([]byte, 0, len(tx))
	for off := 0; off < len(tx); off += gbChunk {
		part := tx[off:min(len(tx), off+gbChunk)]
		r, err := c.l.Exchange(part, 5*time.Second)
		if err != nil {
			return nil, &GBError{"link", "The GB-Link stopped answering (" + err.Error() + ")."}
		}
		rx = append(rx, r...)
	}
	return rx, nil
}

func (c *gbClient) sync(tags []byte, timeout time.Duration) (byte, error) {
	end := time.Now().Add(timeout)
	for {
		if err := c.checkCancel(); err != nil {
			return 0, err
		}
		r, err := c.xchg([]byte{0x00, gbGo1, gbGo2})
		if err != nil {
			return 0, err
		}
		if r[1] == r[2] && strings.IndexByte(string(tags), r[2]) >= 0 {
			return r[2], nil
		}
		if r[2] == gbLoaderTag && strings.IndexByte(string(tags), gbLoaderTag) < 0 {
			return gbLoaderTag, nil // the agent has exited and the loader is back
		}
		if time.Now().After(end) {
			return 0, &GBError{"no-answer", "The Game Boy Color stopped answering."}
		}
	}
}

func (c *gbClient) readFrame() (*gbResult, error) {
	head, err := c.xchg([]byte{0, 0})
	if err != nil {
		return nil, err
	}
	n := int(head[0]) | int(head[1])<<8
	if n < 3 || n > gbFrameMax {
		return nil, nil
	}
	body, err := c.xchg(make([]byte, n+2))
	if err != nil {
		return nil, err
	}
	sum := fletcher(body[:n], fletcher(head, [2]byte{}))
	if sum[0] != body[n] || sum[1] != body[n+1] {
		return nil, nil
	}
	return &gbResult{status: int(body[0]), executed: int(body[1]) | int(body[2])<<8, data: append([]byte(nil), body[3:n]...)}, nil
}

func (c *gbClient) drain() (*gbResult, error) {
	for i := 0; i < 4; i++ {
		f, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		ack := byte(gbNak)
		if f != nil {
			ack = gbAckOK
		}
		if _, err := c.xchg([]byte{ack}); err != nil {
			return nil, err
		}
		if f != nil {
			return f, nil
		}
		if _, err := c.sync([]byte{gbTagDone}, 5*time.Second); err != nil {
			return nil, err
		}
	}
	return nil, &GBError{"link", "The link to the Game Boy Color keeps failing. Check the cable."}
}

func (c *gbClient) sendBlock(buf []byte) error {
	n := len(buf)
	if _, err := c.xchg([]byte{byte(n), byte(n >> 8), byte(^n), byte(^n >> 8)}); err != nil {
		return err
	}
	sum := fletcher(buf, [2]byte{})
	_, err := c.xchg(append(append([]byte(nil), buf...), sum[0], sum[1]))
	return err
}

// run runs a program on the agent and returns its result, sending it (or reading the result)
// again if it's damaged on the way.
func (c *gbClient) run(p *gbProgram, timeout time.Duration) (*gbResult, error) {
	buf, err := p.build()
	if err != nil {
		return nil, err
	}
	tag, err := c.sync([]byte{gbTagReady, gbTagDone}, 5*time.Second)
	if err != nil {
		return nil, err
	}
	failures := 0
	for {
		if err := c.checkCancel(); err != nil {
			return nil, err
		}
		if tag == gbLoaderTag {
			return nil, &GBError{"no-answer", "The Game Boy Color left the app's control."}
		}
		if tag == gbTagDone { // an earlier result is still waiting: our "got it" went missing
			if _, err := c.drain(); err != nil {
				return nil, err
			}
			if tag, err = c.sync([]byte{gbTagReady, gbTagDone}, 5*time.Second); err != nil {
				return nil, err
			}
			continue
		}
		if err := c.sendBlock(buf); err != nil {
			return nil, err
		}
		if tag, err = c.sync([]byte{gbTagDone, gbTagReady}, timeout); err != nil {
			return nil, err
		}
		if tag == gbTagReady {
			if failures++; failures > 4 {
				return nil, &GBError{"link", "The Game Boy Color keeps rejecting programs. Check the cable."}
			}
			c.retries++
			continue
		}
		for i := 0; ; i++ {
			f, err := c.readFrame()
			if err != nil {
				return nil, err
			}
			if f != nil {
				if _, err := c.xchg([]byte{gbAckOK}); err != nil {
					return nil, err
				}
				if f.status == gbStBad {
					return nil, &GBError{"rejected", fmt.Sprintf("The Game Boy Color refused step %d of the program.", f.executed+1)}
				}
				return f, nil
			}
			if i >= 4 {
				return nil, &GBError{"link", "Results from the Game Boy Color keep arriving damaged. Check the cable."}
			}
			c.retries++
			if _, err := c.xchg([]byte{gbNak}); err != nil {
				return nil, err
			}
			if _, err := c.sync([]byte{gbTagDone}, 5*time.Second); err != nil {
				return nil, err
			}
		}
	}
}

func infoProgram() (*gbProgram, gbHandle) {
	p := &gbProgram{}
	return p, p.info()
}

// connect takes control of the GBC program: wakes its loader and uploads the agent, or uses an
// agent that's still running.
func (c *gbClient) connect(timeout time.Duration) error {
	if len(gbcAgent) > gbAgentMax {
		return errors.New("cart agent too large")
	}
	end := time.Now().Add(timeout)
	uploads := 0
	enter := make([]byte, 8)
	for i := range enter {
		enter[i] = gbCmdEnter
	}
	for {
		if err := c.checkCancel(); err != nil {
			return err
		}
		r, err := c.xchg(enter)
		if err != nil {
			return err
		}
		last := r[7]
		switch {
		case last == gbTagReady && r[6] == gbTagReady:
			p, h := infoProgram()
			res, err := c.run(p, 30*time.Second)
			if err != nil {
				return err
			}
			if res.byte(h, 0) == gbAgentVersion {
				return nil
			}
			q := &gbProgram{}
			q.exit()
			if _, err := c.run(q, 30*time.Second); err != nil {
				return err
			}
			continue
		case last == gbTagDone && r[6] == gbTagDone:
			if _, err := c.sync([]byte{gbTagDone}, 2*time.Second); err != nil {
				return err
			}
			if _, err := c.drain(); err != nil {
				return err
			}
			continue
		case last == gbLoaderTag && r[6] == gbLoaderTag:
			if uploads++; uploads > 3 {
				return &GBError{"link", "Sending to the Game Boy Color keeps failing. Make sure the cartridge is in, and check the cable."}
			}
			if err := c.upload(); err != nil {
				return err
			}
			tag, err := c.sync([]byte{gbTagReady, gbLoaderTag}, 3*time.Second)
			if err == nil && tag == gbTagReady {
				p, _ := infoProgram()
				buf, _ := p.build()
				if err := c.sendBlock(buf); err != nil {
					return err
				}
				if _, err := c.sync([]byte{gbTagDone}, 3*time.Second); err != nil {
					return err
				}
				f, err := c.drain()
				if err != nil {
					return err
				}
				if len(f.data) == 0 || f.data[0] != gbAgentVersion {
					return &GBError{"old-agent", "The cart agent didn't start."}
				}
				return nil
			}
			continue
		}
		if time.Now().After(end) {
			return &GBError{"no-answer", "The GBA isn't answering in Game Boy Color mode. Switch it off, then send the program again."}
		}
	}
}

func (c *gbClient) upload() error {
	r, err := c.xchg([]byte{gbLoaderGo})
	if err != nil || r[0] != gbLoaderTag {
		return err
	}
	return c.sendBlock(gbcAgent)
}

// disconnect hands the GBC back to its button menu.
func (c *gbClient) disconnect() error {
	p := &gbProgram{}
	p.exit()
	_, err := c.run(p, 30*time.Second)
	return err
}

// withAgent uploads the agent, runs f with it, and gives the GBC its menu back.
func (c *gbClient) withAgent(f func() error) error {
	if err := c.connect(5 * time.Second); err != nil {
		return err
	}
	err := f()
	stop := c.stop
	c.stop = nil
	if derr := c.disconnect(); err == nil && derr != nil && derr != errGBCancelled {
		err = derr
	}
	c.stop = stop
	return err
}

// ------------------------------------------------------------------------------ cartridges
// GBHeader is a GB or GBC cartridge's header ($0100-$014F).
type GBHeader struct {
	Raw               []byte
	Title             string
	CGB               string // "no", "yes" or "only"
	Type              int
	TypeName          string
	ROMBanks, ROMSize int
	RAMSize           int
	OK                bool // the header checksum is right: a cartridge is there
	Rumble, Battery   bool
	Timer             bool
	HeaderSum, Global int
	Licensee, Version int
	OldLicensee       int
	CartCode          string // the 4-letter code newer GBC games have ($013F)
}

var gbROMBanks = map[int]int{0x52: 72, 0x53: 80, 0x54: 96}
var gbRAMSizes = []int{0, 0x800, 0x2000, 0x8000, 0x20000, 0x10000}

var gbCartTypes = map[int]string{
	0x00: "ROM only", 0x01: "MBC1", 0x02: "MBC1+RAM", 0x03: "MBC1+RAM+BATTERY",
	0x05: "MBC2", 0x06: "MBC2+BATTERY", 0x08: "ROM+RAM", 0x09: "ROM+RAM+BATTERY",
	0x0B: "MMM01", 0x0C: "MMM01+RAM", 0x0D: "MMM01+RAM+BATTERY",
	0x0F: "MBC3+TIMER+BATTERY", 0x10: "MBC3+TIMER+RAM+BATTERY", 0x11: "MBC3",
	0x12: "MBC3+RAM", 0x13: "MBC3+RAM+BATTERY",
	0x19: "MBC5", 0x1A: "MBC5+RAM", 0x1B: "MBC5+RAM+BATTERY", 0x1C: "MBC5+RUMBLE",
	0x1D: "MBC5+RUMBLE+RAM", 0x1E: "MBC5+RUMBLE+RAM+BATTERY",
	0x20: "MBC6", 0x22: "MBC7", 0xFC: "Pocket Camera", 0xFD: "TAMA5", 0xFE: "HuC3", 0xFF: "HuC1",
}

// parseGBHeader decodes the 0x50 bytes from $0100 (Cart Doctor's parseGbHeader).
func parseGBHeader(h []byte) GBHeader {
	sum := 0
	for i := 0x34; i <= 0x4C; i++ {
		sum = (sum - int(h[i]) - 1) & 0xFF
	}
	cgb := h[0x43]&0x80 != 0
	end := 0x44
	if cgb {
		end = 0x43
	}
	var t []byte
	for _, b := range h[0x34:end] {
		if b == 0 {
			break
		}
		if b >= 0x20 && b < 0x7F {
			t = append(t, b)
		}
	}
	typ := int(h[0x47])
	romCode := int(h[0x48])
	banks := gbROMBanks[romCode]
	if romCode <= 8 {
		banks = 2 << romCode
	}
	ram := 0
	if int(h[0x49]) < len(gbRAMSizes) {
		ram = gbRAMSizes[h[0x49]]
	}
	if typ == 0x05 || typ == 0x06 {
		ram = 0x200
	}
	hasRAM := false
	for _, x := range []int{0x02, 0x03, 0x05, 0x06, 0x08, 0x09, 0x0C, 0x0D, 0x10, 0x12, 0x13, 0x1A, 0x1B, 0x1D, 0x1E, 0xFC, 0xFE, 0xFF} {
		hasRAM = hasRAM || x == typ
	}
	if !hasRAM {
		ram = 0
	}
	name := gbCartTypes[typ]
	if name == "" {
		name = fmt.Sprintf("unknown (0x%02X)", typ)
	}
	g := GBHeader{Raw: append([]byte(nil), h...), Title: strings.TrimSpace(string(t)), CGB: "no", Type: typ, TypeName: name,
		ROMBanks: banks, ROMSize: banks * 0x4000, RAMSize: ram, OK: sum == int(h[0x4D]),
		Rumble: typ >= 0x1C && typ <= 0x1E, Battery: strings.Contains(name, "BATTERY"), Timer: strings.Contains(name, "TIMER"),
		HeaderSum: int(h[0x4D]), Global: int(h[0x4E])<<8 | int(h[0x4F]), Version: int(h[0x4C]), OldLicensee: int(h[0x4B])}
	if cgb {
		g.CGB = "yes"
		if h[0x43] == 0xC0 {
			g.CGB = "only"
		}
		if code := string(h[0x3F:0x43]); strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") == "" && len(strings.TrimSpace(code)) == 4 {
			g.CartCode = code
		}
	}
	return g
}

// gbHeaderOfROM reads the header out of a .gb/.gbc file's bytes.
func gbHeaderOfROM(rom []byte) (GBHeader, bool) {
	if len(rom) < 0x150 {
		return GBHeader{}, false
	}
	return parseGBHeader(rom[0x100:0x150]), true
}

// readHeader reads the cartridge's header through the agent.
func (c *gbClient) readHeader() (GBHeader, error) {
	var h GBHeader
	err := c.withAgent(func() error {
		p := &gbProgram{}
		p.wr(0x0000, 0x00)
		p.wr(0x6000, 0x00)
		hd := p.rd(0x0100, 0x50)
		r, err := c.run(p, 30*time.Second)
		if err != nil {
			return err
		}
		h = parseGBHeader(r.bytes(hd))
		return nil
	})
	return h, err
}

// ------------------------------------------------------------------------- fast transfers
func (c *gbClient) byteX(b byte) (byte, error) {
	r, err := c.l.Exchange([]byte{b}, 5*time.Second)
	if err != nil {
		return 0, &GBError{"link", "The GB-Link stopped answering (" + err.Error() + ")."}
	}
	return r[0], nil
}

// fastHeader starts one of the GBC program's transfers (trigger: gbCmdDumpROM and so on) and
// returns its header: the type and the size code. ok false: it didn't start in time.
func (c *gbClient) fastHeader(trigger byte, want int, timeout time.Duration) (sizeIdx int, err error) {
	start := time.Now()
	for {
		if err := c.checkCancel(); err != nil {
			return 0, err
		}
		if time.Since(start) > timeout {
			return 0, &GBError{"no-answer", "The Game Boy Color didn't start the transfer. Switch the GBA off, then start again."}
		}
		rx, err := c.byteX(trigger)
		if err != nil {
			return 0, err
		}
		if rx != fastMagic {
			continue
		}
		var hb [3]byte
		for i := range hb {
			if hb[i], err = c.byteX(0); err != nil {
				return 0, err
			}
		}
		if hb[2] != fastMagic^hb[0]^hb[1] {
			c.byteX(fastFail)
			continue
		}
		if int(hb[0]) != want {
			c.byteX(fastFail) // a different transfer started: refuse it, so nothing is written
			if want == fastRestore {
				return 0, &GBError{"link", "The Game Boy Color started a dump instead of writing the save."}
			}
			continue
		}
		return int(hb[1]), nil
	}
}

func (c *gbClient) abortFast() {
	c.l.Exchange([]byte{fastAbort, fastAbort, fastAbort, fastAbort, fastAbort}, 5*time.Second)
}

// readSection reads 256 bytes and their check; nil if it arrived damaged (it's sent again).
func (c *gbClient) readSection() ([]byte, error) {
	data, err := c.xchg(make([]byte, 256))
	if err != nil {
		return nil, err
	}
	var x byte
	for _, b := range data {
		x ^= b
	}
	ck, err := c.byteX(0)
	if err != nil {
		return nil, err
	}
	if err := c.checkCancel(); err != nil {
		c.byteX(fastAbort)
		return nil, err
	}
	if x == ck {
		_, err = c.byteX(fastOK)
		return data, err
	}
	_, err = c.byteX(fastFail)
	return nil, err
}

// dump runs a ROM (gbCmdDumpROM) or save (gbCmdDumpSave) dump; progress(done, total) in bytes.
func (c *gbClient) dump(save bool, progress func(done, total int)) ([]byte, error) {
	trigger, want := byte(gbCmdDumpROM), fastROM
	if save {
		trigger, want = gbCmdDumpSave, fastSRAM
	}
	idx, err := c.fastHeader(trigger, want, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var per, banks int
	if save {
		if idx < len(fastSRAMBanks) {
			per, banks = fastSRAMBankSizes[idx], fastSRAMBanks[idx]
		}
	} else if idx < len(fastROMBanks) {
		per, banks = 0x40, fastROMBanks[idx]
	}
	if per == 0 || banks == 0 {
		c.byteX(fastFail)
		return nil, &GBError{"rejected", "There's nothing to copy: the cartridge says it has no save memory."}
	}
	if _, err := c.byteX(fastOK); err != nil {
		return nil, err
	}
	total := per * banks * 256
	out := make([]byte, 0, total)
	if progress != nil {
		progress(0, total)
	}
	for len(out) < total {
		var sec []byte
		for tries := 0; sec == nil; tries++ {
			if sec, err = c.readSection(); err != nil {
				return nil, err
			}
			if sec == nil && tries > 30 {
				c.abortFast()
				return nil, &GBError{"link", "The data kept arriving damaged. Check the link cable."}
			}
		}
		out = append(out, sec...)
		if progress != nil {
			progress(len(out), total)
		}
	}
	return out, nil
}

// restore writes save (exactly the cartridge's save size) into the cartridge.
func (c *gbClient) restore(save []byte, progress func(done, total int)) error {
	idx, err := c.fastHeader(gbCmdRestore, fastRestore, 5*time.Second)
	if err != nil {
		return err
	}
	want := 0
	if idx < len(fastSRAMBanks) {
		want = fastSRAMBankSizes[idx] * fastSRAMBanks[idx] * 256
	}
	if want == 0 {
		c.byteX(fastFail)
		return &GBError{"rejected", "This cartridge has no save memory."}
	}
	if len(save) != want {
		c.byteX(fastFail)
		return &GBError{"rejected", fmt.Sprintf("The save is %d bytes, but this cartridge's save memory is %d bytes. Nothing was written.", len(save), want)}
	}
	if _, err := c.byteX(fastOK); err != nil {
		return err
	}
	if progress != nil {
		progress(0, len(save))
	}
	for off := 0; off < len(save); off += 256 {
		sec := save[off : off+256]
		var x byte
		for _, b := range sec {
			x ^= b
		}
		ok := false
		for try := 0; try < 8 && !ok; try++ {
			if err := c.checkCancel(); err != nil {
				c.xchg(make([]byte, 256))
				c.byteX(fastAbort)
				return err
			}
			if _, err := c.xchg(sec); err != nil {
				return err
			}
			if _, err := c.byteX(x); err != nil {
				return err
			}
			ack, err := c.byteX(0)
			if err != nil {
				return err
			}
			ok = ack == fastOK
		}
		if !ok {
			return &GBError{"link", fmt.Sprintf("Writing the save kept failing at 0x%X. The link lost step: start again.", off)}
		}
		if progress != nil {
			progress(off+256, len(save))
		}
	}
	return nil
}

func gbCRC32(b []byte) uint32 { return crc32.ChecksumIEEE(b) }

// gbSaveSizeOf: the save size a header says, as the GBC program sends it.
func gbSaveSizeOf(h GBHeader) int { return h.RAMSize }

// gbTrimROM: the GBC program sends the ROM's whole size from the header.
func gbROMName(h GBHeader) string {
	ext := ".gb"
	if h.CGB != "no" {
		ext = ".gbc"
	}
	t := h.Title
	if t == "" {
		t = "Game Boy game"
	}
	return fileSafe(t) + ext
}

// readSRAMRaw reads 32 KB of cartridge RAM through the agent, banking it the way MBC1, MBC3 and
// MBC5 do (RAM on, then banks 0-3 at $A000). For carts whose header isn't the game's (flash
// carts that show their own menu): the fast transfers go by the header, this doesn't.
func (c *gbClient) readSRAMRaw(progress func(done, total int)) ([]byte, error) {
	out := make([]byte, 0, 0x8000)
	err := c.withAgent(func() error {
		for bank := 0; bank < 4; bank++ {
			for off := 0; off < 0x2000; off += 0x800 {
				p := &gbProgram{}
				p.wr(0x0000, 0x0A)
				p.wr(0x6000, 0x01) // MBC1: banked RAM
				p.wr(0x4000, bank)
				h := p.rd(0xA000+off, 0x800)
				p.wr(0x0000, 0x00)
				r, err := c.run(p, 30*time.Second)
				if err != nil {
					return err
				}
				out = append(out, r.bytes(h)...)
				if progress != nil {
					progress(len(out), 0x8000)
				}
			}
		}
		return nil
	})
	return out, err
}

// writeSRAMRaw writes 32 KB of cartridge RAM the same way, then reads it back to check it.
func (c *gbClient) writeSRAMRaw(data []byte, progress func(done, total int)) error {
	if len(data) != 0x8000 {
		return fmt.Errorf("the save must be 32 KB")
	}
	err := c.withAgent(func() error {
		done := 0
		for bank := 0; bank < 4; bank++ {
			for off := 0; off < 0x2000; off += 0x800 {
				p := &gbProgram{}
				p.wr(0x0000, 0x0A)
				p.wr(0x6000, 0x01)
				p.wr(0x4000, bank)
				p.wrblk(0xA000+off, data[bank*0x2000+off:bank*0x2000+off+0x800])
				p.wr(0x0000, 0x00)
				if _, err := c.run(p, 30*time.Second); err != nil {
					return err
				}
				done += 0x800
				if progress != nil {
					progress(done, 0x8000)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	back, err := c.readSRAMRaw(nil)
	if err != nil {
		return err
	}
	if !bytesEqual(back, data) {
		return linkErr("The cartridge's save didn't match what was written.")
	}
	return nil
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }
