// The cartridge's clock in mGBA (Pokémon Ruby, Sapphire and Emerald).
//
// These games keep time with a real-time clock on the cartridge, and berries, tides and daily
// events go by it. mGBA's clock follows the PC, which is a different time from the cartridge's,
// so after playing in mGBA the game on the GBA finds its time has jumped (berries die, for one).
//
// mGBA takes a game's clock from the end of its .sav: 16 bytes after the save (the clock's date
// and time, its control register, and the moment they were read, as a Unix time), the way
// cartridge dumpers save it. It starts its clock there and lets it run. So before mGBA starts,
// the GBA helper reads the cartridge's clock (CMD_RTC_READ) and the app puts it at the end of the
// .sav. Writing the save back to the cartridge only ever takes the save itself.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CartClock: the cartridge clock's registers
type CartClock struct {
	Status   byte    // bit 7: it lost power (the battery ran dry), bit 6: 24-hour mode
	DateTime [7]byte // BCD: year, month, day, weekday, hour, minute, second
}

// ReadClock reads the cartridge's clock. ok is false if nothing answered (no clock, or a helper
// too old to ask).
func (h *Helper) ReadClock() (CartClock, bool, error) {
	st, d, err := h.Command(cmdRTCRead, nil, nil, 3*time.Second, []int{4, 0}, 2*time.Second) // 0: an older helper says it doesn't know it
	if err != nil {
		return CartClock{}, false, err
	}
	if st != stOK || len(d) < 4 || d[0] == 0 {
		return CartClock{}, false, nil
	}
	c := CartClock{Status: byte(d[1])}
	for i := 0; i < 4; i++ {
		c.DateTime[i] = byte(d[2] >> (8 * i))
	}
	for i := 0; i < 3; i++ {
		c.DateTime[4+i] = byte(d[3] >> (8 * i))
	}
	return c, true, nil
}

func unBCD(b byte) int { return int(b>>4)*10 + int(b&15) }

func validBCD(b byte, max int) bool { return b>>4 <= 9 && b&15 <= 9 && unBCD(b) <= max }

// Time: the clock's date and time, and whether it's running properly
func (c CartClock) Time() (time.Time, bool) {
	d := c.DateTime
	hour := d[4] & 0x3F // bit 7: PM in 12-hour mode
	if c.Status&0x80 != 0 || !validBCD(d[0], 99) || !validBCD(d[1], 12) || d[1] == 0 || !validBCD(d[2], 31) || d[2] == 0 ||
		!validBCD(hour, 23) || !validBCD(d[5], 59) || !validBCD(d[6], 59) {
		return time.Time{}, false
	}
	h := unBCD(hour)
	if c.Status&0x40 == 0 && d[4]&0x80 != 0 && h < 12 { // 12-hour mode, PM
		h += 12
	}
	return time.Date(2000+unBCD(d[0]), time.Month(unBCD(d[1])), unBCD(d[2]), h, unBCD(d[5]), unBCD(d[6]), 0, time.Local), true
}

// mgbaClockFooter: what mGBA reads after a save for the game's clock (GBASavedataRTCBuffer):
// the clock's 7 date and time bytes, its control register, and when they were read
func mgbaClockFooter(c CartClock, readAt time.Time) []byte {
	b := make([]byte, 16)
	copy(b, c.DateTime[:])
	b[7] = c.Status &^ 0x80 // (mGBA takes 0x01 to mean "no control value")
	if b[7] == 1 {
		b[7] = 0x41
	}
	binary.LittleEndian.PutUint64(b[8:], uint64(readAt.Unix()))
	return b
}

// clockGame: Pokémon Ruby, Sapphire or Emerald (the games with a clock on the cartridge)
func clockGame(code string) bool {
	c := strings.ToUpper(code)
	return strings.HasPrefix(c, "AXV") || strings.HasPrefix(c, "AXP") || strings.HasPrefix(c, "BPE")
}

// useCartClock puts the cartridge's clock at the end of the .sav for mGBA, if it's a game with
// one and the GBA is on. Otherwise mGBA uses the PC's clock, as it always has.
func (s *Session) useCartClock() {
	if s.game == nil || !clockGame(s.game.Code) || s.app.settings().PCClock || s.helper == nil {
		return
	}
	st, ok := saveTypes[s.saveType]
	data := s.game.localSave()
	if !ok || len(data) < st.Size {
		return
	}
	if !s.helper.Alive(1500 * time.Millisecond) {
		s.ui.Log("The GBA is off, so mGBA uses this PC's clock this time.")
		return
	}
	c, found, err := s.helper.ReadClock()
	now := time.Now()
	switch {
	case err != nil:
		s.ui.Log("Couldn't read the cartridge's clock (%v); mGBA uses this PC's clock.", err)
		return
	case !found:
		s.ui.Log("The cartridge's clock didn't answer (or the GBA helper is an older one); mGBA uses this PC's clock.")
		return
	}
	t, running := c.Time()
	if !running {
		s.ui.Log("The cartridge's clock isn't running (its battery may have run dry), so mGBA uses this PC's clock.")
		return
	}
	out := append(append([]byte(nil), data[:st.Size]...), mgbaClockFooter(c, now)...)
	if err := os.WriteFile(s.game.Sav, out, 0o644); err != nil {
		s.ui.Log("Couldn't give mGBA the cartridge's clock (%v).", err)
		return
	}
	s.clockUsed = true
	s.ui.Log("mGBA's clock starts from the cartridge's: %s%s.", t.Format("Mon 2 Jan 2006 15:04:05"), clockDiff(t.Sub(now)))
}

func clockDiff(d time.Duration) string {
	if d > -2*time.Minute && d < 2*time.Minute {
		return ""
	}
	dir := "ahead of"
	if d < 0 {
		dir, d = "behind", -d
	}
	var n string
	switch days := int(d.Hours() / 24); {
	case days >= 2:
		n = fmt.Sprintf("%d days", days)
	case d >= 2*time.Hour:
		n = fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		n = fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf(" (%s %s this PC)", n, dir)
}

// trimSav: after mGBA, the .sav without the clock data mGBA writes after the save, unless it's
// the cartridge's clock (a cartridge without a working clock has no use for it)
func (s *Session) trimSav() {
	st, ok := saveTypes[s.saveType]
	data := s.game.localSave()
	if s.clockUsed || !ok || len(data) <= st.Size {
		return
	}
	if err := s.game.writeLocal(data[:st.Size]); err != nil {
		s.ui.Log("Couldn't take mGBA's clock data off %s (%v).", filepath.Base(s.game.Sav), err)
	}
}

// backupMGBASave keeps a copy of a save made in mGBA in the ROM's folder (gblink_backups),
// once for each different save
func (s *Session) backupMGBASave() {
	data := s.game.localSave()
	if st, ok := saveTypes[s.saveType]; ok && len(data) > st.Size {
		data = data[:st.Size]
	}
	if len(data) == 0 {
		return
	}
	h := hash(data)
	if h == s.lastBackup || h == s.state.SyncedHash && s.lastBackup == "" {
		return
	}
	s.lastBackup = h
	s.game.backup(data, "mgba")
}
