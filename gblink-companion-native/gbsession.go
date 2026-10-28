// Playing Game Boy and Game Boy Color games in mGBA with the save on the real cartridge, as GBA
// games are played: the GBA is put into its Game Boy Color mode by GB-Link Cart Doctor's program
// (gbcart.go), the cartridge's save is copied into mGBA's .sav, and every time the game saves,
// it's copied back to the cartridge and read back to check it. The GBA can't be a controller or a
// second screen for these games: it's busy being a Game Boy Color.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// gbStart makes sure the GBA is in Game Boy Color mode with Cart Doctor's program, and that a
// cartridge is in it (which: what to put in, for the player). Returns errCancelled if they cancel.
func (s *Session) gbStart(why, which string) error {
	if s.gbc != nil && s.gbAlive() {
		return nil
	}
	s.link.ConfigureGB()
	c := &gbClient{l: s.link, stop: func() bool { return s.stopped.Load() || s.cancelled() }}
	if c.connect(1500*time.Millisecond) == nil { // still running from before (the GBA stayed on)
		c.disconnect()
		s.gbc = c
		s.ui.SetGBA("on")
		return nil
	}
	s.ui.SetGBA("off")
	for {
		id := s.ui.ShowPrompt(Prompt{Kind: "power", Title: why,
			Text: "Switch the GBA off and take the cartridge out. Then switch the GBA on with no cartridge in: " +
				"it gets a program over the link cable that turns it into a Game Boy Color.",
			Buttons: []Button{{ID: "cancel", Label: "Cancel", Style: "secondary"}}})
		s.link.ConfigureGBA()
		err := s.link.Multiboot(gbcSwitchImage, 365*24*time.Hour, func() bool {
			_, ok := s.ui.Answer(id)
			return ok || s.stopped.Load() || s.cancelled()
		})
		s.ui.ClearPrompt(id)
		if le, ok := err.(*LinkError); ok && le.msg == "Cancelled" {
			return errCancelled
		}
		if err == nil {
			s.ui.SetStatus("Switching the GBA to Game Boy Color mode...", "busy")
			time.Sleep(2 * time.Second)
			s.link.ConfigureGB()
			for {
				a := s.ui.Ask(Prompt{Kind: "question", Title: "Put the cartridge in",
					Text: "The GBA is now a Game Boy Color. Push " + which + " into it, all the way. (If the GBA restarts when " +
						"the cartridge goes in, push it in slowly.)",
					Buttons: []Button{{ID: "go", Label: "It's in", Style: "primary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}})
				if a != "go" {
					return errCancelled
				}
				s.ui.SetStatus("Checking the cartridge...", "busy")
				if err = c.connect(5 * time.Second); err == nil {
					c.disconnect()
					s.gbc = c
					s.ui.SetGBA("on")
					return nil
				}
				if !s.askRetry("The GBA isn't answering", err.Error()+" Check the link cable is pushed in at both ends.") {
					return errCancelled
				}
				if !s.gbAliveLoose() { // it's not in Game Boy Color mode any more: send the program again
					break
				}
			}
			continue
		}
		if !s.askRetry("The GBA didn't connect", err.Error()+" Check the link cable is pushed in at both ends, then try again.") {
			return errCancelled
		}
	}
}

// gbAlive: Cart Doctor's program still answers (the GBA is on, in Game Boy Color mode).
func (s *Session) gbAlive() bool {
	if s.gbc == nil {
		return false
	}
	s.link.ConfigureGB()
	stop := s.gbc.stop
	s.gbc.stop = nil
	defer func() { s.gbc.stop = stop }()
	if s.gbc.connect(1500*time.Millisecond) != nil {
		return false
	}
	s.gbc.disconnect()
	return true
}

func (s *Session) gbAliveLoose() bool {
	c := &gbClient{l: s.link}
	if c.connect(1500*time.Millisecond) != nil {
		return false
	}
	c.disconnect()
	return true
}

// gbHeaderNow reads the cartridge's header. It has to be a cartridge (its header checksum right).
func (s *Session) gbHeaderNow() (GBHeader, error) {
	h, err := s.gbc.readHeader()
	if err != nil {
		return h, err
	}
	if !h.OK {
		return h, linkErr("No Game Boy cartridge was found. Push it in all the way, or clean its contacts, then try again.")
	}
	return h, nil
}

// sameGBGame: the cartridge is the game picked (its title and its whole-ROM checksum).
func sameGBGame(a, b GBHeader) bool {
	return a.Title == b.Title && a.Global == b.Global && a.HeaderSum == b.HeaderSum
}

// gbCheckCart checks the cartridge is the game that's being played.
func (s *Session) gbCheckCart() (GBHeader, error) {
	h, err := s.gbHeaderNow()
	if err != nil {
		return h, err
	}
	if !sameGBGame(h, s.game.GBH) {
		return h, linkErr("The cartridge in the GBA is %s, but the game you picked is %s. Put the right cartridge in.",
			nonBlank(h.Title, "another game"), nonBlank(s.game.GBH.Title, s.game.Name()))
	}
	if h.RAMSize == 0 || !h.Battery {
		return h, linkErr("%s doesn't keep a save on its cartridge, so there's nothing to copy.", nonBlank(h.Title, s.game.Name()))
	}
	s.gbRAM = h.RAMSize
	return h, nil
}

// ----------------------------------------------------------------------------- pull / push
func (s *Session) gbPull() error {
	if err := s.gbStart("Switch on your GBA to load your save", "the "+s.game.Name()+" cartridge"); err != nil {
		return err
	}
	h, err := s.gbCheckCart()
	if err != nil {
		return err
	}
	s.ui.Log("Cartridge: %s (%s), save: %s.", h.Title, h.TypeName, sizeText(h.RAMSize))
	data, err := s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Loading your save from the cartridge", d, t) })
	s.ui.ClearProgress()
	if err != nil {
		return err
	}
	if len(data) != h.RAMSize {
		return linkErr("The cartridge sent %d bytes of save, but its header says %d.", len(data), h.RAMSize)
	}
	s.game.backup(data, "cart")
	if err := s.game.writeLocal(data); err != nil {
		return err
	}
	s.state.Pending, s.state.SyncedHash, s.state.GBSave = false, hash(data), h.RAMSize
	s.game.saveState(s.state)
	s.ui.SetCart("synced", "")
	s.ui.Log("Save loaded into %s.", filepath.Base(s.game.Sav))
	return nil
}

func (s *Session) gbNeedsPush() bool {
	data := s.game.localSave()
	if data == nil || s.gbRAM == 0 || len(data) < s.gbRAM {
		return false
	}
	return hash(data[:s.gbRAM]) != s.state.SyncedHash
}

// gbPush writes the PC's save to the cartridge and reads it back to check it. With why != "",
// the player is asked to switch the GBA on; otherwise it's only written if the GBA answers.
func (s *Session) gbPush(why string) (bool, error) {
	data := s.game.localSave()
	if data == nil || s.gbRAM == 0 {
		return true, nil
	}
	if len(data) < s.gbRAM {
		return false, linkErr("%s is %d bytes, but the cartridge's save is %d bytes.", filepath.Base(s.game.Sav), len(data), s.gbRAM)
	}
	data = data[:s.gbRAM] // mGBA puts the cartridge's clock after the save: it isn't written
	if why != "" {
		if err := s.gbStart(why, "the "+s.game.Name()+" cartridge"); err != nil {
			return false, err
		}
	} else if !s.gbAlive() {
		s.ui.SetGBA("off")
		return false, nil
	}
	s.ui.SetGBA("on")
	if _, err := s.gbCheckCart(); err != nil {
		return false, err
	}
	s.state.Pending = true // until it's checked, it isn't on the cartridge
	s.game.saveState(s.state)
	s.ui.SetWriting(true)
	err := s.gbc.restore(data, func(d, t int) { s.ui.SetProgress("Saving to the cartridge", d, t) })
	if err == nil {
		var back []byte
		back, err = s.gbc.dump(true, func(d, t int) { s.ui.SetProgress("Checking the cartridge's save", d, t) })
		if err == nil && !bytes.Equal(back, data) {
			err = linkErr("The cartridge's save didn't match what was written. Clean the cartridge's contacts, then try again.")
		}
	}
	s.ui.ClearProgress()
	s.ui.SetWriting(false)
	if err != nil {
		return false, err
	}
	s.state.Pending, s.state.SyncedHash = false, hash(data)
	s.game.saveState(s.state)
	s.ui.Log("Save written to the cartridge and checked.")
	s.ui.SetCart("synced", "")
	return true, nil
}

func (s *Session) gbPushWithRetries(why string) bool {
	for {
		s.linkMu.Lock()
		_, err := s.gbPush(why)
		s.linkMu.Unlock()
		if err == nil {
			return true
		}
		if err != errCancelled && s.askRetry("Couldn't save to the cartridge", err.Error()) {
			continue
		}
		s.markPending()
		return false
	}
}

func (s *Session) gbPullWithRetries() error {
	for {
		s.linkMu.Lock()
		err := s.gbPull()
		s.linkMu.Unlock()
		if err == nil || err == errCancelled {
			return err
		}
		if !s.askRetry("Couldn't load the save", err.Error()) {
			return errCancelled
		}
	}
}

// --------------------------------------------------------------------------------- the game
func (s *Session) runGB() {
	defer func() {
		if s.link != nil {
			s.link.ConfigureGBA() // back to 3.3 V for the next GBA program
			s.link.Close()
		}
	}()
	g := s.game
	s.state = g.loadState()
	s.gbRAM = s.state.GBSave
	if s.gbRAM == 0 && g.GBH.Battery {
		s.gbRAM = g.GBH.RAMSize
	}
	s.ui.SetPhase("connect")
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
	if !g.GBH.Battery || g.GBH.RAMSize == 0 {
		s.ui.Log("%s doesn't keep a save on the cartridge: it's played without one.", g.Name())
	} else if s.state.Pending && g.localSave() != nil {
		a := s.ui.Ask(Prompt{Kind: "question", Title: "Your last save isn't on the cartridge yet",
			Text: "Last time, your save couldn't be copied to the cartridge, so the newest save is on this PC.",
			Buttons: []Button{{ID: "write", Label: "Copy this PC's save to the cartridge", Style: "primary"},
				{ID: "load", Label: "Use the cartridge's save instead", Style: "secondary"}}})
		if a == "load" {
			if s.gbPullWithRetries() != nil {
				s.ui.Finish("cancelled", "")
				return
			}
		} else if !s.gbPushWithRetries("Switch on your GBA to copy your last save to it") {
			if s.ui.Ask(Prompt{Kind: "question", Title: "Play anyway?", Text: "You can play with the save on this PC. It'll be copied to the cartridge later.",
				Buttons: []Button{{ID: "play", Label: "Play anyway", Style: "primary"}, {ID: "stop", Label: "Cancel", Style: "secondary"}}}) != "play" {
				s.ui.Finish("cancelled", "")
				return
			}
		}
	} else if s.gbPullWithRetries() != nil {
		if s.ui.Ask(Prompt{Kind: "question", Title: "Play without the cartridge's save?",
			Text:    "mGBA will use the save on this PC (if there is one), and it won't be copied to the cartridge.",
			Buttons: []Button{{ID: "stop", Label: "Cancel", Style: "primary"}, {ID: "play", Label: "Play anyway", Style: "secondary"}}}) != "play" {
			s.ui.Finish("cancelled", "")
			return
		}
		s.gbRAM = 0 // nothing goes to the cartridge this time
	}
	mgba := s.app.settings().MGBA
	if mgba == "" || s.stopped.Load() {
		s.ui.Finish("cancelled", "")
		return
	}
	proc, err := s.startMGBA(mgba)
	if err != nil {
		s.ui.Ask(Prompt{Kind: "error", Title: "mGBA didn't start", Text: err.Error(), Buttons: []Button{{ID: "ok", Label: "OK", Style: "primary"}}})
		s.ui.Finish("cancelled", "")
		return
	}
	s.ui.SetPhase("play")
	s.ui.SetStatus("Playing in mGBA", "ok")
	s.gbWatch(proc)
	time.Sleep(500 * time.Millisecond)
	s.ui.SetPhase("finish")
	s.gbBackupMGBASave()
	if s.gbNeedsPush() {
		s.markPending()
		s.ui.SetStatus("Copying your save to the cartridge...", "busy")
		s.ui.Raise()
		if !s.gbPushWithRetries("Switch on your GBA to save to the cartridge") {
			s.ui.Finish("pending", "Your save is kept on this PC. Next time you play this game, you'll be asked to copy it to the cartridge first.")
			return
		}
	}
	s.ui.Finish("done", "The cartridge has your latest save. You can switch the GBA off.")
}

func (s *Session) gbBackupMGBASave() {
	data := s.game.localSave()
	if s.gbRAM > 0 && len(data) > s.gbRAM {
		data = data[:s.gbRAM]
	}
	if len(data) == 0 {
		return
	}
	if h := hash(data); h != s.lastBackup && h != s.state.SyncedHash {
		s.lastBackup = h
		s.game.backup(data, "mgba")
	}
}

// gbWatch copies each in-game save to the cartridge while mGBA runs.
func (s *Session) gbWatch(p *mgbaProc) {
	var lastSig string
	var stableSince time.Time
	warned := false
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
		}
		st, err := os.Stat(s.game.Sav)
		if err != nil {
			continue
		}
		sig := fmt.Sprint(st.ModTime().UnixNano(), st.Size())
		if sig != lastSig {
			lastSig, stableSince = sig, time.Now()
			continue
		}
		if stableSince.IsZero() || time.Since(stableSince) < 1500*time.Millisecond {
			continue
		}
		stableSince = time.Time{}
		s.linkMu.Lock()
		if !s.gbNeedsPush() {
			s.linkMu.Unlock()
			continue
		}
		s.ui.Log("In-game save detected.")
		s.gbBackupMGBASave()
		ok, err := s.gbPush("")
		s.linkMu.Unlock()
		if err != nil {
			s.ui.Log("Couldn't write the save to the cartridge just now (%v).", err)
			s.markPending()
			continue
		}
		if ok {
			warned = false
			continue
		}
		s.markPending()
		if s.app.settings().AskEachSave {
			s.ui.Raise()
			s.gbPushWithRetries("You saved - switch on your GBA to copy it over")
		} else if !warned {
			s.ui.Log("In-game save kept on this PC: the GBA isn't answering. It'll be copied to the cartridge later.")
			warned = true
		}
	}
}

// gbFindROM looks through the games list for this cartridge's game.
func (a *App) gbFindROM(h GBHeader) string {
	for _, g := range a.ui.Snapshot().Library {
		if !g.GB {
			continue
		}
		if game, err := NewGame(g.ROM); err == nil && sameGBGame(game.GBH, h) {
			return g.ROM
		}
	}
	return ""
}

var errNoGBCart = errors.New("no Game Boy cartridge")

func gbSaveName(rom string) string {
	return strings.TrimSuffix(rom, filepath.Ext(rom)) + ".sav"
}
