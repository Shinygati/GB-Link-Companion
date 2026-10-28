// Flash cart profiles: which commands each kind of GB/GBC or GBA flash cart needs to be read,
// erased and written. Taken from FlashGBX 5.1 by Lesserkuma (github.com/Lesserkuma/FlashGBX,
// GPL-3.0) by GB-Link Cart Doctor's tools/import_flashgbx.py, kept here as JSON
// (assets/flashcarts/). Commands are lists of [address, value]; an address can also be "SA" (the
// sector), "SA+n", or "PA" (the byte being written), and a value "PD" (that byte) or "BS" (the
// write buffer's size).
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var (
	//go:embed assets/flashcarts/gba.json
	gbaProfilesJSON []byte
	//go:embed assets/flashcarts/gb.json
	gbProfilesJSON []byte
)

// FlashProfile is one kind of flash cart, as FlashGBX describes it.
type FlashProfile struct {
	ID                     string             `json:"id"`
	Names                  []string           `json:"names"`
	FlashIDs               [][]int            `json:"flash_ids"`
	FlashSize              int                `json:"flash_size"`
	SectorSize             json.RawMessage    `json:"sector_size"` // a number, or [[size, count], ...]
	SectorSizeFromCFI      bool               `json:"sector_size_from_cfi"`
	ResetEvery             int                `json:"reset_every"`
	CommandSet             string             `json:"command_set"`
	Commands               map[string][][]any `json:"commands"`
	ChipEraseTimeout       int                `json:"chip_erase_timeout"`
	FlashBankSize          int                `json:"flash_bank_size"`
	FlashBankSelectType    int                `json:"flash_bank_select_type"`
	BufferSize             int                `json:"buffer_size"`
	WaitReadStatusRegister bool               `json:"wait_read_status_register"`
	M29W640                bool               `json:"m29w640"`
	StatusRegisterMask     *int               `json:"status_register_mask"`
	StatusRegisterValue    *int               `json:"status_register_value"`
	DoubleDie              bool               `json:"double_die"`
	StartAddr              *int               `json:"start_addr"`
	MBC                    any                `json:"mbc"` // a number, or "manual"
	ManualSelect           bool               `json:"manual_select"`
	Synthetic              bool               `json:"-"`
}

func (p *FlashProfile) Name() string {
	if len(p.Names) == 0 {
		return p.ID
	}
	return p.Names[0]
}

func (p *FlashProfile) cmd(name string) [][]any { return p.Commands[name] }

var profilesOnce sync.Once
var gbaProfiles, gbProfiles []*FlashProfile

func loadProfiles() {
	profilesOnce.Do(func() {
		if err := json.Unmarshal(gbaProfilesJSON, &gbaProfiles); err != nil {
			panic("flash cart profiles: " + err.Error())
		}
		if err := json.Unmarshal(gbProfilesJSON, &gbProfiles); err != nil {
			panic("flash cart profiles: " + err.Error())
		}
	})
}

// GBAFlashProfiles / GBFlashProfiles: the profiles for each system.
func GBAFlashProfiles() []*FlashProfile { loadProfiles(); return gbaProfiles }
func GBFlashProfiles() []*FlashProfile  { loadProfiles(); return gbProfiles }

