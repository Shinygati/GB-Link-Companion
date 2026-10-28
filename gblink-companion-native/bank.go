// The Pokémon Bank: the PC boxes of Pokémon Emerald, FireRed or LeafGreen on a cartridge over
// GB-Link, next to boxes kept on this PC. Pokémon are moved between them as in PKHeX (pick one
// up, put it down), and nothing is changed until "Save": then the cartridge is written first,
// read back and checked, and only after that is the bank on this PC updated (bankstore.go).
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// slotRef: a slot on the cartridge (Bank false) or in the bank.
type slotRef struct {
	Bank      bool
	Box, Slot int
}

// BankSession is what the bank window shows and changes. The window calls its methods; the
// session's goroutine (runBank) does the link work. Everything is behind mu.
type BankSession struct {
	mu sync.Mutex
	s  *Session

	Code, GameName string
	file           string // a save file opened instead of a cartridge (a flash cart's SD card, an emulator)
	Trainer        string
	TID, SID       int
	owner          string
	save           *Gen3Save
	BoxNames       [g3BoxCount]string
	orig, Cart     [g3BoxCount][g3BoxSlots][]byte
	origBank, Bank Bank
	PartyCount     int
	Data           *bankData
	Icons          *bankIcons

	CartBox, BankBox int
	Mode             string           // move, multi or release (bankedit.go)
	Multi, Marked    map[slotRef]bool // chosen in Move Multi, marked in Release
	Held, Sel        *slotRef
	Note             string // the last thing to tell the player (a move that can't be made, saved, ...)
	Ready            bool   // the bank is loaded (and the cartridge's boxes, if CartLoaded)
	CartLoaded       bool   // a cartridge's boxes are loaded beside the bank
	Locked           bool   // a change cut short is waiting for its cartridge: look, don't touch
	Loading          bool   // reading a cartridge
	Saving           bool
	Rev              uint64 // goes up with every change (the window's pictures)

	cmd chan string
}

// openBank is the Pokémon Bank button (Settings › Pokémon Enhancements).
func (a *App) openBank() error {
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("Something is already running.")
	}
	s := &Session{ui: a.ui, app: a, cancel: make(chan struct{}),
		cartWhat: "your Pokémon Emerald, FireRed or LeafGreen cartridge"}
	b := &BankSession{s: s, cmd: make(chan string, 8), Mode: modeMove, Multi: map[slotRef]bool{}, Marked: map[slotRef]bool{}}
	s.bank = b
	a.session = s
	a.mu.Unlock()
	a.ui.update(func(st *State) {
		st.Game = &GameView{Name: "Pokémon Bank"}
		st.Phase, st.Mode, st.Status, st.Progress, st.Prompt = "connect", "bank", "", nil, nil
		st.Cart, st.LastSync, st.GBA, st.Result, st.ResultText, st.BackupPath = "", "", "", "", "", ""
		st.Verify = nil
		st.Controller = ControllerView{}
		st.Log = nil
		st.Bank = b
	})
	go b.run()
	return nil
}

// bankSend passes a button to the bank's session (save, undo, done).
func (a *App) bankSend(c string) {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil && s.bank != nil {
		select {
		case s.bank.cmd <- c:
		default:
		}
	}
}

func bankGameName(code string) string {
	switch {
	case strings.HasPrefix(code, "BPE"):
		return "Pokémon Emerald"
	case strings.HasPrefix(code, "BPR"):
		return "Pokémon FireRed"
	case strings.HasPrefix(code, "BPG"):
		return "Pokémon LeafGreen"
	}
	return "this game"
}

