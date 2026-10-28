// Cartridge tools: backing up Game Boy, Game Boy Color and GBA cartridges (the game and its
// save), writing a save back to one, and writing a ROM to a flash cart. The GBA tools use the
// helper (backup.go, restore.go) and GB-Link Cart Doctor's GBA dumper for flash carts
// (gbacart.go, gbaflash.go); the Game Boy ones put the GBA in Game Boy Color mode (gbcart.go).
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// the tools, by the id the window uses
var toolNames = map[string]string{
	"gba-backup": "Back up a GBA cartridge", "gba-restore": "Write a save to a GBA cartridge", "gba-flash": "Write a ROM to a GBA flash cart",
	"gba-save-backup": "Back up a GBA save", "gb-save-backup": "Back up a Game Boy save",
	"gb-backup": "Back up a Game Boy cartridge", "gb-restore": "Write a save to a Game Boy cartridge", "gb-flash": "Write a ROM to a Game Boy flash cart",
}

// startTool starts one of the cartridge tools.
func (a *App) startTool(tool string) error {
	var err error
	switch tool {
	case "gba-backup":
		err = a.backup()
	case "gba-restore":
		err = a.restoreSave()
	case "gba-flash":
		err = a.flashTool(false)
	case "gba-save-backup":
		err = a.gbaSaveBackup()
	case "gb-save-backup":
		err = a.gbSaveBackup()
	case "gb-backup":
		err = a.gbBackup()
	case "gb-restore":
		err = a.gbRestore()
	case "gb-flash":
		err = a.flashTool(true)
	default:
		return fmt.Errorf("unknown tool %q", tool)
	}
	if err == nil && a.busy() {
		a.ui.update(func(st *State) { st.Tool = tool })
	}
	return err
}

// newToolSession starts a session for a tool (mode: backup, restore or flash).
func (a *App) newToolSession(mode, name string, gb bool) (*Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		return nil, fmt.Errorf("Something is already running.")
	}
	s := &Session{ui: a.ui, app: a, cancel: make(chan struct{})}
	a.session = s
	a.ui.update(func(st *State) {
		st.Game = &GameView{Name: name, GB: gb}
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", mode, "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText, st.BackupPath = "", "", "", "", "", ""
		st.Verify = nil
		st.Controller = ControllerView{}
		st.Log = nil
	})
	return s, nil
}

// connectOrFinish connects to the adapter, asking to plug it in; false: the player gave up.
func (s *Session) connectOrFinish() bool {
	s.ui.SetStatus("Connecting to the GB-Link...", "busy")
	for {
		err := s.connect()
		if err == nil {
			return true
		}
		if !s.askRetry("GB-Link not found", err.Error()+" Plug it in, then try again.") {
			s.ui.Finish("cancelled", "")
			return false
		}
	}
}

func (s *Session) closeGB() {
	if s.link != nil {
		s.link.ConfigureGBA()
		s.link.Close()
	}
}

// gbReady: the GBA in Game Boy Color mode with a cartridge, and its header (retrying on errors).
func (s *Session) gbReady(why, which string) (GBHeader, bool) {
	for {
		err := s.gbStart(why, which)
		var h GBHeader
		if err == nil {
			s.ui.SetStatus("Reading the cartridge...", "busy")
			h, err = s.gbHeaderNow()
		}
		if err == nil {
			return h, true
		}
		if err == errCancelled || err == errGBCancelled || !s.askRetry("Couldn't read the cartridge", err.Error()) {
			return h, false
		}
	}
}

func gbDescribe(h GBHeader) string {
	sys := "Game Boy"
	switch h.CGB {
	case "only":
		sys = "Game Boy Color only"
	case "yes":
		sys = "Game Boy Color"
	}
	save := "no save"
	if h.RAMSize > 0 && h.Battery {
		save = sizeText(h.RAMSize) + " save"
	} else if h.RAMSize > 0 {
		save = "no battery save"
	}
	return fmt.Sprintf("%s, %s game, %s, %s", sys, sizeText(h.ROMSize), save, h.TypeName)
}