// ------------------------------------------------------------------- reading the commands
func isNum(v any) bool { _, ok := v.(float64); return ok }
func num(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
func isNil(v any) bool { return v == nil }
func str(v any) string { s, _ := v.(string); return s }

// el: element i of a command, nil if it's missing.
func el(c []any, i int) any {
	if i < len(c) {
		return c[i]
	}
	return nil
}

// sameCmds compares two command lists (as JSON would).
func sameCmds(a, b [][]any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// resolveAddr: an address in a command for a sector at sa ("SA", "SA+2", or a number).
func resolveAddr(a any, sa int) (int, bool, error) {
	switch {
	case a == nil:
		return 0, false, nil
	case isNum(a):
		return num(a), true, nil
	case str(a) == "SA":
		return sa, true, nil
	case strings.HasPrefix(str(a), "SA+"):
		n, err := strconv.Atoi(str(a)[3:])
		if err != nil {
			return 0, false, fmt.Errorf("unknown address %q in the profile", str(a))
		}
		return sa + n, true, nil
	}
	return 0, false, fmt.Errorf("unknown address %q in the profile", str(a))
}

// sectorMap: the profile's sector layout, as [[size, count], ...] or one size for all.
func (p *FlashProfile) sectorMap() (regions [][2]int, single int, ok bool) {
	if len(p.SectorSize) == 0 || string(p.SectorSize) == "null" {
		return nil, 0, false
	}
	var n int
	if json.Unmarshal(p.SectorSize, &n) == nil {
		return nil, n, true
	}
	var r [][2]int
	if json.Unmarshal(p.SectorSize, &r) == nil && len(r) > 0 {
		return r, 0, true
	}
	return nil, 0, false
}

// ------------------------------------------------------------------------------- CFI
// cfiInfo is what a flash chip says about itself (its "CFI" table): FlashGBX's CFI.Parse.
type cfiInfo struct {
	DeviceSize, BufferSize int
	Regions                [][2]int
	BootFlag               int
}

func swapBitPair(v, p, q int) int {
	if ((v>>p)^(v>>q))&1 != 0 {
		return v ^ (1<<p | 1<<q)
	}
	return v
}

func parseCFI(raw []byte) *cfiInfo {
	if len(raw) < 0x400 {
		return nil
	}
	b := append([]byte(nil), raw...)
	var swaps [][2]int
	switch {
	case string([]byte{b[0x20], b[0x22], b[0x24]}) == "QRY":
	case string([]byte{b[0x20], b[0x22], b[0x24]}) == "RQZ":
		swaps = [][2]int{{0, 1}}
	case b[0x20] == 0x92 && b[0x22] == 0x91 && b[0x24] == 0x9A:
		swaps = [][2]int{{0, 1}, {6, 7}}
	default:
		return nil
	}
	for _, s := range swaps {
		for i := range b {
			b[i] = byte(swapBitPair(int(b[i]), s[0], s[1]))
		}
	}
	if b[0x36] == 0xFF && b[0x48] == 0xFF {
		return nil
	}
	pri := (int(b[0x2A]) | int(b[0x2C])<<8) * 2
	if pri+0x3C >= 0x400 {
		pri = 0x80
	}
	info := &cfiInfo{DeviceSize: 1 << b[0x4E]}
	if e := int(b[0x56])<<8 | int(b[0x54]); e > 1 && e < 31 {
		info.BufferSize = 1 << e
	}
	n := min(4, int(b[0x58]))
	for i := 0; i < n; i++ {
		count := (int(b[0x5C+i*8])<<8 | int(b[0x5A+i*8])) + 1
		size := (int(b[0x60+i*8])<<8 | int(b[0x5E+i*8])) * 256
		info.Regions = append(info.Regions, [2]int{size, count})
	}
	if string([]byte{b[pri], b[pri+2], b[pri+4]}) == "PRI" && b[pri+0x1E] != 0 && b[pri+0x1E] != 0xFF {
		info.BootFlag = int(b[pri+0x1E])
	}
	return info
}

// sectorList: the sectors [offset, size] covering bytes, or nil if the chip can only be erased whole.
func sectorList(p *FlashProfile, cfi *cfiInfo, bytes int) [][2]int {
	regions, single, ok := p.sectorMap()
	if (!ok || p.SectorSizeFromCFI) && p.cmd("sector_erase") != nil {
		if cfi != nil && len(cfi.Regions) > 0 {
			regions, single, ok = cfi.Regions, 0, true
			if cfi.BootFlag == 3 {
				rev := make([][2]int, len(regions))
				for i, r := range regions {
					rev[len(regions)-1-i] = r
				}
				regions = rev
			}
		}
	}
	if p.cmd("sector_erase") == nil || !ok {
		return nil
	}
	var out [][2]int
	pos := 0
	if regions != nil {
		for _, r := range regions {
			for i := 0; i < r[1] && pos < bytes; i++ {
				out = append(out, [2]int{pos, r[0]})
				pos += r[0]
			}
		}
		last := regions[len(regions)-1][0]
		for pos < bytes && last > 0 {
			out = append(out, [2]int{pos, last})
			pos += last
		}
		return out
	}
	if single <= 0 {
		return nil
	}
	for ; pos < bytes; pos += single {
		out = append(out, [2]int{pos, single})
	}
	return out
}

// commandSet: AMD, INTEL or SHARP.
func commandSet(p *FlashProfile) string {
	if p.CommandSet != "" {
		return strings.ToUpper(p.CommandSet)
	}
	if rid := p.cmd("read_identifier"); len(rid) > 0 && num(el(rid[0], 1)) == 0x90 {
		return "INTEL"
	}
	return ""
}

func allFF(b []byte) bool {
	for _, x := range b {
		if x != 0xFF {
			return false
		}
	}
	return true
}

func hexBytes(b []byte) string {
	var s []string
	for _, x := range b {
		s = append(s, fmt.Sprintf("%02X", x))
	}
	return strings.Join(s, " ")
}

// FlashError: a flash cart job that couldn't be done. Code: no-flash, too-big, timeout, verify,
// unsupported, payload, cancelled.
type FlashError struct{ Code, Msg string }

func (e *FlashError) Error() string { return e.Msg }

// FlashDetect is what detection found.
type FlashDetect struct {
	Profile    *FlashProfile
	Candidates []*FlashProfile
	FlashID    []byte
	CFI        *cfiInfo
	Method     string
}

// SizeText: the chip's size for the screen.
func (d *FlashDetect) SizeText() string {
	listed, reported := d.Profile.FlashSize, 0
	if d.CFI != nil {
		reported = d.CFI.DeviceSize
	}
	switch {
	case listed > 0 && reported > 0 && reported != listed:
		return fmt.Sprintf("%s (the chip says %s)", sizeText(listed), sizeText(reported))
	case listed > 0:
		return sizeText(listed)
	case reported > 0:
		return sizeText(reported)
	}
	return "size unknown"
}

// FlashResult is how a write went.
type FlashResult struct {
	Written, Skipped, Cleared int
	Method                    string
}

func sizeText(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
