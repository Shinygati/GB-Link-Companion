// Backing up just a cartridge's save (GBA, Game Boy and Game Boy Color): much quicker than a
// whole backup when the game itself is already on this PC, or isn't wanted.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveBackupDest: where a save backup goes by default (the "Save backups" folder, dated).
func (a *App) saveBackupDest(name string) string {
	dir := filepath.Join(a.backupFolder(), "Save backups")
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return uniquePath(filepath.Join(dir, base+" - "+time.Now().Format("2006-01-02 15-04-05")+".sav"))
}

// askSaveDest asks where to save it; "" if the player cancelled.
func (s *Session) askSaveDest(name, what, dest string) string {
	for {
		a := s.ui.Ask(Prompt{Kind: "question", Title: "Back up " + name + "'s save?",
			Text: what + " will be saved as “" + filepath.Base(dest) + "” in " + filepath.Dir(dest) + ". The game itself isn't copied.",
			Buttons: []Button{{ID: "go", Label: "Back up the save", Style: "primary"},
				{ID: "where", Label: "Save somewhere else…", Style: "secondary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		switch a {
		case "go":
			return dest
		case "where":
			if p, err := pickSaveFile("Save the save file as", filepath.Dir(dest), filepath.Base(dest)); err == nil && p != "" {
				if e := strings.ToLower(filepath.Ext(p)); e != ".sav" && e != ".srm" {
					p += ".sav"
				}
				dest = p
			}
		default:
			return ""
		}
	}
}

func writeSaveBackup(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := writeFileSafely(dest, data); err != nil {
		return err
	}
	back, err := os.ReadFile(dest)
	if err != nil || len(back) != len(data) || hash(back) != hash(data) {
		return fmt.Errorf("the file didn't read back as written")
	}
	return nil
}

func (s *Session) finishSaveBackup(dest string, data []byte, start time.Time) {
	if err := writeSaveBackup(dest, data); err != nil {
		s.ui.Finish("failed", "Couldn't save the file: "+err.Error())
		return
	}
	s.ui.Log("Save copied to %s (%s, %s).", dest, sizeText(len(data)), time.Since(start).Round(100*time.Millisecond))
	s.ui.update(func(st *State) { st.BackupPath = dest })
	s.ui.Finish("savebackup", "“"+filepath.Base(dest)+"” ("+sizeText(len(data))+") is in "+filepath.Dir(dest)+". It was read back to check it.")
}

// ------------------------------------------------------------------------------------- GBA
func (a *App) gbaSaveBackup() error {
	s, err := a.newToolSession("savebackup", "Back up a GBA save", false)
	if err != nil {
		return err
	}
	go s.runGBASaveBackup()
	return nil
}

func (s *Session) runGBASaveBackup() {
	defer func() {
		if s.link != nil {
			s.link.Close()
		}
	}()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	d, _, saveSize, header, ok := s.gbaCartNow("Switch on your GBA to read the cartridge's save")
	if !ok {
		s.ui.Finish("cancelled", "")
		return
	}
	title, code := gbaHeaderText(header[0xA0:0xAC]), gbaHeaderText(header[0xAC:0xB0])
	name := nonBlank(title, "Unknown game")
	s.ui.update(func(st *State) { st.Game = &GameView{Name: name, Title: title, Code: code} })
	if saveSize == 0 {
		s.ui.Finish("failed", "No save memory was found on "+name+" ["+code+"], so there's no save to copy.")
		return
	}
	s.ui.Log("Cartridge: %s [%s], save memory: %s.", title, code, sizeText(saveSize))
	dest := s.askSaveDest(name, "Its save ("+sizeText(saveSize)+")", s.app.saveBackupDest(fileName(title, code)))
	if dest == "" {
		s.ui.Finish("cancelled", "")
		return
	}
	s.ui.SetPhase("dump")
	start := time.Now()
	for {
		s.ui.SetStatus("Copying the save...", "busy")
		data, err := d.dumpSave(func(dn, t int) { s.ui.SetProgress("Copying the save to this PC", dn, t) })
		s.ui.ClearProgress()
		if err == nil {
			s.finishSaveBackup(dest, data, start)
			return
		}
		if s.cancelled() || !s.askRetry("Couldn't copy the save", err.Error()) {
			s.ui.Finish("failed", err.Error())
			return
		}
	}
}

// -------------------------------------------------------------------------------- Game Boy
func (a *App) gbSaveBackup() error {
	s, err := a.newToolSession("savebackup", "Back up a Game Boy save", true)
	if err != nil {
		return err
	}
	go s.runGBSaveBackup()
	return nil
}

func (s *Session) runGBSaveBackup() {
	defer s.closeGB()
	if !s.connectOrFinish() {
		return
	}
	s.ui.SetPhase("load")
	h, ok := s.gbReady("Switch on your GBA to read the Game Boy cartridge's save", "the Game Boy or Game Boy Color cartridge")
	if !ok {
		s.ui.Finish("cancelled", "")
		return
	}
	name := nonBlank(h.Title, "Game Boy game")
	s.ui.update(func(st *State) { st.Game = &GameView{Name: name, Title: gbDescribe(h), GB: true} })
	s.ui.Log("Cartridge: %s (%s).", name, gbDescribe(h))
	if h.RAMSize == 0 || !h.Battery {
		s.ui.Finish("failed", name+" has no battery-backed save memory, so there's no save to copy.")
		return
	}
	dest := s.askSaveDest(name, "Its save ("+sizeText(h.RAMSize)+")", s.app.saveBackupDest(gbROMName(h)))
	if dest == "" {
		s.ui.Finish("cancelled", "")
		return
	}
	s.ui.SetPhase("dump")
	start := time.Now()
	for {
		s.ui.SetStatus("Copying the save...", "busy")
		data, err := s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Copying the save to this PC", d, t) })
		s.ui.ClearProgress()
		if err == nil {
			s.finishSaveBackup(dest, data, start)
			return
		}
		if !s.askRetry("Couldn't copy the save", err.Error()) {
			s.ui.Finish("failed", err.Error())
			return
		}
	}
}