// --------------------------------------------------------------------------- the session
// run opens the bank straight away (no GBA needed to look through it or tidy it up); a
// cartridge's boxes are loaded beside it when the player asks ("Load from cartridge").
func (b *BankSession) run() {
	s := b.s
	defer func() {
		if s.link != nil {
			go s.link.Close()
		}
	}()
	if why := b.openOffline(); why != "" {
		s.ui.Finish("failed", why)
		return
	}
	s.ui.SetPhase("play")
	for {
		select {
		case <-s.cancel:
			s.ui.Finish("cancelled", "Nothing was changed on the cartridge or in the bank.")
			return
		case c := <-b.cmd:
			switch c {
			case "load", "file":
				b.setLoading(true)
				var why string
				if c == "load" {
					why = b.loadCart()
				} else {
					why = b.loadFile()
				}
				b.setLoading(false)
				s.ui.SetStatus("", "")
				s.ui.ClearProgress()
				if why != "" && why != "cancelled" {
					b.setNote(why)
				}
				s.ui.update(func(*State) {})
			case "save":
				if done, why := b.commit(); done {
					s.ui.Finish("failed", why)
					return
				}
				s.ui.SetStatus("", "")
				if s.stopped.Load() { // the window was closed while it saved
					s.ui.Finish("bank", "")
					return
				}
			case "undo":
				b.undo()
			case "release":
				n := b.markedCount()
				if n == 0 {
					continue
				}
				t := fmt.Sprintf("Release %d Pokémon?", n)
				if n == 1 {
					t = "Release this Pokémon?"
				}
				if s.ui.Ask(Prompt{Kind: "question", Title: t,
					Text:    "Once you save, they're gone for good: from the cartridge and from the bank. Until you save, Undo changes brings them back.",
					Buttons: []Button{{ID: "release", Label: "Release", Style: "primary"}, {ID: "keep", Label: "Keep them", Style: "secondary"}}}) == "release" {
					b.releaseMarked()
				}
				s.ui.update(func(*State) {})
			case "done":
				if b.dirty() && s.ui.Ask(Prompt{Kind: "question", Title: "Leave without saving?",
					Text:    "The Pokémon you moved haven't been saved. If you leave now, the cartridge and the bank stay as they were.",
					Buttons: []Button{{ID: "leave", Label: "Leave without saving", Style: "primary"}, {ID: "stay", Label: "Keep going", Style: "secondary"}}}) != "leave" {
					continue
				}
				text := "Your Pokémon are where you left them."
				if b.cartLoaded() {
					text += " You can switch the GBA off."
				}
				s.ui.Finish("bank", text)
				return
			}
		}
	}
}

func (b *BankSession) setLoading(on bool) {
	b.mu.Lock()
	b.Loading = on
	b.Rev++
	b.mu.Unlock()
	b.s.ui.update(func(*State) {})
}

func (b *BankSession) setNote(t string) {
	b.mu.Lock()
	b.Note = t
	b.Rev++
	b.mu.Unlock()
}

func (b *BankSession) cartLoaded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.CartLoaded
}

// openOffline reads the bank from this PC, with the names and icons kept from before (or from a
// ROM in the games list). Returns why it couldn't.
func (b *BankSession) openOffline() string {
	s := b.s
	note := ""
	locked := false
	if j, err := loadJournal(); err != nil {
		return err.Error()
	} else if j != nil {
		locked = true
		name := strings.SplitN(j.Owner, "|", 2)[0]
		note = fmt.Sprintf("The last change was being written to %s's %s when it stopped. Load that cartridge to finish it: "+
			"until then the bank can be looked through but not changed, so no Pokémon are lost.", name, bankGameName(j.Code))
	}
	bank, err := loadBank()
	if err != nil {
		return err.Error()
	}
	note = joinNotes(note, b.loadWarnings())
	data, icons := offlineBankData(s.ui.Snapshot().Library, s.ui.Log)
	empty := copyBoxes([g3BoxCount][g3BoxSlots][]byte{})
	for x := range empty {
		for i := range empty[x] {
			empty[x][i] = make([]byte, pk3Size)
		}
	}
	b.mu.Lock()
	b.orig, b.Cart = empty, copyBoxes(empty)
	b.origBank, b.Bank = bank.clone(), bank
	b.Data, b.Icons = data, icons
	b.Note, b.Locked = note, locked
	b.Ready = true
	b.Rev++
	b.mu.Unlock()
	s.ui.update(func(*State) {})
	return ""
}

func joinNotes(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + " " + b
}

// loadWarnings: what to say about bank files that couldn't be read.
func (b *BankSession) loadWarnings() string {
	w := bankLoadWarnings
	if len(w) == 0 {
		return ""
	}
	for _, x := range w {
		b.s.ui.Log("Bank: couldn't use %s", x)
	}
	return fmt.Sprintf("%d file(s) in the bank's folders couldn't be read (see Details). They'll be kept in its Backups folder when you next save.", len(w))
}

