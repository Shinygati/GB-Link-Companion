// Writing a save file to a cartridge by hand: pick a .sav, and it replaces the save on the cartridge
// in the GBA. The cartridge's own save is backed up to this PC first.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// restoreSave is the game list's "Restore a save" button: choose the file, then start.
func (a *App) restoreSave() error {
	if a.busy() {
		return fmt.Errorf("Something is already running.")
	}
	if !pickMu.TryLock() {
		return nil
	}
	path, err := pickFile("Choose a save file to write to the cartridge", "Save files (*.sav, *.srm)", "*.sav;*.srm", a.backupFolder())
	pickMu.Unlock()
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Couldn't read that file: %v", err)
	}
	if len(data) < 0x200 || len(data) > 1<<20 { // the smallest GBA save is 512 bytes (EEPROM)
		return fmt.Errorf("That doesn't look like a GBA save file (%d bytes).", len(data))
	}
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("Something is already running.")
	}
	s := &Session{ui: a.ui, app: a, cancel: make(chan struct{})}
	a.session = s
	a.mu.Unlock()
	a.ui.update(func(st *State) {
		st.Game = &GameView{Name: "Restore a save"}
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", "restore", "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText, st.BackupPath = "", "", "", "", "", ""
		st.Verify = nil
		st.Controller = ControllerView{}
		st.Log = nil
	})
	go s.runRestore(path, data)
	return nil
}

func (s *Session) runRestore(path string, data []byte) {
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
	var cart CartInfo
	for {
		s.linkMu.Lock()
		err := s.ensureHelper("Switch on your GBA to write the save")
		if err == nil {
			s.ui.SetStatus("Checking the cartridge...", "busy")
			cart, err = s.helper.SaveInfo()
			if err == nil {
				if _, ok := saveTypes[cart.Type]; !ok {
					err = linkErr("No save chip was found on the cartridge.")
				}
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
	st := saveTypes[cart.Type]
	name := cart.Title
	if name == "" {
		name = "Unknown game"
	}
	s.ui.update(func(v *State) { v.Game = &GameView{Name: name, Title: cart.Title, Code: cart.Code} })
	s.ui.Log("Cartridge: %s [%s], save chip: %s.", cart.Title, cart.Code, st.Name)
	if len(data) < st.Size {
		s.ui.Finish("failed", fmt.Sprintf("%s is %d bytes, but this cartridge's save (%s) is %d bytes. Nothing was written.",
			filepath.Base(path), len(data), st.Name, st.Size))
		return
	}
	ask := fmt.Sprintf("This replaces the save on %s [%s] with %s. The cartridge's current save is copied to this PC first.",
		name, cart.Code, filepath.Base(path))
	if len(data) > st.Size+0x100 { // (a few extra bytes at the end can be the clock; more is a different kind of save)
		ask += fmt.Sprintf(" The file is %d bytes and the cartridge's save is %d, so only the first %d bytes are written.", len(data), st.Size, st.Size)
	}
	if s.ui.Ask(Prompt{Kind: "question", Title: "Replace the cartridge's save?", Text: ask,
		Buttons: []Button{{ID: "write", Label: "Replace it", Style: "primary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}}) != "write" {
		s.ui.Finish("cancelled", "")
		return
	}
	data = data[:st.Size]

	// the cartridge's own save first: if anything goes wrong it can be put back
	s.ui.SetPhase("dump")
	var old []byte
	for {
		s.linkMu.Lock()
		var err error
		old, err = s.helper.ReadSave(cart.Type, func(d, t int) { s.ui.SetProgress("Backing up the cartridge's current save", d, t) })
		s.linkMu.Unlock()
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't read the cartridge's save", err.Error()) {
			s.ui.Finish("cancelled", "")
			return
		}
	}
	dir := filepath.Join(s.app.backupFolder(), "Save backups")
	base := strings.TrimSuffix(fileName(cart.Title, cart.Code), ".gba") + " - before restore - " + time.Now().Format("2006-01-02 15-04-05") + ".sav"
	backup := uniquePath(filepath.Join(dir, base))
	if err := os.MkdirAll(dir, 0o755); err == nil {
		err = os.WriteFile(backup, old, 0o644)
		if err != nil {
			backup = ""
		}
	} else {
		backup = ""
	}
	if backup == "" {
		s.ui.Finish("failed", "Couldn't save a copy of the cartridge's current save on this PC first, so nothing was written.")
		return
	}
	s.ui.Log("Cartridge's current save copied to %s.", backup)

	var n int
	for {
		s.linkMu.Lock()
		s.ui.SetWriting(true)
		var err error
		n, err = s.helper.WriteSave(cart.Type, data, old, func(d, t int) { s.ui.SetProgress("Writing the save to the cartridge", d, t) })
		s.ui.SetWriting(false)
		s.linkMu.Unlock()
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't write the save", err.Error()+" The cartridge's old save is copied to "+backup+".") {
			s.ui.Finish("failed", "The save wasn't written completely. The cartridge's old save is copied to "+backup+".")
			return
		}
	}
	if n == 0 {
		s.ui.Finish("restore", "The cartridge already had this save. Nothing needed to be written.")
		return
	}
	s.ui.Log("Save written to the cart and verified (%d of %d blocks changed).", n, max(1, st.Size/saveChunk))
	s.ui.Finish("restore", "“"+filepath.Base(path)+"” is on the cartridge, and it was checked after writing. The cartridge's old save is copied to "+backup+". You can switch the GBA off.")
}
