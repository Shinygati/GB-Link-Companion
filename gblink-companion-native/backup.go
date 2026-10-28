// Backing up a cartridge: copy the game (ROM) and its save to this PC.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash/crc32"

	"os"
	"path/filepath"
	"strings"
	"time"
)

// backup starts a cartridge backup (from the game list's "Back up a cartridge" button).
func (a *App) backup() error {
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("Something is already running.")
	}
	s := &Session{ui: a.ui, app: a, cancel: make(chan struct{})}
	a.session = s
	a.mu.Unlock()
	a.ui.update(func(st *State) {
		st.Game = &GameView{Name: "Back up a cartridge"}
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", "backup", "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText, st.BackupPath = "", "", "", "", "", ""
		st.Verify = nil
		st.Controller = ControllerView{}
		st.Log = nil
	})
	go s.runBackup()
	return nil
}

// cancelBackup is the Cancel button under the progress bar.
func (a *App) cancelBackup() {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil && s.cancel != nil {
		select {
		case <-s.cancel:
		default:
			close(s.cancel)
		}
	}
}

func (s *Session) cancelled() bool {
	select {
	case <-s.cancel:
		return true
	default:
		return false
	}
}

// fileName makes a file name for a backup from the cartridge header.
func fileName(title, code string) string {
	t := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return ' '
		}
		return r
	}, title)
	t = strings.Join(strings.Fields(t), " ")
	if t == "" {
		t = "GBA game"
	}
	if code != "" {
		t += " (" + code + ")"
	}
	return t + ".gba"
}

func uniquePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 2; ; i++ {
		q := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(q); err != nil {
			return q
		}
	}
}