// loadCart connects to the GBA and reads the cartridge's boxes. Returns why it couldn't
// ("cancelled" if the player stopped).
func (b *BankSession) loadCart() string {
	s := b.s
	s.ui.SetStatus("Connecting to the GB-Link...", "busy")
	for {
		err := s.connect()
		if err == nil {
			break
		}
		if !s.askRetry("GB-Link not found", err.Error()+" Plug it in, then try again.") {
			return "cancelled"
		}
	}
	var cart CartInfo
	var rev int
	for {
		s.linkMu.Lock()
		err := s.ensureHelper("Switch on your GBA to open its boxes")
		if err == nil {
			s.ui.SetStatus("Checking the cartridge...", "busy")
			cart, err = s.helper.SaveInfo()
			if err == nil {
				var ri ROMInfo
				if ri, err = s.helper.ROMInfo(); err == nil {
					rev = int(ri.Header[0xBC])
				}
			}
		}
		s.linkMu.Unlock()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't read the cartridge", err.Error()) {
			return "cancelled"
		}
	}
	if bankFamily(cart.Code) == "" {
		return fmt.Sprintf("The cartridge in the GBA is %s [%s]. The Pokémon Bank works with Pokémon Emerald, FireRed and LeafGreen.", nonBlank(cart.Title, "a game it can't use"), cart.Code)
	}
	if _, ok := saveTypes[cart.Type]; !ok {
		return "No save chip was found on the cartridge."
	}
	s.ui.Log("Cartridge: %s [%s] v1.%d, save chip: %s.", cart.Title, cart.Code, rev, saveTypes[cart.Type].Name)
	s.saveType = cart.Type

	var raw []byte
	for {
		s.linkMu.Lock()
		var err error
		raw, err = s.helper.ReadSave(cart.Type, func(d, t int) { s.ui.SetProgress("Reading the save from the cartridge", d, t) })
		s.linkMu.Unlock()
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't read the save", err.Error()) {
			return "cancelled"
		}
	}
	save, err := parseGen3Save(raw, cart.Code)
	if err != nil {
		return "Couldn't open the boxes: " + err.Error() + ". Nothing was changed."
	}
	b.file = ""
	return b.openSave(save, cart.Code, rev, s.helper)
}

// loadFile opens a save file's boxes beside the bank: for flash carts that keep their saves on an
// SD card (EZ-Flash, EverDrive and the like), or an emulator's save.
func (b *BankSession) loadFile() string {
	s := b.s
	if !pickMu.TryLock() {
		return ""
	}
	path, err := pickFile("Open a Pokémon Emerald, FireRed or LeafGreen save", "Save files (*.sav, *.srm)", "*.sav;*.srm", s.app.backupFolder())
	pickMu.Unlock()
	if err != nil || path == "" {
		return "cancelled"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "Couldn't read that file: " + err.Error()
	}
	code := ""
	var save *Gen3Save
	if sv, err := parseGen3Save(raw, "BPEE"); err == nil {
		save, code = sv, "BPEE"
	} else if sv, err2 := parseGen3Save(raw, "BPRE"); err2 == nil {
		switch s.ui.Ask(Prompt{Kind: "question", Title: "FireRed or LeafGreen?",
			Text:    "The save is from FireRed or LeafGreen (the save can't tell which). Which game is it?",
			Buttons: []Button{{ID: "fr", Label: "FireRed", Style: "primary"}, {ID: "lg", Label: "LeafGreen", Style: "secondary"}, {ID: "cancel", Label: "Cancel", Style: "secondary"}}}) {
		case "fr":
			code = "BPRE"
		case "lg":
			code = "BPGE"
		default:
			return "cancelled"
		}
		save = sv
	} else {
		return "Couldn't open the boxes: it isn't an Emerald, FireRed or LeafGreen save (" + err.Error() + "). Nothing was changed."
	}
	if save.looksJapanese() { // a Japanese game's save: its names are read in the Japanese character set
		code = code[:3] + "J"
		save.Japanese = true
	}
	b.file = path
	return b.openSave(save, code, 0, nil)
}

