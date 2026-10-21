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

func (s *Session) runBackup() {
	defer func() {
		if s.link != nil {
			s.link.Close()
		}
	}()
	s.ui.SetStatus("Connecting to the GB-Link...", "busy")
	for {
		err := s.connect()
		if err == nil {
			break
		}
		if !s.askRetry("GB-Link not found", err.Error()+" Plug it in, then try again.") {
			s.ui.Finish("cancelled", "")
			return
		}
	}
	s.ui.SetPhase("load")
	var info ROMInfo
	var cart CartInfo
	for {
		s.linkMu.Lock()
		err := s.ensureHelper("Switch on your GBA to read the cartridge")
		if err == nil {
			s.ui.SetStatus("Checking the cartridge...", "busy")
			info, err = s.helper.ROMInfo()
			if err == nil {
				cart, _ = s.helper.SaveInfo() // the save is backed up too, if there is one
			}
		}
		s.linkMu.Unlock()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't read the cartridge", err.Error()) {
			s.ui.Finish("cancelled", "")
			return
		}
	}
	name := info.Title
	if name == "" {
		name = "Unknown game"
	}
	s.ui.update(func(st *State) { st.Game = &GameView{Name: name, Title: info.Title, Code: info.Code} })
	s.ui.Log("Cartridge: %s [%s], %s%s", info.Title, info.Code, mb(info.Size), map[bool]string{true: " (possible repro cart.)", false: "."}[info.Guessed])
	// A repro cart doesn't show where the game ends, so its size was measured (or guessed). If
	// No-Intro knows this game code, use the size it lists (e.g. BPEE = 16 MB).
	sizeNote := ""
	if info.Repro {
		sizes := map[int]bool{}
		for _, e := range currentDAT().WithSerial(info.Code) {
			if e.Size > 0 && e.Size <= 32<<20 {
				sizes[e.Size] = true
			}
		}
		if len(sizes) == 1 {
			for n := range sizes {
				if n != info.Size || info.Guessed {
					s.ui.Log("No-Intro lists %s as %s; using that size (measured %s).", info.Code, mb(n), mb(info.Size))
				}
				info.Size, info.Guessed = n, false
				sizeNote = "No-Intro lists this game (" + info.Code + ") as " + mb(n) + ", so that's how much will be copied. "
			}
		} else if len(sizes) > 1 && sizes[info.Size] {
			info.Guessed = false // the measured size is one No-Intro lists for this game
		}
	}
	_, hasSave := saveTypes[cart.Type]
	if hasSave {
		s.ui.Log("Save chip: %s.", saveTypes[cart.Type].Name)
	}

	// where to put it
	dest := uniquePath(filepath.Join(s.app.backupFolder(), fileName(info.Title, info.Code)))
	est := time.Duration(float64(info.Size)/30000) * time.Second // ~30 KB/s over the link cable
	for {
		what := "The game (" + mb(info.Size) + ")"
		if hasSave {
			what += " and its save"
		}
		text := sizeNote + what + " will be saved as “" + filepath.Base(dest) + "” in " + filepath.Dir(dest) +
			". This takes " + aboutMinutes(est) + "; keep the GBA on until it's done."
		buttons := []Button{{ID: "go", Label: "Back it up", Style: "primary"},
			{ID: "where", Label: "Save somewhere else…", Style: "secondary"},
			{ID: "cancel", Label: "Cancel", Style: "secondary"}}
		if info.Guessed {
			// a repro cart whose flash has something else stored after the game
			text = "This is a reproduction cartridge with more data stored after the game, so its size " +
				"can't be read for certain. It looks like " + mb(info.Size) + ". If the backup doesn't " +
				"work, back it up again with “Back up all 32 MB”.\n\n" + text
			buttons = []Button{{ID: "go", Label: "Back up " + mb(info.Size), Style: "primary"},
				{ID: "all", Label: "Back up all 32 MB", Style: "secondary"},
				{ID: "where", Label: "Save somewhere else…", Style: "secondary"},
				{ID: "cancel", Label: "Cancel", Style: "secondary"}}
		}
		a := s.ui.Ask(Prompt{Kind: "question", Title: "Back up " + name + "?", Text: text, Buttons: buttons})
		if a == "cancel" {
			s.ui.Finish("cancelled", "")
			return
		}
		if a == "all" {
			info.Size, info.Guessed = 32<<20, false
			est = time.Duration(float64(info.Size)/30000) * time.Second
			s.ui.Log("Backing up all 32 MB.")
			break
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
	part := dest + ".part"
	f, err := os.Create(part)
	if err != nil {
		s.ui.Ask(Prompt{Kind: "error", Title: "Can't save there", Text: err.Error(), Buttons: []Button{{ID: "ok", Label: "OK", Style: "primary"}}})
		s.ui.Finish("cancelled", "")
		return
	}
	steps := (info.Size + romChunk - 1) / romChunk
	start := time.Now()
	fail := func(msg string) {
		f.Close()
		os.Remove(part)
		s.ui.ClearProgress()
		s.tellGBA(msgStopped) // so its screen doesn't stay on "Copying..."
		s.ui.Finish("failed", msg)
	}
	for k := 0; k < steps; k++ {
		if s.cancelled() {
			fail("")
			s.ui.update(func(st *State) { st.Result = "cancelled"; st.ResultText = "The backup was stopped. Nothing was saved." })
			return
		}
		off := k * romChunk
		n := min(romChunk, info.Size-off)
		s.linkMu.Lock()
		data, err := s.helper.ReadROM(off, n, k, steps)
		s.linkMu.Unlock()
		if err != nil {
			fail(err.Error() + " The partial copy was deleted.")
			return
		}
		if _, err := f.Write(data); err != nil {
			fail("Couldn't write the file: " + err.Error())
			return
		}
		done := off + n
		detail := mb(done) + " of " + mb(info.Size)
		if el := time.Since(start); el > 3*time.Second && done < info.Size {
			left := time.Duration(float64(el) * float64(info.Size-done) / float64(done))
			detail += " · " + aboutMinutes(left) + " left"
		}
		s.ui.update(func(st *State) {
			st.Progress = &Progress{Label: "Copying the game to this PC", Done: done, Total: info.Size, Detail: detail, Cancellable: true}
		})
	}
	if err := f.Close(); err != nil {
		fail("Couldn't write the file: " + err.Error())
		return
	}
	os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		fail("Couldn't save the file: " + err.Error())
		return
	}
	s.ui.ClearProgress()
	s.ui.Log("Game saved to %s (%s, %s).", dest, mb(info.Size), time.Since(start).Round(time.Second))

	// check it against No-Intro's list of good dumps
	s.ui.SetStatus("Checking the backup against No-Intro's list of good dumps...", "busy")
	v, trimmed := verifyDump(dest, info.Code, info.Repro)
	if trimmed > 0 {
		info.Size = trimmed
	}
	s.ui.Log("SHA-1 %s, CRC32 %s: %s", v.SHA1, v.CRC, v.Title)
	s.ui.update(func(st *State) { st.Verify = &v })

	// and its save
	saveNote := ""
	if hasSave {
		s.ui.SetStatus("Copying its save...", "busy")
		s.tellGBA(msgBackupSav)
		s.linkMu.Lock()
		data, err := s.helper.ReadSave(cart.Type, func(d, t int) { s.ui.SetProgress("Copying its save to this PC", d, t) })
		s.linkMu.Unlock()
		s.ui.ClearProgress()
		if err == nil {
			g, _ := NewGame(dest)
			if g != nil {
				g.backup(data, "cart")
				err = g.writeLocal(data)
				g.saveState(GameState{SyncedHash: hash(data), SaveType: cart.Type})
			}
		}
		if err != nil {
			saveNote = " Its save couldn't be copied (" + err.Error() + ")."
		} else {
			saveNote = " Its save is next to it, so you can play it straight away."
		}
	}
	warn := ""
	if !headerOK(info.Header) {
		warn = " Note: the game's header checksum doesn't match. That's normal for some repro carts and hacks, but if it's an official cart, re-seat it and back it up again."
	}
	s.tellGBA(msgBackupEnd)
	s.app.addRecent(dest)
	s.ui.update(func(st *State) { st.BackupPath = dest })
	s.ui.Finish("backup", "“"+filepath.Base(dest)+"” ("+mb(info.Size)+") is in "+filepath.Dir(dest)+"."+saveNote+warn)
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