func (a *App) backupFolder() string {
	if d := a.settings().GamesFolder; d != "" {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	home, _ := os.UserHomeDir()
	d := filepath.Join(home, "Documents", "GBA Games")
	os.MkdirAll(d, 0o755)
	return d
}

func mb(n int) string {
	v := float64(n) / (1 << 20)
	if v >= 10 || v == float64(int(v)) {
		return fmt.Sprintf("%.0f MB", v)
	}
	return fmt.Sprintf("%.1f MB", v)
}

func aboutMinutes(d time.Duration) string {
	m := int(d.Minutes() + 0.5)
	switch {
	case d < 45*time.Second:
		return "less than a minute"
	case m <= 1:
		return "about a minute"
	}
	return fmt.Sprintf("about %d minutes", m)
}

// headerOK checks the header checksum at 0xBD (the GBA BIOS checks the same).
func headerOK(h []byte) bool {
	var c byte
	for _, b := range h[0xA0:0xBD] {
		c -= b
	}
	return c-0x19 == h[0xBD]
}

// gbaHeaderText: printable text from a GBA header field.
func gbaHeaderText(h []byte) string {
	var sb strings.Builder
	for _, c := range h {
		if c == 0 {
			break
		}
		if c >= 0x20 && c < 0x7F {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('_')
		}
	}
	return strings.TrimSpace(sb.String())
}

// gbaCartNow starts GB-Link Cart Doctor's GBA program and reads the cartridge's header and sizes.
func (s *Session) gbaCartNow(title string) (d *gbaDumper, romSize, saveSize int, header []byte, ok bool) {
	d, ok = s.startGBADumper(title, "the cartridge")
	if !ok {
		return nil, 0, 0, nil, false
	}
	for {
		s.ui.SetStatus("Checking the cartridge...", "busy")
		var err error
		romSize, saveSize, header, err = d.readInfo()
		if err == nil && header == nil {
			err = linkErr("No cartridge was found in the GBA. Switch the GBA off, push the cartridge in firmly, and start again.")
		}
		if err == nil {
			return d, romSize, saveSize, header, true
		}
		if s.cancelled() || !s.askRetry("Couldn't read the cartridge", err.Error()) {
			return nil, 0, 0, nil, false
		}
	}
}

// romProgress: the progress bar while a game is copied.
func (s *Session) romProgress(start time.Time) func(done, total int) {
	return func(done, total int) {
		detail := mb(done) + " of " + mb(total)
		if el := time.Since(start); el > 3*time.Second && done < total && done > 0 {
			detail += " · " + aboutMinutes(time.Duration(float64(el)*float64(total-done)/float64(done))) + " left"
		}
		s.ui.update(func(st *State) {
			st.Progress = &Progress{Label: "Copying the game to this PC", Done: done, Total: total, Detail: detail, Cancellable: true}
		})
	}
}

// runBackup backs up a GBA cartridge with GB-Link Cart Doctor's GBA program: its streamed ROM
// dump (runs of repeated data cost almost nothing) and its save dump.
func (s *Session) runBackup() {
	defer func() {
		if s.link != nil {
			s.link.Close()
		}
	}()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	d, romSize, saveSize, header, ok := s.gbaCartNow("Switch on your GBA to read the cartridge")
	if !ok {
		s.ui.Finish("cancelled", "")
		return
	}
	title, code := gbaHeaderText(header[0xA0:0xAC]), gbaHeaderText(header[0xAC:0xB0])
	name := nonBlank(title, "Unknown game")
	s.ui.update(func(st *State) { st.Game = &GameView{Name: name, Title: title, Code: code} })
	s.ui.Log("Cartridge: %s [%s], %s, save memory: %s.", title, code, mb(romSize), map[bool]string{true: sizeText(saveSize), false: "none"}[saveSize > 0])
	hasSave := saveSize > 0

	// where to put it
	dest := uniquePath(filepath.Join(s.app.backupFolder(), fileName(title, code)))
	for {
		what := "The game (" + mb(romSize) + ")"
		if hasSave {
			what += " and its save"
		}
		a := s.ui.Ask(Prompt{Kind: "question", Title: "Back up " + name + "?",
			Text: what + " will be saved as “" + filepath.Base(dest) + "” in " + filepath.Dir(dest) + ". Keep the GBA on until it's done.",
			Buttons: []Button{{ID: "go", Label: "Back it up", Style: "primary"},
				{ID: "where", Label: "Save somewhere else…", Style: "secondary"},
				{ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		if a == "cancel" {
			s.ui.Finish("cancelled", "")
			return
		}
		if a == "where" {
			if p, err := pickSaveFile("Save the game as", filepath.Dir(dest), filepath.Base(dest)); err == nil && p != "" {
				if !strings.EqualFold(filepath.Ext(p), ".gba") {
					p += ".gba"
				}
				dest = p
			}
			continue
		}
		break
	}

	// copy the game
	s.ui.SetPhase("dump")
	start := time.Now()
	var rom []byte
	for {
		var err error
		rom, err = d.dumpROM(s.romProgress(start))
		s.ui.ClearProgress()
		if s.cancelled() {
			s.ui.Finish("cancelled", "The backup was stopped. Nothing was saved.")
			return
		}
		if err == nil {
			break
		}
		if !s.askRetry("Couldn't copy the game", err.Error()) {
			s.ui.Finish("failed", err.Error())
			return
		}
		start = time.Now()
	}
	if err := writeFileSafely(dest, rom); err != nil {
		s.ui.Finish("failed", "Couldn't save the file: "+err.Error())
		return
	}
	s.ui.Log("Game saved to %s (%s, %s).", dest, mb(len(rom)), time.Since(start).Round(time.Second))
	size := len(rom)

	// check it against No-Intro's list of good dumps
	s.ui.SetStatus("Checking the backup against No-Intro's list of good dumps...", "busy")
	v, trimmed := verifyDump(dest, code, true)
	if trimmed > 0 {
		size = trimmed
	}
	s.ui.Log("SHA-1 %s, CRC32 %s: %s", v.SHA1, v.CRC, v.Title)
	s.ui.update(func(st *State) { st.Verify = &v })

	// and its save
	saveNote := ""
	if hasSave {
		s.ui.SetStatus("Copying its save...", "busy")
		data, err := d.dumpSave(func(dn, t int) { s.ui.SetProgress("Copying its save to this PC", dn, t) })
		s.ui.ClearProgress()
		if err == nil {
			g, _ := NewGame(dest)
			if g != nil {
				g.backup(data, "cart")
				err = g.writeLocal(data)
				g.saveState(GameState{SyncedHash: hash(data)})
			}
		}
		if err != nil {
			saveNote = " Its save couldn't be copied (" + err.Error() + ")."
		} else {
			saveNote = " Its save is next to it, so you can play it straight away."
		}
	}
	warn := ""
	if !headerOK(header) {
		warn = " Note: the game's header checksum doesn't match. That's normal for some repro carts and hacks, but if it's an official cart, re-seat it and back it up again."
	}
	s.app.addRecent(dest)
	s.ui.update(func(st *State) { st.BackupPath = dest })
	s.ui.Finish("backup", "“"+filepath.Base(dest)+"” ("+mb(size)+") is in "+filepath.Dir(dest)+"."+saveNote+warn)
}

// tellGBA shows a message on the GBA's screen; if the GBA isn't answering, never mind.
func (s *Session) tellGBA(msg int) {
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	if s.helper != nil {
		s.helper.Show(msg)
	}
}

// verifyDump checks a backup against No-Intro's list of good dumps. If the start of a bigger
// backup (e.g. "Back up all 32 MB") is a known dump, the file is trimmed to it; the new size is
// returned (0 = not trimmed).
func verifyDump(path, code string, repro bool) (Verify, int) {
	dat := currentDAT()
	db := "No-Intro's list"
	if dat.Version != "" {
		db += " (version " + dat.Version + ")"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Verify{Status: "unknown", Title: "Couldn't check the backup", Text: err.Error()}, 0
	}
	hashes := func(b []byte) (string, string) {
		h := sha1.Sum(b)
		return hex.EncodeToString(h[:]), fmt.Sprintf("%08x", crc32.ChecksumIEEE(b))
	}
	sh, crc := hashes(data)
	v := Verify{SHA1: sh, CRC: crc}
	if e := dat.Lookup(sh); e != nil {
		v.Status, v.Title = "ok", "Verified: a good dump"
		v.Text = "It matches No-Intro's “" + e.Name + "” exactly."
		return v, 0
	}
	// the game may be just the start of what was copied (the rest of a repro's flash)
	for n := 1 << 20; n < len(data); n <<= 1 {
		psh, pcrc := hashes(data[:n])
		if e := dat.Lookup(psh); e != nil && e.Size == n {
			if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
				f.Truncate(int64(n))
				f.Close()
			}
			v.Status, v.Title, v.SHA1, v.CRC = "ok", "Verified: a good dump", psh, pcrc
			v.Text = "The game is the first " + mb(n) + " of the cartridge, and it matches No-Intro's “" + e.Name +
				"” exactly. The rest of the cartridge held other data, so the backup was trimmed to " + mb(n) + "."
			return v, n
		}
	}
	known := dat.WithSerial(code)
	if len(known) > 0 {
		names := make([]string, 0, len(known))
		for _, e := range known {
			names = append(names, "“"+e.Name+"”")
		}
		v.Status, v.Title = "bad", "Can't verify checksum"
		v.Text = "No-Intro lists " + strings.Join(names, ", ") + " for this game code, and this backup doesn't match any of them."
		if repro {
			v.Text += " This is a reproduction cartridge, and they often hold modified games (for example patched to save without a battery), so that's common and doesn't mean the copy went wrong."
		} else {
			v.Text += " Re-seat the cartridge, clean its contacts and back it up again. If it still doesn't match, the cartridge may hold a modified game or be a bootleg."
		}
		return v, 0
	}
	v.Status, v.Title = "unknown", "Not in the No-Intro list"
	v.Text = "It isn't in " + db + ", so it can't be checked. It may be a hack, homebrew, a game made for repro carts, or newer than the list."
	return v, 0
}