// openSave shows a save's boxes beside the bank (from the cartridge, or b.file).
func (b *BankSession) openSave(save *Gen3Save, code string, rev int, helper *Helper) string {
	s := b.s
	cart := CartInfo{Code: code}
	var err error
	owner := save.Owner()
	trainer, tid, sid := save.Trainer()
	s.ui.Log("Save: %s, ID %05d, saved %d times.", trainer, tid, save.Counter)

	// a change that was cut short last time
	j, err := loadJournal()
	if err != nil {
		return err.Error()
	}
	note := ""
	reload := false
	if j != nil {
		if j.Code != cart.Code || j.Owner != owner {
			name := strings.SplitN(j.Owner, "|", 2)[0]
			return fmt.Sprintf("The last change in the Pokémon Bank was being written to %s's %s when it stopped. "+
				"Put that cartridge in and load it to finish the change first, so no Pokémon are lost.",
				name, bankGameName(j.Code))
		}
		bank, msg := finishJournal(j, save)
		if err := saveBank(bank); err != nil {
			return "Couldn't finish the last change in the bank: " + err.Error()
		}
		clearJournal()
		s.ui.Log("%s", msg)
		note, reload = msg, true
	}
	var bank Bank
	if reload {
		if bank, err = loadBank(); err != nil {
			return err.Error()
		}
		note = joinNotes(note, b.loadWarnings())
	}

	// names and pictures for this game
	lib := s.ui.Snapshot().Library
	s.linkMu.Lock()
	data, icons := gatherBankData(lib, cart.Code, rev, helper, s.ui.Log, func(pages int) {
		s.ui.update(func(st *State) {
			st.Progress = &Progress{Label: "Reading the Pokémon names and pictures from the cartridge", Done: pages, Total: max(pages+1, 22),
				Detail: "Only the first time: they're kept on this PC."}
		})
	})
	s.linkMu.Unlock()
	s.ui.ClearProgress()
	if icons == nil {
		s.ui.Log("No box icons: they're read from an English cartridge, or a ROM in your games list. The Pokémon show by name instead.")
	}

	b.mu.Lock()
	b.Code, b.GameName = cart.Code, bankGameName(cart.Code)
	b.save, b.owner = save, owner
	b.Trainer, b.TID, b.SID = trainer, tid, sid
	b.BoxNames = save.BoxNames()
	b.orig = save.Boxes()
	b.Cart = copyBoxes(b.orig)
	if reload { // the bank on disk changed: start from it (changes made before loading are let go)
		b.origBank, b.Bank = bank.clone(), bank
		if b.BankBox >= len(b.Bank.Boxes) {
			b.BankBox = 0
		}
	}
	b.PartyCount = len(save.Party())
	if data != nil {
		b.Data = data
	}
	if icons != nil {
		b.Icons = icons
	}
	b.CartBox, b.Held, b.Locked = 0, nil, false
	b.Multi, b.Marked = map[slotRef]bool{}, map[slotRef]bool{}
	b.CartLoaded = true
	b.Note = note
	b.Rev++
	b.mu.Unlock()
	title := b.GameName
	if b.file != "" {
		title += " · " + filepath.Base(b.file)
	}
	s.ui.update(func(v *State) { v.Game = &GameView{Name: "Pokémon Bank", Title: title, Code: cart.Code} })
	return ""
}