// gbGlobalOK: the game's own checksum over the whole ROM (the bytes at $014E-$014F) adds up.
func gbGlobalOK(rom []byte) bool {
	if len(rom) < 0x150 {
		return false
	}
	var sum uint16
	for i, b := range rom {
		if i != 0x14E && i != 0x14F {
			sum += uint16(b)
		}
	}
	return sum == uint16(rom[0x14E])<<8|uint16(rom[0x14F])
}

// ------------------------------------------------------------------------- Game Boy backup
func (a *App) gbBackup() error {
	s, err := a.newToolSession("backup", "Back up a Game Boy cartridge", true)
	if err != nil {
		return err
	}
	go s.runGBBackup()
	return nil
}

func (s *Session) runGBBackup() {
	defer s.closeGB()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	h, ok := s.gbReady("Switch on your GBA to read the Game Boy cartridge", "the Game Boy or Game Boy Color cartridge")
	if !ok {
		s.ui.Finish("cancelled", "")
		return
	}
	name := nonBlank(h.Title, "Game Boy game")
	s.ui.update(func(st *State) { st.Game = &GameView{Name: name, Title: gbDescribe(h), GB: true} })
	s.ui.Log("Cartridge: %s (%s).", name, gbDescribe(h))
	dest := uniquePath(filepath.Join(s.app.backupFolder(), gbROMName(h)))
	hasSave := h.RAMSize > 0 && h.Battery
	for {
		what := "The game (" + sizeText(h.ROMSize) + ")"
		if hasSave {
			what += " and its save"
		}
		a := s.ui.Ask(Prompt{Kind: "question", Title: "Back up " + name + "?",
			Text: what + " will be saved as “" + filepath.Base(dest) + "” in " + filepath.Dir(dest) + ". Keep the GBA on until it's done.",
			Buttons: []Button{{ID: "go", Label: "Back it up", Style: "primary"},
				{ID: "where", Label: "Save somewhere else…", Style: "secondary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		if a == "cancel" {
			s.ui.Finish("cancelled", "")
			return
		}
		if a == "where" {
			if p, err := pickSaveFile("Save the game as", filepath.Dir(dest), filepath.Base(dest)); err == nil && p != "" {
				if e := strings.ToLower(filepath.Ext(p)); e != ".gb" && e != ".gbc" {
					p += filepath.Ext(dest)
				}
				dest = p
			}
			continue
		}
		break
	}
	s.ui.SetPhase("dump")
	start := time.Now()
	var rom []byte
	for {
		var err error
		rom, err = s.gbc.dump(false, func(done, total int) {
			detail := mb(done) + " of " + mb(total)
			if el := time.Since(start); el > 3*time.Second && done > 0 && done < total {
				detail += " · " + aboutMinutes(time.Duration(float64(el)*float64(total-done)/float64(done))) + " left"
			}
			s.ui.update(func(st *State) {
				st.Progress = &Progress{Label: "Copying the game to this PC", Done: done, Total: total, Detail: detail, Cancellable: true}
			})
		})
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
	}
	if err := os.WriteFile(dest, rom, 0o644); err != nil {
		s.ui.Finish("failed", "Couldn't save the file: "+err.Error())
		return
	}
	s.ui.Log("Game saved to %s (%s, %s).", dest, sizeText(len(rom)), time.Since(start).Round(time.Second))
	check := " The game's own checksum adds up."
	if !gbGlobalOK(rom) {
		check = " Note: the game's own checksum doesn't add up. Some carts (and hacks) are like that, but if it's an official cartridge, clean its contacts and back it up again."
	}
	saveNote := ""
	if hasSave {
		s.ui.SetStatus("Copying its save...", "busy")
		data, err := s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Copying its save to this PC", d, t) })
		s.ui.ClearProgress()
		if err == nil {
			g, _ := NewGame(dest)
			if g != nil {
				g.backup(data, "cart")
				err = g.writeLocal(data)
				g.saveState(GameState{SyncedHash: hash(data), GBSave: len(data)})
			}
		}
		if err != nil {
			saveNote = " Its save couldn't be copied (" + err.Error() + ")."
		} else {
			saveNote = " Its save is next to it, so you can play it straight away."
		}
	}
	s.app.addRecent(dest)
	s.ui.update(func(st *State) { st.BackupPath = dest })
	s.ui.Finish("backup", "“"+filepath.Base(dest)+"” ("+sizeText(len(rom))+") is in "+filepath.Dir(dest)+"."+saveNote+check)
}

// ------------------------------------------------------------------------ Game Boy restore
func (a *App) gbRestore() error {
	if a.busy() {
		return fmt.Errorf("Something is already running.")
	}
	if !pickMu.TryLock() {
		return nil
	}
	path, err := pickFile("Choose a save file to write to the Game Boy cartridge", "Save files (*.sav, *.srm)", "*.sav;*.srm", a.backupFolder())
	pickMu.Unlock()
	if err != nil || path == "" {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Couldn't read that file: %v", err)
	}
	if len(data) < 0x200 || len(data) > 0x20000+64 {
		return fmt.Errorf("That doesn't look like a Game Boy save file (%d bytes).", len(data))
	}
	s, err := a.newToolSession("restore", "Write a save to a Game Boy cartridge", true)
	if err != nil {
		return err
	}
	go s.runGBRestore(path, data)
	return nil
}

func (s *Session) runGBRestore(path string, data []byte) {
	defer s.closeGB()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	h, ok := s.gbReady("Switch on your GBA to write the save", "the Game Boy or Game Boy Color cartridge")
	if !ok {
		s.ui.Finish("cancelled", "")
		return
	}
	name := nonBlank(h.Title, "Game Boy game")
	s.ui.update(func(st *State) { st.Game = &GameView{Name: name, Title: gbDescribe(h), GB: true} })
	if h.RAMSize == 0 {
		s.ui.Finish("failed", name+" has no save memory on its cartridge.")
		return
	}
	// emulators put the cartridge's clock after the save (48 or 44 bytes): it isn't written
	if len(data) == h.RAMSize+48 || len(data) == h.RAMSize+44 {
		data = data[:h.RAMSize]
	}
	if len(data) != h.RAMSize {
		s.ui.Finish("failed", fmt.Sprintf("%s is %s, but %s's save memory is %s. Nothing was written.",
			filepath.Base(path), sizeText(len(data)), name, sizeText(h.RAMSize)))
		return
	}
	if s.ui.Ask(Prompt{Kind: "question", Title: "Replace " + name + "'s save?",
		Text:    "The save on the cartridge is replaced with “" + filepath.Base(path) + "”. The cartridge's save is copied to this PC first.",
		Buttons: []Button{{ID: "go", Label: "Write the save", Style: "primary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}}) != "go" {
		s.ui.Finish("cancelled", "")
		return
	}
	s.ui.SetPhase("dump")
	old, err := s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Copying the cartridge's save to this PC first", d, t) })
	s.ui.ClearProgress()
	if err != nil {
		s.ui.Finish("failed", "The cartridge's save couldn't be copied first, so nothing was written: "+err.Error())
		return
	}
	dir := filepath.Join(s.app.backupFolder(), "Save backups")
	backup := uniquePath(filepath.Join(dir, fileSafe(name)+" - before restore - "+time.Now().Format("2006-01-02 15-04-05")+".sav"))
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(backup, old, 0o644) != nil {
		s.ui.Finish("failed", "Couldn't save a copy of the cartridge's save on this PC, so nothing was written.")
		return
	}
	s.ui.Log("Cartridge's save copied to %s.", backup)
	for {
		err := s.gbc.restore(data, func(d, t int) { s.ui.SetProgress("Writing the save", d, t) })
		if err == nil {
			var back []byte
			back, err = s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Checking it", d, t) })
			if err == nil && !bytes.Equal(back, data) {
				err = linkErr("The cartridge's save didn't match what was written. Clean the cartridge's contacts, then try again.")
			}
		}
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if !s.askRetry("Couldn't write the save", err.Error()) {
			s.ui.Finish("failed", err.Error()+" The cartridge's save from before is in "+backup+".")
			return
		}
	}
	s.ui.Finish("restore", "“"+filepath.Base(path)+"” is on the cartridge, and it was read back to check it. The save from before is in "+backup+".")
}

// ---------------------------------------------------------------------------- flash carts
func (a *App) flashTool(gb bool) error {
	if a.busy() {
		return fmt.Errorf("Something is already running.")
	}
	if !pickMu.TryLock() {
		return nil
	}
	title, filter, pattern := "Choose a GBA game to write to the flash cart", "GBA games (*.gba)", "*.gba"
	if gb {
		title, filter, pattern = "Choose a Game Boy game to write to the flash cart", "Game Boy games (*.gb, *.gbc)", "*.gb;*.gbc"
	}
	path, err := pickFile(title, filter, pattern, a.settings().GamesFolder)
	pickMu.Unlock()
	if err != nil || path == "" {
		return err
	}
	rom, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Couldn't read that file: %v", err)
	}
	if len(rom) < 0x150 || len(rom) > 1<<25 {
		return fmt.Errorf("That doesn't look like a game (%d bytes).", len(rom))
	}
	name := "Write a ROM to a GBA flash cart"
	if gb {
		name = "Write a ROM to a Game Boy flash cart"
	}
	s, err := a.newToolSession("flash", name, gb)
	if err != nil {
		return err
	}
	if gb {
		go s.runGBFlash(path, rom)
	} else {
		go s.runGBAFlash(path, rom)
	}
	return nil
}

func flashProgress(s *Session, start *time.Time) func(done, total int, phase string) {
	labels := map[string]string{"compare": "Checking what's already on the cart", "erase": "Erasing", "write": "Writing the game"}
	return func(done, total int, phase string) {
		if phase == "write" && start.IsZero() {
			*start = time.Now()
		}
		detail := ""
		if phase == "write" && done > 0 {
			detail = mb(done) + " of " + mb(total)
			if el := time.Since(*start); el > 3*time.Second && done < total {
				detail += " · " + aboutMinutes(time.Duration(float64(el)*float64(total-done)/float64(done))) + " left"
			}
		}
		s.ui.update(func(st *State) {
			st.Progress = &Progress{Label: labels[phase], Done: done, Total: max(total, 1), Detail: detail, Cancellable: true}
		})
	}
}

func flashConfirm(s *Session, det *FlashDetect, path string, rom []byte) bool {
	return s.ui.Ask(Prompt{Kind: "question", Title: "Write this game to the flash cart?",
		Text: fmt.Sprintf("Found: %s, %s (%s). “%s” (%s) replaces what's on it. Sectors that already hold the right data are skipped, "+
			"old data after the game is erased, and everything is read back to check it. A save kept in the flash chip itself "+
			"(\"batteryless\" carts) may be lost, so back it up first.",
			det.Profile.Name(), det.SizeText(), det.Method, filepath.Base(path), sizeText(len(rom))),
		Buttons: []Button{{ID: "go", Label: "Write it", Style: "primary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}}) == "go"
}

func (s *Session) runGBFlash(path string, rom []byte) {
	defer s.closeGB()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	// a blank flash cart has no game on it, so its header isn't checked
	for {
		if err := s.gbStart("Switch on your GBA to write the flash cart", "the flash cart"); err != nil {
			s.ui.Finish("cancelled", "")
			return
		}
		break
	}
	f := &GBFlashcart{c: s.gbc, log: func(t string, a ...any) { s.ui.Log(t, a...) }}
	var det *FlashDetect
	err := s.gbc.withAgent(func() error {
		s.ui.SetStatus("Looking for the flash chip...", "busy")
		var err error
		det, err = f.Detect()
		if err != nil {
			return err
		}
		s.ui.Log("Flash cart: %s, %s (%s).", det.Profile.Name(), det.SizeText(), det.Method)
		if !flashConfirm(s, det, path, rom) {
			return errCancelled
		}
		s.ui.SetPhase("dump")
		var start time.Time
		res, err := f.WriteROM(rom, nil, true, flashProgress(s, &start))
		s.ui.ClearProgress()
		if err == nil {
			s.ui.Log("Written and checked: %s, %d sector(s) unchanged, %d cleared.", sizeText(res.Written), res.Skipped, res.Cleared)
		}
		return err
	})
	s.flashFinish(path, err)
}

func (s *Session) runGBAFlash(path string, rom []byte) {
	defer func() {
		if s.link != nil {
			s.link.Close()
		}
	}()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	d, ok := s.startGBADumper("Switch on your GBA to write the flash cart", "the flash cart")
	if !ok {
		s.ui.Finish("cancelled", "")
		return
	}
	s.ui.SetGBA("on")
	f := &GBAFlashcart{d: d, log: func(t string, a ...any) { s.ui.Log(t, a...) }}
	s.ui.SetStatus("Looking for the flash chip...", "busy")
	det, err := f.Detect()
	if err == nil {
		s.ui.Log("Flash cart: %s, %s (%s).", det.Profile.Name(), det.SizeText(), det.Method)
		if !flashConfirm(s, det, path, rom) {
			err = errCancelled
		} else {
			s.ui.SetPhase("dump")
			var start time.Time
			var res FlashResult
			res, err = f.WriteROM(rom, nil, true, flashProgress(s, &start))
			s.ui.ClearProgress()
			if err == nil {
				s.ui.Log("Written and checked: %s (%s writes), %d sector(s) unchanged, %d cleared.", sizeText(res.Written), res.Method, res.Skipped, res.Cleared)
			}
		}
	}
	s.flashFinish(path, err)
}

// startGBADumper sends GB-Link Cart Doctor's GBA cartridge program to the GBA (multiboot) and
// waits for it to answer; false if the player gave up.
func (s *Session) startGBADumper(title, cart string) (*gbaDumper, bool) {
	d := &gbaDumper{l: s.link, stop: func() bool { return s.cancelled() || s.stopped.Load() }}
	s.link.ConfigureGBA()
	if d.ping(700 * time.Millisecond) { // still running from the last time: no need to switch off and on
		s.ui.SetGBA("on")
		return d, true
	}
	for {
		id := s.ui.ShowPrompt(Prompt{Kind: "power", Title: title,
			Text:    "Hold START and SELECT while you switch the GBA on, with " + cart + " in it. If it's already on, switch it off first.",
			Buttons: []Button{{ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		s.link.ConfigureGBA()
		err := s.link.Multiboot(gbaDumperImage, 365*24*time.Hour, func() bool { _, ok := s.ui.Answer(id); return ok || s.stopped.Load() })
		s.ui.ClearPrompt(id)
		if le, ok := err.(*LinkError); ok && le.msg == "Cancelled" {
			return nil, false
		}
		if err == nil {
			s.ui.SetStatus("Starting the program on the GBA...", "busy")
			time.Sleep(800 * time.Millisecond)
			if d.ping(5 * time.Second) {
				s.ui.SetGBA("on")
				return d, true
			}
			err = linkErr("The GBA didn't start the program.")
		}
		if !s.askRetry("The GBA didn't connect", err.Error()+" Check the link cable is pushed in at both ends, then try again.") {
			return nil, false
		}
	}
}

func (s *Session) flashFinish(path string, err error) {
	switch {
	case err == nil:
		s.ui.Finish("flash", "“"+filepath.Base(path)+"” is on the flash cart and was read back to check it. Switch the GBA off, then on again to play.")
	case err == errCancelled || err == errGBCancelled || s.cancelled():
		s.ui.Finish("cancelled", "If writing had started, the cart holds part of the game: write it again to finish (the parts already written are skipped).")
	default:
		s.ui.Finish("failed", err.Error())
	}
}