func nonBlank(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

func copyBoxes(in [g3BoxCount][g3BoxSlots][]byte) (out [g3BoxCount][g3BoxSlots][]byte) {
	for b := range in {
		for i := range in[b] {
			out[b][i] = append([]byte(nil), in[b][i]...)
		}
	}
	return
}

// ---------------------------------------------------------------------------------- saving
// commit saves the changes: the cartridge first (checked by reading it back), then the bank.
// end is true if the session can't go on (why says what happened).
func (b *BankSession) commit() (end bool, why string) {
	s := b.s
	b.mu.Lock()
	if !b.Ready || b.Saving {
		b.mu.Unlock()
		return false, ""
	}
	cartBoxes := copyBoxes(b.Cart)
	bank := b.Bank.clone()
	dep, wd := b.moves()
	cartChanged, bankChanged := b.cartChanged(), b.bankChanged()
	cnt := b.counts()
	b.Saving, b.Held = true, nil
	b.Rev++
	b.mu.Unlock()
	finish := func(note string) {
		b.mu.Lock()
		b.Saving, b.Note = false, note
		b.Rev++
		b.mu.Unlock()
		s.ui.SetPhase("play")
		s.ui.update(func(*State) {})
	}
	if !cartChanged && !bankChanged {
		finish("Nothing has changed since the last save.")
		return false, ""
	}
	if !cartChanged { // only the bank's own boxes were rearranged
		if err := saveBank(bank); err != nil {
			finish("Couldn't save the bank: " + err.Error())
			return false, ""
		}
		b.mu.Lock()
		b.origBank = bank.clone()
		b.mu.Unlock()
		finish("Saved. (Only the bank's boxes changed: the cartridge didn't need writing.)")
		return false, ""
	}

	newSave := b.save.WithBoxes(cartBoxes)
	// the note first: from here on, nothing can be lost
	j := &bankJournal{Version: bankVersion, Code: b.Code, Owner: b.owner, Bank: bank, Started: time.Now().Format(time.RFC3339)}
	for _, r := range dep {
		j.Deposited = append(j.Deposited, fmt.Sprintf("%x", r))
	}
	for _, r := range wd {
		j.Withdrawn = append(j.Withdrawn, fmt.Sprintf("%x", r))
	}
	if err := saveJournal(j); err != nil {
		finish("Couldn't save on this PC (" + err.Error() + "), so nothing was written to the cartridge.")
		return false, ""
	}
	s.ui.SetPhase("finish")
	if b.file != "" {
		return b.commitFile(newSave, bank, cnt, finish)
	}
	s.ui.SetStatus("Saving to the cartridge...", "busy")

	// the same cartridge, with the same save as when the boxes were opened
	st := saveTypes[s.saveType]
	var now []byte
	for {
		s.linkMu.Lock()
		err := s.ensureHelper("Switch on your GBA to save to the cartridge")
		var cart CartInfo
		if err == nil {
			cart, err = s.helper.SaveInfo()
		}
		if err == nil && cart.Code != b.Code {
			err = linkErr("The cartridge in the GBA is %s [%s], not the %s the boxes were read from. Put that one back in.", cart.Title, cart.Code, b.GameName)
		}
		if err == nil {
			now, err = s.helper.ReadSave(s.saveType, func(d, t int) { s.ui.SetProgress("Checking it's the same cartridge", d, t) })
		}
		s.linkMu.Unlock()
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't check the cartridge", err.Error()) {
			clearJournal()
			finish("Nothing was saved: the cartridge wasn't checked. Your changes are still here; save again when you're ready.")
			return false, ""
		}
	}
	if !bytes.Equal(now, b.save.Raw[:len(now)]) {
		clearJournal()
		other := "It's a different cartridge, or the game was saved on it since its boxes were opened."
		if ns, err := parseGen3Save(now, b.Code); err == nil && ns.Owner() == b.owner {
			other = "The game was saved on it since its boxes were opened."
		}
		return true, other + " Nothing was written, and the bank wasn't changed. Open the Pokémon Bank again to start from the cartridge's save as it is now."
	}

	// a copy of the save that's about to be replaced
	dir := filepath.Join(s.app.backupFolder(), "Save backups")
	base := strings.TrimSuffix(fileName(b.GameName, b.Code), ".gba") + " - before Pokemon Bank - " + time.Now().Format("2006-01-02 15-04-05") + ".sav"
	backup := uniquePath(filepath.Join(dir, base))
	if err := os.MkdirAll(dir, 0o755); err != nil || os.WriteFile(backup, now, 0o644) != nil {
		clearJournal()
		finish("Couldn't save a copy of the cartridge's save on this PC first, so nothing was written.")
		return false, ""
	}
	s.ui.Log("Cartridge's save copied to %s.", backup)

	// write, then read it back
	for {
		s.linkMu.Lock()
		err := s.ensureHelper("Switch on your GBA to save to the cartridge")
		if err == nil {
			s.ui.SetWriting(true)
			_, err = s.helper.WriteSave(s.saveType, newSave[:st.Size], now, func(d, t int) { s.ui.SetProgress("Writing the boxes to the cartridge", d, t) })
			s.ui.SetWriting(false)
		}
		var back []byte
		if err == nil {
			back, err = s.helper.ReadSave(s.saveType, func(d, t int) { s.ui.SetProgress("Reading it back to check it", d, t) })
			if err == nil && !bytes.Equal(back, newSave[:st.Size]) {
				err = linkErr("The cartridge's save didn't match what was written - check the cartridge's contacts.")
			}
		}
		s.linkMu.Unlock()
		s.ui.ClearProgress()
		if err == nil {
			break
		}
		if err == errCancelled || !s.askRetry("Couldn't save to the cartridge", err.Error()) {
			// the note stays: next time this cartridge is opened, the bank is matched to it
			return true, "The boxes weren't written to the cartridge completely, so the bank on this PC wasn't changed yet. " +
				"No Pokémon are lost: open the Pokémon Bank again with this cartridge and it finishes the change. " +
				"The cartridge's save from before is copied to " + backup + "."
		}
	}

	// the cartridge has them: now the bank
	if err := saveBank(bank); err != nil {
		return true, "The cartridge was written, but the bank on this PC couldn't be saved (" + err.Error() + "). " +
			"No Pokémon are lost: open the Pokémon Bank again with this cartridge and it finishes the change."
	}
	clearJournal()
	ns, err := parseGen3Save(newSave, b.Code)
	if err != nil {
		return true, "Saved, but the new save couldn't be opened again: " + err.Error()
	}
	b.mu.Lock()
	b.save = ns
	b.orig = ns.Boxes()
	b.Cart = copyBoxes(b.orig)
	b.origBank, b.Bank = bank.clone(), bank
	b.mu.Unlock()
	s.ui.Log("Saved: %d Pokémon to the bank, %d to the cartridge, %d released.", cnt.ToBank, cnt.ToCart, cnt.Released)
	note := "Saved to the cartridge and checked, then saved in the bank."
	if cnt.ToBank+cnt.ToCart+cnt.Released > 0 {
		note = fmt.Sprintf("Saved: %d Pokémon went to the bank and %d to the cartridge", cnt.ToBank, cnt.ToCart)
		if cnt.Released > 0 {
			note += fmt.Sprintf(", and %d were released", cnt.Released)
		}
		note += ". The cartridge was checked after writing."
	}
	finish(note)
	return false, ""
}

// commitFile writes the changes to the save file that was opened: checked unchanged first, a copy
// of it kept, then written and read back; the bank on this PC last.
func (b *BankSession) commitFile(newSave []byte, bank Bank, cnt bankCounts, finish func(string)) (bool, string) {
	s := b.s
	now, err := os.ReadFile(b.file)
	if err != nil || !bytes.Equal(now, b.save.Raw) {
		clearJournal()
		return true, "The save file changed since its boxes were opened (or it can't be read). Nothing was written, and the bank wasn't changed."
	}
	backup := b.file + ".before-bank-" + time.Now().Format("20060102-150405") + ".bak"
	if os.WriteFile(backup, now, 0o644) != nil {
		clearJournal()
		finish("Couldn't save a copy of the save file first, so nothing was written.")
		return false, ""
	}
	if err := writeFileSafely(b.file, newSave); err != nil {
		return true, "The save file couldn't be written (" + err.Error() + "). No Pokémon are lost: open it in the bank again to finish the change."
	}
	if back, err := os.ReadFile(b.file); err != nil || !bytes.Equal(back, newSave) {
		return true, "The save file didn't read back as written. No Pokémon are lost: open it in the bank again to finish the change. The file from before is " + backup + "."
	}
	s.ui.Log("Save file written: %s (the one from before: %s).", b.file, backup)
	if err := saveBank(bank); err != nil {
		return true, "The save file was written, but the bank on this PC couldn't be saved (" + err.Error() + "). No Pokémon are lost: open the file in the bank again and it finishes the change."
	}
	clearJournal()
	ns, err := parseGen3Save(newSave, b.Code)
	if err != nil {
		return true, "Saved, but the new save couldn't be opened again: " + err.Error()
	}
	b.mu.Lock()
	b.save = ns
	b.orig = ns.Boxes()
	b.Cart = copyBoxes(b.orig)
	b.origBank, b.Bank = bank.clone(), bank
	b.mu.Unlock()
	note := fmt.Sprintf("Saved to the file: %d Pokémon went to the bank and %d to the game", cnt.ToBank, cnt.ToCart)
	if cnt.Released > 0 {
		note += fmt.Sprintf(", and %d were released", cnt.Released)
	}
	finish(note + ". Copy the file back to your flash cart's SD card.")
	return false, ""
}
